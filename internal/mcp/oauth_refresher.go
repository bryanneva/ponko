package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/bryanneva/ponko/internal/llm"
)

type OAuthRefresher struct {
	Client       *Client
	TokenURL     string
	ClientID     string
	ClientSecret string
	RefreshToken string
	mu           sync.Mutex
}

func (r *OAuthRefresher) Initialize(ctx context.Context) error {
	err := r.Client.Initialize(ctx)
	if err != nil && isUnauthorized(err) {
		if refreshErr := r.refresh(ctx); refreshErr != nil {
			return fmt.Errorf("token refresh failed: %w", refreshErr)
		}
		return r.Client.Initialize(ctx)
	}
	return err
}

func (r *OAuthRefresher) ListTools(ctx context.Context) ([]llm.Tool, error) {
	tools, err := r.Client.ListTools(ctx)
	if err != nil && isUnauthorized(err) {
		if refreshErr := r.refresh(ctx); refreshErr != nil {
			return nil, fmt.Errorf("token refresh failed: %w", refreshErr)
		}
		return r.Client.ListTools(ctx)
	}
	return tools, err
}

func (r *OAuthRefresher) CallTool(ctx context.Context, name string, arguments map[string]any) (string, error) {
	result, err := r.Client.CallTool(ctx, name, arguments)
	if err != nil && isUnauthorized(err) {
		if refreshErr := r.refresh(ctx); refreshErr != nil {
			return "", fmt.Errorf("token refresh failed: %w", refreshErr)
		}
		return r.Client.CallTool(ctx, name, arguments)
	}
	return result, err
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func (r *OAuthRefresher) refresh(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {r.RefreshToken},
		"client_id":     {r.ClientID},
		"client_secret": {r.ClientSecret},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("creating token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, doErr := r.Client.HTTP.Do(req)
	if doErr != nil {
		return fmt.Errorf("sending token request: %w", doErr)
	}
	defer func() { _ = resp.Body.Close() }()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return fmt.Errorf("reading token response: %w", readErr)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token endpoint error (%d): %s", resp.StatusCode, string(body))
	}

	var tok tokenResponse
	if unmarshalErr := json.Unmarshal(body, &tok); unmarshalErr != nil {
		return fmt.Errorf("unmarshaling token response: %w", unmarshalErr)
	}

	r.Client.BearerToken = tok.AccessToken
	if tok.RefreshToken != "" {
		r.RefreshToken = tok.RefreshToken
	}

	return nil
}

func isUnauthorized(err error) bool {
	return strings.Contains(err.Error(), "(401)")
}
