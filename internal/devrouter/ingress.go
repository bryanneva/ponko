package devrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/bryanneva/ponko/internal/config"
	"github.com/bryanneva/ponko/internal/event"
	"github.com/bryanneva/ponko/internal/exec"
	"github.com/bryanneva/ponko/internal/runtime"
)

// IngressJob classifies a GitHub issue into an execution track.
type IngressJob struct{}

// NewIngressJob creates a new IngressJob.
func NewIngressJob() *IngressJob { return &IngressJob{} }

// Run classifies the pipeline's issue, sets track and rationale, and transitions to planning.
func (j *IngressJob) Run(
	ctx context.Context,
	p *Pipeline,
	store PipelineStore,
	rt runtime.AgentRuntime,
	bus event.Bus,
	cfg config.DevRouterConfig,
	ex exec.CommandExecutor,
) error {
	// CORRECTNESS: fetch errors are logged and swallowed intentionally.
	// If gh CLI is unavailable or the issue is inaccessible, the pipeline
	// advances with whatever title/body are already stored (may be empty).
	// Returning the error here would stall the pipeline in StageClassifying
	// indefinitely; degraded classification is preferable to a hard block.
	labels, err := j.fetchIssueContent(ctx, p, store, ex)
	if err != nil {
		log.Printf("devrouter: ingress: fetch issue content for %s: %v", p.IssueURL, err)
	}

	prompt := ClassificationPrompt(p.IssueTitle, p.IssueBody, labels)

	result, err := rt.Execute(ctx, runtime.ExecuteRequest{
		Prompt: prompt,
		Model:  cfg.ClassificationModel,
	})
	if err != nil {
		return fmt.Errorf("classify issue: %w", err)
	}

	track, rationale := parseClassificationResponse(result.Output)

	if err := store.SetTrack(ctx, p.ID, track); err != nil {
		return fmt.Errorf("set track: %w", err)
	}
	if err := store.SetClassificationRationale(ctx, p.ID, rationale); err != nil {
		return fmt.Errorf("set rationale: %w", err)
	}
	if err := store.UpdateStage(ctx, p.ID, StageClassifying, StagePlanning); err != nil {
		return fmt.Errorf("transition to planning: %w", err)
	}

	_ = bus.Emit(event.Event{
		Type:   event.JobCompleted,
		TaskID: p.ID,
		Payload: map[string]any{
			"job":       "ingress",
			"track":     string(track),
			"rationale": rationale,
		},
	})

	return nil
}

// fetchIssueContent ensures p.IssueTitle and p.IssueBody are populated.
// If title is already set, it only fetches labels. Returns the label names.
// Returns nil without error if exec is nil (useful in tests that don't exercise fetch).
func (j *IngressJob) fetchIssueContent(ctx context.Context, p *Pipeline, store PipelineStore, ex exec.CommandExecutor) ([]string, error) {
	if p.IssueURL == "" || ex == nil {
		return nil, nil
	}

	if p.IssueTitle == "" {
		// Fetch title, body, and labels in one call
		out, err := ex.Run(ctx, "", "gh", "issue", "view", p.IssueURL, "--json", "title,body,labels")
		if err != nil {
			return nil, fmt.Errorf("gh issue view: %w", err)
		}
		var issue ghListIssue
		if err := json.Unmarshal(out, &issue); err != nil {
			return nil, fmt.Errorf("parse issue view: %w", err)
		}
		p.IssueTitle = issue.Title
		p.IssueBody = issue.Body
		if err := store.SetIssueContent(ctx, p.ID, issue.Title, issue.Body); err != nil {
			return nil, fmt.Errorf("persist issue content: %w", err)
		}
		return labelNames(issue.Labels), nil
	}

	// Title already populated — only fetch labels
	out, err := ex.Run(ctx, "", "gh", "issue", "view", p.IssueURL, "--json", "labels")
	if err != nil {
		return nil, fmt.Errorf("gh issue view labels: %w", err)
	}
	var issue ghListIssue
	if err := json.Unmarshal(out, &issue); err != nil {
		return nil, fmt.Errorf("parse labels: %w", err)
	}
	return labelNames(issue.Labels), nil
}

type classificationResponse struct {
	Track     string `json:"track"`
	Rationale string `json:"rationale"`
}

// parseClassificationResponse extracts track and rationale from LLM JSON output.
// Falls back to TrackRPI if parsing fails.
func parseClassificationResponse(output string) (Track, string) {
	var resp classificationResponse
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		log.Printf("devrouter: classify parse error, falling back to rpi. raw output: %s", output)
		return TrackRPI, ""
	}

	switch Track(resp.Track) {
	case TrackFix, TrackRPI, TrackRalph:
		return Track(resp.Track), resp.Rationale
	default:
		log.Printf("devrouter: unknown track %q, falling back to rpi", resp.Track)
		return TrackRPI, resp.Rationale
	}
}

// DefaultDevRouterConfig returns a config with production defaults applied.
func DefaultDevRouterConfig() config.DevRouterConfig {
	return config.DevRouterConfig{
		ClassificationModel: "sonnet",
		PlanningModel:       "sonnet",
		ExecutionModel:      "sonnet",
		MaxStories:          10,
		MaxRetries:          3,
		IntakeLabel:         "ponko-runner:dev-router",
	}
}
