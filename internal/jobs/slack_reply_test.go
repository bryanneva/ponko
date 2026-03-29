package jobs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/riverqueue/river"

	"github.com/bryanneva/ponko/internal/slack"
)

func TestSlackReplyArgs_Kind(t *testing.T) {
	args := SlackReplyArgs{}
	if args.Kind() != "slack_reply" {
		t.Errorf("Kind() = %q, want %q", args.Kind(), "slack_reply")
	}
}

func TestThreadTSForReply(t *testing.T) {
	tests := []struct {
		name             string
		channelType      string
		threadTS         string
		expectedThreadTS string
	}{
		{
			name:             "channel reply uses thread_ts",
			channelType:      "channel",
			threadTS:         "111.222",
			expectedThreadTS: "111.222",
		},
		{
			name:             "group reply uses thread_ts",
			channelType:      "group",
			threadTS:         "111.222",
			expectedThreadTS: "111.222",
		},
		{
			name:             "DM reply omits thread_ts",
			channelType:      "im",
			threadTS:         "111.222",
			expectedThreadTS: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			threadTS := threadTSForReply(tt.threadTS, tt.channelType)
			if threadTS != tt.expectedThreadTS {
				t.Errorf("threadTS = %q, want %q", threadTS, tt.expectedThreadTS)
			}
		})
	}
}


type slackPostMessageReq struct {
	Channel  string `json:"channel"`
	Text     string `json:"text"`
	ThreadTS string `json:"thread_ts,omitempty"`
}

type slackReactionReq struct {
	Channel   string `json:"channel"`
	Name      string `json:"name"`
	Timestamp string `json:"timestamp"`
}

func TestSlackReplyWorker_Work(t *testing.T) {
	t.Run("channel message with response posts in thread with check_mark", func(t *testing.T) {
		var mu sync.Mutex
		calls := map[string]bool{}
		var postedReq slackPostMessageReq

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()

			switch r.URL.Path {
			case "/reactions.remove":
				var req slackReactionReq
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("decoding reaction remove: %v", err)
				}
				if req.Name != "eyes" {
					t.Errorf("expected eyes reaction removal, got %q", req.Name)
				}
				calls["reactions.remove"] = true
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok": true}`))

			case "/chat.postMessage":
				if err := json.NewDecoder(r.Body).Decode(&postedReq); err != nil {
					t.Errorf("decoding post message: %v", err)
				}
				calls["chat.postMessage"] = true
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok": true}`))

			case "/reactions.add":
				var req slackReactionReq
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("decoding reaction add: %v", err)
				}
				if req.Name != "white_check_mark" {
					t.Errorf("expected white_check_mark reaction, got %q", req.Name)
				}
				calls["reactions.add"] = true
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok": true}`))

			default:
				t.Errorf("unexpected API call: %s", r.URL.Path)
			}
		}))
		defer server.Close()

		worker := &SlackReplyWorker{Slack: slack.NewClient("test-token", server.URL)}
		job := &river.Job[SlackReplyArgs]{
			Args: SlackReplyArgs{
				WorkflowID:  "wf-1",
				Response:    "Hello **world**",
				Channel:     "C123",
				ThreadTS:    "111.222",
				EventTS:     "333.444",
				ChannelType: "channel",
			},
		}

		err := worker.Work(context.Background(), job)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		mu.Lock()
		defer mu.Unlock()

		if !calls["reactions.remove"] {
			t.Error("expected reactions.remove call")
		}
		if !calls["chat.postMessage"] {
			t.Error("expected chat.postMessage call")
		}
		if !calls["reactions.add"] {
			t.Error("expected reactions.add call")
		}
		if postedReq.ThreadTS != "111.222" {
			t.Errorf("expected thread_ts 111.222, got %q", postedReq.ThreadTS)
		}
	})

	t.Run("DM message posts as top-level (no thread_ts)", func(t *testing.T) {
		var mu sync.Mutex
		var postedReq slackPostMessageReq

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()

			switch r.URL.Path {
			case "/reactions.remove", "/reactions.add":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok": true}`))
			case "/chat.postMessage":
				if err := json.NewDecoder(r.Body).Decode(&postedReq); err != nil {
					t.Errorf("decoding post message: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok": true}`))
			}
		}))
		defer server.Close()

		worker := &SlackReplyWorker{Slack: slack.NewClient("test-token", server.URL)}
		job := &river.Job[SlackReplyArgs]{
			Args: SlackReplyArgs{
				WorkflowID:  "wf-2",
				Response:    "DM reply",
				Channel:     "D123",
				ThreadTS:    "111.222",
				EventTS:     "333.444",
				ChannelType: "im",
			},
		}

		err := worker.Work(context.Background(), job)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		mu.Lock()
		defer mu.Unlock()

		if postedReq.ThreadTS != "" {
			t.Errorf("expected empty thread_ts for DM, got %q", postedReq.ThreadTS)
		}
	})

	t.Run("empty response sends error message with warning reaction", func(t *testing.T) {
		var mu sync.Mutex
		var postedReq slackPostMessageReq
		var addedReaction string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()

			switch r.URL.Path {
			case "/reactions.remove":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok": true}`))
			case "/chat.postMessage":
				if err := json.NewDecoder(r.Body).Decode(&postedReq); err != nil {
					t.Errorf("decoding post message: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok": true}`))
			case "/reactions.add":
				var req slackReactionReq
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("decoding reaction add: %v", err)
				}
				addedReaction = req.Name
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok": true}`))
			}
		}))
		defer server.Close()

		worker := &SlackReplyWorker{Slack: slack.NewClient("test-token", server.URL)}
		job := &river.Job[SlackReplyArgs]{
			Args: SlackReplyArgs{
				WorkflowID:  "wf-3",
				Response:    "",
				Channel:     "C123",
				ThreadTS:    "111.222",
				EventTS:     "333.444",
				ChannelType: "channel",
			},
		}

		err := worker.Work(context.Background(), job)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		mu.Lock()
		defer mu.Unlock()

		if !strings.Contains(postedReq.Text, "failed") {
			t.Errorf("expected error message containing 'failed', got %q", postedReq.Text)
		}
		if !strings.Contains(postedReq.Text, "wf-3") {
			t.Errorf("expected error message containing workflow ID 'wf-3', got %q", postedReq.Text)
		}
		if addedReaction != "warning" {
			t.Errorf("expected warning reaction, got %q", addedReaction)
		}
	})

	t.Run("non-empty response applies MarkdownToMrkdwn conversion", func(t *testing.T) {
		var mu sync.Mutex
		var postedReq slackPostMessageReq

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()

			switch r.URL.Path {
			case "/reactions.remove", "/reactions.add":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok": true}`))
			case "/chat.postMessage":
				if err := json.NewDecoder(r.Body).Decode(&postedReq); err != nil {
					t.Errorf("decoding post message: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok": true}`))
			}
		}))
		defer server.Close()

		worker := &SlackReplyWorker{Slack: slack.NewClient("test-token", server.URL)}
		job := &river.Job[SlackReplyArgs]{
			Args: SlackReplyArgs{
				WorkflowID:  "wf-4",
				Response:    "Hello **bold** world",
				Channel:     "C123",
				ThreadTS:    "111.222",
				EventTS:     "333.444",
				ChannelType: "channel",
			},
		}

		err := worker.Work(context.Background(), job)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		mu.Lock()
		defer mu.Unlock()

		expected := slack.MarkdownToMrkdwn("Hello **bold** world")
		if postedReq.Text != expected {
			t.Errorf("expected MarkdownToMrkdwn output %q, got %q", expected, postedReq.Text)
		}
	})

	t.Run("no eventTS skips reaction removal and addition", func(t *testing.T) {
		var mu sync.Mutex
		reactionCalled := false

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()

			switch r.URL.Path {
			case "/reactions.remove", "/reactions.add":
				reactionCalled = true
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok": true}`))
			case "/chat.postMessage":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok": true}`))
			}
		}))
		defer server.Close()

		worker := &SlackReplyWorker{Slack: slack.NewClient("test-token", server.URL)}
		job := &river.Job[SlackReplyArgs]{
			Args: SlackReplyArgs{
				WorkflowID:  "wf-5",
				Response:    "No event TS",
				Channel:     "C123",
				ThreadTS:    "111.222",
				EventTS:     "",
				ChannelType: "channel",
			},
		}

		err := worker.Work(context.Background(), job)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		mu.Lock()
		defer mu.Unlock()

		if reactionCalled {
			t.Error("expected no reaction calls when eventTS is empty")
		}
	})

	t.Run("permanent Slack errors are discarded without retry", func(t *testing.T) {
		for _, slackErr := range slack.PermanentErrors {
			t.Run(slackErr, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/reactions.remove":
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write([]byte(`{"ok": true}`))
					case "/chat.postMessage":
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write([]byte(`{"ok": false, "error": "` + slackErr + `"}`))
					}
				}))
				defer server.Close()

				worker := &SlackReplyWorker{Slack: slack.NewClient("test-token", server.URL)}
				job := &river.Job[SlackReplyArgs]{
					Args: SlackReplyArgs{
						WorkflowID:  "wf-6",
						Response:    "Will fail",
						Channel:     "C999",
						ThreadTS:    "111.222",
						EventTS:     "333.444",
						ChannelType: "channel",
					},
				}

				err := worker.Work(context.Background(), job)
				if err != nil {
					t.Fatalf("expected nil (permanent error discarded), got %v", err)
				}
			})
		}
	})

	t.Run("transient Slack errors are retried", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/reactions.remove":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok": true}`))
			case "/chat.postMessage":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok": false, "error": "server_error"}`))
			}
		}))
		defer server.Close()

		worker := &SlackReplyWorker{Slack: slack.NewClient("test-token", server.URL)}
		job := &river.Job[SlackReplyArgs]{
			Args: SlackReplyArgs{
				WorkflowID:  "wf-6",
				Response:    "Will fail",
				Channel:     "C999",
				ThreadTS:    "111.222",
				EventTS:     "333.444",
				ChannelType: "channel",
			},
		}

		err := worker.Work(context.Background(), job)
		if err == nil {
			t.Fatal("expected error for transient failure, got nil")
		}
		if !strings.Contains(err.Error(), "posting slack message") {
			t.Errorf("expected 'posting slack message' in error, got %q", err.Error())
		}
	})

	t.Run("reaction remove error is non-fatal", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/reactions.remove":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok": false, "error": "no_reaction"}`))
			case "/chat.postMessage":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok": true}`))
			case "/reactions.add":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok": true}`))
			}
		}))
		defer server.Close()

		worker := &SlackReplyWorker{Slack: slack.NewClient("test-token", server.URL)}
		job := &river.Job[SlackReplyArgs]{
			Args: SlackReplyArgs{
				WorkflowID:  "wf-7",
				Response:    "Should still work",
				Channel:     "C123",
				ThreadTS:    "111.222",
				EventTS:     "333.444",
				ChannelType: "channel",
			},
		}

		err := worker.Work(context.Background(), job)
		if err != nil {
			t.Fatalf("expected no error (reaction remove is non-fatal), got %v", err)
		}
	})

	t.Run("reaction add error is non-fatal", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/reactions.remove":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok": true}`))
			case "/chat.postMessage":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok": true}`))
			case "/reactions.add":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok": false, "error": "already_reacted"}`))
			}
		}))
		defer server.Close()

		worker := &SlackReplyWorker{Slack: slack.NewClient("test-token", server.URL)}
		job := &river.Job[SlackReplyArgs]{
			Args: SlackReplyArgs{
				WorkflowID:  "wf-8",
				Response:    "Should still work",
				Channel:     "C123",
				ThreadTS:    "111.222",
				EventTS:     "333.444",
				ChannelType: "channel",
			},
		}

		err := worker.Work(context.Background(), job)
		if err != nil {
			t.Fatalf("expected no error (reaction add is non-fatal), got %v", err)
		}
	})
}
