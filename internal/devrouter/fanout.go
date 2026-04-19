package devrouter

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bryanneva/ponko/internal/event"
)

// FanOutJob validates the plan and initializes story tracking.
type FanOutJob struct{}

// NewFanOutJob creates a new FanOutJob.
func NewFanOutJob() *FanOutJob { return &FanOutJob{} }

// Run validates the plan output, sets CurrentStoryIndex to 0, and transitions to executing.
func (j *FanOutJob) Run(
	ctx context.Context,
	p *Pipeline,
	store PipelineStore,
	bus event.Bus,
) error {
	stories, err := ParseStories(p.PlanOutput)
	if err != nil {
		return fmt.Errorf("fanout: parse plan: %w", err)
	}
	if len(stories) == 0 {
		return fmt.Errorf("fanout: plan has no stories")
	}

	for i, s := range stories {
		if s.Title == "" || s.Description == "" || len(s.SuccessCriteria) == 0 {
			return fmt.Errorf("fanout: story[%d] is missing required fields (title, description, success_criteria)", i)
		}
	}

	if err := store.SetCurrentStoryIndex(ctx, p.ID, 0); err != nil {
		return fmt.Errorf("fanout: set current story index: %w", err)
	}
	if err := store.UpdateStage(ctx, p.ID, StageFanout, StageExecuting); err != nil {
		return fmt.Errorf("fanout: transition to executing: %w", err)
	}

	summaries := make([]string, len(stories))
	for i, s := range stories {
		summaries[i] = s.Title
	}

	_ = bus.Emit(event.Event{
		Type:   event.JobCompleted,
		TaskID: p.ID,
		Payload: map[string]any{
			"job":         "fanout",
			"story_count": len(stories),
			"stories":     summaries,
		},
	})

	return nil
}

type planWrapper struct {
	Stories []Story `json:"stories"`
}

// ParseStories decodes the plan JSON and returns the stories slice.
func ParseStories(planOutput string) ([]Story, error) {
	var plan planWrapper
	if err := json.Unmarshal([]byte(planOutput), &plan); err != nil {
		return nil, fmt.Errorf("invalid plan JSON: %w", err)
	}
	return plan.Stories, nil
}
