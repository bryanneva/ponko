package devrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/bryanneva/ponko/internal/config"
	"github.com/bryanneva/ponko/internal/event"
	"github.com/bryanneva/ponko/internal/runtime"
)

// PlanningJob invokes the LLM to produce a structured plan with stories.
type PlanningJob struct{}

// NewPlanningJob creates a new PlanningJob.
func NewPlanningJob() *PlanningJob { return &PlanningJob{} }

// Run invokes the planning LLM, parses the stories, and transitions to fanout.
func (j *PlanningJob) Run(
	ctx context.Context,
	p *Pipeline,
	store PipelineStore,
	rt runtime.AgentRuntime,
	bus event.Bus,
	cfg config.DevRouterConfig,
) error {
	prompt := PlanningPrompt(p.Track, p.IssueTitle, p.IssueBody, p.ClassificationRationale)

	result, err := rt.Execute(ctx, runtime.ExecuteRequest{
		Prompt: prompt,
		Model:  cfg.PlanningModel,
	})
	if err != nil {
		return fmt.Errorf("planning execute: %w", err)
	}

	stories, planJSON := parsePlanResponse(result.Output, p)

	if err := store.SetPlanOutput(ctx, p.ID, planJSON); err != nil {
		return fmt.Errorf("set plan output: %w", err)
	}
	if err := store.SetStoryCount(ctx, p.ID, len(stories)); err != nil {
		return fmt.Errorf("set story count: %w", err)
	}
	if err := store.UpdateStage(ctx, p.ID, StagePlanning, StageFanout); err != nil {
		return fmt.Errorf("transition to fanout: %w", err)
	}

	_ = bus.Emit(event.Event{
		Type:   event.JobCompleted,
		TaskID: p.ID,
		Payload: map[string]any{
			"job":         "planning",
			"story_count": len(stories),
		},
	})

	return nil
}

type planResponse struct {
	Stories []Story `json:"stories"`
}

// parsePlanResponse parses LLM plan output and validates stories.
// Falls back to a single-story plan if parsing fails or stories are invalid.
func parsePlanResponse(output string, p *Pipeline) ([]Story, string) {
	var resp planResponse
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		log.Printf("devrouter: plan parse error for pipeline %s, using fallback. raw: %s", p.ID, output)
		return fallbackPlan(p)
	}

	var valid []Story
	for _, s := range resp.Stories {
		if s.Title == "" || s.Description == "" || len(s.SuccessCriteria) == 0 {
			log.Printf("devrouter: skipping story with missing fields: %+v", s)
			continue
		}
		valid = append(valid, s)
	}

	if len(valid) == 0 {
		log.Printf("devrouter: no valid stories in plan for pipeline %s, using fallback", p.ID)
		return fallbackPlan(p)
	}

	planJSON, _ := json.Marshal(planResponse{Stories: valid})
	return valid, string(planJSON)
}

// fallbackPlan creates a minimal single-story plan wrapping the entire issue.
func fallbackPlan(p *Pipeline) ([]Story, string) {
	story := Story{
		Title:           fmt.Sprintf("Implement issue %s", p.IssueURL),
		Description:     "Implement the full issue as described.",
		SuccessCriteria: []string{"Issue requirements are met", "Tests pass", "Lint passes"},
	}
	plan := planResponse{Stories: []Story{story}}
	planJSON, _ := json.Marshal(plan)
	return []Story{story}, string(planJSON)
}
