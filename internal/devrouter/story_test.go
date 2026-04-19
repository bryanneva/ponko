package devrouter_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bryanneva/ponko/internal/devrouter"
	"github.com/bryanneva/ponko/internal/devrouter/testutil"
	"github.com/bryanneva/ponko/internal/runtime"
)

func makePipelineWithPlan(t *testing.T, store devrouter.PipelineStore, track devrouter.Track, stories []devrouter.Story) *devrouter.Pipeline {
	t.Helper()
	planOutput := makePlanOutput(stories)
	p := &devrouter.Pipeline{
		ID:         "story-test-" + string(track),
		Stage:      devrouter.StageExecuting,
		Track:      track,
		StoryCount: len(stories),
		PlanOutput: planOutput,
		IssueURL:   "https://github.com/owner/repo/issues/5",
	}
	if err := store.Create(context.Background(), p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return p
}

func TestStoryJob_SuccessAdvancesIndex(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	rt := &testutil.MockRuntime{Output: "done"}
	bus := &testutil.DiscardBus{}
	budget := &testutil.AlwaysAffordBudget{}
	cfg := devrouter.DefaultDevRouterConfig()

	stories := []devrouter.Story{
		{Title: "Story 1", Description: "Do it", SuccessCriteria: []string{"works"}},
		{Title: "Story 2", Description: "Do more", SuccessCriteria: []string{"also works"}},
	}
	p := makePipelineWithPlan(t, store, devrouter.TrackFix, stories)

	job := devrouter.NewStoryJob()
	if err := job.Run(ctx, p, 0, store, rt, bus, budget, cfg); err != nil {
		t.Fatalf("StoryJob.Run: %v", err)
	}

	got, _ := store.Get(ctx, p.ID)
	if got.StoriesCompleted != 1 {
		t.Errorf("expected 1 story completed, got %d", got.StoriesCompleted)
	}
	if got.CurrentStoryIndex != 1 {
		t.Errorf("expected CurrentStoryIndex 1, got %d", got.CurrentStoryIndex)
	}
	// Not the last story, so still executing
	if got.Stage != devrouter.StageExecuting {
		t.Errorf("expected stage executing, got %s", got.Stage)
	}
}

func TestStoryJob_LastStoryTransitionsToValidating(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	rt := &testutil.MockRuntime{Output: "done"}
	bus := &testutil.DiscardBus{}
	budget := &testutil.AlwaysAffordBudget{}
	cfg := devrouter.DefaultDevRouterConfig()

	stories := []devrouter.Story{
		{Title: "Only Story", Description: "Do everything", SuccessCriteria: []string{"works"}},
	}
	p := makePipelineWithPlan(t, store, devrouter.TrackRPI, stories)

	job := devrouter.NewStoryJob()
	if err := job.Run(ctx, p, 0, store, rt, bus, budget, cfg); err != nil {
		t.Fatalf("StoryJob.Run: %v", err)
	}

	got, _ := store.Get(ctx, p.ID)
	if got.Stage != devrouter.StageValidating {
		t.Errorf("expected stage validating after last story, got %s", got.Stage)
	}
}

func TestStoryJob_TrackToSkillMapping(t *testing.T) {
	ctx := context.Background()
	stories := []devrouter.Story{
		{Title: "Story", Description: "Do it", SuccessCriteria: []string{"works"}},
	}
	cases := []struct {
		track    devrouter.Track
		wantSkill string
	}{
		{devrouter.TrackFix, "fix"},
		{devrouter.TrackRPI, "rpi-implement"},
		{devrouter.TrackRalph, "ralph-ship"},
	}

	for _, tc := range cases {
		store := testutil.NewMemoryPipelineStore()
		rt := &testutil.MockRuntime{Output: "done"}
		bus := &testutil.DiscardBus{}
		budget := &testutil.AlwaysAffordBudget{}
		cfg := devrouter.DefaultDevRouterConfig()

		p := makePipelineWithPlan(t, store, tc.track, stories)

		job := devrouter.NewStoryJob()
		if err := job.Run(ctx, p, 0, store, rt, bus, budget, cfg); err != nil {
			t.Fatalf("StoryJob.Run(%s): %v", tc.track, err)
		}

		if len(rt.Calls) == 0 {
			t.Fatalf("expected Execute call for track %s", tc.track)
		}
		if rt.Calls[0].Skill != tc.wantSkill {
			t.Errorf("track %s: expected skill %q, got %q", tc.track, tc.wantSkill, rt.Calls[0].Skill)
		}
	}
}

func TestStoryJob_RetriesOnFailure(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	callCount := 0
	rt := &testutil.MockRuntime{}
	rt.Err = errors.New("agent failed")
	bus := &testutil.DiscardBus{}
	budget := &testutil.AlwaysAffordBudget{}
	cfg := devrouter.DefaultDevRouterConfig()
	cfg.MaxRetries = 2

	stories := []devrouter.Story{
		{Title: "Story", Description: "Do it", SuccessCriteria: []string{"works"}},
	}
	p := makePipelineWithPlan(t, store, devrouter.TrackFix, stories)
	_ = callCount

	job := devrouter.NewStoryJob()
	// Run should not error — it transitions to failed after retries
	if err := job.Run(ctx, p, 0, store, rt, bus, budget, cfg); err != nil {
		t.Fatalf("StoryJob.Run should handle retries gracefully: %v", err)
	}

	got, _ := store.Get(ctx, p.ID)
	if got.Stage != devrouter.StageFailed {
		t.Errorf("expected stage failed after max retries, got %s", got.Stage)
	}
	// Should have tried MaxRetries times
	if len(rt.Calls) != cfg.MaxRetries {
		t.Errorf("expected %d Execute calls, got %d", cfg.MaxRetries, len(rt.Calls))
	}
}

func TestStoryJob_RecordsCost(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	rt := &testutil.MockRuntime{Output: "done", CostUSD: 0.05}
	bus := &testutil.DiscardBus{}
	budget := &testutil.AlwaysAffordBudget{}
	cfg := devrouter.DefaultDevRouterConfig()

	stories := []devrouter.Story{
		{Title: "Story", Description: "Do it", SuccessCriteria: []string{"works"}},
	}
	p := makePipelineWithPlan(t, store, devrouter.TrackFix, stories)

	job := devrouter.NewStoryJob()
	if err := job.Run(ctx, p, 0, store, rt, bus, budget, cfg); err != nil {
		t.Fatalf("StoryJob.Run: %v", err)
	}

	got, _ := store.Get(ctx, p.ID)
	if got.CostUSD == 0 {
		t.Error("expected cost to be recorded")
	}
}

// Compile-time check that MockRuntime satisfies runtime.AgentRuntime
var _ runtime.AgentRuntime = (*testutil.MockRuntime)(nil)
