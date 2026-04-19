package devrouter

import (
	"context"
	"fmt"

	"github.com/bryanneva/ponko/internal/approval"
	"github.com/bryanneva/ponko/internal/event"
)

// CI status values returned by CIChecker.
const (
	CIStatusPending = "pending"
	CIStatusGreen   = "green"
	CIStatusFailed  = "failed"
)

// CIChecker checks the CI status of a branch in a repo.
// Returns one of CIStatusPending, CIStatusGreen, or CIStatusFailed.
type CIChecker interface {
	CheckCI(ctx context.Context, repo, branch string) (string, error)
}

// Commenter posts comments on GitHub issues.
type Commenter interface {
	Comment(ctx context.Context, issueURL, body string) error
}

// ApprovalChecker polls a GitHub issue for /approve or /reject signals.
type ApprovalChecker interface {
	CheckApproval(ctx context.Context, issueURL string) (approval.Status, error)
}

// ValidationJob gates the pipeline on CI passing and human approval.
type ValidationJob struct{}

// NewValidationJob creates a new ValidationJob.
func NewValidationJob() *ValidationJob { return &ValidationJob{} }

// Run checks CI and/or approval state depending on the current pipeline stage.
func (j *ValidationJob) Run(
	ctx context.Context,
	p *Pipeline,
	store PipelineStore,
	bus event.Bus,
	commenter Commenter,
	ci CIChecker,
	approvalChecker ApprovalChecker,
) error {
	switch p.Stage {
	case StageValidating:
		return j.runCICheck(ctx, p, store, bus, commenter, ci)
	case StageAwaitingApproval:
		return j.runApprovalCheck(ctx, p, store, bus, approvalChecker)
	default:
		return fmt.Errorf("validation job: unexpected stage %s", p.Stage)
	}
}

func (j *ValidationJob) runCICheck(
	ctx context.Context,
	p *Pipeline,
	store PipelineStore,
	bus event.Bus,
	commenter Commenter,
	ci CIChecker,
) error {
	status, err := ci.CheckCI(ctx, p.Repo, "")
	if err != nil {
		return fmt.Errorf("check CI: %w", err)
	}

	switch status {
	case CIStatusPending:
		// Stay in validating, next pass will re-check
		return nil

	case CIStatusGreen:
		summary := fmt.Sprintf(
			"✅ **Dev Router pipeline completed**\n\nTrack: `%s`\nStories completed: %d/%d\nCost: $%.4f\n\nCI is green. Please review and reply `/approve` to complete or `/reject` to cancel.",
			p.Track, p.StoriesCompleted, p.StoryCount, p.CostUSD,
		)
		_ = commenter.Comment(ctx, p.IssueURL, summary)
		if err := store.UpdateStage(ctx, p.ID, StageValidating, StageAwaitingApproval); err != nil {
			return fmt.Errorf("transition to awaiting_approval: %w", err)
		}
		_ = bus.Emit(event.Event{
			Type:   event.JobCompleted,
			TaskID: p.ID,
			Payload: map[string]any{
				"job":    "validation",
				"ci":     "green",
				"stage":  string(StageAwaitingApproval),
			},
		})

	case CIStatusFailed:
		if err := store.UpdateStage(ctx, p.ID, StageValidating, StageFailed); err != nil {
			return fmt.Errorf("transition to failed: %w", err)
		}
		_ = bus.Emit(event.Event{
			Type:   event.TaskFailed,
			TaskID: p.ID,
			Payload: map[string]any{
				"job":    "validation",
				"reason": "CI failed",
			},
		})

	default:
		return fmt.Errorf("unknown CI status: %s", status)
	}

	return nil
}

func (j *ValidationJob) runApprovalCheck(
	ctx context.Context,
	p *Pipeline,
	store PipelineStore,
	bus event.Bus,
	approvalChecker ApprovalChecker,
) error {
	status, err := approvalChecker.CheckApproval(ctx, p.IssueURL)
	if err != nil {
		return fmt.Errorf("check approval: %w", err)
	}

	switch status {
	case approval.Pending:
		// Stay in awaiting_approval
		return nil

	case approval.Approved:
		if err := store.UpdateStage(ctx, p.ID, StageAwaitingApproval, StageCompleted); err != nil {
			return fmt.Errorf("transition to completed: %w", err)
		}
		_ = bus.Emit(event.Event{
			Type:   event.TaskCompleted,
			TaskID: p.ID,
			Payload: map[string]any{
				"job": "validation",
			},
		})

	case approval.Rejected:
		if err := store.UpdateStage(ctx, p.ID, StageAwaitingApproval, StageFailed); err != nil {
			return fmt.Errorf("transition to failed: %w", err)
		}
		_ = bus.Emit(event.Event{
			Type:   event.TaskFailed,
			TaskID: p.ID,
			Payload: map[string]any{
				"job":    "validation",
				"reason": "rejected by human",
			},
		})
	}

	return nil
}
