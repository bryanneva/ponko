package saga_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bryanneva/ponko/internal/saga"
	"github.com/bryanneva/ponko/internal/testutil"
	"github.com/bryanneva/ponko/internal/workflow"
)

func cleanupConversation(t *testing.T, pool *pgxpool.Pool, channelID, threadTS string) {
	t.Helper()
	ctx := context.Background()
	// Delete turns first (FK), then conversations
	_, _ = pool.Exec(ctx,
		`DELETE FROM conversation_turns WHERE conversation_id IN (SELECT conversation_id FROM conversations WHERE channel_id = $1 AND thread_ts = $2)`,
		channelID, threadTS)
	_, _ = pool.Exec(ctx,
		`DELETE FROM conversations WHERE channel_id = $1 AND thread_ts = $2`,
		channelID, threadTS)
}

func TestFindOrCreateConversation_CreatesNew(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	cleanupConversation(t, pool, "C_TEST_NEW", "1000.0001")
	t.Cleanup(func() { cleanupConversation(t, pool, "C_TEST_NEW", "1000.0001") })

	convID, isNew, err := saga.FindOrCreateConversation(ctx, pool, "C_TEST_NEW", "1000.0001")
	if err != nil {
		t.Fatalf("FindOrCreateConversation: %v", err)
	}
	if !isNew {
		t.Error("expected isNew=true for first call")
	}
	if convID == "" {
		t.Error("expected non-empty conversation ID")
	}
}

func TestFindOrCreateConversation_ReturnsExisting(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	cleanupConversation(t, pool, "C_TEST_EXIST", "1000.0002")
	t.Cleanup(func() { cleanupConversation(t, pool, "C_TEST_EXIST", "1000.0002") })

	convID1, _, err := saga.FindOrCreateConversation(ctx, pool, "C_TEST_EXIST", "1000.0002")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	convID2, isNew, err := saga.FindOrCreateConversation(ctx, pool, "C_TEST_EXIST", "1000.0002")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if isNew {
		t.Error("expected isNew=false for second call")
	}
	if convID1 != convID2 {
		t.Errorf("conversation IDs differ: %s vs %s", convID1, convID2)
	}
}

func TestAddTurn(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	cleanupConversation(t, pool, "C_TEST_TURN", "1000.0003")
	t.Cleanup(func() { cleanupConversation(t, pool, "C_TEST_TURN", "1000.0003") })

	convID, _, err := saga.FindOrCreateConversation(ctx, pool, "C_TEST_TURN", "1000.0003")
	if err != nil {
		t.Fatalf("FindOrCreateConversation: %v", err)
	}

	wfID, err := workflow.CreateWorkflow(ctx, pool, "echo")
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	err = saga.AddTurn(ctx, pool, convID, wfID, "message")
	if err != nil {
		t.Fatalf("AddTurn: %v", err)
	}

	wfID2, err := workflow.CreateWorkflow(ctx, pool, "echo")
	if err != nil {
		t.Fatalf("CreateWorkflow 2: %v", err)
	}

	err = saga.AddTurn(ctx, pool, convID, wfID2, "approval")
	if err != nil {
		t.Fatalf("AddTurn 2: %v", err)
	}

	// Verify turns via GetConversation
	conv, err := saga.GetConversation(ctx, pool, convID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if len(conv.Turns) != 2 {
		t.Fatalf("len(Turns) = %d, want 2", len(conv.Turns))
	}
	if conv.Turns[0].TurnIndex != 0 {
		t.Errorf("first turn index = %d, want 0", conv.Turns[0].TurnIndex)
	}
	if conv.Turns[1].TurnIndex != 1 {
		t.Errorf("second turn index = %d, want 1", conv.Turns[1].TurnIndex)
	}
}

func TestTransitionStatus(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	cleanupConversation(t, pool, "C_TEST_TRANS", "1000.0004")
	t.Cleanup(func() { cleanupConversation(t, pool, "C_TEST_TRANS", "1000.0004") })

	convID, _, err := saga.FindOrCreateConversation(ctx, pool, "C_TEST_TRANS", "1000.0004")
	if err != nil {
		t.Fatalf("FindOrCreateConversation: %v", err)
	}

	err = saga.TransitionStatus(ctx, pool, convID, "active", "awaiting_approval")
	if err != nil {
		t.Fatalf("TransitionStatus active->awaiting_approval: %v", err)
	}

	// Transition from wrong state should fail
	err = saga.TransitionStatus(ctx, pool, convID, "active", "completed")
	if err == nil {
		t.Error("expected error for invalid transition from wrong state")
	}

	err = saga.TransitionStatus(ctx, pool, convID, "awaiting_approval", "active")
	if err != nil {
		t.Fatalf("TransitionStatus awaiting_approval->active: %v", err)
	}
}

func TestGetConversation(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	cleanupConversation(t, pool, "C_TEST_GET", "1000.0005")
	t.Cleanup(func() { cleanupConversation(t, pool, "C_TEST_GET", "1000.0005") })

	convID, _, err := saga.FindOrCreateConversation(ctx, pool, "C_TEST_GET", "1000.0005")
	if err != nil {
		t.Fatalf("FindOrCreateConversation: %v", err)
	}

	wfID, err := workflow.CreateWorkflow(ctx, pool, "echo")
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	err = saga.AddTurn(ctx, pool, convID, wfID, "message")
	if err != nil {
		t.Fatalf("AddTurn: %v", err)
	}

	conv, err := saga.GetConversation(ctx, pool, convID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}

	if conv.ConversationID != convID {
		t.Errorf("ConversationID = %q, want %q", conv.ConversationID, convID)
	}
	if conv.ChannelID != "C_TEST_GET" {
		t.Errorf("ChannelID = %q, want C_TEST_GET", conv.ChannelID)
	}
	if conv.ThreadTS != "1000.0005" {
		t.Errorf("ThreadTS = %q, want 1000.0005", conv.ThreadTS)
	}
	if conv.Status != "active" {
		t.Errorf("Status = %q, want active", conv.Status)
	}
	if len(conv.Turns) != 1 {
		t.Fatalf("len(Turns) = %d, want 1", len(conv.Turns))
	}
	if conv.Turns[0].WorkflowID != wfID {
		t.Errorf("Turn WorkflowID = %q, want %q", conv.Turns[0].WorkflowID, wfID)
	}
	if conv.Turns[0].TriggerType != "message" {
		t.Errorf("Turn TriggerType = %q, want message", conv.Turns[0].TriggerType)
	}
	if conv.Turns[0].TurnIndex != 0 {
		t.Errorf("Turn TurnIndex = %d, want 0", conv.Turns[0].TurnIndex)
	}
}

func TestGetConversation_NotFound(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	_, err := saga.GetConversation(ctx, pool, "00000000-0000-0000-0000-000000000000")
	if err == nil {
		t.Error("expected error for non-existent conversation")
	}
}
