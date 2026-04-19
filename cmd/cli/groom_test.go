package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bryanneva/ponko/internal/budget"
	"github.com/bryanneva/ponko/internal/config"
	"github.com/bryanneva/ponko/internal/event"
	"github.com/bryanneva/ponko/internal/runtime"
)

// mockRuntime records whether Execute was called.
type mockRuntime struct {
	called bool
}

func (m *mockRuntime) Execute(_ context.Context, _ runtime.ExecuteRequest) (*runtime.ExecuteResult, error) {
	m.called = true
	return &runtime.ExecuteResult{}, nil
}

// blockingRuntime blocks until the context is cancelled.
type blockingRuntime struct{}

func (b *blockingRuntime) Execute(ctx context.Context, _ runtime.ExecuteRequest) (*runtime.ExecuteResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// nopBus discards events.
type nopBus struct{}

func (nopBus) Emit(_ event.Event) error { return nil }

// nopBudget accepts all spending.
type nopBudget struct{}

func (nopBudget) CanAffordRun(_ context.Context, _ string, _ float64) bool  { return true }
func (nopBudget) CanAffordDay(_ context.Context, _ float64) bool             { return true }
func (nopBudget) CanAffordTask(_ context.Context, _ string, _ float64) bool  { return true }
func (nopBudget) Record(_ context.Context, _, _ string, _ float64) error     { return nil }
func (nopBudget) RunSpent(_ context.Context, _ string) (float64, error)      { return 0, nil }
func (nopBudget) DaySpent(_ context.Context, _ string) (float64, error)      { return 0, nil }

var _ budget.Controller = nopBudget{}

func TestGroomProject_DryRunSkipsExecute(t *testing.T) {
	repoPath := t.TempDir() // empty dir → no state files → batch intake path

	cfg := &config.Config{
		Repos: []config.Repo{
			{Owner: "acme", Name: "myapp", Path: repoPath},
		},
		Groom: config.GroomConfig{
			DefaultModel:   "haiku",
			SynthesisModel: "sonnet",
			MaxBudgetUSD:   1.0,
		},
	}
	proj := config.GroomProject{Name: "My Project", Repo: "myapp"}
	rt := &mockRuntime{}
	deps := jobDeps{
		bus:    nopBus{},
		budget: nopBudget{},
		runID:  "test-run",
		dryRun: true,
	}

	if err := groomProject(context.Background(), cfg, proj, rt, deps); err != nil {
		t.Fatalf("groomProject: %v", err)
	}
	if rt.called {
		t.Error("expected rt.Execute to NOT be called in dry-run mode, but it was")
	}
}

func TestGroomProject_TimeoutKillsHungStep(t *testing.T) {
	repoPath := t.TempDir() // empty dir → no state files → batch intake path

	cfg := &config.Config{
		Repos: []config.Repo{
			{Owner: "acme", Name: "myapp", Path: repoPath},
		},
		Groom: config.GroomConfig{
			DefaultModel:   "haiku",
			SynthesisModel: "sonnet",
			MaxBudgetUSD:   1.0,
			StepTimeout:    config.Duration(10 * time.Millisecond),
		},
	}
	proj := config.GroomProject{Name: "My Project", Repo: "myapp"}
	rt := &blockingRuntime{}
	deps := jobDeps{
		bus:    nopBus{},
		budget: nopBudget{},
		runID:  "test-run",
	}

	err := groomProject(context.Background(), cfg, proj, rt, deps)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded in error chain, got: %v", err)
	}
}
