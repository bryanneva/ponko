package saga_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bryanneva/ponko/internal/saga"
	"github.com/bryanneva/ponko/internal/testutil"
	"github.com/bryanneva/ponko/internal/workflow"
)

func cleanupOutbox(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, _ = pool.Exec(context.Background(), `DELETE FROM outbox`)
}

func createTestWorkflow(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	wfID, err := workflow.CreateWorkflow(context.Background(), pool, "echo")
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	return wfID
}

func TestEnqueue(t *testing.T) {
	pool := testutil.TestDB(t)
	cleanupOutbox(t, pool)
	t.Cleanup(func() { cleanupOutbox(t, pool) })

	wfID := createTestWorkflow(t, pool)

	outboxID, err := saga.Enqueue(context.Background(), pool, saga.OutboxEntry{
		WorkflowID:  wfID,
		ChannelID:   "C_OUTBOX_TEST",
		ThreadTS:    "2000.0001",
		MessageType: "text",
		Content:     json.RawMessage(`{"text":"hello"}`),
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if outboxID == "" {
		t.Error("expected non-empty outbox ID")
	}
}

func TestClaimPending(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()
	cleanupOutbox(t, pool)
	t.Cleanup(func() { cleanupOutbox(t, pool) })

	wfID := createTestWorkflow(t, pool)

	_, err := saga.Enqueue(ctx, pool, saga.OutboxEntry{
		WorkflowID:  wfID,
		ChannelID:   "C_CLAIM_TEST",
		ThreadTS:    "2000.0002",
		MessageType: "text",
		Content:     json.RawMessage(`{"text":"msg1"}`),
	})
	if err != nil {
		t.Fatalf("Enqueue 1: %v", err)
	}

	_, err = saga.Enqueue(ctx, pool, saga.OutboxEntry{
		WorkflowID:  wfID,
		ChannelID:   "C_CLAIM_TEST",
		ThreadTS:    "2000.0002",
		MessageType: "text",
		Content:     json.RawMessage(`{"text":"msg2"}`),
	})
	if err != nil {
		t.Fatalf("Enqueue 2: %v", err)
	}

	entries, err := saga.ClaimPending(ctx, pool, 10)
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 claimed entries, got %d", len(entries))
	}

	// Claimed entries should have status "delivering"
	if entries[0].Status != "delivering" {
		t.Errorf("entry status = %q, want delivering", entries[0].Status)
	}
	if entries[0].Attempts != 1 {
		t.Errorf("entry attempts = %d, want 1", entries[0].Attempts)
	}

	// Second claim should return nothing (already claimed)
	entries2, err := saga.ClaimPending(ctx, pool, 10)
	if err != nil {
		t.Fatalf("ClaimPending 2: %v", err)
	}
	if len(entries2) != 0 {
		t.Errorf("expected 0 entries on second claim, got %d", len(entries2))
	}
}

func TestMarkDelivered(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()
	cleanupOutbox(t, pool)
	t.Cleanup(func() { cleanupOutbox(t, pool) })

	wfID := createTestWorkflow(t, pool)

	outboxID, err := saga.Enqueue(ctx, pool, saga.OutboxEntry{
		WorkflowID:  wfID,
		ChannelID:   "C_DELIVER_TEST",
		MessageType: "text",
		Content:     json.RawMessage(`{"text":"done"}`),
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	err = saga.MarkDelivered(ctx, pool, outboxID)
	if err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}

	// Should not be claimable after delivery
	entries, err := saga.ClaimPending(ctx, pool, 10)
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 pending entries after delivery, got %d", len(entries))
	}
}

func TestMarkFailed(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()
	cleanupOutbox(t, pool)
	t.Cleanup(func() { cleanupOutbox(t, pool) })

	wfID := createTestWorkflow(t, pool)

	outboxID, err := saga.Enqueue(ctx, pool, saga.OutboxEntry{
		WorkflowID:  wfID,
		ChannelID:   "C_FAIL_TEST",
		MessageType: "text",
		Content:     json.RawMessage(`{"text":"fail"}`),
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	err = saga.MarkFailed(ctx, pool, outboxID)
	if err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	// Should not be claimable after failure
	entries, err := saga.ClaimPending(ctx, pool, 10)
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 pending entries after failure, got %d", len(entries))
	}
}

func TestResetForRetry(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()
	cleanupOutbox(t, pool)
	t.Cleanup(func() { cleanupOutbox(t, pool) })

	wfID := createTestWorkflow(t, pool)

	_, err := saga.Enqueue(ctx, pool, saga.OutboxEntry{
		WorkflowID:  wfID,
		ChannelID:   "C_RETRY_TEST",
		MessageType: "text",
		Content:     json.RawMessage(`{"text":"retry"}`),
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Claim it
	entries, err := saga.ClaimPending(ctx, pool, 10)
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	// Reset for retry
	err = saga.ResetForRetry(ctx, pool, entries[0].OutboxID)
	if err != nil {
		t.Fatalf("ResetForRetry: %v", err)
	}

	// Should be claimable again
	entries2, err := saga.ClaimPending(ctx, pool, 10)
	if err != nil {
		t.Fatalf("ClaimPending 2: %v", err)
	}
	if len(entries2) != 1 {
		t.Errorf("expected 1 entry after retry reset, got %d", len(entries2))
	}
	if entries2[0].Attempts != 2 {
		t.Errorf("attempts = %d, want 2", entries2[0].Attempts)
	}
}

func TestClaimPending_Empty(t *testing.T) {
	pool := testutil.TestDB(t)
	cleanupOutbox(t, pool)
	t.Cleanup(func() { cleanupOutbox(t, pool) })

	entries, err := saga.ClaimPending(context.Background(), pool, 10)
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries from empty outbox, got %d", len(entries))
	}
}
