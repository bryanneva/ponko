package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const defaultBaseURL = "https://api.anthropic.com"

type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
}

func NewClient(apiKey string, baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		apiKey:  apiKey,
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout:   httpClientTimeout,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

const httpClientTimeout = 120 * time.Second

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"

	ModelSonnet = "claude-sonnet-4-6"
	ModelHaiku  = "claude-haiku-4-5-20251001"

	maxToolUseIterations = 10
)

type UserScope struct {
	SlackUserID string
	DisplayName string
	ChannelID   string
}

type ToolCaller interface {
	CallTool(ctx context.Context, name string, arguments map[string]any, userScope *UserScope) (string, error)
}

type cacheControl struct {
	Type string `json:"type"`
}

type systemBlock struct {
	CacheControl *cacheControl `json:"cache_control,omitempty"`
	Type         string        `json:"type"`
	Text         string        `json:"text"`
}

type Tool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema"`
}

type toolUseRequest struct {
	System    []systemBlock   `json:"system,omitempty"`
	Model     string          `json:"model"`
	Messages  []toolUseMsg    `json:"messages"`
	Tools     []Tool          `json:"tools"`
	MaxTokens int             `json:"max_tokens"`
}

type toolUseMsg struct {
	Content any    `json:"content"`
	Role    string `json:"role"`
}

type toolUseContentBlock struct {
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Text  string          `json:"text,omitempty"`
	Type  string          `json:"type"`
	Input json.RawMessage `json:"input,omitempty"`
}

type usageInfo struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

type toolUseResponse struct {
	StopReason string               `json:"stop_reason"`
	Content    []toolUseContentBlock `json:"content"`
	Usage      usageInfo             `json:"usage"`
}

type toolResultBlock struct {
	Content string `json:"content"`
	Type    string `json:"type"`
	ID      string `json:"tool_use_id"`
	IsError bool   `json:"is_error,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesRequest struct {
	Model     string        `json:"model"`
	System    []systemBlock `json:"system,omitempty"`
	Messages  []Message     `json:"messages"`
	MaxTokens int           `json:"max_tokens"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type messagesResponse struct {
	Content []contentBlock `json:"content"`
	Usage   usageInfo      `json:"usage"`
}

type apiError struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func systemBlocks(prompt string) []systemBlock {
	if prompt == "" {
		return nil
	}
	return []systemBlock{{
		Type:         "text",
		Text:         prompt,
		CacheControl: &cacheControl{Type: "ephemeral"},
	}}
}

func (c *Client) SendConversation(ctx context.Context, systemPrompt string, messages []Message, model string) (string, error) {
	reqBody := messagesRequest{
		Model:     model,
		System:    systemBlocks(systemPrompt),
		MaxTokens: 2048,
		Messages:  messages,
	}
	return c.sendRequest(ctx, reqBody)
}

func (c *Client) SendMessage(ctx context.Context, prompt string, model string) (string, error) {
	reqBody := messagesRequest{
		Model:     model,
		MaxTokens: 1024,
		Messages:  []Message{{Role: RoleUser, Content: prompt}},
	}
	return c.sendRequest(ctx, reqBody)
}

func (c *Client) doAPIRequest(ctx context.Context, reqBody any) ([]byte, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	requestStart := time.Now()
	resp, err := c.httpClient.Do(req)
	elapsed := time.Since(requestStart)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	if elapsed > httpClientTimeout*3/4 {
		slog.Info("claude api request was slow",
			"elapsed", elapsed,
			"threshold", httpClientTimeout,
		)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr apiError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error.Message != "" {
			return nil, fmt.Errorf("claude API error (%d): %s", resp.StatusCode, apiErr.Error.Message)
		}
		return nil, fmt.Errorf("claude API error (%d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

func (c *Client) sendRequest(ctx context.Context, reqBody messagesRequest) (string, error) {
	respBody, err := c.doAPIRequest(ctx, reqBody)
	if err != nil {
		return "", err
	}

	var result messagesResponse
	if err = json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("unmarshaling response: %w", err)
	}

	slog.Info("claude api usage",
		"model", reqBody.Model,
		"input_tokens", result.Usage.InputTokens,
		"output_tokens", result.Usage.OutputTokens,
		"cache_creation_input_tokens", result.Usage.CacheCreationInputTokens,
		"cache_read_input_tokens", result.Usage.CacheReadInputTokens,
	)

	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty response from Claude")
	}

	return result.Content[0].Text, nil
}

func (c *Client) SendConversationWithTools(ctx context.Context, systemPrompt string, messages []Message, tools []Tool, toolCaller ToolCaller, userScope *UserScope, model string) (string, error) {
	tuMessages := make([]toolUseMsg, 0, len(messages))
	for _, m := range messages {
		tuMessages = append(tuMessages, toolUseMsg{Role: m.Role, Content: m.Content})
	}

	cachedTools := make([]Tool, len(tools))
	copy(cachedTools, tools)
	if len(cachedTools) > 0 {
		cachedTools[len(cachedTools)-1].CacheControl = &cacheControl{Type: "ephemeral"}
	}

	sysBlocks := systemBlocks(systemPrompt)

	for range maxToolUseIterations {
		reqBody := toolUseRequest{
			Model:     model,
			System:    sysBlocks,
			MaxTokens: 4096,
			Messages:  tuMessages,
			Tools:     cachedTools,
		}

		tuResp, sendErr := c.sendToolUseRequest(ctx, reqBody)
		if sendErr != nil {
			return "", sendErr
		}

		slog.Info("claude response", "stop_reason", tuResp.StopReason, "content_blocks", len(tuResp.Content))

		if tuResp.StopReason != "tool_use" {
			return extractText(tuResp.Content), nil
		}

		tuMessages = append(tuMessages, toolUseMsg{Role: RoleAssistant, Content: tuResp.Content})

		for _, block := range tuResp.Content {
			if block.Type != "tool_use" {
				continue
			}

			var args map[string]any
			if unmarshalErr := json.Unmarshal(block.Input, &args); unmarshalErr != nil {
				slog.Warn("failed to unmarshal tool arguments",
					"tool", block.Name, "error", unmarshalErr, "raw_input", string(block.Input))
				args = map[string]any{}
			}

			start := time.Now()
			toolResult, toolErr := toolCaller.CallTool(ctx, block.Name, args, userScope)
			duration := time.Since(start)

			resultBlock := toolResultBlock{
				Type: "tool_result",
				ID:   block.ID,
			}
			if toolErr != nil {
				slog.Warn("tool call failed", "tool", block.Name, "error", toolErr, "duration", duration)
				resultBlock.Content = toolErr.Error()
				resultBlock.IsError = true
			} else {
				slog.Info("tool call succeeded", "tool", block.Name, "duration", duration)
				resultBlock.Content = toolResult
			}
			tuMessages = append(tuMessages, toolUseMsg{Role: RoleUser, Content: []toolResultBlock{resultBlock}})
		}
	}

	return "", fmt.Errorf("tool use loop exceeded %d iterations", maxToolUseIterations)
}

func (c *Client) sendToolUseRequest(ctx context.Context, reqBody toolUseRequest) (*toolUseResponse, error) {
	respBody, err := c.doAPIRequest(ctx, reqBody)
	if err != nil {
		return nil, err
	}

	var result toolUseResponse
	if unmarshalErr := json.Unmarshal(respBody, &result); unmarshalErr != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", unmarshalErr)
	}

	slog.Info("claude api usage",
		"model", reqBody.Model,
		"input_tokens", result.Usage.InputTokens,
		"output_tokens", result.Usage.OutputTokens,
		"cache_creation_input_tokens", result.Usage.CacheCreationInputTokens,
		"cache_read_input_tokens", result.Usage.CacheReadInputTokens,
	)

	return &result, nil
}

func extractText(blocks []toolUseContentBlock) string {
	for _, b := range blocks {
		if b.Type == "text" {
			return b.Text
		}
	}
	return ""
}
