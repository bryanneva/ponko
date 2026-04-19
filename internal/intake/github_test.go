package intake_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/bryanneva/ponko/internal/config"
	"github.com/bryanneva/ponko/internal/intake"
	"github.com/bryanneva/ponko/internal/task"
)

// --- mock executor ---

type mockExec struct {
	responses map[string][]byte
	calls     []mockCall
}

type mockCall struct {
	name string
	args []string
}

func (m *mockExec) Run(_ context.Context, _, name string, args ...string) ([]byte, error) {
	m.calls = append(m.calls, mockCall{name: name, args: args})
	key := fmt.Sprintf("%s %s", name, argsKey(args))
	if resp, ok := m.responses[key]; ok {
		return resp, nil
	}
	return []byte("[]"), nil
}

func argsKey(args []string) string {
	key := ""
	for _, a := range args {
		key += a + " "
	}
	return key
}

// --- helpers ---

func buildIssueJSON(url string, number int, title string, labels []string) []byte {
	type labelItem struct {
		Name string `json:"name"`
	}
	type issue struct {
		URL    string      `json:"url"`
		Title  string      `json:"title"`
		Body   string      `json:"body"`
		Labels []labelItem `json:"labels"`
		Number int         `json:"number"`
	}
	ls := make([]labelItem, len(labels))
	for i, l := range labels {
		ls[i] = labelItem{Name: l}
	}
	data, _ := json.Marshal([]issue{{URL: url, Number: number, Title: title, Labels: ls, Body: "body"}})
	return data
}

func singleRepoConfig() config.Config {
	return config.Config{
		Repos: []config.Repo{
			{Owner: "acme", Name: "myapp", Path: "/repos/myapp"},
		},
		IntakeLabel:    "ponko-runner:ready",
		MaxActiveTasks: 3,
		StateLabels:    config.StateLabels{InProgress: "ponko-runner:in-progress"},
	}
}

// --- tests ---

func TestIntake_FetchesIssues(t *testing.T) {
	issueData := buildIssueJSON("https://github.com/acme/myapp/issues/1", 1, "Fix bug", []string{"ponko-runner:ready"})

	exec := &mockExec{
		responses: map[string][]byte{},
	}
	// Pre-populate so the gh issue list call returns our issue
	for k := range exec.responses {
		_ = k
	}
	// Use a wildcard-style: just set the response for any call
	exec.responses = map[string][]byte{
		"gh issue list --repo acme/myapp --label ponko-runner:ready --json url,number,title,labels,body --limit 50 ": issueData,
	}

	cfg := singleRepoConfig()
	poller := intake.NewIntake(cfg, exec)

	tasks, err := poller.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Title != "Fix bug" {
		t.Errorf("Title: got %q, want 'Fix bug'", tasks[0].Title)
	}
}

func TestIntake_PollDoesNotDeduplicateExistingTasks(t *testing.T) {
	url := "https://github.com/acme/myapp/issues/1"
	issueData := buildIssueJSON(url, 1, "Fix bug", []string{"ponko-runner:ready"})

	exec := &mockExec{
		responses: map[string][]byte{
			"gh issue list --repo acme/myapp --label ponko-runner:ready --json url,number,title,labels,body --limit 50 ": issueData,
		},
	}

	cfg := singleRepoConfig()
	poller := intake.NewIntake(cfg, exec)

	tasks, err := poller.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 task (River handles dedup), got %d", len(tasks))
	}
}

func TestIntake_PollDoesNotApplyActiveTaskLimit(t *testing.T) {
	issueData := buildIssueJSON("https://github.com/acme/myapp/issues/1", 1, "Fix bug", []string{"ponko-runner:ready"})

	exec := &mockExec{
		responses: map[string][]byte{
			"gh issue list --repo acme/myapp --label ponko-runner:ready --json url,number,title,labels,body --limit 50 ": issueData,
		},
	}

	cfg := singleRepoConfig()
	poller := intake.NewIntake(cfg, exec)

	tasks, err := poller.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 task (River enforces active limits), got %d", len(tasks))
	}
}

func TestIntake_MarkInProgressAppliesLabelChanges(t *testing.T) {
	exec := &mockExec{responses: map[string][]byte{}}
	cfg := singleRepoConfig()
	poller := intake.NewIntake(cfg, exec)

	err := poller.MarkInProgress(context.Background(), &task.Task{
		IssueURL:    "https://github.com/acme/myapp/issues/1",
		Repo:        "acme/myapp",
		IssueNumber: 1,
	})
	if err != nil {
		t.Fatalf("MarkInProgress: %v", err)
	}

	// Should have called gh issue edit to update labels
	var editCalled bool
	for _, call := range exec.calls {
		if call.name == "gh" {
			for _, arg := range call.args {
				if arg == "edit" {
					editCalled = true
				}
			}
		}
	}
	if !editCalled {
		t.Error("expected gh issue edit to be called for label update")
	}
}

// --- mockExecWithError supports returning errors ---

type mockExecWithError struct {
	err      error
	response []byte
}

func (m *mockExecWithError) Run(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
	return m.response, m.err
}

func TestIntake_HasLabel_Present(t *testing.T) {
	labelJSON := `{"labels":[{"name":"ponko-runner:approved"},{"name":"bug"}]}`
	exec := &mockExecWithError{response: []byte(labelJSON)}
	cfg := singleRepoConfig()
	poller := intake.NewIntake(cfg, exec)

	got, err := poller.HasLabel(context.Background(), "https://github.com/acme/myapp/issues/1", "ponko-runner:approved")
	if err != nil {
		t.Fatalf("HasLabel: %v", err)
	}
	if !got {
		t.Error("expected true for present label")
	}
}

func TestIntake_HasLabel_Absent(t *testing.T) {
	labelJSON := `{"labels":[{"name":"bug"}]}`
	exec := &mockExecWithError{response: []byte(labelJSON)}
	cfg := singleRepoConfig()
	poller := intake.NewIntake(cfg, exec)

	got, err := poller.HasLabel(context.Background(), "https://github.com/acme/myapp/issues/1", "ponko-runner:approved")
	if err != nil {
		t.Fatalf("HasLabel: %v", err)
	}
	if got {
		t.Error("expected false for absent label")
	}
}

func TestIntake_HasLabel_GhError(t *testing.T) {
	exec := &mockExecWithError{err: fmt.Errorf("gh: not found")}
	cfg := singleRepoConfig()
	poller := intake.NewIntake(cfg, exec)

	_, err := poller.HasLabel(context.Background(), "https://github.com/acme/myapp/issues/1", "ponko-runner:approved")
	if err == nil {
		t.Error("expected error to propagate")
	}
}
