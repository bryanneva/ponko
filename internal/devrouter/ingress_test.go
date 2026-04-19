package devrouter_test

import (
	"context"
	"testing"

	"github.com/bryanneva/ponko/internal/devrouter"
	"github.com/bryanneva/ponko/internal/devrouter/testutil"
)

func TestIngressJob_ParsesTrackAndRationale(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	rt := &testutil.MockRuntime{
		Output: `{"track": "fix", "rationale": "simple one-line fix"}`,
	}
	bus := &testutil.DiscardBus{}
	cfg := devrouter.DefaultDevRouterConfig()

	p := &devrouter.Pipeline{
		ID:          "test-pipeline",
		IssueURL:    "https://github.com/owner/repo/issues/1",
		IssueNumber: 1,
		Stage:       devrouter.StagePending,
	}
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.UpdateStage(ctx, p.ID, devrouter.StagePending, devrouter.StageClassifying); err != nil {
		t.Fatalf("UpdateStage: %v", err)
	}
	p.Stage = devrouter.StageClassifying

	job := devrouter.NewIngressJob()
	if err := job.Run(ctx, p, store, rt, bus, cfg, nil); err != nil {
		t.Fatalf("IngressJob.Run: %v", err)
	}

	got, err := store.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Track != devrouter.TrackFix {
		t.Errorf("expected track fix, got %s", got.Track)
	}
	if got.ClassificationRationale == "" {
		t.Error("expected rationale to be set")
	}
	if got.Stage != devrouter.StagePlanning {
		t.Errorf("expected stage planning, got %s", got.Stage)
	}
}

func TestIngressJob_FetchesContentWhenEmpty(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	rt := &testutil.MockRuntime{
		Output: `{"track":"rpi","rationale":"needs planning"}`,
	}
	bus := &testutil.DiscardBus{}
	cfg := devrouter.DefaultDevRouterConfig()
	ex := &testutil.MockExecutor{
		Output: []byte(`{"title":"Fix the bug","body":"Some body text","labels":[{"name":"bug"},{"name":"ponko-runner:dev-router"}]}`),
	}

	p := &devrouter.Pipeline{
		ID:       "fetch-test",
		IssueURL: "https://github.com/owner/repo/issues/10",
		Stage:    devrouter.StageClassifying,
	}
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	job := devrouter.NewIngressJob()
	if err := job.Run(ctx, p, store, rt, bus, cfg, ex); err != nil {
		t.Fatalf("IngressJob.Run: %v", err)
	}

	got, _ := store.Get(ctx, p.ID)
	if got.IssueTitle != "Fix the bug" {
		t.Errorf("IssueTitle: got %q, want %q", got.IssueTitle, "Fix the bug")
	}
	if got.IssueBody != "Some body text" {
		t.Errorf("IssueBody: got %q, want %q", got.IssueBody, "Some body text")
	}
	// Verify exec was called with title,body,labels JSON flag
	if len(ex.Calls) == 0 {
		t.Fatal("expected exec to be called")
	}
}

func TestIngressJob_SkipsFetchWhenPopulated(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	rt := &testutil.MockRuntime{
		Output: `{"track":"fix","rationale":"one-liner"}`,
	}
	bus := &testutil.DiscardBus{}
	cfg := devrouter.DefaultDevRouterConfig()
	// Exec returns labels-only JSON (title already populated, only labels needed)
	ex := &testutil.MockExecutor{
		Output: []byte(`{"labels":[{"name":"bug"}]}`),
	}

	p := &devrouter.Pipeline{
		ID:         "skip-fetch-test",
		IssueURL:   "https://github.com/owner/repo/issues/11",
		IssueTitle: "Already has a title",
		IssueBody:  "Already has a body",
		Stage:      devrouter.StageClassifying,
	}
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	job := devrouter.NewIngressJob()
	if err := job.Run(ctx, p, store, rt, bus, cfg, ex); err != nil {
		t.Fatalf("IngressJob.Run: %v", err)
	}

	// Title/body should be unchanged
	got, _ := store.Get(ctx, p.ID)
	if got.IssueTitle != "Already has a title" {
		t.Errorf("IssueTitle should not change, got %q", got.IssueTitle)
	}
	// Exec should have been called for labels only (not SetIssueContent)
	if len(ex.Calls) == 0 {
		t.Fatal("expected exec to be called for labels")
	}
	// Verify it only fetched labels (not title,body,labels)
	lastCall := ex.Calls[len(ex.Calls)-1]
	for _, arg := range lastCall {
		if arg == "title,body,labels" {
			t.Error("should not fetch title,body,labels when title is already set")
		}
	}
}

func TestIngressJob_FallsBackToRPIOnBadJSON(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	rt := &testutil.MockRuntime{
		Output: `not valid json at all`,
	}
	bus := &testutil.DiscardBus{}
	cfg := devrouter.DefaultDevRouterConfig()

	p := &devrouter.Pipeline{
		ID:    "test-pipeline-2",
		Stage: devrouter.StageClassifying,
	}
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	job := devrouter.NewIngressJob()
	if err := job.Run(ctx, p, store, rt, bus, cfg, nil); err != nil {
		t.Fatalf("IngressJob.Run should not fail on bad JSON: %v", err)
	}

	got, _ := store.Get(ctx, p.ID)
	if got.Track != devrouter.TrackRPI {
		t.Errorf("expected fallback track rpi, got %s", got.Track)
	}
}
