package task

import "context"

// Store defines the persistence interface for tasks.
type Store interface {
	// Create persists a new task. Auto-generates ID if empty and sets CreatedAt/UpdatedAt.
	Create(ctx context.Context, t *Task) error

	// Get returns the task with the given ID, or nil if not found.
	Get(ctx context.Context, id string) (*Task, error)

	// GetByIssueURL returns the task for the given GitHub issue URL, or nil if not found.
	GetByIssueURL(ctx context.Context, issueURL string) (*Task, error)

	// ListByStatus returns tasks whose status matches any of the given statuses,
	// ordered by created_at ASC.
	ListByStatus(ctx context.Context, statuses ...Status) ([]*Task, error)

	// UpdateStatus updates the task's status and optionally sets last_error (pass "" to clear).
	UpdateStatus(ctx context.Context, id string, status Status, lastError string) error

	// UpdatePhase sets the current phase name for the task.
	UpdatePhase(ctx context.Context, id string, phase string) error

	// IncrAttempts atomically increments the task's attempts counter by one.
	IncrAttempts(ctx context.Context, id string) error

	// AddCost atomically increments the task's cost_usd by delta.
	AddCost(ctx context.Context, id string, delta float64) error

	// CountActive returns the number of tasks in queued, in_progress, or blocked state.
	CountActive(ctx context.Context) (int, error)
}
