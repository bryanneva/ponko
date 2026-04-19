package runtime_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bryanneva/ponko/internal/runtime"
)

// mockExec captures calls and returns configured responses.
type mockExec struct {
	output []byte
	err    error
	calls  []execCall
}

type execCall struct {
	dir  string
	name string
	args []string
}

func (m *mockExec) Run(_ context.Context, dir, name string, args ...string) ([]byte, error) {
	m.calls = append(m.calls, execCall{dir: dir, name: name, args: args})
	return m.output, m.err
}

func jsonOutput(costUSD float64, output string) []byte {
	data, _ := json.Marshal(map[string]any{
		"cost_usd": costUSD,
		"result":   output,
	})
	return data
}

func TestClaudeRuntime_SuccessfulExecution(t *testing.T) {
	exec := &mockExec{
		output: jsonOutput(0.42, "done"),
	}
	rt := runtime.NewClaudeRuntime(exec)
	ctx := context.Background()

	req := runtime.ExecuteRequest{
		WorkingDir:   "/repos/myapp",
		Skill:        "fix",
		IssueTitle:   "Fix the bug",
		IssueBody:    "Bug description",
		IssueURL:     "https://github.com/acme/myapp/issues/1",
		MaxBudgetUSD: 5.0,
	}
	result, err := rt.Execute(ctx, req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.CostUSD < 0.41 || result.CostUSD > 0.43 {
		t.Errorf("CostUSD: got %f, want ~0.42", result.CostUSD)
	}
}

func TestClaudeRuntime_PromptContainsIssueContext(t *testing.T) {
	exec := &mockExec{output: jsonOutput(0.1, "ok")}
	rt := runtime.NewClaudeRuntime(exec)
	ctx := context.Background()

	req := runtime.ExecuteRequest{
		WorkingDir:   "/repos/myapp",
		Skill:        "fix",
		IssueTitle:   "My issue title",
		IssueBody:    "My issue body",
		IssueURL:     "https://github.com/acme/myapp/issues/42",
		MaxBudgetUSD: 3.0,
	}
	_, _ = rt.Execute(ctx, req)

	if len(exec.calls) == 0 {
		t.Fatal("expected at least one exec call")
	}
	call := exec.calls[0]

	// Find -p argument (prompt)
	var prompt string
	for i, arg := range call.args {
		if arg == "-p" && i+1 < len(call.args) {
			prompt = call.args[i+1]
		}
	}
	if prompt == "" {
		t.Fatal("expected -p argument with prompt")
	}
	for _, want := range []string{"My issue title", "My issue body", "fix"} {
		if !contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestClaudeRuntime_BudgetFlagPassed(t *testing.T) {
	exec := &mockExec{output: jsonOutput(0.1, "ok")}
	rt := runtime.NewClaudeRuntime(exec)

	req := runtime.ExecuteRequest{
		WorkingDir:   "/repos/myapp",
		Skill:        "fix",
		MaxBudgetUSD: 2.5,
	}
	_, _ = rt.Execute(context.Background(), req)

	if len(exec.calls) == 0 {
		t.Fatal("no exec calls")
	}
	args := exec.calls[0].args
	var found bool
	for i, a := range args {
		if a == "--max-budget-usd" && i+1 < len(args) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected --max-budget-usd flag in args: %v", args)
	}
}

func TestClaudeRuntime_PreviousErrorAppendedToPrompt(t *testing.T) {
	exec := &mockExec{output: jsonOutput(0.1, "ok")}
	rt := runtime.NewClaudeRuntime(exec)

	req := runtime.ExecuteRequest{
		WorkingDir:    "/repos/myapp",
		Skill:         "fix",
		PreviousError: "tests failed on last attempt",
	}
	_, _ = rt.Execute(context.Background(), req)

	if len(exec.calls) == 0 {
		t.Fatal("no exec calls")
	}
	var prompt string
	for i, arg := range exec.calls[0].args {
		if arg == "-p" && i+1 < len(exec.calls[0].args) {
			prompt = exec.calls[0].args[i+1]
		}
	}
	if !contains(prompt, "tests failed on last attempt") {
		t.Errorf("prompt does not contain PreviousError; prompt: %s", prompt)
	}
}

func TestClaudeRuntime_ModelFlagPassed(t *testing.T) {
	exec := &mockExec{output: jsonOutput(0.1, "ok")}
	rt := runtime.NewClaudeRuntime(exec)

	req := runtime.ExecuteRequest{
		WorkingDir:   "/repos/myapp",
		Skill:        "fix",
		MaxBudgetUSD: 1.0,
		Model:        "haiku",
	}
	_, _ = rt.Execute(context.Background(), req)

	if len(exec.calls) == 0 {
		t.Fatal("no exec calls")
	}
	args := exec.calls[0].args
	var found bool
	for i, a := range args {
		if a == "--model" && i+1 < len(args) && args[i+1] == "haiku" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected --model haiku in args: %v", args)
	}
}

func TestClaudeRuntime_MaxTurnsFlagPassed(t *testing.T) {
	exec := &mockExec{output: jsonOutput(0.1, "ok")}
	rt := runtime.NewClaudeRuntime(exec)

	req := runtime.ExecuteRequest{
		WorkingDir:   "/repos/myapp",
		Skill:        "fix",
		MaxBudgetUSD: 1.0,
		MaxTurns:     10,
	}
	_, _ = rt.Execute(context.Background(), req)

	if len(exec.calls) == 0 {
		t.Fatal("no exec calls")
	}
	args := exec.calls[0].args
	var found bool
	for i, a := range args {
		if a == "--max-turns" && i+1 < len(args) && args[i+1] == "10" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected --max-turns 10 in args: %v", args)
	}
}

func TestClaudeRuntime_NoMaxTurnsFlagWhenZero(t *testing.T) {
	exec := &mockExec{output: jsonOutput(0.1, "ok")}
	rt := runtime.NewClaudeRuntime(exec)

	req := runtime.ExecuteRequest{
		WorkingDir:   "/repos/myapp",
		Skill:        "fix",
		MaxBudgetUSD: 1.0,
	}
	_, _ = rt.Execute(context.Background(), req)

	if len(exec.calls) == 0 {
		t.Fatal("no exec calls")
	}
	for _, a := range exec.calls[0].args {
		if a == "--max-turns" {
			t.Error("--max-turns flag should not be present when MaxTurns is 0")
		}
	}
}

func TestClaudeRuntime_NoModelFlagWhenEmpty(t *testing.T) {
	exec := &mockExec{output: jsonOutput(0.1, "ok")}
	rt := runtime.NewClaudeRuntime(exec)

	req := runtime.ExecuteRequest{
		WorkingDir:   "/repos/myapp",
		Skill:        "fix",
		MaxBudgetUSD: 1.0,
	}
	_, _ = rt.Execute(context.Background(), req)

	if len(exec.calls) == 0 {
		t.Fatal("no exec calls")
	}
	for _, a := range exec.calls[0].args {
		if a == "--model" {
			t.Error("--model flag should not be present when Model is empty")
		}
	}
}

func TestClaudeRuntime_PromptOverride(t *testing.T) {
	exec := &mockExec{output: jsonOutput(0.1, "ok")}
	rt := runtime.NewClaudeRuntime(exec)

	req := runtime.ExecuteRequest{
		WorkingDir:   "/repos/myapp",
		Skill:        "fix",
		IssueTitle:   "Should be ignored",
		MaxBudgetUSD: 1.0,
		Prompt:       "Skill: /groom-consume 42",
	}
	_, _ = rt.Execute(context.Background(), req)

	if len(exec.calls) == 0 {
		t.Fatal("no exec calls")
	}
	var prompt string
	for i, arg := range exec.calls[0].args {
		if arg == "-p" && i+1 < len(exec.calls[0].args) {
			prompt = exec.calls[0].args[i+1]
		}
	}
	if prompt != "Skill: /groom-consume 42" {
		t.Errorf("expected custom prompt, got %q", prompt)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
