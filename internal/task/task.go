// Package task manages task lifecycle and state machine.
package task

import (
	"fmt"
	"time"
)

// Status represents the lifecycle state of a task.
type Status string

const (
	StatusQueued           Status = "queued"
	StatusInProgress       Status = "in_progress"
	StatusBlocked          Status = "blocked"
	StatusAwaitingApproval Status = "awaiting_approval"
	StatusCompleted        Status = "completed"
	StatusFailed           Status = "failed"
	StatusCancelled        Status = "cancelled"
)

func (s Status) String() string { return string(s) }

// validTransitions defines allowed state machine edges.
var validTransitions = map[Status][]Status{
	StatusQueued:           {StatusInProgress},
	StatusInProgress:       {StatusBlocked, StatusAwaitingApproval, StatusCompleted, StatusFailed},
	StatusAwaitingApproval: {StatusInProgress, StatusCancelled},
	StatusBlocked:          {StatusInProgress, StatusFailed},
	StatusCompleted:        {},
	StatusFailed:           {},
	StatusCancelled:        {},
}

// CanTransitionTo returns true if the transition from s to target is valid.
func (s Status) CanTransitionTo(target Status) bool {
	for _, allowed := range validTransitions[s] {
		if allowed == target {
			return true
		}
	}
	return false
}

// Task represents a unit of work tracked by the orchestrator.
type Task struct {
	UpdatedAt   time.Time
	CreatedAt   time.Time
	LockedAt    *time.Time
	Status      Status
	BlockReason string
	IssueURL    string
	Body        string
	Workflow    string
	ID          string
	Phase       string
	Title       string
	Repo        string
	LastError   string
	LockedBy    string
	Labels      []string
	CostUSD     float64
	IssueNumber int
	Attempts    int
}

// TransitionTo moves the task to the given status, returning an error if the
// transition is invalid. On success, UpdatedAt is set to now.
func (t *Task) TransitionTo(target Status) error {
	if !t.Status.CanTransitionTo(target) {
		return fmt.Errorf("invalid transition: %s → %s", t.Status, target)
	}
	t.Status = target
	t.UpdatedAt = time.Now()
	return nil
}
