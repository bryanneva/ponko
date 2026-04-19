package scheduler

import (
	"context"
	"log"
	"sync"
	"time"
)

// Scheduler runs registered AutomatedTasks on a fixed interval with backpressure.
// Each tick runs all tasks serially. If a tick fires while tasks are running,
// it is dropped — the next cycle begins only after all tasks complete.
type Scheduler struct {
	startedAt time.Time
	states    map[string]*taskState
	tasks     []AutomatedTask
	interval  time.Duration
	mu        sync.RWMutex
}

type taskState struct {
	lastRunAt *time.Time
	lastError string
	running   bool
}

// New creates a Scheduler with the given interval and tasks.
func New(interval time.Duration, tasks []AutomatedTask) *Scheduler {
	states := make(map[string]*taskState, len(tasks))
	for _, t := range tasks {
		states[t.Name()] = &taskState{}
	}
	return &Scheduler{
		interval: interval,
		tasks:    tasks,
		states:   states,
	}
}

// Start runs the scheduler loop until ctx is cancelled. It blocks, so callers
// should invoke it in a goroutine.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	s.startedAt = time.Now()
	s.mu.Unlock()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runAll(ctx)
		}
	}
}

func (s *Scheduler) runAll(ctx context.Context) {
	for _, t := range s.tasks {
		if ctx.Err() != nil {
			return
		}

		s.mu.Lock()
		s.states[t.Name()].running = true
		s.mu.Unlock()

		err := t.Run(ctx)
		now := time.Now()

		s.mu.Lock()
		st := s.states[t.Name()]
		st.running = false
		st.lastRunAt = &now
		if err != nil {
			st.lastError = err.Error()
		} else {
			st.lastError = ""
		}
		s.mu.Unlock()

		if err != nil {
			log.Printf("scheduler: task %q failed: %v", t.Name(), err)
		}
	}
}

// Statuses returns a snapshot of all task statuses.
func (s *Scheduler) Statuses() []TaskStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]TaskStatus, 0, len(s.tasks))
	for _, t := range s.tasks {
		st := s.states[t.Name()]
		nextRunAt := s.startedAt.Add(s.interval)
		if st.lastRunAt != nil {
			nextRunAt = st.lastRunAt.Add(s.interval)
		}
		out = append(out, TaskStatus{
			Name:      t.Name(),
			LastRunAt: st.lastRunAt,
			NextRunAt: nextRunAt,
			Running:   st.running,
			LastError: st.lastError,
		})
	}
	return out
}
