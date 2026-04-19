package devrouter_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bryanneva/ponko/internal/devrouter"
	"github.com/bryanneva/ponko/internal/devrouter/testutil"
)

func makePlanOutput(stories []devrouter.Story) string {
	type planResp struct {
		Stories []devrouter.Story `json:"stories"`
	}
	b, _ := json.Marshal(planResp{Stories: stories})
	return string(b)
}

func TestFanOutJob_ValidPlanTransitionsToExecuting(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	bus := &testutil.DiscardBus{}

	stories := []devrouter.Story{
		{Title: "Story 1", Description: "Do thing 1", SuccessCriteria: []string{"works"}},
		{Title: "Story 2", Description: "Do thing 2", SuccessCriteria: []string{"also works"}},
	}
	planOutput := makePlanOutput(stories)

	p := &devrouter.Pipeline{
		ID:         "fanout-test",
		Stage:      devrouter.StageFanout,
		StoryCount: 2,
		PlanOutput: planOutput,
	}
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	job := devrouter.NewFanOutJob()
	if err := job.Run(ctx, p, store, bus); err != nil {
		t.Fatalf("FanOutJob.Run: %v", err)
	}

	got, _ := store.Get(ctx, p.ID)
	if got.Stage != devrouter.StageExecuting {
		t.Errorf("expected stage executing, got %s", got.Stage)
	}
	if got.CurrentStoryIndex != 0 {
		t.Errorf("expected CurrentStoryIndex 0, got %d", got.CurrentStoryIndex)
	}
}

func TestFanOutJob_EmptyStoriesReturnsError(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	bus := &testutil.DiscardBus{}

	p := &devrouter.Pipeline{
		ID:         "fanout-empty",
		Stage:      devrouter.StageFanout,
		StoryCount: 0,
		PlanOutput: `{"stories":[]}`,
	}
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	job := devrouter.NewFanOutJob()
	err := job.Run(ctx, p, store, bus)
	if err == nil {
		t.Error("expected error for empty stories")
	}
}

func TestFanOutJob_InvalidJSONReturnsError(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	bus := &testutil.DiscardBus{}

	p := &devrouter.Pipeline{
		ID:         "fanout-bad-json",
		Stage:      devrouter.StageFanout,
		StoryCount: 1,
		PlanOutput: `not json`,
	}
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	job := devrouter.NewFanOutJob()
	err := job.Run(ctx, p, store, bus)
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}
