// Package event defines the event types and the Bus interface for the ponko-runner audit trail.
package event

import "time"

// EventType identifies what happened.
type EventType string

const (
	TaskCreated    EventType = "task.created"
	TaskStarted    EventType = "task.started"
	PhaseStarted   EventType = "phase.started"
	PhaseCompleted EventType = "phase.completed"
	TaskBlocked    EventType = "task.blocked"
	TaskCompleted  EventType = "task.completed"
	TaskFailed     EventType = "task.failed"
	TaskCancelled  EventType = "task.cancelled"
	BudgetExceeded EventType = "budget.exceeded"

	// Job events track groom (and future automated task) executions.
	// job_type field distinguishes the source; currently only "groom".
	JobStarted   EventType = "job.started"
	JobCompleted EventType = "job.completed"
	JobFailed    EventType = "job.failed"
)

// Event is a structured audit log entry.
type Event struct {
	Timestamp     time.Time      `json:"timestamp"`
	Payload       map[string]any `json:"payload,omitempty"`
	Type          EventType      `json:"type"`
	TaskID        string         `json:"task_id"`
	CorrelationID string         `json:"correlation_id"`
}

// Bus appends events to the audit log.
type Bus interface {
	Emit(e Event) error
}
