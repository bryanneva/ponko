package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBlockJSONSerialization(t *testing.T) {
	t.Run("section block", func(t *testing.T) {
		block := SectionBlock{
			Text: TextObject{Type: "mrkdwn", Text: "Hello *world*"},
		}
		data, err := json.Marshal(block)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if got["type"] != "section" {
			t.Errorf("expected type section, got %v", got["type"])
		}
		text := got["text"].(map[string]any)
		if text["type"] != "mrkdwn" {
			t.Errorf("expected text type mrkdwn, got %v", text["type"])
		}
		if text["text"] != "Hello *world*" {
			t.Errorf("expected text 'Hello *world*', got %v", text["text"])
		}
	})

	t.Run("actions block with buttons", func(t *testing.T) {
		block := ActionsBlock{
			Elements: []ButtonElement{
				{
					Text:     TextObject{Type: "plain_text", Text: "Approve"},
					ActionID: "approve_plan",
				},
				{
					Text:     TextObject{Type: "plain_text", Text: "Reject"},
					ActionID: "reject_plan",
					Style:    "danger",
				},
			},
		}
		data, err := json.Marshal(block)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if got["type"] != "actions" {
			t.Errorf("expected type actions, got %v", got["type"])
		}
		elements := got["elements"].([]any)
		if len(elements) != 2 {
			t.Fatalf("expected 2 elements, got %d", len(elements))
		}
		btn := elements[0].(map[string]any)
		if btn["type"] != "button" {
			t.Errorf("expected button type, got %v", btn["type"])
		}
		if btn["action_id"] != "approve_plan" {
			t.Errorf("expected action_id approve_plan, got %v", btn["action_id"])
		}
		rejectBtn := elements[1].(map[string]any)
		if rejectBtn["style"] != "danger" {
			t.Errorf("expected style danger, got %v", rejectBtn["style"])
		}
	})

	t.Run("button with value", func(t *testing.T) {
		btn := ButtonElement{
			Text:     TextObject{Type: "plain_text", Text: "Click"},
			ActionID: "my_action",
			Value:    "workflow-123",
		}
		data, err := json.Marshal(btn)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if got["value"] != "workflow-123" {
			t.Errorf("expected value workflow-123, got %v", got["value"])
		}
	})

	t.Run("button without optional fields omits them", func(t *testing.T) {
		btn := ButtonElement{
			Text:     TextObject{Type: "plain_text", Text: "Click"},
			ActionID: "my_action",
		}
		data, err := json.Marshal(btn)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if _, ok := got["style"]; ok {
			t.Error("expected style to be omitted")
		}
		if _, ok := got["value"]; ok {
			t.Error("expected value to be omitted")
		}
	})
}

func TestPostBlocks(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.URL.Path != "/chat.postMessage" {
				t.Errorf("expected /chat.postMessage, got %s", r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Errorf("expected Bearer test-token, got %q", r.Header.Get("Authorization"))
			}

			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decoding request: %v", err)
			}
			if req["channel"] != "C123" {
				t.Errorf("expected channel C123, got %v", req["channel"])
			}
			if req["text"] != "fallback text" {
				t.Errorf("expected text 'fallback text', got %v", req["text"])
			}
			if req["thread_ts"] != "1234567890.123456" {
				t.Errorf("expected thread_ts, got %v", req["thread_ts"])
			}

			blocks := req["blocks"].([]any)
			if len(blocks) != 1 {
				t.Fatalf("expected 1 block, got %d", len(blocks))
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok": true, "ts": "1234567890.999999"}`))
		}))
		defer server.Close()

		client := NewClient("test-token", server.URL)
		blocks := []Block{
			SectionBlock{Text: TextObject{Type: "mrkdwn", Text: "hello"}},
		}
		ts, err := client.PostBlocks(context.Background(), "C123", "fallback text", blocks, "1234567890.123456")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ts != "1234567890.999999" {
			t.Errorf("expected ts 1234567890.999999, got %q", ts)
		}
	})

	t.Run("slack error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok": false, "error": "channel_not_found"}`))
		}))
		defer server.Close()

		client := NewClient("test-token", server.URL)
		_, err := client.PostBlocks(context.Background(), "C999", "text", nil, "")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err.Error() != "slack API error: channel_not_found" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal server error"))
		}))
		defer server.Close()

		client := NewClient("test-token", server.URL)
		_, err := client.PostBlocks(context.Background(), "C123", "text", nil, "")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestUpdateMessage(t *testing.T) {
	t.Run("success with text only", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.URL.Path != "/chat.update" {
				t.Errorf("expected /chat.update, got %s", r.URL.Path)
			}

			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decoding request: %v", err)
			}
			if req["channel"] != "C123" {
				t.Errorf("expected channel C123, got %v", req["channel"])
			}
			if req["ts"] != "1234567890.999999" {
				t.Errorf("expected ts, got %v", req["ts"])
			}
			if req["text"] != "updated text" {
				t.Errorf("expected updated text, got %v", req["text"])
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok": true}`))
		}))
		defer server.Close()

		client := NewClient("test-token", server.URL)
		err := client.UpdateMessage(context.Background(), "C123", "1234567890.999999", "updated text", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("success with blocks", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decoding request: %v", err)
			}
			blocks := req["blocks"].([]any)
			if len(blocks) != 1 {
				t.Fatalf("expected 1 block, got %d", len(blocks))
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok": true}`))
		}))
		defer server.Close()

		client := NewClient("test-token", server.URL)
		blocks := []Block{
			SectionBlock{Text: TextObject{Type: "mrkdwn", Text: "approved"}},
		}
		err := client.UpdateMessage(context.Background(), "C123", "1234567890.999999", "approved", blocks)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("slack error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok": false, "error": "message_not_found"}`))
		}))
		defer server.Close()

		client := NewClient("test-token", server.URL)
		err := client.UpdateMessage(context.Background(), "C123", "1234567890.999999", "text", nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
