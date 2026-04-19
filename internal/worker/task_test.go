package worker_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riversqlite"
	"github.com/riverqueue/river/rivertest"
	"github.com/riverqueue/river/rivertype"

	"github.com/bryanneva/ponko/internal/config"
	"github.com/bryanneva/ponko/internal/event"
	"github.com/bryanneva/ponko/internal/runtime"
	"github.com/bryanneva/ponko/internal/sqlite"
	"github.com/bryanneva/ponko/internal/worker"
)

type taskWorkerRuntime struct {
	result *runtime.ExecuteResult
	err    error
	calls  []runtime.ExecuteRequest
}

func (r *taskWorkerRuntime) Execute(_ context.Context, req runtime.ExecuteRequest) (*runtime.ExecuteResult, error) {
	r.calls = append(r.calls, req)
	if r.err != nil {
		return nil, r.err
	}
	if r.result != nil {
		return r.result, nil
	}
	return &runtime.ExecuteResult{}, nil
}

type taskWorkerBudget struct {
	records   []budgetRecord
	canAfford bool
}

type budgetRecord struct {
	taskID string
	runID  string
	cost   float64
}

func (b *taskWorkerBudget) CanAffordRun(context.Context, string, float64) bool  { return b.canAfford }
func (b *taskWorkerBudget) CanAffordDay(context.Context, float64) bool          { return b.canAfford }
func (b *taskWorkerBudget) CanAffordTask(context.Context, string, float64) bool { return b.canAfford }
func (b *taskWorkerBudget) Record(_ context.Context, taskID, runID string, cost float64) error {
	b.records = append(b.records, budgetRecord{taskID: taskID, runID: runID, cost: cost})
	return nil
}
func (b *taskWorkerBudget) RunSpent(context.Context, string) (float64, error) { return 0, nil }
func (b *taskWorkerBudget) DaySpent(context.Context, string) (float64, error) { return 0, nil }

type taskWorkerBus struct {
	events []event.Event
}

func (b *taskWorkerBus) Emit(e event.Event) error {
	b.events = append(b.events, e)
	return nil
}

type taskWorkerGate struct {
	err  error
	open bool
}

func (g *taskWorkerGate) HasLabel(context.Context, string, string) (bool, error) {
	if g.err != nil {
		return false, g.err
	}
	return g.open, nil
}

func testWorkerDB(t *testing.T) (*sql.DB, *river.Client[*sql.Tx]) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	err = sqlite.MigrateRiver(ctx, db)
	if err != nil {
		t.Fatalf("MigrateRiver: %v", err)
	}
	client, err := river.NewClient(riversqlite.New(db), &river.Config{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return db, client
}

func taskWorkerConfig() config.Config {
	return config.Config{
		Repos: []config.Repo{{Owner: "x", Name: "y", Path: "/repo/y"}},
		Workflows: map[string]config.Workflow{
			"fix": {
				Phases: []config.Phase{
					{Name: "research", Skill: "rpi-research", Model: "sonnet"},
					{Name: "wrap-up", Skill: "wrap-up", Model: "haiku"},
				},
			},
			"gated": {
				Phases: []config.Phase{
					{Name: "research", Skill: "rpi-research", Gate: "approval"},
					{Name: "wrap-up", Skill: "wrap-up"},
				},
			},
			"approval-fix": {
				Phases: []config.Phase{
					{Name: "research", Skill: "rpi-research", Approval: true},
					{Name: "wrap-up", Skill: "wrap-up"},
				},
			},
		},
		Gates:  map[string]string{"approval": "ponko-runner:approved"},
		Budget: config.DefaultBudget(),
	}
}

func taskWorkerArgs(workflow, phase string) worker.TaskPhaseArgs {
	return worker.TaskPhaseArgs{
		IssueURL:    "https://github.com/x/y/issues/1",
		Repo:        "x/y",
		IssueNumber: 1,
		Title:       "Fix issue",
		Labels:      []string{"ponko-runner:ready"},
		Body:        "body",
		Workflow:    workflow,
		Phase:       phase,
	}
}

func taskWorkerJob(args worker.TaskPhaseArgs) *river.Job[worker.TaskPhaseArgs] {
	return &river.Job[worker.TaskPhaseArgs]{
		JobRow: &rivertype.JobRow{ID: 123},
		Args:   args,
	}
}

func taskWorkerContext(ctx context.Context, client *river.Client[*sql.Tx]) context.Context {
	return rivertest.WorkContext(ctx, client)
}

func riverJobCount(t *testing.T, db *sql.DB, kind string) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM river_job WHERE kind = ?", kind).Scan(&count); err != nil {
		t.Fatalf("count river_job kind %s: %v", kind, err)
	}
	return count
}

func TestTaskPhaseWorkerHappyPathInsertsNextPhase(t *testing.T) {
	db, client := testWorkerDB(t)
	rt := &taskWorkerRuntime{result: &runtime.ExecuteResult{CostUSD: 0.42}}
	budget := &taskWorkerBudget{canAfford: true}
	bus := &taskWorkerBus{}
	w := worker.NewTaskPhaseWorker(worker.TaskPhaseWorkerDeps{
		Cfg:     taskWorkerConfig(),
		Budget:  budget,
		Bus:     bus,
		Runtime: rt,
	})

	err := w.Work(taskWorkerContext(context.Background(), client), taskWorkerJob(taskWorkerArgs("fix", "research")))
	if err != nil {
		t.Fatalf("Work: %v", err)
	}

	if riverJobCount(t, db, "task_phase") != 1 {
		t.Fatalf("expected one next task_phase job")
	}
	if len(rt.calls) != 1 {
		t.Fatalf("runtime calls = %d, want 1", len(rt.calls))
	}
	if got := rt.calls[0].Skill; got != "rpi-research" {
		t.Fatalf("runtime skill = %q, want rpi-research", got)
	}
	if len(budget.records) != 1 || budget.records[0].taskID != "https://github.com/x/y/issues/1" {
		t.Fatalf("budget records = %#v, want issue URL task id", budget.records)
	}
	if len(bus.events) == 0 {
		t.Fatalf("expected events")
	}
}

func TestTaskPhaseWorkerBudgetExceededSnoozes(t *testing.T) {
	_, client := testWorkerDB(t)
	rt := &taskWorkerRuntime{result: &runtime.ExecuteResult{CostUSD: 0.42}}
	w := worker.NewTaskPhaseWorker(worker.TaskPhaseWorkerDeps{
		Cfg:     taskWorkerConfig(),
		Budget:  &taskWorkerBudget{canAfford: false},
		Bus:     &taskWorkerBus{},
		Runtime: rt,
	})

	err := w.Work(taskWorkerContext(context.Background(), client), taskWorkerJob(taskWorkerArgs("fix", "research")))
	var snoozeErr *river.JobSnoozeError
	if !errors.As(err, &snoozeErr) {
		t.Fatalf("Work error = %v, want JobSnoozeError", err)
	}
	if len(rt.calls) != 0 {
		t.Fatalf("runtime calls = %d, want 0", len(rt.calls))
	}
}

func TestTaskPhaseWorkerGateBlockedSnoozes(t *testing.T) {
	_, client := testWorkerDB(t)
	rt := &taskWorkerRuntime{result: &runtime.ExecuteResult{CostUSD: 0.42}}
	w := worker.NewTaskPhaseWorker(worker.TaskPhaseWorkerDeps{
		Cfg:         taskWorkerConfig(),
		Budget:      &taskWorkerBudget{canAfford: true},
		Bus:         &taskWorkerBus{},
		Runtime:     rt,
		GateChecker: &taskWorkerGate{open: false},
	})

	err := w.Work(taskWorkerContext(context.Background(), client), taskWorkerJob(taskWorkerArgs("gated", "research")))
	var snoozeErr *river.JobSnoozeError
	if !errors.As(err, &snoozeErr) {
		t.Fatalf("Work error = %v, want JobSnoozeError", err)
	}
	if len(rt.calls) != 0 {
		t.Fatalf("runtime calls = %d, want 0", len(rt.calls))
	}
}

func TestTaskPhaseWorkerLastPhaseCompletesWithoutNextInsert(t *testing.T) {
	db, client := testWorkerDB(t)
	w := worker.NewTaskPhaseWorker(worker.TaskPhaseWorkerDeps{
		Cfg:     taskWorkerConfig(),
		Budget:  &taskWorkerBudget{canAfford: true},
		Bus:     &taskWorkerBus{},
		Runtime: &taskWorkerRuntime{result: &runtime.ExecuteResult{CostUSD: 0.1}},
	})

	err := w.Work(taskWorkerContext(context.Background(), client), taskWorkerJob(taskWorkerArgs("fix", "wrap-up")))
	if err != nil {
		t.Fatalf("Work: %v", err)
	}
	if riverJobCount(t, db, "task_phase") != 0 {
		t.Fatalf("expected no next task_phase jobs")
	}
}

func TestTaskPhaseWorkerExecutionErrorReturnsError(t *testing.T) {
	_, client := testWorkerDB(t)
	execErr := errors.New("execution failed")
	w := worker.NewTaskPhaseWorker(worker.TaskPhaseWorkerDeps{
		Cfg:     taskWorkerConfig(),
		Budget:  &taskWorkerBudget{canAfford: true},
		Bus:     &taskWorkerBus{},
		Runtime: &taskWorkerRuntime{err: execErr},
	})

	err := w.Work(taskWorkerContext(context.Background(), client), taskWorkerJob(taskWorkerArgs("fix", "research")))
	if !errors.Is(err, execErr) {
		t.Fatalf("Work error = %v, want %v", err, execErr)
	}
}

func TestTaskPhaseWorkerApprovalPhaseInsertsPoller(t *testing.T) {
	db, client := testWorkerDB(t)
	commenter := &approvalCommenter{}
	w := worker.NewTaskPhaseWorker(worker.TaskPhaseWorkerDeps{
		Cfg:       taskWorkerConfig(),
		Budget:    &taskWorkerBudget{canAfford: true},
		Bus:       &taskWorkerBus{},
		Runtime:   &taskWorkerRuntime{result: &runtime.ExecuteResult{CostUSD: 0.1}},
		Commenter: commenter,
	})

	err := w.Work(taskWorkerContext(context.Background(), client), taskWorkerJob(taskWorkerArgs("approval-fix", "research")))
	if err != nil {
		t.Fatalf("Work: %v", err)
	}
	if riverJobCount(t, db, "approval_poller") != 1 {
		t.Fatalf("expected one approval_poller job")
	}
	if riverJobCount(t, db, "task_phase") != 0 {
		t.Fatalf("expected no direct task_phase job")
	}
	if len(commenter.comments) != 1 {
		t.Fatalf("comments = %d, want 1", len(commenter.comments))
	}
}
