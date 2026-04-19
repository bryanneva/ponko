package devrouter

import (
	"context"
	"fmt"
	"log"

	"github.com/bryanneva/ponko/internal/budget"
	"github.com/bryanneva/ponko/internal/config"
	"github.com/bryanneva/ponko/internal/event"
	"github.com/bryanneva/ponko/internal/exec"
	"github.com/bryanneva/ponko/internal/runtime"
)

// terminalStages are pipeline stages that need no further processing.
var terminalStages = map[Stage]bool{
	StageCompleted: true,
	StageFailed:    true,
}

// PipelineRunner advances pipelines through their stages one step per invocation.
type PipelineRunner struct {
	store           PipelineStore
	rt              runtime.AgentRuntime
	bus             event.Bus
	budget          budget.Controller
	commenter       Commenter
	ciChecker       CIChecker
	approvalChecker ApprovalChecker
	exec            exec.CommandExecutor
	cfg             config.DevRouterConfig
	dryRun          bool
}

// NewPipelineRunner constructs a PipelineRunner with all required dependencies.
func NewPipelineRunner(
	store PipelineStore,
	rt runtime.AgentRuntime,
	bus event.Bus,
	budgetCtrl budget.Controller,
	commenter Commenter,
	ci CIChecker,
	approvalChecker ApprovalChecker,
	cfg config.DevRouterConfig,
	ex exec.CommandExecutor,
) *PipelineRunner {
	return &PipelineRunner{
		store:           store,
		rt:              rt,
		bus:             bus,
		budget:          budgetCtrl,
		commenter:       commenter,
		ciChecker:       ci,
		approvalChecker: approvalChecker,
		cfg:             cfg,
		exec:            ex,
	}
}

// SetDryRun enables dry-run mode: actions are logged but not executed.
func (r *PipelineRunner) SetDryRun(dryRun bool) { r.dryRun = dryRun }

// RunOnce reads the pipeline with the given ID and advances it by one stage.
func (r *PipelineRunner) RunOnce(ctx context.Context, pipelineID string) error {
	p, err := r.store.Get(ctx, pipelineID)
	if err != nil {
		return fmt.Errorf("runner: get pipeline %s: %w", pipelineID, err)
	}
	if p == nil {
		return fmt.Errorf("runner: pipeline %s not found", pipelineID)
	}

	if terminalStages[p.Stage] {
		return nil
	}

	if r.dryRun {
		log.Printf("[dry-run] pipeline %s (stage=%s) would be advanced", p.ID, p.Stage)
		return nil
	}

	// Budget check before agent invocations
	if p.Stage == StagePending || p.Stage == StagePlanning || p.Stage == StageExecuting {
		if !r.budget.CanAffordRun(ctx, p.ID, 1.0) || !r.budget.CanAffordDay(ctx, 1.0) {
			_ = r.bus.Emit(event.Event{Type: event.BudgetExceeded, TaskID: p.ID})
			return nil
		}
	}

	return r.dispatch(ctx, p)
}

// dispatch routes the pipeline to the correct job based on its current stage.
func (r *PipelineRunner) dispatch(ctx context.Context, p *Pipeline) error {
	switch p.Stage {
	case StagePending:
		// Transition to classifying, then run ingress
		if err := r.store.UpdateStage(ctx, p.ID, StagePending, StageClassifying); err != nil {
			return fmt.Errorf("runner: transition to classifying: %w", err)
		}
		p.Stage = StageClassifying
		return NewIngressJob().Run(ctx, p, r.store, r.rt, r.bus, r.cfg, r.exec)

	case StageClassifying:
		return NewIngressJob().Run(ctx, p, r.store, r.rt, r.bus, r.cfg, r.exec)

	case StagePlanning:
		return NewPlanningJob().Run(ctx, p, r.store, r.rt, r.bus, r.cfg)

	case StageFanout:
		return NewFanOutJob().Run(ctx, p, r.store, r.bus)

	case StageExecuting:
		return NewStoryJob().Run(ctx, p, p.CurrentStoryIndex, r.store, r.rt, r.bus, r.budget, r.cfg)

	case StageValidating, StageAwaitingApproval:
		return NewValidationJob().Run(ctx, p, r.store, r.bus, r.commenter, r.ciChecker, r.approvalChecker)

	default:
		return fmt.Errorf("runner: unknown stage %s for pipeline %s", p.Stage, p.ID)
	}
}

// ProcessAll advances all non-terminal pipelines by one step.
func (r *PipelineRunner) ProcessAll(ctx context.Context) error {
	pipelines, err := r.store.List(ctx, 0)
	if err != nil {
		return fmt.Errorf("runner: list pipelines: %w", err)
	}

	var errs []error
	for _, p := range pipelines {
		if terminalStages[p.Stage] {
			continue
		}
		if err := r.RunOnce(ctx, p.ID); err != nil {
			log.Printf("runner: error advancing pipeline %s: %v", p.ID, err)
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("runner: %d pipeline(s) failed: first error: %w", len(errs), errs[0])
	}
	return nil
}
