package worker

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/riverqueue/river"

	"github.com/bryanneva/ponko/internal/approval"
	"github.com/bryanneva/ponko/internal/event"
)

const approvalPollInterval = 5 * time.Minute

// ApprovalPollerArgs is the River job args for waiting on human approval.
type ApprovalPollerArgs struct {
	IssueURL      string        `json:"issue_url"`
	NextPhaseArgs TaskPhaseArgs `json:"next_phase_args"`
}

func (ApprovalPollerArgs) Kind() string { return "approval_poller" }

// ApprovalPollerWorkerDeps holds external dependencies for ApprovalPollerWorker.
type ApprovalPollerWorkerDeps struct {
	ApprovalChecker ApprovalChecker
	Commenter       Commenter
	Bus             event.Bus
}

// ApprovalPollerWorker polls an issue until approval or rejection is present.
type ApprovalPollerWorker struct {
	river.WorkerDefaults[ApprovalPollerArgs]
	deps ApprovalPollerWorkerDeps
}

// NewApprovalPollerWorker constructs an ApprovalPollerWorker.
func NewApprovalPollerWorker(deps ApprovalPollerWorkerDeps) *ApprovalPollerWorker {
	return &ApprovalPollerWorker{deps: deps}
}

func (w *ApprovalPollerWorker) Work(ctx context.Context, job *river.Job[ApprovalPollerArgs]) error {
	args := job.Args
	if w.deps.ApprovalChecker == nil {
		return river.JobSnooze(approvalPollInterval)
	}

	status, err := w.deps.ApprovalChecker.CheckApproval(ctx, args.IssueURL)
	if err != nil {
		log.Printf("approval check for %s: %v", args.IssueURL, err)
		return river.JobSnooze(approvalPollInterval)
	}

	switch status {
	case approval.Approved:
		w.emit(event.Event{
			Type:   event.TaskStarted,
			TaskID: args.IssueURL,
			Payload: map[string]any{
				"issue_url": args.IssueURL,
				"approval":  "approved",
			},
		})
		_, err := river.ClientFromContext[*sql.Tx](ctx).Insert(ctx, args.NextPhaseArgs, &river.InsertOpts{
			MaxAttempts: TaskPhaseMaxAttempts,
		})
		return err

	case approval.Rejected:
		if w.deps.Commenter != nil {
			_ = w.deps.Commenter.Comment(ctx, args.IssueURL, "Task cancelled per rejection.")
		}
		w.emit(event.Event{
			Type:   event.TaskCancelled,
			TaskID: args.IssueURL,
			Payload: map[string]any{
				"issue_url": args.IssueURL,
				"approval":  "rejected",
			},
		})
		return nil

	default:
		return river.JobSnooze(approvalPollInterval)
	}
}

// RegisterApprovalPoller adds the ApprovalPollerWorker to a River workers set.
func RegisterApprovalPoller(workers *river.Workers, deps ApprovalPollerWorkerDeps) {
	river.AddWorker(workers, NewApprovalPollerWorker(deps))
}

func (w *ApprovalPollerWorker) emit(e event.Event) {
	if w.deps.Bus == nil {
		return
	}
	_ = w.deps.Bus.Emit(e)
}
