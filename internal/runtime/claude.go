package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/bryanneva/ponko/internal/exec"
)

// ClaudeRuntime implements AgentRuntime by invoking the Claude Code CLI.
type ClaudeRuntime struct {
	ex exec.CommandExecutor
}

// NewClaudeRuntime creates a ClaudeRuntime that uses ex to invoke the CLI.
func NewClaudeRuntime(ex exec.CommandExecutor) *ClaudeRuntime {
	return &ClaudeRuntime{ex: ex}
}

// Execute builds a prompt from the request, invokes claude CLI, and parses the result.
func (r *ClaudeRuntime) Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	prompt := req.Prompt
	if prompt == "" {
		prompt = buildPrompt(req)
	}

	args := []string{
		"-p", prompt,
		"--output-format", "json",
		"--permission-mode", "bypassPermissions",
		"--max-budget-usd", strconv.FormatFloat(req.MaxBudgetUSD, 'f', 2, 64),
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(req.MaxTurns))
	}

	start := time.Now()
	out, err := r.ex.Run(ctx, req.WorkingDir, "claude", args...)
	elapsed := time.Since(start).Seconds()

	exitCode := 0
	if err != nil {
		exitCode = 1
		return &ExecuteResult{
			Output:       string(out),
			ExitCode:     exitCode,
			DurationSecs: elapsed,
		}, fmt.Errorf("claude exited with error: %w", err)
	}

	costUSD := parseCost(out)
	return &ExecuteResult{
		Output:       string(out),
		CostUSD:      costUSD,
		ExitCode:     0,
		DurationSecs: elapsed,
	}, nil
}

func buildPrompt(req ExecuteRequest) string {
	prompt := fmt.Sprintf("Skill: /%s\n\nIssue: %s\n\nURL: %s\n\n%s",
		req.Skill, req.IssueTitle, req.IssueURL, req.IssueBody)
	if req.PreviousError != "" {
		prompt += fmt.Sprintf("\n\nNote: previous attempt failed with error:\n%s", req.PreviousError)
	}
	return prompt
}

// parseCost extracts cost_usd from the claude JSON output. Returns 0 on parse failure.
func parseCost(output []byte) float64 {
	var result struct {
		CostUSD float64 `json:"cost_usd"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return 0
	}
	return result.CostUSD
}
