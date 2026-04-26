package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAISendMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("expected /chat/completions, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("expected Authorization 'Bearer test-key', got %q", got)
		}
		if got := r.Header.Get("HTTP-Referer"); got != "https://ponko.example" {
			t.Errorf("expected HTTP-Referer set, got %q", got)
		}
		if got := r.Header.Get("X-Title"); got != "Ponko" {
			t.Errorf("expected X-Title set, got %q", got)
		}

		var req oaiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		if req.Model != "anthropic/claude-haiku-4" {
			t.Errorf("expected mapped model 'anthropic/claude-haiku-4', got %q", req.Model)
		}
		if len(req.Messages) != 1 || req.Messages[0].Role != "user" || req.Messages[0].Content != "hello" {
			t.Errorf("unexpected messages: %+v", req.Messages)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(oaiResponse{
			Choices: []oaiChoice{{Message: oaiMessage{Role: "assistant", Content: "hi there"}}},
			Usage:   oaiUsage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7},
		})
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Referer: "https://ponko.example",
		Title:   "Ponko",
		ModelOverrides: map[string]string{
			ModelHaiku: "anthropic/claude-haiku-4",
		},
	})

	resp, err := client.SendMessage(context.Background(), "hello", ModelHaiku)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if resp != "hi there" {
		t.Errorf("expected 'hi there', got %q", resp)
	}
}

func TestOpenAISendConversationPassesSystemAsFirstMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req oaiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		if len(req.Messages) != 3 {
			t.Fatalf("expected 3 messages (system+user+assistant), got %d", len(req.Messages))
		}
		if req.Messages[0].Role != "system" || req.Messages[0].Content != "You are Otto." {
			t.Errorf("expected first message system/'You are Otto.', got %+v", req.Messages[0])
		}
		if req.Messages[1].Role != "user" || req.Messages[2].Role != "assistant" {
			t.Errorf("user/assistant ordering wrong: %+v", req.Messages)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(oaiResponse{
			Choices: []oaiChoice{{Message: oaiMessage{Role: "assistant", Content: "ok"}}},
		})
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{APIKey: "k", BaseURL: server.URL})
	_, err := client.SendConversation(context.Background(), "You are Otto.", []Message{
		{Role: RoleUser, Content: "hi"},
		{Role: RoleAssistant, Content: "hello"},
	}, "openai/gpt-4o-mini")
	if err != nil {
		t.Fatalf("SendConversation: %v", err)
	}
}

func TestOpenAIPassThroughModelWhenNotMapped(t *testing.T) {
	var observedModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req oaiRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		observedModel = req.Model
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(oaiResponse{Choices: []oaiChoice{{Message: oaiMessage{Content: "ok"}}}})
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{APIKey: "k", BaseURL: server.URL})
	_, err := client.SendMessage(context.Background(), "hi", "openai/gpt-4o")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if observedModel != "openai/gpt-4o" {
		t.Errorf("expected unmapped model passed through, got %q", observedModel)
	}
}

func TestOpenAIAPIErrorSurface(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(oaiAPIError{
			Error: struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			}{Message: "insufficient credits", Code: "billing"},
		})
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{APIKey: "k", BaseURL: server.URL})
	_, err := client.SendMessage(context.Background(), "hi", "x")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "insufficient credits") {
		t.Errorf("error should surface API message; got: %v", err)
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should include status code; got: %v", err)
	}
}

type stubToolCaller struct {
	results map[string]string
	calls   []string
}

func (s *stubToolCaller) CallTool(_ context.Context, name string, _ map[string]any, _ *UserScope) (string, error) {
	s.calls = append(s.calls, name)
	if r, ok := s.results[name]; ok {
		return r, nil
	}
	return "default", nil
}

func TestOpenAIToolUseLoop(t *testing.T) {
	step := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req oaiRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		step++
		switch step {
		case 1:
			// First turn: model asks for a tool call.
			_ = json.NewEncoder(w).Encode(oaiResponse{
				Choices: []oaiChoice{{
					Message: oaiMessage{
						Role: "assistant",
						ToolCalls: []oaiToolCall{{
							ID:   "call_1",
							Type: "function",
							Function: oaiFunctionCall{
								Name:      "search",
								Arguments: `{"query":"weather"}`,
							},
						}},
					},
					FinishReason: "tool_calls",
				}},
			})
		case 2:
			// Second turn: model gives final answer. Verify the tool result was appended.
			toolMsgIdx := -1
			for i, m := range req.Messages {
				if m.Role == "tool" && m.ToolCallID == "call_1" {
					toolMsgIdx = i
					break
				}
			}
			if toolMsgIdx == -1 {
				t.Errorf("expected tool result message in second-turn request, got messages: %+v", req.Messages)
			}
			_ = json.NewEncoder(w).Encode(oaiResponse{
				Choices: []oaiChoice{{
					Message:      oaiMessage{Role: "assistant", Content: "sunny"},
					FinishReason: "stop",
				}},
			})
		default:
			t.Fatalf("unexpected request step %d", step)
		}
	}))
	defer server.Close()

	tools := []Tool{{
		Name:        "search",
		Description: "Search the web",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
	}}
	caller := &stubToolCaller{results: map[string]string{"search": "weather: sunny"}}

	client := NewOpenAIClient(OpenAIConfig{APIKey: "k", BaseURL: server.URL})
	resp, err := client.SendConversationWithTools(context.Background(), "be helpful",
		[]Message{{Role: RoleUser, Content: "what's the weather?"}}, tools, caller, nil, "openai/gpt-4o")
	if err != nil {
		t.Fatalf("SendConversationWithTools: %v", err)
	}
	if resp != "sunny" {
		t.Errorf("expected 'sunny', got %q", resp)
	}
	if len(caller.calls) != 1 || caller.calls[0] != "search" {
		t.Errorf("expected one call to 'search', got %+v", caller.calls)
	}
}
