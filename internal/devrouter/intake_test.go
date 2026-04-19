package devrouter_test

import (
	"context"
	"testing"

	"github.com/bryanneva/ponko/internal/config"
	"github.com/bryanneva/ponko/internal/devrouter"
	"github.com/bryanneva/ponko/internal/devrouter/testutil"
)

var testRepos = []config.Repo{
	{Owner: "owner", Name: "repo", Path: "/tmp/repo"},
}

const testIntakeLabel = "ponko-runner:dev-router"

func TestPollNewIssues_CreatesNewPipeline(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	ex := &testutil.MockExecutor{
		Responses: [][]byte{
			// gh issue list response
			[]byte(`[{"url":"https://github.com/owner/repo/issues/1","number":1,"title":"New feature","body":"Do the thing","labels":[]}]`),
			// gh issue edit response (remove label)
			[]byte(`{}`),
		},
	}

	n, err := devrouter.PollNewIssues(ctx, testRepos, testIntakeLabel, store, ex, false)
	if err != nil {
		t.Fatalf("PollNewIssues: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 new pipeline, got %d", n)
	}

	pipelines, _ := store.List(ctx, 0)
	if len(pipelines) != 1 {
		t.Fatalf("expected 1 pipeline in store, got %d", len(pipelines))
	}
	p := pipelines[0]
	if p.IssueURL != "https://github.com/owner/repo/issues/1" {
		t.Errorf("IssueURL: got %q", p.IssueURL)
	}
	if p.IssueTitle != "New feature" {
		t.Errorf("IssueTitle: got %q", p.IssueTitle)
	}
	if p.IssueBody != "Do the thing" {
		t.Errorf("IssueBody: got %q", p.IssueBody)
	}
	if p.Stage != devrouter.StagePending {
		t.Errorf("Stage: got %s", p.Stage)
	}
}

func TestPollNewIssues_SkipsDuplicates(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()

	// Pre-populate store with the issue URL
	existing := &devrouter.Pipeline{
		IssueURL: "https://github.com/owner/repo/issues/1",
		Stage:    devrouter.StagePending,
	}
	if err := store.Create(ctx, existing); err != nil {
		t.Fatalf("Create: %v", err)
	}

	ex := &testutil.MockExecutor{
		Output: []byte(`[{"url":"https://github.com/owner/repo/issues/1","number":1,"title":"Existing","body":"","labels":[]}]`),
	}

	n, err := devrouter.PollNewIssues(ctx, testRepos, testIntakeLabel, store, ex, false)
	if err != nil {
		t.Fatalf("PollNewIssues: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 new pipelines (duplicate), got %d", n)
	}

	pipelines, _ := store.List(ctx, 0)
	if len(pipelines) != 1 {
		t.Errorf("expected still 1 pipeline in store, got %d", len(pipelines))
	}
}

func TestPollNewIssues_RemovesIntakeLabel(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	ex := &testutil.MockExecutor{
		Responses: [][]byte{
			[]byte(`[{"url":"https://github.com/owner/repo/issues/2","number":2,"title":"Issue","body":"","labels":[]}]`),
			[]byte(`{}`),
		},
	}

	_, err := devrouter.PollNewIssues(ctx, testRepos, testIntakeLabel, store, ex, false)
	if err != nil {
		t.Fatalf("PollNewIssues: %v", err)
	}

	// Verify gh issue edit --remove-label was called
	var foundLabelRemoval bool
	for _, call := range ex.Calls {
		for i, arg := range call {
			if arg == "--remove-label" && i+1 < len(call) && call[i+1] == testIntakeLabel {
				foundLabelRemoval = true
			}
		}
	}
	if !foundLabelRemoval {
		t.Errorf("expected gh issue edit --remove-label %q to be called", testIntakeLabel)
	}
}

func TestPollNewIssues_DryRun(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	ex := &testutil.MockExecutor{
		Output: []byte(`[{"url":"https://github.com/owner/repo/issues/3","number":3,"title":"Issue","body":"","labels":[]}]`),
	}

	n, err := devrouter.PollNewIssues(ctx, testRepos, testIntakeLabel, store, ex, true)
	if err != nil {
		t.Fatalf("PollNewIssues dry-run: %v", err)
	}
	if n != 0 {
		t.Errorf("dry-run: expected 0 new pipelines, got %d", n)
	}

	pipelines, _ := store.List(ctx, 0)
	if len(pipelines) != 0 {
		t.Errorf("dry-run: expected no pipelines created, got %d", len(pipelines))
	}
	// Should not have called gh issue edit in dry-run
	for _, call := range ex.Calls {
		for _, arg := range call {
			if arg == "--remove-label" {
				t.Error("dry-run: should not call gh issue edit --remove-label")
			}
		}
	}
}

func TestPollNewIssues_EmptyRepos(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	ex := &testutil.MockExecutor{}

	n, err := devrouter.PollNewIssues(ctx, nil, testIntakeLabel, store, ex, false)
	if err != nil {
		t.Fatalf("PollNewIssues empty repos: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0, got %d", n)
	}
	if len(ex.Calls) != 0 {
		t.Errorf("expected no exec calls, got %d", len(ex.Calls))
	}
}
