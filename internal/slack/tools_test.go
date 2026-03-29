package slack

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExecuteReadSlackThread(t *testing.T) {
	t.Run("success with display names", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/conversations.replies", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("channel") != "C00TESTCHAN" {
				t.Errorf("unexpected channel: %s", r.URL.Query().Get("channel"))
			}
			if r.URL.Query().Get("ts") != "1774549751.570729" {
				t.Errorf("unexpected ts: %s", r.URL.Query().Get("ts"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"ok": true,
				"messages": [
					{"user": "U111", "text": "What should we do about the bug?", "ts": "1774549751.570729"},
					{"user": "U222", "text": "I think we should fix it ASAP", "ts": "1774549760.000000"}
				]
			}`))
		})
		mux.HandleFunc("/users.info", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Query().Get("user") {
			case "U111":
				_, _ = w.Write([]byte(`{"ok": true, "user": {"profile": {"display_name": "Alice"}, "tz": "America/New_York", "is_admin": false}}`))
			case "U222":
				_, _ = w.Write([]byte(`{"ok": true, "user": {"profile": {"display_name": "Bob"}, "tz": "America/Chicago", "is_admin": false}}`))
			default:
				_, _ = w.Write([]byte(`{"ok": false, "error": "user_not_found"}`))
			}
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		client := NewClient("test-token", server.URL)
		result, err := ExecuteReadSlackThread(context.Background(), client, "https://example.slack.com/archives/C00TESTCHAN/p1774549751570729")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "[Alice]") {
			t.Errorf("expected result to contain display name Alice, got: %s", result)
		}
		if !strings.Contains(result, "[Bob]") {
			t.Errorf("expected result to contain display name Bob, got: %s", result)
		}
		if !strings.Contains(result, "What should we do about the bug?") {
			t.Errorf("expected result to contain message text, got: %s", result)
		}
		if !strings.Contains(result, "I think we should fix it ASAP") {
			t.Errorf("expected result to contain reply text, got: %s", result)
		}
	})

	t.Run("falls back to user ID on profile lookup failure", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/conversations.replies", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"ok": true,
				"messages": [
					{"user": "U111", "text": "Hello", "ts": "1774549751.570729"},
					{"user": "U999", "text": "World", "ts": "1774549760.000000"}
				]
			}`))
		})
		mux.HandleFunc("/users.info", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Query().Get("user") {
			case "U111":
				_, _ = w.Write([]byte(`{"ok": true, "user": {"profile": {"display_name": "Alice"}, "tz": "America/New_York", "is_admin": false}}`))
			default:
				_, _ = w.Write([]byte(`{"ok": false, "error": "user_not_found"}`))
			}
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		client := NewClient("test-token", server.URL)
		result, err := ExecuteReadSlackThread(context.Background(), client, "https://example.slack.com/archives/C00TESTCHAN/p1774549751570729")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "[Alice]") {
			t.Errorf("expected resolved name Alice, got: %s", result)
		}
		if !strings.Contains(result, "[U999]") {
			t.Errorf("expected fallback to user ID U999, got: %s", result)
		}
	})

	t.Run("invalid URL", func(t *testing.T) {
		client := NewClient("test-token", "http://unused")
		_, err := ExecuteReadSlackThread(context.Background(), client, "https://example.com/not-slack")
		if err == nil {
			t.Fatal("expected error for invalid URL")
		}
	})

	t.Run("slack API error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok": false, "error": "not_in_channel"}`))
		}))
		defer server.Close()

		client := NewClient("test-token", server.URL)
		_, err := ExecuteReadSlackThread(context.Background(), client, "https://example.slack.com/archives/C00TESTCHAN/p1774549751570729")
		if err == nil {
			t.Fatal("expected error for Slack API error")
		}
		if !strings.Contains(err.Error(), "not_in_channel") {
			t.Errorf("expected error to mention not_in_channel, got: %v", err)
		}
	})
}
