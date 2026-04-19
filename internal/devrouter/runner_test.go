package devrouter_test

import (
	"context"
	"testing"

	"github.com/bryanneva/ponko/internal/approval"
	"github.com/bryanneva/ponko/internal/devrouter"
	"github.com/bryanneva/ponko/internal/devrouter/testutil"
)

func buildRunner(store devrouter.PipelineStore, rt *testutil.MockRuntime) *devrouter.PipelineRunner {
	bus := &testutil.DiscardBus{}
	budget := &testutil.AlwaysAffordBudget{}
	commenter := &testutil.NoOpCommenter{}
	ciChecker := &testutil.MockCIChecker{Status: "green"}
	approvalChecker := &testutil.MockApprovalChecker{Status: approval.Pending}
	cfg := devrouter.DefaultDevRouterConfig()
	return devrouter.NewPipelineRunner(store, rt, bus, budget, commenter, ciChecker, approvalChecker, cfg, nil)
}

func TestPipelineRunner_RunOnce_Pending_To_Classifying(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	rt := &testutil.MockRuntime{Output: `{"track":"fix","rationale":"simple"}`}
	runner := buildRunner(store, rt)

	p := &devrouter.Pipeline{
		ID:    "run-once-pending",
		Stage: devrouter.StagePending,
	}
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := runner.RunOnce(ctx, p.ID); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	got, _ := store.Get(ctx, p.ID)
	// IngressJob transitions pending→classifying→planning
	if got.Stage != devrouter.StagePlanning {
		t.Errorf("expected stage planning, got %s", got.Stage)
	}
}

func TestPipelineRunner_ProcessAll_SkipsTerminalPipelines(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	rt := &testutil.MockRuntime{Output: "done"}
	runner := buildRunner(store, rt)

	completed := &devrouter.Pipeline{ID: "completed", Stage: devrouter.StageCompleted}
	failed := &devrouter.Pipeline{ID: "failed", Stage: devrouter.StageFailed}
	if err := store.Create(ctx, completed); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Create(ctx, failed); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// ProcessAll should not error even with terminal pipelines
	if err := runner.ProcessAll(ctx); err != nil {
		t.Fatalf("ProcessAll: %v", err)
	}

	// Completed and failed stages should not change
	gotC, _ := store.Get(ctx, "completed")
	if gotC.Stage != devrouter.StageCompleted {
		t.Errorf("completed pipeline should not change, got %s", gotC.Stage)
	}
}

func TestPipelineRunner_DryRun_DoesNotAdvance(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	rt := &testutil.MockRuntime{Output: `{"track":"fix","rationale":"simple"}`}
	bus := &testutil.DiscardBus{}
	budget := &testutil.AlwaysAffordBudget{}
	commenter := &testutil.NoOpCommenter{}
	ciChecker := &testutil.MockCIChecker{Status: "green"}
	approvalChecker := &testutil.MockApprovalChecker{Status: approval.Pending}
	cfg := devrouter.DefaultDevRouterConfig()

	runner := devrouter.NewPipelineRunner(store, rt, bus, budget, commenter, ciChecker, approvalChecker, cfg, nil)
	runner.SetDryRun(true)

	p := &devrouter.Pipeline{ID: "dry-run", Stage: devrouter.StagePending}
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := runner.RunOnce(ctx, p.ID); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	got, _ := store.Get(ctx, p.ID)
	// Dry run should not change the stage
	if got.Stage != devrouter.StagePending {
		t.Errorf("dry run should not advance stage, got %s", got.Stage)
	}
}
