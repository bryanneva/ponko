package worker

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/riverqueue/river"

	"github.com/bryanneva/ponko/internal/approval"
	"github.com/bryanneva/ponko/internal/budget"
	"github.com/bryanneva/ponko/internal/config"
	"github.com/bryanneva/ponko/internal/event"
	"github.com/bryanneva/ponko/internal/runtime"
)

const TaskPhaseMaxAttempts = 3

const blockedTaskSnoozeInterval = 5 * time.Minute

// TaskPhaseArgs is the River job args for one phase of an issue workflow.
type TaskPhaseArgs struct {
	IssueURL    string   `json:"issue_url" river:"unique"`
	Repo        string   `json:"repo"`
	Title       string   `json:"title"`
	Body        string   `json:"body"`
	Workflow    string   `json:"workflow"`
	Phase       string   `json:"phase"`
	Labels      []string `json:"labels"`
	IssueNumber int      `json:"issue_number"`
	CostUSD     float64  `json:"cost_usd"`
}

func (TaskPhaseArgs) Kind() string { return "task_phase" }

func (TaskPhaseArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: TaskPhaseMaxAttempts}
}

// TaskPhaseWorkerDeps holds external dependencies for TaskPhaseWorker.
type TaskPhaseWorkerDeps struct {
	Budget      budget.Controller
	Bus         event.Bus
	Runtime     runtime.AgentRuntime
	GateChecker GateChecker
	Commenter   Commenter
	Cfg         config.Config
}

// GateChecker verifies whether a gate condition is met.
type GateChecker interface {
	HasLabel(ctx context.Context, issueURL, label string) (bool, error)
}

// Commenter posts comments on GitHub issues.
type Commenter interface {
	Comment(ctx context.Context, issueURL, body string) error
}

// ApprovalChecker polls a GitHub issue for approval signals.
type ApprovalChecker interface {
	CheckApproval(ctx context.Context, issueURL string) (approval.Status, error)
}

// TaskPhaseWorker executes one workflow phase.
type TaskPhaseWorker struct {
	river.WorkerDefaults[TaskPhaseArgs]
	deps TaskPhaseWorkerDeps
}

// NewTaskPhaseWorker constructs a TaskPhaseWorker.
func NewTaskPhaseWorker(deps TaskPhaseWorkerDeps) *TaskPhaseWorker {
	return &TaskPhaseWorker{deps: deps}
}

func (w *TaskPhaseWorker) Work(ctx context.Context, job *river.Job[TaskPhaseArgs]) error {
	args := job.Args
	runID := strconv.FormatInt(job.ID, 10)

	if w.deps.Budget == nil {
		return fmt.Errorf("task phase worker: missing budget controller")
	}
	if w.deps.Runtime == nil {
		return fmt.Errorf("task phase worker: missing runtime")
	}

	if !w.deps.Budget.CanAffordRun(ctx, runID, 1.0) ||
		!w.deps.Budget.CanAffordDay(ctx, 1.0) ||
		!w.deps.Budget.CanAffordTask(ctx, args.IssueURL, 1.0) {
		w.emit(event.Event{
			Type:   event.BudgetExceeded,
			TaskID: args.IssueURL,
			Payload: map[string]any{
				"issue_url": args.IssueURL,
			},
		})
		return river.JobSnooze(blockedTaskSnoozeInterval)
	}

	wf, ok := w.deps.Cfg.Workflows[args.Workflow]
	if !ok {
		return fmt.Errorf("unknown workflow %q for %s", args.Workflow, args.IssueURL)
	}
	phase := currentPhase(wf, args.Phase)
	if phase == nil {
		return fmt.Errorf("no phase found for workflow=%s phase=%q issue=%s", args.Workflow, args.Phase, args.IssueURL)
	}

	if phase.Gate != "" && w.deps.GateChecker != nil {
		requiredLabel, ok := w.deps.Cfg.Gates[phase.Gate]
		if !ok {
			return fmt.Errorf("unknown gate %q for %s", phase.Gate, args.IssueURL)
		}
		approved, err := w.deps.GateChecker.HasLabel(ctx, args.IssueURL, requiredLabel)
		if err != nil {
			log.Printf("gate check for %s: %v", args.IssueURL, err)
			return river.JobSnooze(blockedTaskSnoozeInterval)
		}
		if !approved {
			w.emit(event.Event{
				Type:   event.TaskBlocked,
				TaskID: args.IssueURL,
				Payload: map[string]any{
					"issue_url": args.IssueURL,
					"gate":      phase.Gate,
				},
			})
			return river.JobSnooze(blockedTaskSnoozeInterval)
		}
	}

	w.emit(event.Event{
		Type:   event.PhaseStarted,
		TaskID: args.IssueURL,
		Payload: map[string]any{
			"issue_url": args.IssueURL,
			"phase":     phase.Name,
		},
	})

	result, err := w.deps.Runtime.Execute(ctx, runtime.ExecuteRequest{
		WorkingDir:   w.deps.Cfg.RepoPath(args.Repo),
		Skill:        phase.Skill,
		IssueTitle:   args.Title,
		IssueBody:    args.Body,
		IssueURL:     args.IssueURL,
		MaxBudgetUSD: w.deps.Cfg.Budget.PerTaskUSD,
		Model:        phase.Model,
		Provider:     phase.Provider,
	})
	if err != nil {
		return err
	}
	if result == nil {
		result = &runtime.ExecuteResult{}
	}

	if result.CostUSD > 0 {
		recordErr := w.deps.Budget.Record(ctx, args.IssueURL, runID, result.CostUSD)
		if recordErr != nil {
			log.Printf("record budget for %s: %v", args.IssueURL, recordErr)
		}
		args.CostUSD += result.CostUSD
	}

	w.emit(event.Event{
		Type:   event.PhaseCompleted,
		TaskID: args.IssueURL,
		Payload: map[string]any{
			"issue_url": args.IssueURL,
			"phase":     phase.Name,
			"cost_usd":  result.CostUSD,
		},
	})

	if phase.Approval {
		if w.deps.Commenter != nil {
			summary := fmt.Sprintf("Phase **%s** completed for task %s.\n\nReply `/approve` to continue or `/reject` to cancel.", phase.Name, args.Title)
			_ = w.deps.Commenter.Comment(ctx, args.IssueURL, summary)
		}
		w.emit(event.Event{
			Type:   event.TaskBlocked,
			TaskID: args.IssueURL,
			Payload: map[string]any{
				"issue_url":     args.IssueURL,
				"approval_gate": phase.Name,
			},
		})
		next := nextPhase(wf, phase.Name)
		if next == nil {
			return nil
		}
		_, err = river.ClientFromContext[*sql.Tx](ctx).Insert(ctx, ApprovalPollerArgs{
			IssueURL:      args.IssueURL,
			NextPhaseArgs: nextPhaseArgs(args, next.Name),
		}, nil)
		return err
	}

	next := nextPhase(wf, phase.Name)
	if next == nil {
		w.emit(event.Event{
			Type:   event.TaskCompleted,
			TaskID: args.IssueURL,
			Payload: map[string]any{
				"issue_url": args.IssueURL,
			},
		})
		return nil
	}

	_, err = river.ClientFromContext[*sql.Tx](ctx).Insert(ctx, nextPhaseArgs(args, next.Name), &river.InsertOpts{
		MaxAttempts: TaskPhaseMaxAttempts,
	})
	return err
}

// RegisterTaskPhase adds the TaskPhaseWorker to a River workers set.
func RegisterTaskPhase(workers *river.Workers, deps TaskPhaseWorkerDeps) {
	river.AddWorker(workers, NewTaskPhaseWorker(deps))
}

func (w *TaskPhaseWorker) emit(e event.Event) {
	if w.deps.Bus == nil {
		return
	}
	_ = w.deps.Bus.Emit(e)
}

func nextPhaseArgs(args TaskPhaseArgs, phase string) TaskPhaseArgs {
	args.Phase = phase
	return args
}

func currentPhase(wf config.Workflow, currentPhaseName string) *config.Phase {
	if currentPhaseName == "" {
		if len(wf.Phases) > 0 {
			return &wf.Phases[0]
		}
		return nil
	}
	for i, p := range wf.Phases {
		if p.Name == currentPhaseName {
			return &wf.Phases[i]
		}
	}
	return nil
}

func nextPhase(wf config.Workflow, phaseName string) *config.Phase {
	for i, p := range wf.Phases {
		if p.Name == phaseName && i+1 < len(wf.Phases) {
			return &wf.Phases[i+1]
		}
	}
	return nil
}
