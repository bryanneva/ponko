package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewClientHasTimeout(t *testing.T) {
	client := NewClient("test-key", "")
	if client.httpClient.Timeout != httpClientTimeout {
		t.Errorf("expected timeout %v, got %v", httpClientTimeout, client.httpClient.Timeout)
	}
}

func TestDoAPIRequestTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(messagesResponse{
			Content: []contentBlock{{Type: "text", Text: "too slow"}},
		})
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL)
	client.httpClient.Timeout = 50 * time.Millisecond

	_, err := client.SendMessage(context.Background(), "hello", ModelHaiku)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "Client.Timeout") && !strings.Contains(err.Error(), "deadline exceeded") {
		t.Errorf("expected timeout-related error, got: %v", err)
	}
}

func TestSendMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("expected x-api-key 'test-key', got %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("expected anthropic-version '2023-06-01', got %q", r.Header.Get("anthropic-version"))
		}
		if r.Header.Get("content-type") != "application/json" {
			t.Errorf("expected content-type 'application/json', got %q", r.Header.Get("content-type"))
		}

		var req messagesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		if req.Model != ModelHaiku {
			t.Errorf("expected model %q, got %q", ModelHaiku, req.Model)
		}
		if len(req.Messages) != 1 || req.Messages[0].Content != "hello" {
			t.Errorf("unexpected messages: %+v", req.Messages)
		}

		resp := messagesResponse{
			Content: []contentBlock{{Type: "text", Text: "HELLO"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL)
	result, err := client.SendMessage(context.Background(), "hello", ModelHaiku)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "HELLO" {
		t.Errorf("expected 'HELLO', got %q", result)
	}
}

func TestSendConversation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Fatalf("decoding request: %v", err)
		}

		var system []systemBlock
		if err := json.Unmarshal(raw["system"], &system); err != nil {
			t.Fatalf("system field should be an array: %v", err)
		}
		if len(system) != 1 {
			t.Fatalf("expected 1 system block, got %d", len(system))
		}
		if system[0].Text != "You are a helpful assistant." {
			t.Errorf("expected system text 'You are a helpful assistant.', got %q", system[0].Text)
		}
		if system[0].CacheControl == nil || system[0].CacheControl.Type != "ephemeral" {
			t.Errorf("expected cache_control ephemeral on system block")
		}

		// Verify messages array
		var messages []Message
		if err := json.Unmarshal(raw["messages"], &messages); err != nil {
			t.Fatalf("decoding messages: %v", err)
		}
		if len(messages) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(messages))
		}
		if messages[0].Role != "user" || messages[0].Content != "Hello" {
			t.Errorf("unexpected first message: %+v", messages[0])
		}
		if messages[1].Role != "assistant" || messages[1].Content != "Hi there!" {
			t.Errorf("unexpected second message: %+v", messages[1])
		}

		var maxTokens int
		if err := json.Unmarshal(raw["max_tokens"], &maxTokens); err != nil {
			t.Fatalf("decoding max_tokens: %v", err)
		}
		if maxTokens != 2048 {
			t.Errorf("expected max_tokens 2048, got %d", maxTokens)
		}

		resp := messagesResponse{
			Content: []contentBlock{{Type: "text", Text: "How can I help?"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL)
	messages := []Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
	}
	result, err := client.SendConversation(context.Background(), "You are a helpful assistant.", messages, ModelSonnet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "How can I help?" {
		t.Errorf("expected 'How can I help?', got %q", result)
	}
}

func TestSendMessageAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": "invalid api key"},
		})
	}))
	defer server.Close()

	client := NewClient("bad-key", server.URL)
	_, err := client.SendMessage(context.Background(), "hello", ModelHaiku)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

type mockToolCaller struct {
	calledArgs map[string]any
	err        error
	calledName string
	result     string
}

func (m *mockToolCaller) CallTool(_ context.Context, name string, arguments map[string]any, _ *UserScope) (string, error) {
	m.calledName = name
	m.calledArgs = arguments
	if m.err != nil {
		return "", m.err
	}
	return m.result, nil
}

func TestSendConversationWithTools(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		if callCount == 1 {
			var raw map[string]json.RawMessage
			if decodeErr := json.NewDecoder(r.Body).Decode(&raw); decodeErr != nil {
				t.Fatalf("decoding request: %v", decodeErr)
			}

			var system []systemBlock
			if unmarshalErr := json.Unmarshal(raw["system"], &system); unmarshalErr != nil {
				t.Fatalf("system field should be an array: %v", unmarshalErr)
			}
			if len(system) != 1 || system[0].CacheControl == nil || system[0].CacheControl.Type != "ephemeral" {
				t.Errorf("expected cache_control ephemeral on system block")
			}

			var tools []Tool
			if unmarshalErr := json.Unmarshal(raw["tools"], &tools); unmarshalErr != nil {
				t.Fatalf("decoding tools: %v", unmarshalErr)
			}
			if len(tools) != 1 {
				t.Fatalf("expected 1 tool, got %d", len(tools))
			}
			if tools[0].CacheControl == nil || tools[0].CacheControl.Type != "ephemeral" {
				t.Errorf("expected cache_control ephemeral on last tool")
			}

			_ = json.NewEncoder(w).Encode(toolUseResponse{
				StopReason: "tool_use",
				Content: []toolUseContentBlock{
					{Type: "text", Text: "Let me search for that."},
					{Type: "tool_use", ID: "toolu_123", Name: "search_thoughts", Input: json.RawMessage(`{"query":"golang"}`)},
				},
			})
			return
		}

		var raw map[string]json.RawMessage
		if decodeErr := json.NewDecoder(r.Body).Decode(&raw); decodeErr != nil {
			t.Fatalf("decoding request: %v", decodeErr)
		}
		var msgs []json.RawMessage
		if unmarshalErr := json.Unmarshal(raw["messages"], &msgs); unmarshalErr != nil {
			t.Fatalf("decoding messages: %v", unmarshalErr)
		}
		if len(msgs) != 3 {
			t.Errorf("expected 3 messages on second call, got %d", len(msgs))
		}

		_ = json.NewEncoder(w).Encode(toolUseResponse{
			StopReason: "end_turn",
			Content: []toolUseContentBlock{
				{Type: "text", Text: "I found some results about golang."},
			},
		})
	}))
	defer server.Close()

	mock := &mockToolCaller{result: "found 3 thoughts"}
	client := NewClient("test-key", server.URL)
	tools := []Tool{{Name: "search_thoughts", Description: "Search thoughts", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	messages := []Message{{Role: "user", Content: "search for golang"}}

	result, resultErr := client.SendConversationWithTools(context.Background(), "system prompt", messages, tools, mock, nil, ModelSonnet)
	if resultErr != nil {
		t.Fatalf("unexpected error: %v", resultErr)
	}
	if result != "I found some results about golang." {
		t.Errorf("expected final text, got %q", result)
	}
	if mock.calledName != "search_thoughts" {
		t.Errorf("expected tool 'search_thoughts', got %q", mock.calledName)
	}
	if mock.calledArgs["query"] != "golang" {
		t.Errorf("expected query 'golang', got %v", mock.calledArgs["query"])
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls, got %d", callCount)
	}
}

func TestSendConversationWithToolsError(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		if callCount == 1 {
			_ = json.NewEncoder(w).Encode(toolUseResponse{
				StopReason: "tool_use",
				Content: []toolUseContentBlock{
					{Type: "tool_use", ID: "toolu_456", Name: "capture_thought", Input: json.RawMessage(`{"text":"hello"}`)},
				},
			})
			return
		}

		_ = json.NewEncoder(w).Encode(toolUseResponse{
			StopReason: "end_turn",
			Content: []toolUseContentBlock{
				{Type: "text", Text: "Sorry, I couldn't save that."},
			},
		})
	}))
	defer server.Close()

	mock := &mockToolCaller{err: fmt.Errorf("connection refused")}
	client := NewClient("test-key", server.URL)
	tools := []Tool{{Name: "capture_thought", Description: "Save a thought", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	messages := []Message{{Role: "user", Content: "remember this"}}

	result, resultErr := client.SendConversationWithTools(context.Background(), "", messages, tools, mock, nil, ModelSonnet)
	if resultErr != nil {
		t.Fatalf("unexpected error: %v", resultErr)
	}
	if result != "Sorry, I couldn't save that." {
		t.Errorf("expected error response text, got %q", result)
	}
}

func TestSendMessageEmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(messagesResponse{Content: []contentBlock{}})
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL)
	_, err := client.SendMessage(context.Background(), "hello", ModelHaiku)
	if err == nil {
		t.Fatal("expected error for empty response, got nil")
	}
}
