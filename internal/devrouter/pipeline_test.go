package devrouter_test

import (
	"testing"

	"github.com/bryanneva/ponko/internal/devrouter"
)

func TestValidateTransition_ValidEdges(t *testing.T) {
	cases := []struct {
		from devrouter.Stage
		to   devrouter.Stage
	}{
		{devrouter.StagePending, devrouter.StageClassifying},
		{devrouter.StageClassifying, devrouter.StagePlanning},
		{devrouter.StagePlanning, devrouter.StageFanout},
		{devrouter.StageFanout, devrouter.StageExecuting},
		{devrouter.StageExecuting, devrouter.StageValidating},
		{devrouter.StageExecuting, devrouter.StageFailed},
		{devrouter.StageValidating, devrouter.StageAwaitingApproval},
		{devrouter.StageValidating, devrouter.StageFailed},
		{devrouter.StageAwaitingApproval, devrouter.StageCompleted},
		{devrouter.StageAwaitingApproval, devrouter.StageFailed},
	}

	for _, tc := range cases {
		if err := devrouter.ValidateTransition(tc.from, tc.to); err != nil {
			t.Errorf("expected valid transition %s → %s, got error: %v", tc.from, tc.to, err)
		}
	}
}

func TestValidateTransition_InvalidEdges(t *testing.T) {
	cases := []struct {
		from devrouter.Stage
		to   devrouter.Stage
	}{
		{devrouter.StagePending, devrouter.StageCompleted},
		{devrouter.StagePending, devrouter.StageExecuting},
		{devrouter.StageCompleted, devrouter.StagePending},
		{devrouter.StageFailed, devrouter.StagePending},
		{devrouter.StageClassifying, devrouter.StageExecuting},
		{devrouter.StagePlanning, devrouter.StageCompleted},
	}

	for _, tc := range cases {
		if err := devrouter.ValidateTransition(tc.from, tc.to); err == nil {
			t.Errorf("expected error for invalid transition %s → %s, got nil", tc.from, tc.to)
		}
	}
}

func TestStageString(t *testing.T) {
	if devrouter.StagePending.String() != "pending" {
		t.Errorf("expected 'pending', got %q", devrouter.StagePending.String())
	}
	if devrouter.StageCompleted.String() != "completed" {
		t.Errorf("expected 'completed', got %q", devrouter.StageCompleted.String())
	}
}

func TestTrackConstants(t *testing.T) {
	tracks := []devrouter.Track{devrouter.TrackFix, devrouter.TrackRPI, devrouter.TrackRalph}
	if len(tracks) != 3 {
		t.Errorf("expected 3 tracks")
	}
}
