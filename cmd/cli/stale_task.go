package main

import (
	"context"
	"log"
)

// StaleIssueCleanupTask is a stub AutomatedTask that will eventually identify
// GitHub issues stuck in the evaluating state for too long and reset them to pending.
// Currently a no-op placeholder to verify the scheduler's multi-task wiring.
type StaleIssueCleanupTask struct{}

func (s *StaleIssueCleanupTask) Name() string { return "stale-issue-cleanup" }

func (s *StaleIssueCleanupTask) Run(_ context.Context) error {
	log.Printf("stale-issue-cleanup: not yet implemented, skipping")
	return nil
}
