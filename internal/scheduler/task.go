// Package scheduler runs AutomatedTasks on a fixed interval with backpressure.
package scheduler

import (
	"context"
	"time"
)

// AutomatedTask is a unit of work the scheduler can run on a recurring basis.
type AutomatedTask interface {
	// Name returns the task identifier used in logs and observability.
	Name() string
	// Run executes one cycle of the task. It must respect ctx cancellation.
	Run(ctx context.Context) error
}

// TaskStatus is a snapshot of a single task's scheduler state.
type TaskStatus struct {
	NextRunAt time.Time  `json:"next_run_at"`
	LastRunAt *time.Time `json:"last_run_at,omitempty"`
	Name      string     `json:"name"`
	LastError string     `json:"last_error,omitempty"`
	Running   bool       `json:"running"`
}
