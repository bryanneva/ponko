package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bryanneva/ponko/internal/testutil"
	"github.com/bryanneva/ponko/internal/workflow"
)

func setupExecuteRetryTest(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()

	wfID, err := workflow.CreateWorkflow(ctx, pool, "test")
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}

	payload := receivePayload{
		Message:   "test message",
		ThreadKey: "C123:1234567.890",
		Channel:   "C123",
	}
	payloadData, _ := json.Marshal(payload)
	if err := workflow.SaveOutput(ctx, pool, wfID, "receive", payloadData); err != nil {
		t.Fatalf("save receive output: %v", err)
	}

	if err := workflow.SetTotalTasks(ctx, pool, wfID, 1); err != nil {
		t.Fatalf("set total tasks: %v", err)
	}

	return wfID
}

func TestExecuteWorker_FinalAttemptSavesFailureOutput(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	wfID := setupExecuteRetryTest(t, pool)

	worker := &ExecuteWorker{Pool: pool}

	_, _, err := worker.handleExecuteFailure(ctx, wfID, 0, "execute-0", "do something", fmt.Errorf("LLM API error"))
	if err != nil {
		t.Fatalf("handleExecuteFailure: %v", err)
	}

	outputs, err := workflow.GetOutputsByPrefix(ctx, pool, wfID, "execute-")
	if err != nil {
		t.Fatalf("get outputs: %v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("expected 1 output, got %d", len(outputs))
	}

	var result struct {
		Instruction string `json:"instruction"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(outputs[0].Data, &result); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if result.Instruction != "do something" {
		t.Errorf("expected instruction 'do something', got %q", result.Instruction)
	}
	if result.Error != "LLM API error" {
		t.Errorf("expected error 'LLM API error', got %q", result.Error)
	}
}

func TestExecuteWorker_FinalAttemptIncrementsCompletedTasks(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	wfID := setupExecuteRetryTest(t, pool)

	worker := &ExecuteWorker{Pool: pool}

	completed, total, err := worker.handleExecuteFailure(ctx, wfID, 0, "execute-0", "do something", fmt.Errorf("error"))
	if err != nil {
		t.Fatalf("handleExecuteFailure: %v", err)
	}
	if completed != 1 {
		t.Errorf("expected completed_tasks=1, got %d", completed)
	}
	if total != 1 {
		t.Errorf("expected total_tasks=1, got %d", total)
	}
}

func TestAugmentInstructionWithError(t *testing.T) {
	augmented := augmentInstructionWithError("do something", "tool not found: web_search")

	expected := "do something\n\nNote: A previous attempt at this task failed with error: tool not found: web_search\nPlease try a different approach if possible."
	if augmented != expected {
		t.Errorf("unexpected augmented instruction:\ngot:  %s\nwant: %s", augmented, expected)
	}
}

func TestAugmentInstructionWithError_EmptyError(t *testing.T) {
	augmented := augmentInstructionWithError("do something", "")
	if augmented != "do something" {
		t.Errorf("expected instruction unchanged when error is empty, got: %s", augmented)
	}
}
