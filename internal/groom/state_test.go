package groom_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bryanneva/ponko/internal/groom"
)

func writeStateFile(t *testing.T, dir string, filename string, state groom.IssueState) {
	t.Helper()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), data, 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func issue(number int) groom.IssueInfo {
	return groom.IssueInfo{Number: number}
}

func TestIsTerminal(t *testing.T) {
	tests := []struct {
		state    groom.PipelineState
		terminal bool
	}{
		{groom.StatePending, false},
		{groom.StateEvaluating, false},
		{groom.StateGroomed, true},
		{groom.StateBlocked, true},
		{groom.StateHasGaps, true},
	}
	for _, tt := range tests {
		s := groom.IssueState{Pipeline: groom.Pipeline{State: tt.state}}
		if got := s.IsTerminal(); got != tt.terminal {
			t.Errorf("state %q: IsTerminal() = %v, want %v", tt.state, got, tt.terminal)
		}
	}
}

func TestNeedsSynthesis(t *testing.T) {
	s := groom.IssueState{
		Pipeline: groom.Pipeline{State: groom.StateEvaluating},
		Roles: map[string]groom.Role{
			"pm":          {Status: "complete"},
			"engineering": {Status: "complete"},
			"design":      {Status: "complete"},
		},
	}
	if !s.NeedsSynthesis() {
		t.Error("expected NeedsSynthesis=true when all roles complete")
	}

	s.Roles["pm"] = groom.Role{Status: "pending"}
	if s.NeedsSynthesis() {
		t.Error("expected NeedsSynthesis=false when a role is pending")
	}

	s.Pipeline.State = groom.StateGroomed
	s.Roles["pm"] = groom.Role{Status: "complete"}
	if s.NeedsSynthesis() {
		t.Error("expected NeedsSynthesis=false when pipeline is terminal")
	}
}

func TestNeedsSynthesis_SkippedRoles(t *testing.T) {
	s := groom.IssueState{
		Pipeline: groom.Pipeline{State: groom.StateEvaluating},
		Roles: map[string]groom.Role{
			"pm":          {Status: "complete"},
			"engineering": {Status: "complete"},
			"design":      {Status: "skipped"},
		},
	}
	if !s.NeedsSynthesis() {
		t.Error("expected NeedsSynthesis=true when non-skipped roles are complete")
	}
}

func TestNeedsSynthesis_EmptyRoles(t *testing.T) {
	s := groom.IssueState{
		Pipeline: groom.Pipeline{State: groom.StateEvaluating},
		Roles:    map[string]groom.Role{},
	}
	if s.NeedsSynthesis() {
		t.Error("expected NeedsSynthesis=false with empty roles")
	}
}

func TestReadIssueStates(t *testing.T) {
	repoPath := t.TempDir()
	dir := filepath.Join(repoPath, ".context", "groom-consume")

	writeStateFile(t, dir, "42.json", groom.IssueState{
		Issue:    issue(42),
		Pipeline: groom.Pipeline{State: groom.StatePending},
	})
	writeStateFile(t, dir, "43.json", groom.IssueState{
		Issue:    issue(43),
		Pipeline: groom.Pipeline{State: groom.StateGroomed},
	})

	states, err := groom.ReadIssueStates(repoPath)
	if err != nil {
		t.Fatalf("ReadIssueStates: %v", err)
	}
	if len(states) != 2 {
		t.Fatalf("expected 2 states, got %d", len(states))
	}
}

func TestReadIssueStates_NoDir(t *testing.T) {
	states, err := groom.ReadIssueStates(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(states) != 0 {
		t.Errorf("expected 0 states for missing dir, got %d", len(states))
	}
}

func TestNextActionableIssue(t *testing.T) {
	states := []groom.IssueState{
		{Issue: issue(1), Pipeline: groom.Pipeline{State: groom.StateGroomed}},
		{Issue: issue(2), Pipeline: groom.Pipeline{State: groom.StateEvaluating}},
		{Issue: issue(3), Pipeline: groom.Pipeline{State: groom.StatePending}},
	}

	next := groom.NextActionableIssue(states)
	if next == nil {
		t.Fatal("expected non-nil")
	}
	if next.Issue.Number != 2 {
		t.Errorf("expected issue 2, got %d", next.Issue.Number)
	}
}

func TestNextActionableIssue_AllTerminal(t *testing.T) {
	states := []groom.IssueState{
		{Issue: issue(1), Pipeline: groom.Pipeline{State: groom.StateGroomed}},
		{Issue: issue(2), Pipeline: groom.Pipeline{State: groom.StateBlocked}},
	}
	if next := groom.NextActionableIssue(states); next != nil {
		t.Errorf("expected nil, got issue %d", next.Issue.Number)
	}
}

func TestReadBatchCursor(t *testing.T) {
	repoPath := t.TempDir()
	dir := filepath.Join(repoPath, ".context", "groom-consume")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	cursor := groom.BatchCursor{
		Project: "Daily Driver",
		Items: []groom.BatchItem{
			{Repo: "acme/myapp", Issue: 1, ConsumeStatus: "done"},
			{Repo: "acme/myapp", Issue: 2, ConsumeStatus: "pending"},
		},
		CurrentIndex: 1,
	}
	data, _ := json.Marshal(cursor)
	if err := os.WriteFile(filepath.Join(dir, "_batch-daily-driver.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	got, err := groom.ReadBatchCursor(repoPath, "Daily Driver")
	if err != nil {
		t.Fatalf("ReadBatchCursor: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil cursor")
	}
	if len(got.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(got.Items))
	}
	if !got.HasRemainingItems() {
		t.Error("expected HasRemainingItems=true")
	}
}

func TestReadBatchCursor_NoneExists(t *testing.T) {
	got, err := groom.ReadBatchCursor(t.TempDir(), "Nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil cursor for missing file")
	}
}

func TestBatchCursor_NoRemainingItems(t *testing.T) {
	cursor := groom.BatchCursor{
		Items: []groom.BatchItem{
			{Issue: 1, ConsumeStatus: "done"},
			{Issue: 2, ConsumeStatus: "done"},
		},
	}
	if cursor.HasRemainingItems() {
		t.Error("expected HasRemainingItems=false when all done")
	}
}

func TestReadIssueStates_SkipsBatchFiles(t *testing.T) {
	repoPath := t.TempDir()
	dir := filepath.Join(repoPath, ".context", "groom-consume")

	writeStateFile(t, dir, "_batch-daily-driver.json", groom.IssueState{
		Issue:    issue(0),
		Pipeline: groom.Pipeline{State: ""},
	})
	writeStateFile(t, dir, "42.json", groom.IssueState{
		Issue:    issue(42),
		Pipeline: groom.Pipeline{State: groom.StatePending},
	})

	states, err := groom.ReadIssueStates(repoPath)
	if err != nil {
		t.Fatalf("ReadIssueStates: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("expected 1 state (batch file skipped), got %d", len(states))
	}
	if states[0].Issue.Number != 42 {
		t.Errorf("expected issue 42, got %d", states[0].Issue.Number)
	}
}
