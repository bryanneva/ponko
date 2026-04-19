// Package intake polls GitHub Issues and converts them into task.Task values.
package intake

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/bryanneva/ponko/internal/approval"
	"github.com/bryanneva/ponko/internal/config"
	"github.com/bryanneva/ponko/internal/exec"
	"github.com/bryanneva/ponko/internal/task"
)

// Intake polls GitHub Issues and returns new tasks for processing.
type Intake struct {
	exec exec.CommandExecutor
	cfg  config.Config
}

// NewIntake creates an Intake that polls the repos in cfg.
func NewIntake(cfg config.Config, ex exec.CommandExecutor) *Intake {
	return &Intake{cfg: cfg, exec: ex}
}

// ghIssue is the JSON shape returned by `gh issue list`.
type ghIssue struct {
	URL    string    `json:"url"`
	Title  string    `json:"title"`
	Body   string    `json:"body"`
	Labels []ghLabel `json:"labels"`
	Number int       `json:"number"`
}

type ghLabel struct {
	Name string `json:"name"`
}

// HasLabel reports whether the GitHub issue at issueURL carries the given label name.
func (i *Intake) HasLabel(ctx context.Context, issueURL, label string) (bool, error) {
	out, err := i.exec.Run(ctx, "", "gh", "issue", "view", issueURL, "--json", "labels")
	if err != nil {
		return false, fmt.Errorf("gh issue view %s: %w", issueURL, err)
	}
	var result struct {
		Labels []ghLabel `json:"labels"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return false, fmt.Errorf("parse labels for %s: %w", issueURL, err)
	}
	for _, l := range result.Labels {
		if l.Name == label {
			return true, nil
		}
	}
	return false, nil
}

// Comment posts a comment on a GitHub issue.
func (i *Intake) Comment(ctx context.Context, issueURL, body string) error {
	_, err := i.exec.Run(ctx, "", "gh", "issue", "comment", issueURL, "--body", body)
	if err != nil {
		return fmt.Errorf("gh issue comment %s: %w", issueURL, err)
	}
	return nil
}

// CheckApproval polls a GitHub issue's comments for /approve or /reject commands.
// Also checks for a configured approval label (from cfg.StateLabels).
func (i *Intake) CheckApproval(ctx context.Context, issueURL string) (approval.Status, error) {
	out, err := i.exec.Run(ctx, "", "gh", "issue", "view", issueURL, "--json", "comments,labels")
	if err != nil {
		return approval.Pending, fmt.Errorf("gh issue view %s: %w", issueURL, err)
	}

	var result struct {
		Comments []struct {
			Body string `json:"body"`
		} `json:"comments"`
		Labels []ghLabel `json:"labels"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return approval.Pending, fmt.Errorf("parse comments for %s: %w", issueURL, err)
	}

	// Check labels first — use the approval gate label from config if available
	approvalLabel := "ponko-runner:approved"
	for gateName, label := range i.cfg.Gates {
		if gateName == "approval" {
			approvalLabel = label
			break
		}
	}
	for _, l := range result.Labels {
		if l.Name == approvalLabel {
			return approval.Approved, nil
		}
	}

	// Check comments (most recent first — scan backwards)
	for idx := len(result.Comments) - 1; idx >= 0; idx-- {
		body := strings.TrimSpace(result.Comments[idx].Body)
		if body == "/approve" {
			return approval.Approved, nil
		}
		if body == "/reject" {
			return approval.Rejected, nil
		}
	}

	return approval.Pending, nil
}

// Poll fetches issues from GitHub and returns task stubs (not yet persisted).
func (i *Intake) Poll(ctx context.Context) ([]*task.Task, error) {
	return i.PollRaw(ctx)
}

// PollRaw fetches issues from GitHub without applying deduplication or labels.
func (i *Intake) PollRaw(ctx context.Context) ([]*task.Task, error) {
	var newTasks []*task.Task

	for _, repo := range i.cfg.Repos {
		repoSlug := repo.Owner + "/" + repo.Name
		repoPath := i.cfg.RepoPath(repoSlug)

		out, err := i.exec.Run(ctx, repoPath, "gh", "issue", "list",
			"--repo", repoSlug,
			"--label", i.cfg.IntakeLabel,
			"--json", "url,number,title,labels,body",
			"--limit", "50",
		)
		if err != nil {
			return nil, fmt.Errorf("gh issue list %s: %w", repoSlug, err)
		}

		var issues []ghIssue
		if err := json.Unmarshal(out, &issues); err != nil {
			return nil, fmt.Errorf("parse gh output for %s: %w", repoSlug, err)
		}

		for _, issue := range issues {
			labels := make([]string, len(issue.Labels))
			for j, l := range issue.Labels {
				labels[j] = l.Name
			}

			newTasks = append(newTasks, &task.Task{
				IssueURL:    issue.URL,
				Repo:        repoSlug,
				IssueNumber: issue.Number,
				Title:       issue.Title,
				Labels:      labels,
				Body:        issue.Body,
				Status:      task.StatusQueued,
			})
		}
	}

	return newTasks, nil
}

// MarkInProgress updates GitHub labels after a task has been enqueued.
func (i *Intake) MarkInProgress(ctx context.Context, t *task.Task) error {
	if i.cfg.StateLabels.InProgress == "" {
		return nil
	}
	_, err := i.exec.Run(ctx, i.cfg.RepoPath(t.Repo), "gh", "issue", "edit",
		"--repo", t.Repo,
		strconv.Itoa(t.IssueNumber),
		"--add-label", i.cfg.StateLabels.InProgress,
		"--remove-label", i.cfg.IntakeLabel,
	)
	if err != nil {
		return fmt.Errorf("mark issue %s in progress: %w", t.IssueURL, err)
	}
	return nil
}
