package workflow_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bryanneva/ponko/internal/testutil"
	"github.com/bryanneva/ponko/internal/workflow"
)

func TestWorkflowCRUD(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	// CreateWorkflow
	wfID, err := workflow.CreateWorkflow(ctx, pool, "echo")
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if wfID == "" {
		t.Fatal("expected non-empty workflow_id")
	}

	// GetWorkflow
	wf, err := workflow.GetWorkflow(ctx, pool, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.WorkflowType != "echo" {
		t.Errorf("expected workflow_type 'echo', got %q", wf.WorkflowType)
	}
	if wf.Status != "pending" {
		t.Errorf("expected status 'pending', got %q", wf.Status)
	}

	// CreateStep
	stepID, err := workflow.CreateStep(ctx, pool, wfID, "receive")
	if err != nil {
		t.Fatalf("CreateStep: %v", err)
	}
	if stepID == "" {
		t.Fatal("expected non-empty step_id")
	}

	// UpdateStepStatus
	err = workflow.UpdateStepStatus(ctx, pool, stepID, "complete")
	if err != nil {
		t.Fatalf("UpdateStepStatus: %v", err)
	}

	// SaveOutput
	outputData := json.RawMessage(`{"result":"echoed"}`)
	err = workflow.SaveOutput(ctx, pool, wfID, "receive", outputData)
	if err != nil {
		t.Fatalf("SaveOutput: %v", err)
	}

	// GetOutput
	data, err := workflow.GetOutput(ctx, pool, wfID, "receive")
	if err != nil {
		t.Fatalf("GetOutput: %v", err)
	}
	var got map[string]string
	unmarshalErr := json.Unmarshal(data, &got)
	if unmarshalErr != nil {
		t.Fatalf("unmarshal output data: %v", unmarshalErr)
	}
	if got["result"] != "echoed" {
		t.Errorf("expected result 'echoed', got %q", got["result"])
	}

	// UpdateWorkflowStatus
	err = workflow.UpdateWorkflowStatus(ctx, pool, wfID, "completed")
	if err != nil {
		t.Fatalf("UpdateWorkflowStatus: %v", err)
	}

	// Verify full workflow with details
	wf, err = workflow.GetWorkflow(ctx, pool, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow after updates: %v", err)
	}
	if wf.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", wf.Status)
	}
	if len(wf.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(wf.Steps))
	}
	if wf.Steps[0].Status != "complete" {
		t.Errorf("expected step status 'complete', got %q", wf.Steps[0].Status)
	}
	if wf.Steps[0].CompletedAt == nil {
		t.Error("expected completed_at to be set")
	}
	if len(wf.Outputs) != 1 {
		t.Fatalf("expected 1 output, got %d", len(wf.Outputs))
	}
}

func TestGetOutputsByPrefix(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	wfID, err := workflow.CreateWorkflow(ctx, pool, "test")
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	// Save multiple execute outputs
	for i, result := range []string{"result-0", "result-1", "result-2"} {
		stepName := "execute-" + string(rune('0'+i))
		data := json.RawMessage(`{"result":"` + result + `"}`)
		if saveErr := workflow.SaveOutput(ctx, pool, wfID, stepName, data); saveErr != nil {
			t.Fatalf("SaveOutput(%s): %v", stepName, saveErr)
		}
	}

	// Save a non-execute output that shouldn't be returned
	if saveErr := workflow.SaveOutput(ctx, pool, wfID, "plan", json.RawMessage(`{"tasks":[]}`)); saveErr != nil {
		t.Fatalf("SaveOutput(plan): %v", saveErr)
	}

	outputs, err := workflow.GetOutputsByPrefix(ctx, pool, wfID, "execute-")
	if err != nil {
		t.Fatalf("GetOutputsByPrefix: %v", err)
	}

	if len(outputs) != 3 {
		t.Fatalf("expected 3 outputs, got %d", len(outputs))
	}

	for i, o := range outputs {
		expected := "execute-" + string(rune('0'+i))
		if o.StepName != expected {
			t.Errorf("output %d: expected step_name %q, got %q", i, expected, o.StepName)
		}
	}
}

func TestSetTotalTasks(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	wfID, err := workflow.CreateWorkflow(ctx, pool, "test")
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	if setErr := workflow.SetTotalTasks(ctx, pool, wfID, 3); setErr != nil {
		t.Fatalf("SetTotalTasks: %v", setErr)
	}

	// Verify by incrementing and checking total
	completed, total, err := workflow.IncrementCompletedTasks(ctx, pool, wfID)
	if err != nil {
		t.Fatalf("IncrementCompletedTasks: %v", err)
	}
	if total != 3 {
		t.Errorf("expected total_tasks 3, got %d", total)
	}
	if completed != 1 {
		t.Errorf("expected completed_tasks 1, got %d", completed)
	}
}

func TestIncrementCompletedTasks(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	wfID, err := workflow.CreateWorkflow(ctx, pool, "test")
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	if err := workflow.SetTotalTasks(ctx, pool, wfID, 3); err != nil {
		t.Fatalf("SetTotalTasks: %v", err)
	}

	// Increment 3 times, verify counts
	for i := 1; i <= 3; i++ {
		completed, total, err := workflow.IncrementCompletedTasks(ctx, pool, wfID)
		if err != nil {
			t.Fatalf("IncrementCompletedTasks (iter %d): %v", i, err)
		}
		if completed != i {
			t.Errorf("iter %d: expected completed %d, got %d", i, i, completed)
		}
		if total != 3 {
			t.Errorf("iter %d: expected total 3, got %d", i, total)
		}
	}
}
