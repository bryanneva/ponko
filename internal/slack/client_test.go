package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPostMessage(t *testing.T) {
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
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json, got %q", r.Header.Get("Content-Type"))
		}

		var req postMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		if req.Channel != "C123" {
			t.Errorf("expected channel C123, got %q", req.Channel)
		}
		if req.Text != "hello world" {
			t.Errorf("expected text 'hello world', got %q", req.Text)
		}
		if req.ThreadTS != "" {
			t.Errorf("expected empty thread_ts, got %q", req.ThreadTS)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)
	err := client.PostMessage(context.Background(), "C123", "hello world", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPostMessageWithThread(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req postMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		if req.ThreadTS != "1234567890.123456" {
			t.Errorf("expected thread_ts '1234567890.123456', got %q", req.ThreadTS)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)
	err := client.PostMessage(context.Background(), "C123", "reply", "1234567890.123456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPostMessageSlackError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": false, "error": "channel_not_found"}`))
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)
	err := client.PostMessage(context.Background(), "C999", "hello", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "slack API error: channel_not_found" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestPostMessageHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)
	err := client.PostMessage(context.Background(), "C123", "hello", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetUserProfile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users.info" {
			t.Errorf("expected /users.info, got %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Query().Get("user") != "U12345" {
			t.Errorf("expected user query param U12345, got %q", r.URL.Query().Get("user"))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": true, "user": {"tz": "America/New_York", "profile": {"display_name": "TestUser"}}}`))
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)
	profile, err := client.GetUserProfile(context.Background(), "U12345")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.Timezone != "America/New_York" {
		t.Errorf("expected America/New_York, got %q", profile.Timezone)
	}
	if profile.DisplayName != "TestUser" {
		t.Errorf("expected TestUser, got %q", profile.DisplayName)
	}
}

func TestGetUserProfileSlackError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": false, "error": "user_not_found"}`))
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)
	_, err := client.GetUserProfile(context.Background(), "U99999")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAddReaction(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.URL.Path != "/reactions.add" {
				t.Errorf("expected /reactions.add, got %s", r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Errorf("expected Bearer test-token, got %q", r.Header.Get("Authorization"))
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("expected application/json, got %q", r.Header.Get("Content-Type"))
			}

			var req reactionRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decoding request: %v", err)
			}
			if req.Channel != "C123" {
				t.Errorf("expected channel C123, got %q", req.Channel)
			}
			if req.Timestamp != "1234567890.123456" {
				t.Errorf("expected timestamp 1234567890.123456, got %q", req.Timestamp)
			}
			if req.Name != "eyes" {
				t.Errorf("expected name eyes, got %q", req.Name)
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok": true}`))
		}))
		defer server.Close()

		client := NewClient("test-token", server.URL)
		err := client.AddReaction(context.Background(), "C123", "1234567890.123456", "eyes")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("slack API error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok": false, "error": "already_reacted"}`))
		}))
		defer server.Close()

		client := NewClient("test-token", server.URL)
		err := client.AddReaction(context.Background(), "C123", "1234567890.123456", "eyes")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err.Error() != "slack API error: already_reacted" {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal server error"))
		}))
		defer server.Close()

		client := NewClient("test-token", server.URL)
		err := client.AddReaction(context.Background(), "C123", "1234567890.123456", "eyes")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestRemoveReaction(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.URL.Path != "/reactions.remove" {
				t.Errorf("expected /reactions.remove, got %s", r.URL.Path)
			}

			var req reactionRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decoding request: %v", err)
			}
			if req.Channel != "C456" {
				t.Errorf("expected channel C456, got %q", req.Channel)
			}
			if req.Name != "thumbsup" {
				t.Errorf("expected name thumbsup, got %q", req.Name)
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok": true}`))
		}))
		defer server.Close()

		client := NewClient("test-token", server.URL)
		err := client.RemoveReaction(context.Background(), "C456", "1234567890.654321", "thumbsup")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("slack API error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok": false, "error": "no_reaction"}`))
		}))
		defer server.Close()

		client := NewClient("test-token", server.URL)
		err := client.RemoveReaction(context.Background(), "C456", "1234567890.654321", "thumbsup")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err.Error() != "slack API error: no_reaction" {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("bad gateway"))
		}))
		defer server.Close()

		client := NewClient("test-token", server.URL)
		err := client.RemoveReaction(context.Background(), "C456", "1234567890.654321", "thumbsup")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestGetChannelName(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/conversations.info" {
				t.Errorf("expected /conversations.info, got %s", r.URL.Path)
			}
			if r.Method != http.MethodGet {
				t.Errorf("expected GET, got %s", r.Method)
			}
			if r.URL.Query().Get("channel") != "C789" {
				t.Errorf("expected channel query param C789, got %q", r.URL.Query().Get("channel"))
			}
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Errorf("expected Bearer test-token, got %q", r.Header.Get("Authorization"))
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok": true, "channel": {"name": "general"}}`))
		}))
		defer server.Close()

		client := NewClient("test-token", server.URL)
		name, err := client.GetChannelName(context.Background(), "C789")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if name != "general" {
			t.Errorf("expected general, got %q", name)
		}
	})

	t.Run("slack API error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok": false, "error": "channel_not_found"}`))
		}))
		defer server.Close()

		client := NewClient("test-token", server.URL)
		_, err := client.GetChannelName(context.Background(), "C999")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err.Error() != "slack API error: channel_not_found" {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal server error"))
		}))
		defer server.Close()

		client := NewClient("test-token", server.URL)
		_, err := client.GetChannelName(context.Background(), "C789")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestGetConversationReplies(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/conversations.replies" {
				t.Errorf("expected /conversations.replies, got %s", r.URL.Path)
			}
			if r.Method != http.MethodGet {
				t.Errorf("expected GET, got %s", r.Method)
			}
			if r.URL.Query().Get("channel") != "C00TESTCHAN" {
				t.Errorf("expected channel C00TESTCHAN, got %q", r.URL.Query().Get("channel"))
			}
			if r.URL.Query().Get("ts") != "1774549751.570729" {
				t.Errorf("expected ts 1774549751.570729, got %q", r.URL.Query().Get("ts"))
			}
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Errorf("expected Bearer test-token, got %q", r.Header.Get("Authorization"))
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"ok": true,
				"messages": [
					{"user": "U111", "text": "parent message", "ts": "1774549751.570729"},
					{"user": "U222", "text": "first reply", "ts": "1774549760.000000"},
					{"user": "U111", "text": "second reply", "ts": "1774549770.000000"}
				]
			}`))
		}))
		defer server.Close()

		client := NewClient("test-token", server.URL)
		msgs, err := client.GetConversationReplies(context.Background(), "C00TESTCHAN", "1774549751.570729")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(msgs) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(msgs))
		}
		if msgs[0].UserID != "U111" || msgs[0].Text != "parent message" {
			t.Errorf("unexpected first message: %+v", msgs[0])
		}
		if msgs[1].UserID != "U222" || msgs[1].Text != "first reply" {
			t.Errorf("unexpected second message: %+v", msgs[1])
		}
		if msgs[2].Timestamp != "1774549770.000000" {
			t.Errorf("unexpected third message timestamp: %q", msgs[2].Timestamp)
		}
	})

	t.Run("slack API error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok": false, "error": "channel_not_found"}`))
		}))
		defer server.Close()

		client := NewClient("test-token", server.URL)
		_, err := client.GetConversationReplies(context.Background(), "C999", "1234567890.123456")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err.Error() != "slack API error: channel_not_found" {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal server error"))
		}))
		defer server.Close()

		client := NewClient("test-token", server.URL)
		_, err := client.GetConversationReplies(context.Background(), "C123", "1234567890.123456")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestNewClientDefaultBaseURL(t *testing.T) {
	client := NewClient("token", "")
	if client.baseURL != "https://slack.com/api" {
		t.Errorf("expected default base URL, got %q", client.baseURL)
	}
}
