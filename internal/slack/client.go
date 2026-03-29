package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const defaultBaseURL = "https://slack.com/api"

type Client struct {
	httpClient *http.Client
	baseURL    string
	botToken   string
}

func NewClient(botToken string, baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		botToken:   botToken,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

type UserProfile struct {
	DisplayName string
	Timezone    string
	IsAdmin     bool
}

type userInfoResponse struct {
	Error string `json:"error,omitempty"`
	User  struct {
		Profile struct {
			DisplayName string `json:"display_name"`
		} `json:"profile"`
		TZ      string `json:"tz"`
		IsAdmin bool   `json:"is_admin"`
	} `json:"user"`
	OK bool `json:"ok"`
}

type reactionRequest struct {
	Channel   string `json:"channel"`
	Name      string `json:"name"`
	Timestamp string `json:"timestamp"`
}

type postMessageRequest struct {
	Channel  string `json:"channel"`
	Text     string `json:"text"`
	ThreadTS string `json:"thread_ts,omitempty"`
}

type postMessageResponse struct {
	Error string `json:"error,omitempty"`
	OK    bool   `json:"ok"`
}

func (c *Client) AddReaction(ctx context.Context, channel string, timestamp string, emoji string) error {
	return c.doReaction(ctx, "/reactions.add", channel, timestamp, emoji)
}

func (c *Client) RemoveReaction(ctx context.Context, channel string, timestamp string, emoji string) error {
	return c.doReaction(ctx, "/reactions.remove", channel, timestamp, emoji)
}

func (c *Client) doReaction(ctx context.Context, endpoint string, channel string, timestamp string, emoji string) error {
	reqBody := reactionRequest{
		Channel:   channel,
		Name:      emoji,
		Timestamp: timestamp,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling reaction request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating reaction request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.botToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending reaction request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading reaction response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack API HTTP error (%d): %s", resp.StatusCode, string(respBody))
	}

	var result postMessageResponse
	if err = json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("unmarshaling reaction response: %w", err)
	}

	if !result.OK {
		return fmt.Errorf("slack API error: %s", result.Error)
	}

	return nil
}

func (c *Client) PostMessage(ctx context.Context, channel string, text string, threadTS string) error {
	reqBody := postMessageRequest{
		Channel:  channel,
		Text:     text,
		ThreadTS: threadTS,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat.postMessage", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.botToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack API HTTP error (%d): %s", resp.StatusCode, string(respBody))
	}

	var result postMessageResponse
	if err = json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("unmarshaling response: %w", err)
	}

	if !result.OK {
		return fmt.Errorf("slack API error: %s", result.Error)
	}

	return nil
}


type ThreadMessage struct {
	UserID    string
	Text      string
	Timestamp string
}

type conversationsRepliesResponse struct {
	Error    string `json:"error,omitempty"`
	Messages []struct {
		User string `json:"user"`
		Text string `json:"text"`
		TS   string `json:"ts"`
	} `json:"messages"`
	OK bool `json:"ok"`
}

func (c *Client) GetConversationReplies(ctx context.Context, channelID, timestamp string) ([]ThreadMessage, error) {
	params := url.Values{"channel": {channelID}, "ts": {timestamp}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/conversations.replies?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("creating conversations.replies request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.botToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending conversations.replies request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading conversations.replies response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("slack API HTTP error (%d): %s", resp.StatusCode, string(respBody))
	}

	var result conversationsRepliesResponse
	if err = json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling conversations.replies response: %w", err)
	}

	if !result.OK {
		return nil, fmt.Errorf("slack API error: %s", result.Error)
	}

	messages := make([]ThreadMessage, len(result.Messages))
	for i, m := range result.Messages {
		messages[i] = ThreadMessage{UserID: m.User, Text: m.Text, Timestamp: m.TS}
	}
	return messages, nil
}

type conversationsInfoResponse struct {
	Channel struct {
		Name string `json:"name"`
	} `json:"channel"`
	Error string `json:"error,omitempty"`
	OK    bool   `json:"ok"`
}

func (c *Client) GetChannelName(ctx context.Context, channelID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/conversations.info?channel="+channelID, nil)
	if err != nil {
		return "", fmt.Errorf("creating conversations.info request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.botToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("sending conversations.info request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading conversations.info response: %w", err)
	}

	var result conversationsInfoResponse
	if err = json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("unmarshaling conversations.info response: %w", err)
	}

	if !result.OK {
		return "", fmt.Errorf("slack API error: %s", result.Error)
	}

	return result.Channel.Name, nil
}

func (c *Client) GetUserProfile(ctx context.Context, userID string) (UserProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/users.info?user="+userID, nil)
	if err != nil {
		return UserProfile{}, fmt.Errorf("creating user info request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.botToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return UserProfile{}, fmt.Errorf("sending user info request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return UserProfile{}, fmt.Errorf("reading user info response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return UserProfile{}, fmt.Errorf("slack API HTTP error (%d): %s", resp.StatusCode, string(respBody))
	}

	var result userInfoResponse
	if err = json.Unmarshal(respBody, &result); err != nil {
		return UserProfile{}, fmt.Errorf("unmarshaling user info response: %w", err)
	}

	if !result.OK {
		return UserProfile{}, fmt.Errorf("slack API error: %s", result.Error)
	}

	return UserProfile{
		DisplayName: result.User.Profile.DisplayName,
		Timezone:    result.User.TZ,
		IsAdmin:     result.User.IsAdmin,
	}, nil
}
