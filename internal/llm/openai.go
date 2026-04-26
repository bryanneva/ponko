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

const defaultOpenAIBaseURL = "https://openrouter.ai/api/v1"

var _ Provider = (*OpenAIClient)(nil)

// OpenAIClient implements the Provider/LLMClient interface against any
// OpenAI-compatible Chat Completions endpoint (OpenAI, OpenRouter, vLLM, etc.).
//
// Model strings passed by callers are remapped through ModelOverrides so
// existing Claude-shaped calls (llm.ModelSonnet, llm.ModelHaiku) route to
// provider-specific names like "anthropic/claude-sonnet-4" on OpenRouter.
type OpenAIClient struct {
	httpClient     *http.Client
	modelOverrides map[string]string
	baseURL        string
	apiKey         string
	referer        string
	title          string
}

// OpenAIConfig configures an OpenAIClient.
//
// ModelOverrides maps canonical model strings (llm.ModelSonnet,
// llm.ModelHaiku) to provider-specific names; unknown models pass through
// unchanged. Referer and Title populate OpenRouter's HTTP-Referer and
// X-Title headers for billing attribution and are ignored by other endpoints.
type OpenAIConfig struct {
	ModelOverrides map[string]string
	APIKey         string
	BaseURL        string
	Referer        string
	Title          string
}

func NewOpenAIClient(cfg OpenAIConfig) *OpenAIClient {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultOpenAIBaseURL
	}
	return &OpenAIClient{
		apiKey:         cfg.APIKey,
		baseURL:        cfg.BaseURL,
		referer:        cfg.Referer,
		title:          cfg.Title,
		modelOverrides: cfg.ModelOverrides,
		httpClient: &http.Client{
			Timeout:   httpClientTimeout,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

type oaiMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	Name       string        `json:"name,omitempty"`
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
}

type oaiToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function oaiFunctionCall `json:"function"`
}

type oaiFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type oaiTool struct {
	Type     string         `json:"type"`
	Function oaiFunctionDef `json:"function"`
}

type oaiFunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type oaiRequest struct {
	Model     string       `json:"model"`
	Messages  []oaiMessage `json:"messages"`
	Tools     []oaiTool    `json:"tools,omitempty"`
	MaxTokens int          `json:"max_tokens,omitempty"`
}

type oaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type oaiChoice struct {
	FinishReason string     `json:"finish_reason"`
	Message      oaiMessage `json:"message"`
	Index        int        `json:"index"`
}

type oaiResponse struct {
	ID      string      `json:"id"`
	Choices []oaiChoice `json:"choices"`
	Usage   oaiUsage    `json:"usage"`
}

type oaiAPIError struct {
	Error struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
}

func (c *OpenAIClient) resolveModel(model string) string {
	if mapped, ok := c.modelOverrides[model]; ok && mapped != "" {
		return mapped
	}
	return model
}

func systemMessage(prompt string) []oaiMessage {
	if prompt == "" {
		return nil
	}
	return []oaiMessage{{Role: "system", Content: prompt}}
}

func toOAIMessages(messages []Message) []oaiMessage {
	out := make([]oaiMessage, 0, len(messages))
	for _, m := range messages {
		out = append(out, oaiMessage{Role: m.Role, Content: m.Content})
	}
	return out
}

func toOAITools(tools []Tool) []oaiTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]oaiTool, 0, len(tools))
	for _, t := range tools {
		params := t.InputSchema
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, oaiTool{
			Type: "function",
			Function: oaiFunctionDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		})
	}
	return out
}

func (c *OpenAIClient) doRequest(ctx context.Context, reqBody oaiRequest) (*oaiResponse, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	if c.referer != "" {
		req.Header.Set("HTTP-Referer", c.referer)
	}
	if c.title != "" {
		req.Header.Set("X-Title", c.title)
	}

	requestStart := time.Now()
	resp, err := c.httpClient.Do(req)
	elapsed := time.Since(requestStart)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	if elapsed > httpClientTimeout*3/4 {
		slog.Info("openai-compat api request was slow",
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
		var apiErr oaiAPIError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error.Message != "" {
			return nil, fmt.Errorf("openai-compat API error (%d): %s", resp.StatusCode, apiErr.Error.Message)
		}
		return nil, fmt.Errorf("openai-compat API error (%d): %s", resp.StatusCode, string(respBody))
	}

	var result oaiResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	slog.Info("openai-compat api usage",
		"model", reqBody.Model,
		"prompt_tokens", result.Usage.PromptTokens,
		"completion_tokens", result.Usage.CompletionTokens,
		"total_tokens", result.Usage.TotalTokens,
	)

	return &result, nil
}

func (c *OpenAIClient) SendMessage(ctx context.Context, prompt string, model string) (string, error) {
	reqBody := oaiRequest{
		Model:     c.resolveModel(model),
		MaxTokens: 1024,
		Messages:  []oaiMessage{{Role: RoleUser, Content: prompt}},
	}
	resp, err := c.doRequest(ctx, reqBody)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty response from OpenAI-compat provider")
	}
	return resp.Choices[0].Message.Content, nil
}

func (c *OpenAIClient) SendConversation(ctx context.Context, systemPrompt string, messages []Message, model string) (string, error) {
	reqBody := oaiRequest{
		Model:     c.resolveModel(model),
		MaxTokens: 2048,
		Messages:  append(systemMessage(systemPrompt), toOAIMessages(messages)...),
	}
	resp, err := c.doRequest(ctx, reqBody)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty response from OpenAI-compat provider")
	}
	return resp.Choices[0].Message.Content, nil
}

func (c *OpenAIClient) SendConversationWithTools(ctx context.Context, systemPrompt string, messages []Message, tools []Tool, toolCaller ToolCaller, userScope *UserScope, model string) (string, error) {
	resolved := c.resolveModel(model)
	oaiTools := toOAITools(tools)
	conv := append(systemMessage(systemPrompt), toOAIMessages(messages)...)

	for range maxToolUseIterations {
		reqBody := oaiRequest{
			Model:     resolved,
			MaxTokens: 4096,
			Messages:  conv,
			Tools:     oaiTools,
		}

		resp, err := c.doRequest(ctx, reqBody)
		if err != nil {
			return "", err
		}
		if len(resp.Choices) == 0 {
			return "", fmt.Errorf("empty response from OpenAI-compat provider")
		}

		choice := resp.Choices[0]
		slog.Info("openai-compat response",
			"finish_reason", choice.FinishReason,
			"tool_calls", len(choice.Message.ToolCalls),
		)

		if len(choice.Message.ToolCalls) == 0 {
			return choice.Message.Content, nil
		}

		conv = append(conv, choice.Message)
		for _, call := range choice.Message.ToolCalls {
			var args map[string]any
			if call.Function.Arguments != "" {
				if unmarshalErr := json.Unmarshal([]byte(call.Function.Arguments), &args); unmarshalErr != nil {
					slog.Warn("failed to unmarshal tool arguments",
						"tool", call.Function.Name, "error", unmarshalErr, "raw_input", call.Function.Arguments)
					args = map[string]any{}
				}
			} else {
				args = map[string]any{}
			}

			start := time.Now()
			toolResult, toolErr := toolCaller.CallTool(ctx, call.Function.Name, args, userScope)
			duration := time.Since(start)

			content := toolResult
			if toolErr != nil {
				slog.Warn("tool call failed", "tool", call.Function.Name, "error", toolErr, "duration", duration)
				content = fmt.Sprintf("ERROR: %s", toolErr.Error())
			} else {
				slog.Info("tool call succeeded", "tool", call.Function.Name, "duration", duration)
			}

			conv = append(conv, oaiMessage{
				Role:       "tool",
				ToolCallID: call.ID,
				Name:       call.Function.Name,
				Content:    content,
			})
		}
	}

	return "", fmt.Errorf("tool use loop exceeded %d iterations", maxToolUseIterations)
}
