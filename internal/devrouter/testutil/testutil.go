// Package testutil provides test helpers for the devrouter package.
package testutil

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/bryanneva/ponko/internal/approval"
	"github.com/bryanneva/ponko/internal/devrouter"
	"github.com/bryanneva/ponko/internal/event"
	"github.com/bryanneva/ponko/internal/runtime"
)

// MockExecutor is a test double for exec.CommandExecutor.
// If Responses is set, calls consume it in order (with Output as fallback).
type MockExecutor struct {
	Output    []byte
	Err       error
	Responses [][]byte   // per-call outputs consumed in order
	Calls     [][]string // each element is [name, args...]
	mu        sync.Mutex
	callIdx   int
}

func (m *MockExecutor) Run(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	call := append([]string{name}, args...)
	m.Calls = append(m.Calls, call)
	if m.Err != nil {
		return nil, m.Err
	}
	if len(m.Responses) > 0 && m.callIdx < len(m.Responses) {
		out := m.Responses[m.callIdx]
		m.callIdx++
		return out, nil
	}
	return m.Output, nil
}

// NoOpCommenter records whether it was called but does nothing else.
type NoOpCommenter struct {
	called bool
}

func (c *NoOpCommenter) Comment(_ context.Context, _, _ string) error {
	c.called = true
	return nil
}

func (c *NoOpCommenter) WasCalled() bool { return c.called }

// MockCIChecker returns a fixed CI status.
type MockCIChecker struct {
	Status string
}

func (m *MockCIChecker) CheckCI(_ context.Context, _, _ string) (string, error) {
	return m.Status, nil
}

// MockApprovalChecker returns a fixed approval status.
type MockApprovalChecker struct {
	Status approval.Status
}

func (m *MockApprovalChecker) CheckApproval(_ context.Context, _ string) (approval.Status, error) {
	return m.Status, nil
}

// AlwaysAffordBudget is a mock budget.Controller that always approves spending.
type AlwaysAffordBudget struct{}

func (a *AlwaysAffordBudget) CanAffordRun(_ context.Context, _ string, _ float64) bool  { return true }
func (a *AlwaysAffordBudget) CanAffordDay(_ context.Context, _ float64) bool            { return true }
func (a *AlwaysAffordBudget) CanAffordTask(_ context.Context, _ string, _ float64) bool { return true }
func (a *AlwaysAffordBudget) Record(_ context.Context, _, _ string, _ float64) error    { return nil }
func (a *AlwaysAffordBudget) RunSpent(_ context.Context, _ string) (float64, error)     { return 0, nil }
func (a *AlwaysAffordBudget) DaySpent(_ context.Context, _ string) (float64, error)     { return 0, nil }

// MockRuntime is a test double for runtime.AgentRuntime.
type MockRuntime struct {
	Err     error
	Output  string
	Calls   []runtime.ExecuteRequest
	CostUSD float64
	mu      sync.Mutex
}

func (m *MockRuntime) Execute(_ context.Context, req runtime.ExecuteRequest) (*runtime.ExecuteResult, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, req)
	m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	return &runtime.ExecuteResult{Output: m.Output, CostUSD: m.CostUSD}, nil
}

// DiscardBus silently drops all events.
type DiscardBus struct{}

func (d *DiscardBus) Emit(_ event.Event) error { return nil }

// MemoryPipelineStore is an in-memory PipelineStore for tests.
type MemoryPipelineStore struct {
	pipelines map[string]*devrouter.Pipeline
	mu        sync.Mutex
}

// NewMemoryPipelineStore returns an empty in-memory store.
func NewMemoryPipelineStore() *MemoryPipelineStore {
	return &MemoryPipelineStore{pipelines: make(map[string]*devrouter.Pipeline)}
}

func (s *MemoryPipelineStore) clone(p *devrouter.Pipeline) *devrouter.Pipeline {
	cp := *p
	return &cp
}

func (s *MemoryPipelineStore) Create(_ context.Context, p *devrouter.Pipeline) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	s.pipelines[p.ID] = s.clone(p)
	return nil
}

func (s *MemoryPipelineStore) Get(_ context.Context, id string) (*devrouter.Pipeline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pipelines[id]
	if !ok {
		return nil, nil
	}
	return s.clone(p), nil
}

func (s *MemoryPipelineStore) UpdateStage(_ context.Context, id string, from, to devrouter.Stage) error {
	if err := devrouter.ValidateTransition(from, to); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pipelines[id]
	if !ok {
		return fmt.Errorf("pipeline %s not found", id)
	}
	p.Stage = to
	p.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *MemoryPipelineStore) SetTrack(_ context.Context, id string, track devrouter.Track) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pipelines[id]
	if !ok {
		return fmt.Errorf("pipeline %s not found", id)
	}
	p.Track = track
	p.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *MemoryPipelineStore) SetClassificationRationale(_ context.Context, id string, rationale string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pipelines[id]
	if !ok {
		return fmt.Errorf("pipeline %s not found", id)
	}
	p.ClassificationRationale = rationale
	p.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *MemoryPipelineStore) SetPlanOutput(_ context.Context, id string, planOutput string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pipelines[id]
	if !ok {
		return fmt.Errorf("pipeline %s not found", id)
	}
	p.PlanOutput = planOutput
	p.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *MemoryPipelineStore) SetStoryCount(_ context.Context, id string, count int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pipelines[id]
	if !ok {
		return fmt.Errorf("pipeline %s not found", id)
	}
	p.StoryCount = count
	p.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *MemoryPipelineStore) IncrStoriesCompleted(_ context.Context, id string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pipelines[id]
	if !ok {
		return 0, fmt.Errorf("pipeline %s not found", id)
	}
	p.StoriesCompleted++
	p.UpdatedAt = time.Now().UTC()
	return p.StoriesCompleted, nil
}

func (s *MemoryPipelineStore) SetCurrentStoryIndex(_ context.Context, id string, index int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pipelines[id]
	if !ok {
		return fmt.Errorf("pipeline %s not found", id)
	}
	p.CurrentStoryIndex = index
	p.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *MemoryPipelineStore) SetPRNumber(_ context.Context, id string, prNumber int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pipelines[id]
	if !ok {
		return fmt.Errorf("pipeline %s not found", id)
	}
	p.PRNumber = prNumber
	p.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *MemoryPipelineStore) AddCost(_ context.Context, id string, delta float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pipelines[id]
	if !ok {
		return fmt.Errorf("pipeline %s not found", id)
	}
	p.CostUSD += delta
	p.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *MemoryPipelineStore) GetByIssueURL(_ context.Context, issueURL string) (*devrouter.Pipeline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.pipelines {
		if p.IssueURL == issueURL {
			return s.clone(p), nil
		}
	}
	return nil, nil
}

func (s *MemoryPipelineStore) SetIssueContent(_ context.Context, id, title, body string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pipelines[id]
	if !ok {
		return fmt.Errorf("pipeline %s not found", id)
	}
	p.IssueTitle = title
	p.IssueBody = body
	p.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *MemoryPipelineStore) List(_ context.Context, limit int) ([]*devrouter.Pipeline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]*devrouter.Pipeline, 0, len(s.pipelines))
	for _, p := range s.pipelines {
		result = append(result, s.clone(p))
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}
