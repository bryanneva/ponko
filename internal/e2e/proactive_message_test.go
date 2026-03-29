//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/riverqueue/river"

	"github.com/bryanneva/ponko/internal/jobs"
	"github.com/bryanneva/ponko/internal/llm"
	"github.com/bryanneva/ponko/internal/queue"
	"github.com/bryanneva/ponko/internal/schedule"
	"github.com/bryanneva/ponko/internal/slack"
	"github.com/bryanneva/ponko/internal/testutil"
)

func TestProactiveMessageScheduling(t *testing.T) {
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	if anthropicKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping e2e test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pool := testutil.TestDB(t)
	runMigrations(t, pool)
	cleanTestData(t, pool)

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM scheduled_messages WHERE channel_id LIKE 'C_E2E_PROACTIVE%'")
	})

	claudeClient := llm.NewClient(anthropicKey, "")

	slackMessages := make(chan string, 10)
	slackStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true}`)
		if r.URL.Path == "/chat.postMessage" {
			var body struct {
				Channel string `json:"channel"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				select {
				case slackMessages <- body.Channel:
				default:
				}
			}
		}
	}))
	defer slackStub.Close()
	slackClient := slack.NewClient("test-bot-token", slackStub.URL)

	workers := river.NewWorkers()
	river.AddWorker(workers, &jobs.SchedulerTickWorker{Pool: pool})
	river.AddWorker(workers, &jobs.ProactiveMessageWorker{
		Pool:         pool,
		Claude:       claudeClient,
		Slack:        slackClient,
		SystemPrompt: "You are a test bot. Reply with exactly one short sentence.",
	})

	riverClient, err := queue.New(ctx, pool, workers, nil)
	if err != nil {
		t.Fatalf("creating river client: %v", err)
	}

	if err := riverClient.Start(ctx); err != nil {
		t.Fatalf("starting river client: %v", err)
	}
	t.Cleanup(func() {
		if stopErr := riverClient.Stop(context.Background()); stopErr != nil {
			t.Logf("error stopping river client: %v", stopErr)
		}
	})

	// Insert a one-shot scheduled message due in the past
	oneShotMsg := schedule.Message{
		ChannelID: "C_E2E_PROACTIVE_ONESHOT",
		Prompt:    "Say hello",
		NextRunAt: time.Now().Add(-time.Hour),
		OneShot:   true,
	}
	if err := schedule.Create(ctx, pool, oneShotMsg); err != nil {
		t.Fatalf("creating one-shot message: %v", err)
	}

	var oneShotID string
	if err := pool.QueryRow(ctx, "SELECT id FROM scheduled_messages WHERE channel_id = 'C_E2E_PROACTIVE_ONESHOT'").Scan(&oneShotID); err != nil {
		t.Fatalf("getting one-shot id: %v", err)
	}

	// Insert a recurring scheduled message due in the past
	cronExpr := "0 9 * * 1-5"
	recurringMsg := schedule.Message{
		ChannelID:    "C_E2E_PROACTIVE_RECUR",
		Prompt:       "Say good morning",
		ScheduleCron: &cronExpr,
		NextRunAt:    time.Now().Add(-time.Hour),
	}
	if err := schedule.Create(ctx, pool, recurringMsg); err != nil {
		t.Fatalf("creating recurring message: %v", err)
	}

	var recurringID string
	if err := pool.QueryRow(ctx, "SELECT id FROM scheduled_messages WHERE channel_id = 'C_E2E_PROACTIVE_RECUR'").Scan(&recurringID); err != nil {
		t.Fatalf("getting recurring id: %v", err)
	}

	// Enqueue a scheduler tick job
	_, err = riverClient.Insert(ctx, jobs.SchedulerTickArgs{}, nil)
	if err != nil {
		t.Fatalf("inserting scheduler tick: %v", err)
	}

	// Wait for both Slack messages to arrive
	received := make(map[string]bool)
	deadline := time.After(60 * time.Second)
	for len(received) < 2 {
		select {
		case ch := <-slackMessages:
			received[ch] = true
			t.Logf("received Slack message for channel: %s", ch)
		case <-deadline:
			t.Fatalf("timed out waiting for Slack messages, received: %v", received)
		}
	}

	// Verify ProactiveMessageArgs jobs were enqueued with correct data
	rows, err := pool.Query(ctx,
		"SELECT args FROM river_job WHERE kind = 'proactive_message' AND args->>'schedule_id' IN ($1, $2)",
		oneShotID, recurringID,
	)
	if err != nil {
		t.Fatalf("querying river jobs: %v", err)
	}
	defer rows.Close()

	enqueuedArgs := make(map[string]jobs.ProactiveMessageArgs)
	for rows.Next() {
		var rawArgs json.RawMessage
		if err := rows.Scan(&rawArgs); err != nil {
			t.Fatalf("scanning river job: %v", err)
		}
		var args jobs.ProactiveMessageArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			t.Fatalf("unmarshaling args: %v", err)
		}
		enqueuedArgs[args.ScheduleID] = args
	}

	if a, ok := enqueuedArgs[oneShotID]; !ok {
		t.Error("one-shot ProactiveMessageArgs not found in river_job")
	} else {
		if a.ChannelID != "C_E2E_PROACTIVE_ONESHOT" {
			t.Errorf("one-shot channel_id = %q, want C_E2E_PROACTIVE_ONESHOT", a.ChannelID)
		}
		if a.Prompt != "Say hello" {
			t.Errorf("one-shot prompt = %q, want 'Say hello'", a.Prompt)
		}
	}

	if a, ok := enqueuedArgs[recurringID]; !ok {
		t.Error("recurring ProactiveMessageArgs not found in river_job")
	} else {
		if a.ChannelID != "C_E2E_PROACTIVE_RECUR" {
			t.Errorf("recurring channel_id = %q, want C_E2E_PROACTIVE_RECUR", a.ChannelID)
		}
		if a.Prompt != "Say good morning" {
			t.Errorf("recurring prompt = %q, want 'Say good morning'", a.Prompt)
		}
	}

	// Verify one-shot message has enabled = false
	var oneShotEnabled bool
	if err := pool.QueryRow(ctx, "SELECT enabled FROM scheduled_messages WHERE id = $1", oneShotID).Scan(&oneShotEnabled); err != nil {
		t.Fatalf("checking one-shot enabled: %v", err)
	}
	if oneShotEnabled {
		t.Error("one-shot message should be disabled after execution")
	}

	// Verify recurring message has next_run_at advanced to a future weekday at 9am
	var nextRunAt time.Time
	if err := pool.QueryRow(ctx, "SELECT next_run_at FROM scheduled_messages WHERE id = $1", recurringID).Scan(&nextRunAt); err != nil {
		t.Fatalf("checking recurring next_run_at: %v", err)
	}
	if !nextRunAt.After(time.Now()) {
		t.Errorf("recurring next_run_at should be in the future, got %v", nextRunAt)
	}
	if nextRunAt.UTC().Hour() != 9 || nextRunAt.UTC().Minute() != 0 {
		t.Errorf("recurring next_run_at should be at 09:00 UTC, got %v", nextRunAt.UTC())
	}
	weekday := nextRunAt.UTC().Weekday()
	if weekday == time.Saturday || weekday == time.Sunday {
		t.Errorf("recurring next_run_at should be a weekday, got %v", weekday)
	}
}
