// Package groom reads groom-consume state files to determine next actions.
package groom

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PipelineState represents the grooming state of an issue.
type PipelineState string

const (
	StatePending    PipelineState = "pending"
	StateEvaluating PipelineState = "evaluating"
	StateGroomed    PipelineState = "groomed"
	StateBlocked    PipelineState = "blocked"
	StateHasGaps    PipelineState = "has_gaps"
)

// IssueState is the parsed state from a groom-consume JSON file.
type IssueState struct {
	Roles    map[string]Role `json:"roles"`
	Pipeline Pipeline        `json:"pipeline"`
	Issue    IssueInfo       `json:"issue"`
}

// IssueInfo holds issue metadata from the state file.
type IssueInfo struct {
	Repo   string `json:"repo"`
	Title  string `json:"title"`
	Number int    `json:"number"`
}

// Pipeline holds the overall pipeline state.
type Pipeline struct {
	State PipelineState `json:"state"`
}

// Role holds per-role evaluation status.
type Role struct {
	Status string `json:"status"`
}

// IsTerminal returns true if the issue needs no more grooming steps.
func (s *IssueState) IsTerminal() bool {
	switch s.Pipeline.State {
	case StateGroomed, StateBlocked, StateHasGaps:
		return true
	default:
		return false
	}
}

// NeedsSynthesis returns true if all roles have completed evaluation
// and the pipeline is ready for synthesis.
func (s *IssueState) NeedsSynthesis() bool {
	if s.Pipeline.State != StateEvaluating || len(s.Roles) == 0 {
		return false
	}
	for _, r := range s.Roles {
		if r.Status == "complete" || r.Status == "skipped" {
			continue
		}
		return false
	}
	return true
}

// ReadIssueStates reads all groom-consume state files from the repo's .context directory.
func ReadIssueStates(repoPath string) ([]IssueState, error) {
	dir := filepath.Join(repoPath, ".context", "groom-consume")

	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read groom state dir: %w", err)
	}

	var states []IssueState
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read state file %s: %w", e.Name(), err)
		}
		var s IssueState
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("parse state file %s: %w", e.Name(), err)
		}
		states = append(states, s)
	}

	return states, nil
}

// NextActionableIssue returns the first non-terminal issue, or nil if all are done.
func NextActionableIssue(states []IssueState) *IssueState {
	for i := range states {
		if !states[i].IsTerminal() {
			return &states[i]
		}
	}
	return nil
}

// BatchCursor is the parsed state from a batch cursor file (_batch-*.json).
type BatchCursor struct {
	Project      string      `json:"project"`
	Items        []BatchItem `json:"items"`
	CurrentIndex int         `json:"currentIndex"`
}

// BatchItem is one issue in the batch cursor.
type BatchItem struct {
	Repo          string `json:"repo"`
	ConsumeStatus string `json:"consumeStatus"`
	Issue         int    `json:"issue"`
}

// HasRemainingItems returns true if the batch has unconsumed items.
func (b *BatchCursor) HasRemainingItems() bool {
	for _, item := range b.Items {
		if item.ConsumeStatus == "pending" {
			return true
		}
	}
	return false
}

// ReadBatchCursor reads the batch cursor file for a project, if it exists.
func ReadBatchCursor(repoPath, projectName string) (*BatchCursor, error) {
	slug := strings.ToLower(strings.ReplaceAll(projectName, " ", "-"))
	path := filepath.Join(repoPath, ".context", "groom-consume", "_batch-"+slug+".json")

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read batch cursor: %w", err)
	}

	var cursor BatchCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return nil, fmt.Errorf("parse batch cursor: %w", err)
	}
	return &cursor, nil
}
