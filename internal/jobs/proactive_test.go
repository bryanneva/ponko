package jobs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/riverqueue/river"

	"github.com/bryanneva/ponko/internal/llm"
	"github.com/bryanneva/ponko/internal/slack"
	"github.com/bryanneva/ponko/internal/testutil"
)

func TestProactiveMessageWorker_Work(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	t.Run("cron schedule computes next_run_at and marks run", func(t *testing.T) {
		cron := "0 9 * * *"

		// Insert a scheduled message
		var schedID string
		scanErr := pool.QueryRow(ctx,
			`INSERT INTO scheduled_messages (channel_id, prompt, schedule_cron, next_run_at, one_shot, enabled)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 RETURNING id`,
			"C-PROACTIVE-1", "test prompt", &cron, time.Now().Add(-1*time.Minute), false, true,
		).Scan(&schedID)
		if scanErr != nil {
			t.Fatalf("inserting schedule: %v", scanErr)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM scheduled_messages WHERE id = $1", schedID)
		})

		var mu sync.Mutex
		var postCalled bool

		// Track ordering: MarkRun (DB) happens before PostMessage (Slack)
		claudeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]any{
				"content": []map[string]string{{"type": "text", "text": "Hello from Claude"}},
				"usage":   map[string]int{"input_tokens": 10, "output_tokens": 5},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer claudeServer.Close()

		slackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			postCalled = true
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer slackServer.Close()

		worker := &ProactiveMessageWorker{
			Pool:         pool,
			Claude:       llm.NewClient("test-key", claudeServer.URL),
			Slack:        slack.NewClient("test-token", slackServer.URL),
			SystemPrompt: "test prompt",
		}

		job := &river.Job[ProactiveMessageArgs]{
			Args: ProactiveMessageArgs{
				ScheduleID:   schedID,
				ChannelID:    "C-PROACTIVE-1",
				Prompt:       "test prompt",
				ScheduleCron: &cron,
			},
		}

		if workErr := worker.Work(ctx, job); workErr != nil {
			t.Fatalf("Work() returned error: %v", workErr)
		}

		// Verify MarkRun updated next_run_at (should be in the future for cron)
		var nextRunAt time.Time
		var enabled bool
		scanErr = pool.QueryRow(ctx,
			`SELECT next_run_at, enabled FROM scheduled_messages WHERE id = $1`, schedID,
		).Scan(&nextRunAt, &enabled)
		if scanErr != nil {
			t.Fatalf("querying schedule: %v", scanErr)
		}
		if !nextRunAt.After(time.Now()) {
			t.Errorf("expected next_run_at in the future, got %v", nextRunAt)
		}
		if !enabled {
			t.Error("expected schedule to remain enabled for cron schedule")
		}

		mu.Lock()
		if !postCalled {
			t.Error("expected PostMessage to be called")
		}
		mu.Unlock()
	})

	t.Run("one-shot schedule disables after run", func(t *testing.T) {
		// Insert a one-shot scheduled message (no cron)
		var schedID string
		scanErr := pool.QueryRow(ctx,
			`INSERT INTO scheduled_messages (channel_id, prompt, schedule_cron, next_run_at, one_shot, enabled)
			 VALUES ($1, $2, NULL, $3, $4, $5)
			 RETURNING id`,
			"C-PROACTIVE-2", "one-shot prompt", time.Now().Add(-1*time.Minute), true, true,
		).Scan(&schedID)
		if scanErr != nil {
			t.Fatalf("inserting schedule: %v", scanErr)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM scheduled_messages WHERE id = $1", schedID)
		})

		claudeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]any{
				"content": []map[string]string{{"type": "text", "text": "One-shot reply"}},
				"usage":   map[string]int{"input_tokens": 10, "output_tokens": 5},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer claudeServer.Close()

		slackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer slackServer.Close()

		worker := &ProactiveMessageWorker{
			Pool:         pool,
			Claude:       llm.NewClient("test-key", claudeServer.URL),
			Slack:        slack.NewClient("test-token", slackServer.URL),
			SystemPrompt: "test prompt",
		}

		job := &river.Job[ProactiveMessageArgs]{
			Args: ProactiveMessageArgs{
				ScheduleID: schedID,
				ChannelID:  "C-PROACTIVE-2",
				Prompt:     "one-shot prompt",
				// ScheduleCron is nil for one-shot
			},
		}

		if workErr := worker.Work(ctx, job); workErr != nil {
			t.Fatalf("Work() returned error: %v", workErr)
		}

		// Verify one-shot was disabled
		var enabled bool
		scanErr = pool.QueryRow(ctx,
			`SELECT enabled FROM scheduled_messages WHERE id = $1`, schedID,
		).Scan(&enabled)
		if scanErr != nil {
			t.Fatalf("querying schedule: %v", scanErr)
		}
		if enabled {
			t.Error("expected one-shot schedule to be disabled after run")
		}
	})

	t.Run("Claude error is propagated", func(t *testing.T) {
		var schedID string
		scanErr := pool.QueryRow(ctx,
			`INSERT INTO scheduled_messages (channel_id, prompt, schedule_cron, next_run_at, one_shot, enabled)
			 VALUES ($1, $2, NULL, $3, $4, $5)
			 RETURNING id`,
			"C-PROACTIVE-3", "error prompt", time.Now().Add(-1*time.Minute), true, true,
		).Scan(&schedID)
		if scanErr != nil {
			t.Fatalf("inserting schedule: %v", scanErr)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM scheduled_messages WHERE id = $1", schedID)
		})

		claudeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"server error"}}`))
		}))
		defer claudeServer.Close()

		slackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("PostMessage should not be called when Claude fails")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer slackServer.Close()

		worker := &ProactiveMessageWorker{
			Pool:         pool,
			Claude:       llm.NewClient("test-key", claudeServer.URL),
			Slack:        slack.NewClient("test-token", slackServer.URL),
			SystemPrompt: "test prompt",
		}

		job := &river.Job[ProactiveMessageArgs]{
			Args: ProactiveMessageArgs{
				ScheduleID: schedID,
				ChannelID:  "C-PROACTIVE-3",
				Prompt:     "error prompt",
			},
		}

		workErr := worker.Work(ctx, job)
		if workErr == nil {
			t.Fatal("expected error when Claude fails")
		}
	})

	t.Run("PostMessage error is propagated", func(t *testing.T) {
		var schedID string
		scanErr := pool.QueryRow(ctx,
			`INSERT INTO scheduled_messages (channel_id, prompt, schedule_cron, next_run_at, one_shot, enabled)
			 VALUES ($1, $2, NULL, $3, $4, $5)
			 RETURNING id`,
			"C-PROACTIVE-4", "slack error prompt", time.Now().Add(-1*time.Minute), true, true,
		).Scan(&schedID)
		if scanErr != nil {
			t.Fatalf("inserting schedule: %v", scanErr)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM scheduled_messages WHERE id = $1", schedID)
		})

		claudeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]any{
				"content": []map[string]string{{"type": "text", "text": "Reply text"}},
				"usage":   map[string]int{"input_tokens": 10, "output_tokens": 5},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer claudeServer.Close()

		slackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":false,"error":"channel_not_found"}`))
		}))
		defer slackServer.Close()

		worker := &ProactiveMessageWorker{
			Pool:         pool,
			Claude:       llm.NewClient("test-key", claudeServer.URL),
			Slack:        slack.NewClient("test-token", slackServer.URL),
			SystemPrompt: "test prompt",
		}

		job := &river.Job[ProactiveMessageArgs]{
			Args: ProactiveMessageArgs{
				ScheduleID: schedID,
				ChannelID:  "C-PROACTIVE-4",
				Prompt:     "slack error prompt",
			},
		}

		workErr := worker.Work(ctx, job)
		if workErr == nil {
			t.Fatal("expected error when Slack PostMessage fails")
		}
	})

	t.Run("markdown is converted to mrkdwn before posting", func(t *testing.T) {
		var schedID string
		scanErr := pool.QueryRow(ctx,
			`INSERT INTO scheduled_messages (channel_id, prompt, schedule_cron, next_run_at, one_shot, enabled)
			 VALUES ($1, $2, NULL, $3, $4, $5)
			 RETURNING id`,
			"C-PROACTIVE-5", "markdown prompt", time.Now().Add(-1*time.Minute), true, true,
		).Scan(&schedID)
		if scanErr != nil {
			t.Fatalf("inserting schedule: %v", scanErr)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM scheduled_messages WHERE id = $1", schedID)
		})

		claudeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Return markdown with bold syntax
			resp := map[string]any{
				"content": []map[string]string{{"type": "text", "text": "**bold text**"}},
				"usage":   map[string]int{"input_tokens": 10, "output_tokens": 5},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer claudeServer.Close()

		var mu sync.Mutex
		var postedText string

		slackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Text string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
				mu.Lock()
				postedText = req.Text
				mu.Unlock()
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer slackServer.Close()

		worker := &ProactiveMessageWorker{
			Pool:         pool,
			Claude:       llm.NewClient("test-key", claudeServer.URL),
			Slack:        slack.NewClient("test-token", slackServer.URL),
			SystemPrompt: "test prompt",
		}

		job := &river.Job[ProactiveMessageArgs]{
			Args: ProactiveMessageArgs{
				ScheduleID: schedID,
				ChannelID:  "C-PROACTIVE-5",
				Prompt:     "markdown prompt",
			},
		}

		if workErr := worker.Work(ctx, job); workErr != nil {
			t.Fatalf("Work() returned error: %v", workErr)
		}

		mu.Lock()
		defer mu.Unlock()
		// MarkdownToMrkdwn converts **bold** to *bold*
		if postedText != "*bold text*" {
			t.Errorf("expected mrkdwn conversion, got %q", postedText)
		}
	})
}
