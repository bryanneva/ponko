package devrouter_test

import (
	"context"
	"testing"

	"github.com/bryanneva/ponko/internal/devrouter"
	"github.com/bryanneva/ponko/internal/devrouter/testutil"
)

func TestPlanningJob_ParsesStoriesAndTransitions(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	rt := &testutil.MockRuntime{
		Output: `{"stories":[{"title":"Fix bug","description":"Fix the null ptr","success_criteria":["tests pass"]},{"title":"Verify","description":"Check edge cases","success_criteria":["no regressions"]}]}`,
	}
	bus := &testutil.DiscardBus{}
	cfg := devrouter.DefaultDevRouterConfig()

	p := &devrouter.Pipeline{
		ID:                      "plan-test",
		Stage:                   devrouter.StagePlanning,
		Track:                   devrouter.TrackRPI,
		ClassificationRationale: "multi-step fix",
	}
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	job := devrouter.NewPlanningJob()
	if err := job.Run(ctx, p, store, rt, bus, cfg); err != nil {
		t.Fatalf("PlanningJob.Run: %v", err)
	}

	got, _ := store.Get(ctx, p.ID)
	if got.Stage != devrouter.StageFanout {
		t.Errorf("expected stage fanout, got %s", got.Stage)
	}
	if got.StoryCount != 2 {
		t.Errorf("expected 2 stories, got %d", got.StoryCount)
	}
	if got.PlanOutput == "" {
		t.Error("expected plan output to be set")
	}
}

func TestPlanningJob_FallsBackToSingleStoryOnBadJSON(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	rt := &testutil.MockRuntime{
		Output: `not json`,
	}
	bus := &testutil.DiscardBus{}
	cfg := devrouter.DefaultDevRouterConfig()

	p := &devrouter.Pipeline{
		ID:          "plan-fallback",
		Stage:       devrouter.StagePlanning,
		Track:       devrouter.TrackFix,
		IssueURL:    "https://github.com/owner/repo/issues/2",
		IssueNumber: 2,
	}
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	job := devrouter.NewPlanningJob()
	if err := job.Run(ctx, p, store, rt, bus, cfg); err != nil {
		t.Fatalf("PlanningJob.Run should not fail on bad JSON: %v", err)
	}

	got, _ := store.Get(ctx, p.ID)
	if got.StoryCount != 1 {
		t.Errorf("expected fallback 1 story, got %d", got.StoryCount)
	}
	if got.Stage != devrouter.StageFanout {
		t.Errorf("expected stage fanout, got %s", got.Stage)
	}
}

func TestPlanningJob_ValidatesStoriesHaveRequiredFields(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	rt := &testutil.MockRuntime{
		// Story missing success_criteria
		Output: `{"stories":[{"title":"Do work","description":"Do it","success_criteria":["criterion"]}]}`,
	}
	bus := &testutil.DiscardBus{}
	cfg := devrouter.DefaultDevRouterConfig()

	p := &devrouter.Pipeline{
		ID:    "plan-valid",
		Stage: devrouter.StagePlanning,
		Track: devrouter.TrackFix,
	}
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	job := devrouter.NewPlanningJob()
	if err := job.Run(ctx, p, store, rt, bus, cfg); err != nil {
		t.Fatalf("PlanningJob.Run: %v", err)
	}

	got, _ := store.Get(ctx, p.ID)
	if got.StoryCount != 1 {
		t.Errorf("expected 1 story, got %d", got.StoryCount)
	}
}
