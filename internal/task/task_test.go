package task_test

import (
	"testing"
	"time"

	"github.com/bryanneva/ponko/internal/task"
)

// Re-export for convenience in this test file.
type Status = task.Status
type Task = task.Task

const (
	StatusQueued           = task.StatusQueued
	StatusInProgress       = task.StatusInProgress
	StatusBlocked          = task.StatusBlocked
	StatusAwaitingApproval = task.StatusAwaitingApproval
	StatusCompleted        = task.StatusCompleted
	StatusFailed           = task.StatusFailed
	StatusCancelled        = task.StatusCancelled
)

func TestStatusTransitions_Valid(t *testing.T) {
	validTransitions := []struct {
		from Status
		to   Status
	}{
		{StatusQueued, StatusInProgress},
		{StatusInProgress, StatusBlocked},
		{StatusInProgress, StatusCompleted},
		{StatusInProgress, StatusFailed},
		{StatusBlocked, StatusInProgress},
		{StatusBlocked, StatusFailed},
		{StatusInProgress, StatusAwaitingApproval},
		{StatusAwaitingApproval, StatusInProgress},
		{StatusAwaitingApproval, StatusCancelled},
	}

	for _, tt := range validTransitions {
		if !tt.from.CanTransitionTo(tt.to) {
			t.Errorf("expected %s→%s to be valid", tt.from, tt.to)
		}
	}
}

func TestStatusTransitions_Invalid(t *testing.T) {
	invalidTransitions := []struct {
		from Status
		to   Status
	}{
		{StatusQueued, StatusCompleted},
		{StatusCompleted, StatusQueued},
		{StatusFailed, StatusInProgress},
	}

	for _, tt := range invalidTransitions {
		if tt.from.CanTransitionTo(tt.to) {
			t.Errorf("expected %s→%s to be invalid", tt.from, tt.to)
		}
	}
}

func TestTaskTransitionTo_Valid(t *testing.T) {
	task := &Task{
		Status:    StatusQueued,
		UpdatedAt: time.Now().Add(-time.Minute),
	}
	before := task.UpdatedAt

	if err := task.TransitionTo(StatusInProgress); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if task.Status != StatusInProgress {
		t.Errorf("expected status %s, got %s", StatusInProgress, task.Status)
	}
	if !task.UpdatedAt.After(before) {
		t.Error("expected UpdatedAt to be updated")
	}
}

func TestTaskTransitionTo_Invalid(t *testing.T) {
	task := &Task{Status: StatusQueued}
	if err := task.TransitionTo(StatusCompleted); err == nil {
		t.Error("expected error for invalid transition queued→completed")
	}
	if task.Status != StatusQueued {
		t.Error("status should not change on invalid transition")
	}
}
