package devrouter

import (
	"context"
	"fmt"
	"log"

	"github.com/bryanneva/ponko/internal/budget"
	"github.com/bryanneva/ponko/internal/config"
	"github.com/bryanneva/ponko/internal/event"
	"github.com/bryanneva/ponko/internal/runtime"
)

// StoryJob executes a single story by invoking the AgentRuntime.
type StoryJob struct{}

// NewStoryJob creates a new StoryJob.
func NewStoryJob() *StoryJob { return &StoryJob{} }

// skillForTrack maps execution tracks to skill names.
func skillForTrack(track Track) string {
	switch track {
	case TrackFix:
		return "fix"
	case TrackRPI:
		return "rpi-implement"
	case TrackRalph:
		return "ralph-ship"
	default:
		return "fix"
	}
}

// Run executes the story at storyIndex, retrying on failure up to cfg.MaxRetries times.
// On the last story, transitions to validating. On failure after all retries, transitions to failed.
func (j *StoryJob) Run(
	ctx context.Context,
	p *Pipeline,
	storyIndex int,
	store PipelineStore,
	rt runtime.AgentRuntime,
	bus event.Bus,
	budgetCtrl budget.Controller,
	cfg config.DevRouterConfig,
) error {
	stories, err := ParseStories(p.PlanOutput)
	if err != nil {
		return fmt.Errorf("story job: parse plan: %w", err)
	}
	if storyIndex >= len(stories) {
		return fmt.Errorf("story job: storyIndex %d out of range (len=%d)", storyIndex, len(stories))
	}

	story := stories[storyIndex]

	// Collect previous story results for context
	var previousResults []string
	for i := 0; i < storyIndex; i++ {
		previousResults = append(previousResults, fmt.Sprintf("Story %d (%s): completed", i+1, stories[i].Title))
	}

	prompt := StoryPrompt(story, p.IssueTitle, p.IssueBody, previousResults)
	skill := skillForTrack(p.Track)

	var lastErr error
	var result *runtime.ExecuteResult

	for attempt := 0; attempt < cfg.MaxRetries; attempt++ {
		result, lastErr = rt.Execute(ctx, runtime.ExecuteRequest{
			Prompt:    prompt,
			Skill:     skill,
			Model:     cfg.ExecutionModel,
			IssueURL:  p.IssueURL,
		})
		if lastErr == nil {
			break
		}
		log.Printf("devrouter: story %d attempt %d/%d failed: %v", storyIndex, attempt+1, cfg.MaxRetries, lastErr)
	}

	if lastErr != nil {
		log.Printf("devrouter: story %d failed after %d retries, transitioning to failed", storyIndex, cfg.MaxRetries)
		_ = store.UpdateStage(ctx, p.ID, StageExecuting, StageFailed)
		_ = bus.Emit(event.Event{
			Type:   event.TaskFailed,
			TaskID: p.ID,
			Payload: map[string]any{
				"story_index": storyIndex,
				"error":       lastErr.Error(),
			},
		})
		return nil
	}

	// Record cost
	if result.CostUSD > 0 {
		_ = store.AddCost(ctx, p.ID, result.CostUSD)
		_ = budgetCtrl.Record(ctx, p.ID, "", result.CostUSD)
	}

	newCount, err := store.IncrStoriesCompleted(ctx, p.ID)
	if err != nil {
		return fmt.Errorf("story job: incr stories completed: %w", err)
	}

	_ = bus.Emit(event.Event{
		Type:   event.JobCompleted,
		TaskID: p.ID,
		Payload: map[string]any{
			"job":               "story",
			"story_index":       storyIndex,
			"stories_completed": newCount,
		},
	})

	isLastStory := storyIndex == p.StoryCount-1
	if isLastStory {
		if err := store.UpdateStage(ctx, p.ID, StageExecuting, StageValidating); err != nil {
			return fmt.Errorf("story job: transition to validating: %w", err)
		}
	} else {
		if err := store.SetCurrentStoryIndex(ctx, p.ID, storyIndex+1); err != nil {
			return fmt.Errorf("story job: advance story index: %w", err)
		}
	}

	return nil
}
