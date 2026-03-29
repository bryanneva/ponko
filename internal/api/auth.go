package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	sessionCookieName = "ponko_session"
	cookieMaxAge      = 7 * 24 * 60 * 60 // 7 days in seconds
)

type AuthConfig struct {
	ClientID     string
	ClientSecret string
	DashboardURL string
	TeamID       string
	SigningKey    []byte
	Secure       bool
}

type sessionPayload struct {
	UserID      string `json:"userId"`
	DisplayName string `json:"displayName"`
	Exp         int64  `json:"exp"`
}

type contextKey string

const userContextKey contextKey = "user"

type SessionUser struct {
	UserID      string
	DisplayName string
}

func handleAuthSlack(cfg AuthConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state, err := generateState()
		if err != nil {
			slog.Error("failed to generate state", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "oauth_state",
			Value:    state,
			Path:     "/",
			MaxAge:   300,
			HttpOnly: true,
			Secure:   cfg.Secure,
			SameSite: http.SameSiteLaxMode,
		})

		params := url.Values{
			"client_id":    {cfg.ClientID},
			"user_scope":   {"identity.basic"},
			"redirect_uri": {cfg.DashboardURL + "/api/auth/slack/callback"},
			"state":        {state},
		}
		http.Redirect(w, r, "https://slack.com/oauth/v2/authorize?"+params.Encode(), http.StatusFound)
	}
}

func handleAuthSlackCallback(cfg AuthConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stateCookie, err := r.Cookie("oauth_state")
		if err != nil || stateCookie.Value == "" {
			writeError(w, http.StatusBadRequest, "missing state")
			return
		}
		if r.URL.Query().Get("state") != stateCookie.Value {
			writeError(w, http.StatusBadRequest, "invalid state")
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "oauth_state",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   cfg.Secure,
			SameSite: http.SameSiteLaxMode,
		})

		code := r.URL.Query().Get("code")
		if code == "" {
			writeError(w, http.StatusBadRequest, "missing code")
			return
		}

		identity, err := exchangeSlackCode(cfg, code)
		if err != nil {
			slog.Error("slack oauth exchange failed", "error", err)
			writeError(w, http.StatusInternalServerError, "oauth exchange failed")
			return
		}

		if cfg.TeamID != "" && identity.TeamID != cfg.TeamID {
			writeError(w, http.StatusForbidden, "wrong workspace")
			return
		}

		payload := sessionPayload{
			UserID:      identity.UserID,
			DisplayName: identity.DisplayName,
			Exp:         time.Now().Add(7 * 24 * time.Hour).Unix(),
		}
		cookieValue, err := signCookie(payload, cfg.SigningKey)
		if err != nil {
			slog.Error("failed to sign cookie", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    cookieValue,
			Path:     "/",
			MaxAge:   cookieMaxAge,
			HttpOnly: true,
			Secure:   cfg.Secure,
			SameSite: http.SameSiteLaxMode,
		})

		http.Redirect(w, r, "/", http.StatusFound)
	}
}

func handleAuthMe(signingKey []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "not authenticated")
			return
		}

		payload, err := verifyCookie(cookie.Value, signingKey)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid session")
			return
		}

		if time.Now().Unix() > payload.Exp {
			writeError(w, http.StatusUnauthorized, "session expired")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"userId":      payload.UserID,
			"displayName": payload.DisplayName,
		})
	}
}

func handleAuthLogout(secure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		})
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func validateSession(r *http.Request, signingKey []byte) (*http.Request, bool) {
	if len(signingKey) == 0 {
		return r, false
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return r, false
	}
	payload, err := verifyCookie(cookie.Value, signingKey)
	if err != nil || time.Now().Unix() > payload.Exp {
		return r, false
	}
	ctx := context.WithValue(r.Context(), userContextKey, SessionUser{
		UserID:      payload.UserID,
		DisplayName: payload.DisplayName,
	})
	return r.WithContext(ctx), true
}

func requireSession(signingKey []byte, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authedReq, ok := validateSession(r, signingKey)
		if !ok {
			writeError(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		next(w, authedReq)
	}
}

// Cookie signing: base64(payload).base64(hmac-sha256(payload))

func signCookie(payload sessionPayload, key []byte) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(data)
	sig := computeHMAC([]byte(encoded), key)
	return encoded + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func verifyCookie(value string, key []byte) (sessionPayload, error) {
	parts := strings.SplitN(value, ".", 2)
	if len(parts) != 2 {
		return sessionPayload{}, fmt.Errorf("invalid cookie format")
	}

	expectedSig := computeHMAC([]byte(parts[0]), key)
	actualSig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return sessionPayload{}, fmt.Errorf("decode signature: %w", err)
	}

	if !hmac.Equal(expectedSig, actualSig) {
		return sessionPayload{}, fmt.Errorf("signature mismatch")
	}

	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return sessionPayload{}, fmt.Errorf("decode payload: %w", err)
	}

	var payload sessionPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return sessionPayload{}, fmt.Errorf("unmarshal payload: %w", err)
	}

	return payload, nil
}

func computeHMAC(data, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

type slackIdentity struct {
	UserID      string
	DisplayName string
	TeamID      string
}

type oauthResponse struct {
	AuthedUser struct {
		AccessToken string `json:"access_token"`
		ID          string `json:"id"`
	} `json:"authed_user"`
	Team struct {
		ID string `json:"id"`
	} `json:"team"`
	OK bool `json:"ok"`
}

type identityResponse struct {
	User struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"user"`
	Team struct {
		ID string `json:"id"`
	} `json:"team"`
	Error string `json:"error"`
	OK    bool   `json:"ok"`
}

func exchangeSlackCode(cfg AuthConfig, code string) (*slackIdentity, error) {
	params := url.Values{
		"client_id":     {cfg.ClientID},
		"client_secret": {cfg.ClientSecret},
		"code":          {code},
		"redirect_uri":  {cfg.DashboardURL + "/api/auth/slack/callback"},
	}

	resp, err := http.PostForm("https://slack.com/api/oauth.v2.access", params)
	if err != nil {
		return nil, fmt.Errorf("slack oauth request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("read response: %w", readErr)
	}

	var result oauthResponse
	if unmarshalErr := json.Unmarshal(body, &result); unmarshalErr != nil {
		return nil, fmt.Errorf("decode response: %w", unmarshalErr)
	}

	if !result.OK {
		return nil, fmt.Errorf("slack oauth error: %s", string(body))
	}

	identityReq, err := http.NewRequest("GET", "https://slack.com/api/users.identity", nil)
	if err != nil {
		return nil, fmt.Errorf("create identity request: %w", err)
	}
	identityReq.Header.Set("Authorization", "Bearer "+result.AuthedUser.AccessToken)

	identityResp, err := http.DefaultClient.Do(identityReq)
	if err != nil {
		return nil, fmt.Errorf("identity request: %w", err)
	}
	defer func() { _ = identityResp.Body.Close() }()

	var identityResult identityResponse
	if decodeErr := json.NewDecoder(identityResp.Body).Decode(&identityResult); decodeErr != nil {
		return nil, fmt.Errorf("decode identity response: %w", decodeErr)
	}

	if !identityResult.OK {
		return nil, fmt.Errorf("slack identity error: %s", identityResult.Error)
	}

	return &slackIdentity{
		UserID:      identityResult.User.ID,
		DisplayName: identityResult.User.Name,
		TeamID:      identityResult.Team.ID,
	}, nil
}
