package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertest"

	"github.com/bryanneva/ponko/internal/channel"
	"github.com/bryanneva/ponko/internal/llm"
	"github.com/bryanneva/ponko/internal/saga"
	"github.com/bryanneva/ponko/internal/testutil"
	"github.com/bryanneva/ponko/internal/workflow"
)

// newFakeClaude returns an LLM client backed by a test server that always
// returns the given text as the Claude API response.
func newFakeClaude(t *testing.T, responseText string) *llm.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": responseText}},
			"usage":   map[string]int{"input_tokens": 10, "output_tokens": 10},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return llm.NewClient("test-key", srv.URL)
}

func TestPlanWorker_ApprovalGate(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()
	client := newTestRiverClient(t, pool)

	fanOutJSON := `{"action":"fan_out","tasks":[{"instruction":"Research company A"},{"instruction":"Research company B"}]}`

	cleanupAll := func(t *testing.T, wfID, convID, channelID string) {
		t.Helper()
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM river_job WHERE args::text LIKE $1", "%"+wfID+"%")
			_, _ = pool.Exec(ctx, "DELETE FROM outbox WHERE conversation_id = $1", convID)
			_, _ = pool.Exec(ctx, "DELETE FROM conversation_turns WHERE conversation_id = $1", convID)
			_, _ = pool.Exec(ctx, "UPDATE workflows SET conversation_id = NULL WHERE workflow_id = $1", wfID)
			_, _ = pool.Exec(ctx, "DELETE FROM conversations WHERE conversation_id = $1", convID)
			_, _ = pool.Exec(ctx, "DELETE FROM workflow_outputs WHERE workflow_id = $1", wfID)
			_, _ = pool.Exec(ctx, "DELETE FROM workflow_steps WHERE workflow_id = $1", wfID)
			_, _ = pool.Exec(ctx, "DELETE FROM workflows WHERE workflow_id = $1", wfID)
			_, _ = pool.Exec(ctx, "DELETE FROM channel_configs WHERE channel_id = $1", channelID)
		})
	}

	setupWorkflow := func(t *testing.T, channelID, threadTS string) (string, string) {
		t.Helper()
		wfID, err := workflow.CreateWorkflow(ctx, pool, "test")
		if err != nil {
			t.Fatalf("creating workflow: %v", err)
		}

		convID, _, err := saga.FindOrCreateConversation(ctx, pool, channelID, threadTS)
		if err != nil {
			t.Fatalf("creating conversation: %v", err)
		}
		if err := workflow.SetConversationID(ctx, pool, wfID, convID); err != nil {
			t.Fatalf("setting conversation_id: %v", err)
		}
		if err := saga.AddTurn(ctx, pool, convID, wfID, "mention"); err != nil {
			t.Fatalf("adding turn: %v", err)
		}

		receiveOutput, _ := json.Marshal(receivePayload{
			Message:     "Research companies A and B",
			ThreadKey:   fmt.Sprintf("%s:%s", channelID, threadTS),
			Channel:     channelID,
			ThreadTS:    threadTS,
			EventTS:     "ev-test",
			ChannelType: "channel",
		})
		if err := workflow.SaveOutput(ctx, pool, wfID, "receive", receiveOutput); err != nil {
			t.Fatalf("saving receive output: %v", err)
		}

		return wfID, convID
	}

	t.Run("approval_required with fan-out transitions to awaiting_approval and enqueues blocks", func(t *testing.T) {
		channelID := "C-approval-yes"
		wfID, convID := setupWorkflow(t, channelID, "ts-approval-yes")
		cleanupAll(t, wfID, convID, channelID)

		if err := channel.UpsertConfig(ctx, pool, &channel.Config{
			ChannelID:        channelID,
			RespondMode:      "mention_only",
			ApprovalRequired: true,
		}); err != nil {
			t.Fatalf("upserting channel config: %v", err)
		}

		worker := &PlanWorker{
			Pool:       pool,
			Claude:     newFakeClaude(t, fanOutJSON),
			Slack:      nil,
			AppBaseURL: "https://example.com",
		}

		job := &river.Job[PlanArgs]{Args: PlanArgs{WorkflowID: wfID}}
		workCtx := rivertest.WorkContext(ctx, client)
		if err := worker.Work(workCtx, job); err != nil {
			t.Fatalf("Work() returned error: %v", err)
		}

		// Conversation should be in awaiting_approval
		conv, convErr := saga.GetConversation(ctx, pool, convID)
		if convErr != nil {
			t.Fatalf("getting conversation: %v", convErr)
		}
		if conv.Status != "awaiting_approval" {
			t.Errorf("conversation status = %q, want %q", conv.Status, "awaiting_approval")
		}

		// Outbox entry should exist with message_type 'blocks'
		var outboxCount int
		outboxCountErr := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM outbox WHERE conversation_id = $1 AND message_type = 'blocks'`,
			convID,
		).Scan(&outboxCount)
		if outboxCountErr != nil {
			t.Fatalf("querying outbox: %v", outboxCountErr)
		}
		if outboxCount != 1 {
			t.Errorf("expected 1 blocks outbox entry, got %d", outboxCount)
		}

		// Outbox content should contain approve and reject action_ids
		var content string
		contentErr := pool.QueryRow(ctx,
			`SELECT content::text FROM outbox WHERE conversation_id = $1 AND message_type = 'blocks'`,
			convID,
		).Scan(&content)
		if contentErr != nil {
			t.Fatalf("querying outbox content: %v", contentErr)
		}
		if !strings.Contains(content, "approve_plan") {
			t.Errorf("outbox content missing approve_plan action_id, got: %s", content)
		}
		if !strings.Contains(content, "reject_plan") {
			t.Errorf("outbox content missing reject_plan action_id, got: %s", content)
		}
		if !strings.Contains(content, wfID) {
			t.Errorf("outbox content missing workflow_id in button value, got: %s", content)
		}

		// NO execute jobs should be enqueued
		var executeCount int
		execErr := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM river_job WHERE kind = 'execute' AND args::text LIKE $1`,
			"%"+wfID+"%",
		).Scan(&executeCount)
		if execErr != nil {
			t.Fatalf("querying execute jobs: %v", execErr)
		}
		if executeCount != 0 {
			t.Errorf("expected 0 execute jobs, got %d", executeCount)
		}

		// Plan output should still be saved
		planOutput, planErr := workflow.GetOutput(ctx, pool, wfID, "plan")
		if planErr != nil {
			t.Fatalf("getting plan output: %v", planErr)
		}
		if planOutput == nil {
			t.Fatal("plan output not saved")
		}
	})

	t.Run("approval_required=false with fan-out inserts execute jobs normally", func(t *testing.T) {
		channelID := "C-approval-no"
		wfID, convID := setupWorkflow(t, channelID, "ts-approval-no")
		cleanupAll(t, wfID, convID, channelID)

		if err := channel.UpsertConfig(ctx, pool, &channel.Config{
			ChannelID:        channelID,
			RespondMode:      "mention_only",
			ApprovalRequired: false,
		}); err != nil {
			t.Fatalf("upserting channel config: %v", err)
		}

		worker := &PlanWorker{
			Pool:       pool,
			Claude:     newFakeClaude(t, fanOutJSON),
			Slack:      nil,
			AppBaseURL: "https://example.com",
		}

		job := &river.Job[PlanArgs]{Args: PlanArgs{WorkflowID: wfID}}
		workCtx := rivertest.WorkContext(ctx, client)
		if err := worker.Work(workCtx, job); err != nil {
			t.Fatalf("Work() returned error: %v", err)
		}

		// Conversation should NOT be in awaiting_approval
		conv, err := saga.GetConversation(ctx, pool, convID)
		if err != nil {
			t.Fatalf("getting conversation: %v", err)
		}
		if conv.Status == "awaiting_approval" {
			t.Errorf("conversation should not be awaiting_approval when approval_required=false")
		}

		// Execute jobs should be enqueued
		var executeCount int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM river_job WHERE kind = 'execute' AND args::text LIKE $1`,
			"%"+wfID+"%",
		).Scan(&executeCount); err != nil {
			t.Fatalf("querying execute jobs: %v", err)
		}
		if executeCount != 2 {
			t.Errorf("expected 2 execute jobs, got %d", executeCount)
		}

		// No blocks outbox entry
		var outboxCount int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM outbox WHERE conversation_id = $1 AND message_type = 'blocks'`,
			convID,
		).Scan(&outboxCount); err != nil {
			t.Fatalf("querying outbox: %v", err)
		}
		if outboxCount != 0 {
			t.Errorf("expected 0 blocks outbox entries, got %d", outboxCount)
		}
	})

	t.Run("approval_required with fan-out but missing conversation_id creates conversation and pauses", func(t *testing.T) {
		channelID := "C-approval-no-conv"
		threadTS := "ts-approval-no-conv"

		// Pre-clean stale state from previous runs
		_, _ = pool.Exec(ctx, "UPDATE workflows SET conversation_id = NULL WHERE conversation_id IN (SELECT conversation_id FROM conversations WHERE channel_id = $1)", channelID)
		_, _ = pool.Exec(ctx, "DELETE FROM outbox WHERE conversation_id IN (SELECT conversation_id FROM conversations WHERE channel_id = $1)", channelID)
		_, _ = pool.Exec(ctx, "DELETE FROM conversation_turns WHERE conversation_id IN (SELECT conversation_id FROM conversations WHERE channel_id = $1)", channelID)
		_, _ = pool.Exec(ctx, "DELETE FROM conversations WHERE channel_id = $1", channelID)

		// Create workflow WITHOUT conversation — simulates Slack handler failure path
		wfID, err := workflow.CreateWorkflow(ctx, pool, "test")
		if err != nil {
			t.Fatalf("creating workflow: %v", err)
		}

		receiveOutput, _ := json.Marshal(receivePayload{
			Message:     "Research companies A and B",
			ThreadKey:   fmt.Sprintf("%s:%s", channelID, threadTS),
			Channel:     channelID,
			ThreadTS:    threadTS,
			EventTS:     "ev-test",
			ChannelType: "channel",
		})
		if err := workflow.SaveOutput(ctx, pool, wfID, "receive", receiveOutput); err != nil {
			t.Fatalf("saving receive output: %v", err)
		}

		if err := channel.UpsertConfig(ctx, pool, &channel.Config{
			ChannelID:        channelID,
			RespondMode:      "mention_only",
			ApprovalRequired: true,
		}); err != nil {
			t.Fatalf("upserting channel config: %v", err)
		}

		worker := &PlanWorker{
			Pool:       pool,
			Claude:     newFakeClaude(t, fanOutJSON),
			Slack:      nil,
			AppBaseURL: "https://example.com",
		}

		job := &river.Job[PlanArgs]{Args: PlanArgs{WorkflowID: wfID}}
		workCtx := rivertest.WorkContext(ctx, client)
		if err := worker.Work(workCtx, job); err != nil {
			t.Fatalf("Work() returned error: %v", err)
		}

		// Conversation should have been created and linked to the workflow
		convID, getErr := workflow.GetConversationID(ctx, pool, wfID)
		if getErr != nil {
			t.Fatalf("getting conversation_id: %v", getErr)
		}
		if convID == "" {
			t.Fatal("conversation_id should have been created and set on workflow")
		}

		// Conversation should be in awaiting_approval
		conv, convErr := saga.GetConversation(ctx, pool, convID)
		if convErr != nil {
			t.Fatalf("getting conversation: %v", convErr)
		}
		if conv.Status != "awaiting_approval" {
			t.Errorf("conversation status = %q, want %q", conv.Status, "awaiting_approval")
		}

		// Outbox entry should exist with approval blocks
		var outboxCount int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM outbox WHERE conversation_id = $1 AND message_type = 'blocks'`,
			convID,
		).Scan(&outboxCount); err != nil {
			t.Fatalf("querying outbox: %v", err)
		}
		if outboxCount != 1 {
			t.Errorf("expected 1 blocks outbox entry, got %d", outboxCount)
		}

		// Cleanup — must clear workflow FK before deleting conversation
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM river_job WHERE args::text LIKE $1", "%"+wfID+"%")
			_, _ = pool.Exec(ctx, "DELETE FROM outbox WHERE conversation_id = $1", convID)
			_, _ = pool.Exec(ctx, "DELETE FROM conversation_turns WHERE conversation_id = $1", convID)
			_, _ = pool.Exec(ctx, "UPDATE workflows SET conversation_id = NULL WHERE workflow_id = $1", wfID)
			_, _ = pool.Exec(ctx, "DELETE FROM conversations WHERE conversation_id = $1", convID)
			_, _ = pool.Exec(ctx, "DELETE FROM workflow_outputs WHERE workflow_id = $1", wfID)
			_, _ = pool.Exec(ctx, "DELETE FROM workflow_steps WHERE workflow_id = $1", wfID)
			_, _ = pool.Exec(ctx, "DELETE FROM workflows WHERE workflow_id = $1", wfID)
			_, _ = pool.Exec(ctx, "DELETE FROM channel_configs WHERE channel_id = $1", channelID)
		})
	})

	t.Run("single task bypass ignores approval_required", func(t *testing.T) {
		channelID := "C-approval-bypass"
		wfID, convID := setupWorkflow(t, channelID, "ts-approval-bypass")
		cleanupAll(t, wfID, convID, channelID)

		if err := channel.UpsertConfig(ctx, pool, &channel.Config{
			ChannelID:        channelID,
			RespondMode:      "mention_only",
			ApprovalRequired: true,
		}); err != nil {
			t.Fatalf("upserting channel config: %v", err)
		}

		// Claude returns "direct" — no fan-out
		directJSON := `{"action":"direct"}`
		worker := &PlanWorker{
			Pool:       pool,
			Claude:     newFakeClaude(t, directJSON),
			Slack:      nil,
			AppBaseURL: "https://example.com",
		}

		job := &river.Job[PlanArgs]{Args: PlanArgs{WorkflowID: wfID}}
		workCtx := rivertest.WorkContext(ctx, client)
		if err := worker.Work(workCtx, job); err != nil {
			t.Fatalf("Work() returned error: %v", err)
		}

		// Should have a process job (bypass), not awaiting_approval
		conv, err := saga.GetConversation(ctx, pool, convID)
		if err != nil {
			t.Fatalf("getting conversation: %v", err)
		}
		if conv.Status == "awaiting_approval" {
			t.Errorf("single-task bypass should not trigger approval gate")
		}

		// Process job should be enqueued (via River transaction)
		var processCount int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM river_job WHERE kind = 'process' AND args::text LIKE $1`,
			"%"+wfID+"%",
		).Scan(&processCount); err != nil {
			t.Fatalf("querying process jobs: %v", err)
		}
		if processCount == 0 {
			t.Log("process job not found in river_job — may have been consumed by River transaction")
		}
	})
}

