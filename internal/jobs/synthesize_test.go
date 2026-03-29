package jobs

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bryanneva/ponko/internal/testutil"
	"github.com/bryanneva/ponko/internal/workflow"
)

func setupSynthesizeTest(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()

	wfID, err := workflow.CreateWorkflow(ctx, pool, "test")
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}

	payload := receivePayload{
		Message:   "test question",
		ThreadKey: "C123:1234567.890",
		Channel:   "C123",
		EventTS:   "1234567.890",
	}
	payloadData, _ := json.Marshal(payload)
	if err := workflow.SaveOutput(ctx, pool, wfID, "receive", payloadData); err != nil {
		t.Fatalf("save receive output: %v", err)
	}

	return wfID
}

func TestBuildSynthesisInputs_AllSuccess(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	wfID := setupSynthesizeTest(t, pool)

	output1, _ := json.Marshal(map[string]string{"instruction": "task 1", "result": "result 1"})
	output2, _ := json.Marshal(map[string]string{"instruction": "task 2", "result": "result 2"})
	_ = workflow.SaveOutput(ctx, pool, wfID, "execute-0", output1)
	_ = workflow.SaveOutput(ctx, pool, wfID, "execute-1", output2)

	outputs, _ := workflow.GetOutputsByPrefix(ctx, pool, wfID, "execute-")
	prompt, userMsg := buildSynthesisInputs("test question", outputs)

	if strings.Contains(prompt, "failed") {
		t.Error("prompt should not mention failures when all tasks succeeded")
	}
	if !strings.Contains(userMsg, "result 1") || !strings.Contains(userMsg, "result 2") {
		t.Errorf("user message should contain results, got: %s", userMsg)
	}
	if strings.Contains(userMsg, "Failed") {
		t.Errorf("user message should not contain failures, got: %s", userMsg)
	}
}

func TestBuildSynthesisInputs_MixedSuccessAndFailure(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	wfID := setupSynthesizeTest(t, pool)

	output1, _ := json.Marshal(map[string]string{"instruction": "task 1", "result": "result 1"})
	output2, _ := json.Marshal(map[string]string{"instruction": "task 2", "error": "API timeout"})
	_ = workflow.SaveOutput(ctx, pool, wfID, "execute-0", output1)
	_ = workflow.SaveOutput(ctx, pool, wfID, "execute-1", output2)

	outputs, _ := workflow.GetOutputsByPrefix(ctx, pool, wfID, "execute-")
	prompt, userMsg := buildSynthesisInputs("test question", outputs)

	if !strings.Contains(prompt, "failed") {
		t.Error("prompt should mention failures when some tasks failed")
	}
	if !strings.Contains(userMsg, "result 1") {
		t.Errorf("user message should contain successful result, got: %s", userMsg)
	}
	if !strings.Contains(userMsg, "API timeout") {
		t.Errorf("user message should contain failure info, got: %s", userMsg)
	}
}

func TestBuildSynthesisInputs_AllFailed(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	wfID := setupSynthesizeTest(t, pool)

	output1, _ := json.Marshal(map[string]string{"instruction": "task 1", "error": "error 1"})
	output2, _ := json.Marshal(map[string]string{"instruction": "task 2", "error": "error 2"})
	_ = workflow.SaveOutput(ctx, pool, wfID, "execute-0", output1)
	_ = workflow.SaveOutput(ctx, pool, wfID, "execute-1", output2)

	outputs, _ := workflow.GetOutputsByPrefix(ctx, pool, wfID, "execute-")
	prompt, userMsg := buildSynthesisInputs("test question", outputs)

	if !strings.Contains(prompt, "failed") {
		t.Error("prompt should mention failures when all tasks failed")
	}
	if !strings.Contains(userMsg, "error 1") || !strings.Contains(userMsg, "error 2") {
		t.Errorf("user message should contain both errors, got: %s", userMsg)
	}
	if strings.Contains(userMsg, "Sub-task results:") {
		t.Errorf("user message should not have success section, got: %s", userMsg)
	}
}

func TestBuildSynthesisInputs_IntegrationWithPartialFailure(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	wfID := setupSynthesizeTest(t, pool)

	output1, _ := json.Marshal(map[string]string{"instruction": "task 1", "result": "result 1"})
	output2, _ := json.Marshal(map[string]string{"instruction": "task 2", "error": "API timeout"})
	_ = workflow.SaveOutput(ctx, pool, wfID, "execute-0", output1)
	_ = workflow.SaveOutput(ctx, pool, wfID, "execute-1", output2)

	outputs, _ := workflow.GetOutputsByPrefix(ctx, pool, wfID, "execute-")
	prompt, userMsg := buildSynthesisInputs("test question", outputs)

	if prompt == "" || userMsg == "" {
		t.Fatal("expected non-empty synthesis inputs")
	}
	if !strings.Contains(prompt, "failed") {
		t.Error("prompt should mention failures")
	}
	if !strings.Contains(userMsg, "API timeout") {
		t.Errorf("synthesis user message should include failure info, got: %s", userMsg)
	}
	if !strings.Contains(userMsg, "result 1") {
		t.Errorf("synthesis user message should include success result, got: %s", userMsg)
	}
}
