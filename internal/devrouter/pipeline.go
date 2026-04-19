// Package devrouter implements the autonomous issue execution pipeline.
package devrouter

import (
	"fmt"
	"time"
)

// Stage represents the lifecycle state of a pipeline.
type Stage string

const (
	StagePending          Stage = "pending"
	StageClassifying      Stage = "classifying"
	StagePlanning         Stage = "planning"
	StageFanout           Stage = "fanout"
	StageExecuting        Stage = "executing"
	StageValidating       Stage = "validating"
	StageAwaitingApproval Stage = "awaiting_approval"
	StageCompleted        Stage = "completed"
	StageFailed           Stage = "failed"
)

func (s Stage) String() string { return string(s) }

// Track represents which execution track a pipeline follows.
type Track string

const (
	TrackFix   Track = "fix"
	TrackRPI   Track = "rpi"
	TrackRalph Track = "ralph"
)

func (t Track) String() string { return string(t) }

// validStageTransitions defines allowed pipeline stage edges.
var validStageTransitions = map[Stage][]Stage{
	StagePending:          {StageClassifying},
	StageClassifying:      {StagePlanning},
	StagePlanning:         {StageFanout},
	StageFanout:           {StageExecuting},
	StageExecuting:        {StageValidating, StageFailed},
	StageValidating:       {StageAwaitingApproval, StageFailed},
	StageAwaitingApproval: {StageCompleted, StageFailed},
	StageCompleted:        {},
	StageFailed:           {},
}

// ValidateTransition returns an error if the transition from → to is not a valid pipeline edge.
func ValidateTransition(from, to Stage) error {
	allowed, ok := validStageTransitions[from]
	if !ok {
		return fmt.Errorf("unknown stage: %s", from)
	}
	for _, s := range allowed {
		if s == to {
			return nil
		}
	}
	return fmt.Errorf("invalid pipeline transition: %s → %s", from, to)
}

// Story is a single unit of work within a pipeline plan.
type Story struct {
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	SuccessCriteria []string `json:"success_criteria"`
}

// Pipeline tracks the full lifecycle of autonomous issue execution.
type Pipeline struct {
	UpdatedAt               time.Time
	CreatedAt               time.Time
	ClassificationRationale string
	TaskID                  string
	IssueURL                string
	Repo                    string
	ID                      string
	IssueTitle              string
	IssueBody               string
	Track                   Track
	Stage                   Stage
	PlanOutput              string
	IssueNumber             int
	PRNumber                int
	CurrentStoryIndex       int
	CostUSD                 float64
	StoriesCompleted        int
	StoryCount              int
}
