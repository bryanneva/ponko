// Package runtime defines the AgentRuntime interface and adapters.
package runtime

import "context"

// ExecuteRequest carries everything the agent needs to do a phase of work.
type ExecuteRequest struct {
	WorkingDir    string
	Skill         string
	IssueTitle    string
	IssueBody     string
	IssueURL      string
	PreviousError string
	Model         string
	Provider      string
	Prompt        string
	MaxBudgetUSD  float64
	MaxTurns      int
}

// ExecuteResult is what the agent returns after completing (or failing) a phase.
type ExecuteResult struct {
	Output       string
	CostUSD      float64
	ExitCode     int
	DurationSecs float64
}

// AgentRuntime runs a skill against a repository and returns the result.
type AgentRuntime interface {
	Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResult, error)
}
