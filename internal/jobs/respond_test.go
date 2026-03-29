package jobs

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertest"

	"github.com/bryanneva/ponko/internal/saga"
	"github.com/bryanneva/ponko/internal/testutil"
	"github.com/bryanneva/ponko/internal/workflow"
)

func TestRespondWorker_Work(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()
	client := newTestRiverClient(t, pool)

	t.Run("fetches process output and completes workflow", func(t *testing.T) {
		wfID, createErr := workflow.CreateWorkflow(ctx, pool, "test")
		if createErr != nil {
			t.Fatalf("creating workflow: %v", createErr)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM workflow_outputs WHERE workflow_id = $1", wfID)
			_, _ = pool.Exec(ctx, "DELETE FROM workflow_steps WHERE workflow_id = $1", wfID)
			_, _ = pool.Exec(ctx, "DELETE FROM workflows WHERE workflow_id = $1", wfID)
		})

		processOutput, _ := json.Marshal(map[string]string{
			"response":     "Hello back!",
			"channel":      "C456",
			"thread_ts":    "ts456",
			"event_ts":     "ev456",
			"channel_type": "channel",
		})
		if saveErr := workflow.SaveOutput(ctx, pool, wfID, "process", processOutput); saveErr != nil {
			t.Fatalf("saving process output: %v", saveErr)
		}

		worker := &RespondWorker{Pool: pool}
		job := &river.Job[RespondArgs]{
			Args: RespondArgs{WorkflowID: wfID},
		}

		workCtx := rivertest.WorkContext(ctx, client)
		if workErr := worker.Work(workCtx, job); workErr != nil {
			t.Fatalf("Work() returned error: %v", workErr)
		}

		wf, err := workflow.GetWorkflow(ctx, pool, wfID)
		if err != nil {
			t.Fatalf("getting workflow: %v", err)
		}

		// Verify respond step created and completed
		if len(wf.Steps) != 1 {
			t.Fatalf("expected 1 step, got %d", len(wf.Steps))
		}
		if wf.Steps[0].StepName != "respond" {
			t.Errorf("step name = %q, want %q", wf.Steps[0].StepName, "respond")
		}
		if wf.Steps[0].Status != "complete" {
			t.Errorf("step status = %q, want %q", wf.Steps[0].Status, "complete")
		}

		// Verify respond output saved (pass-through of process output)
		var respondOutput map[string]string
		for _, o := range wf.Outputs {
			if o.StepName == "respond" {
				if err := json.Unmarshal(o.Data, &respondOutput); err != nil {
					t.Fatalf("unmarshaling respond output: %v", err)
				}
			}
		}
		if respondOutput == nil {
			t.Fatal("respond output not found")
		}
		if respondOutput["response"] != "Hello back!" {
			t.Errorf("respond output response = %q, want %q", respondOutput["response"], "Hello back!")
		}
		if respondOutput["channel"] != "C456" {
			t.Errorf("respond output channel = %q, want %q", respondOutput["channel"], "C456")
		}

		// Verify workflow status set to completed
		if wf.Status != "completed" {
			t.Errorf("workflow status = %q, want %q", wf.Status, "completed")
		}

		// Verify slack_reply job was enqueued
		var jobCount int
		scanErr := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM river_job WHERE kind = 'slack_reply' AND args::text LIKE $1`,
			"%"+wfID+"%",
		).Scan(&jobCount)
		if scanErr != nil {
			t.Fatalf("querying river_job: %v", scanErr)
		}
		if jobCount != 1 {
			t.Errorf("expected 1 slack_reply job enqueued, got %d", jobCount)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM river_job WHERE kind = 'slack_reply' AND args::text LIKE $1", "%"+wfID+"%")
		})
	})

	t.Run("returns error when process output missing", func(t *testing.T) {
		wfID, err := workflow.CreateWorkflow(ctx, pool, "test")
		if err != nil {
			t.Fatalf("creating workflow: %v", err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM workflow_outputs WHERE workflow_id = $1", wfID)
			_, _ = pool.Exec(ctx, "DELETE FROM workflow_steps WHERE workflow_id = $1", wfID)
			_, _ = pool.Exec(ctx, "DELETE FROM workflows WHERE workflow_id = $1", wfID)
		})

		worker := &RespondWorker{Pool: pool}
		job := &river.Job[RespondArgs]{
			Args: RespondArgs{WorkflowID: wfID},
		}

		workCtx := rivertest.WorkContext(ctx, client)
		err = worker.Work(workCtx, job)
		if err == nil {
			t.Fatal("expected error when process output is missing")
		}
	})

	t.Run("with conversation_id routes through outbox", func(t *testing.T) {
		wfID, createErr := workflow.CreateWorkflow(ctx, pool, "test")
		if createErr != nil {
			t.Fatalf("creating workflow: %v", createErr)
		}

		// Create a conversation and link it to the workflow
		convID, _, convErr := saga.FindOrCreateConversation(ctx, pool, "C-outbox", "ts-outbox")
		if convErr != nil {
			t.Fatalf("creating conversation: %v", convErr)
		}
		if setErr := workflow.SetConversationID(ctx, pool, wfID, convID); setErr != nil {
			t.Fatalf("setting conversation_id: %v", setErr)
		}
		if turnErr := saga.AddTurn(ctx, pool, convID, wfID, "mention"); turnErr != nil {
			t.Fatalf("adding turn: %v", turnErr)
		}

		processOutput, _ := json.Marshal(map[string]string{
			"response":     "Outbox response",
			"channel":      "C-outbox",
			"thread_ts":    "ts-outbox",
			"event_ts":     "ev-outbox",
			"channel_type": "channel",
		})
		if saveErr := workflow.SaveOutput(ctx, pool, wfID, "process", processOutput); saveErr != nil {
			t.Fatalf("saving process output: %v", saveErr)
		}

		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM outbox WHERE conversation_id = $1", convID)
			_, _ = pool.Exec(ctx, "DELETE FROM conversation_turns WHERE conversation_id = $1", convID)
			_, _ = pool.Exec(ctx, "DELETE FROM conversations WHERE conversation_id = $1", convID)
			_, _ = pool.Exec(ctx, "DELETE FROM workflow_outputs WHERE workflow_id = $1", wfID)
			_, _ = pool.Exec(ctx, "DELETE FROM workflow_steps WHERE workflow_id = $1", wfID)
			_, _ = pool.Exec(ctx, "DELETE FROM workflows WHERE workflow_id = $1", wfID)
		})

		worker := &RespondWorker{Pool: pool}
		job := &river.Job[RespondArgs]{
			Args: RespondArgs{WorkflowID: wfID},
		}

		workCtx := rivertest.WorkContext(ctx, client)
		if err := worker.Work(workCtx, job); err != nil {
			t.Fatalf("Work() returned error: %v", err)
		}

		// Verify outbox entry was created
		var outboxCount int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM outbox WHERE conversation_id = $1 AND workflow_id = $2`,
			convID, wfID,
		).Scan(&outboxCount); err != nil {
			t.Fatalf("querying outbox: %v", err)
		}
		if outboxCount != 1 {
			t.Errorf("expected 1 outbox entry, got %d", outboxCount)
		}

		// Verify outbox entry has correct fields
		var msgType, content, status string
		if err := pool.QueryRow(ctx,
			`SELECT message_type, content, status FROM outbox WHERE conversation_id = $1`,
			convID,
		).Scan(&msgType, &content, &status); err != nil {
			t.Fatalf("querying outbox entry: %v", err)
		}
		if msgType != "text" {
			t.Errorf("message_type = %q, want %q", msgType, "text")
		}
		if status != "pending" {
			t.Errorf("status = %q, want %q", status, "pending")
		}

		// Verify NO slack_reply job was enqueued
		var slackJobCount int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM river_job WHERE kind = 'slack_reply' AND args::text LIKE $1`,
			"%"+wfID+"%",
		).Scan(&slackJobCount); err != nil {
			t.Fatalf("querying river_job: %v", err)
		}
		if slackJobCount != 0 {
			t.Errorf("expected 0 slack_reply jobs, got %d", slackJobCount)
		}

		// Verify conversation transitioned to completed
		conv, err := saga.GetConversation(ctx, pool, convID)
		if err != nil {
			t.Fatalf("getting conversation: %v", err)
		}
		if conv.Status != "completed" {
			t.Errorf("conversation status = %q, want %q", conv.Status, "completed")
		}

		// Verify workflow status still completed
		wf, err := workflow.GetWorkflow(ctx, pool, wfID)
		if err != nil {
			t.Fatalf("getting workflow: %v", err)
		}
		if wf.Status != "completed" {
			t.Errorf("workflow status = %q, want %q", wf.Status, "completed")
		}
	})

	t.Run("without conversation_id uses legacy slack_reply path", func(t *testing.T) {
		// This test confirms backward compatibility — same behavior as the
		// first test, but explicitly framed as the "no conversation_id" case
		wfID, err := workflow.CreateWorkflow(ctx, pool, "test")
		if err != nil {
			t.Fatalf("creating workflow: %v", err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM river_job WHERE kind = 'slack_reply' AND args::text LIKE $1", "%"+wfID+"%")
			_, _ = pool.Exec(ctx, "DELETE FROM workflow_outputs WHERE workflow_id = $1", wfID)
			_, _ = pool.Exec(ctx, "DELETE FROM workflow_steps WHERE workflow_id = $1", wfID)
			_, _ = pool.Exec(ctx, "DELETE FROM workflows WHERE workflow_id = $1", wfID)
		})

		processOutput, _ := json.Marshal(map[string]string{
			"response":     "Legacy response",
			"channel":      "C-legacy",
			"thread_ts":    "ts-legacy",
			"event_ts":     "ev-legacy",
			"channel_type": "channel",
		})
		if err := workflow.SaveOutput(ctx, pool, wfID, "process", processOutput); err != nil {
			t.Fatalf("saving process output: %v", err)
		}

		worker := &RespondWorker{Pool: pool}
		job := &river.Job[RespondArgs]{
			Args: RespondArgs{WorkflowID: wfID},
		}

		workCtx := rivertest.WorkContext(ctx, client)
		if err := worker.Work(workCtx, job); err != nil {
			t.Fatalf("Work() returned error: %v", err)
		}

		// Verify slack_reply job WAS enqueued (legacy path)
		var slackJobCount int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM river_job WHERE kind = 'slack_reply' AND args::text LIKE $1`,
			"%"+wfID+"%",
		).Scan(&slackJobCount); err != nil {
			t.Fatalf("querying river_job: %v", err)
		}
		if slackJobCount != 1 {
			t.Errorf("expected 1 slack_reply job, got %d", slackJobCount)
		}

		// Verify NO outbox entry was created
		var outboxCount int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM outbox WHERE workflow_id = $1`, wfID,
		).Scan(&outboxCount); err != nil {
			t.Fatalf("querying outbox: %v", err)
		}
		if outboxCount != 0 {
			t.Errorf("expected 0 outbox entries, got %d", outboxCount)
		}
	})

}
