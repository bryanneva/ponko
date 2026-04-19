package devrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/bryanneva/ponko/internal/config"
	"github.com/bryanneva/ponko/internal/exec"
)

// ghListIssue is the JSON shape of a single item from `gh issue list`.
type ghListIssue struct {
	URL    string `json:"url"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Number int `json:"number"`
}

// labelNames extracts the Name field from a slice of label structs.
func labelNames(labels []struct {
	Name string `json:"name"`
}) []string {
	names := make([]string, len(labels))
	for i, l := range labels {
		names[i] = l.Name
	}
	return names
}

// PollNewIssues scans each repo for issues carrying intakeLabel, creates a pipeline
// for each new one, removes the intake label, and returns the count of new pipelines.
// It is idempotent: issues already tracked in the store are skipped.
func PollNewIssues(
	ctx context.Context,
	repos []config.Repo,
	intakeLabel string,
	store PipelineStore,
	ex exec.CommandExecutor,
	dryRun bool,
) (int, error) {
	var created int

	for _, repo := range repos {
		repoSlug := repo.Owner + "/" + repo.Name

		out, err := ex.Run(ctx, repo.Path, "gh", "issue", "list",
			"--repo", repoSlug,
			"--label", intakeLabel,
			"--json", "url,number,title,body,labels",
			"--limit", "50",
		)
		if err != nil {
			return created, fmt.Errorf("gh issue list %s: %w", repoSlug, err)
		}

		var issues []ghListIssue
		if err := json.Unmarshal(out, &issues); err != nil {
			return created, fmt.Errorf("parse gh output for %s: %w", repoSlug, err)
		}

		for _, issue := range issues {
			existing, err := store.GetByIssueURL(ctx, issue.URL)
			if err != nil {
				return created, fmt.Errorf("dedup check for %s: %w", issue.URL, err)
			}
			if existing != nil {
				continue
			}

			if dryRun {
				log.Printf("[dry-run] would create pipeline for %s#%d", repoSlug, issue.Number)
				continue
			}

			p := &Pipeline{
				IssueURL:    issue.URL,
				Repo:        repoSlug,
				IssueNumber: issue.Number,
				IssueTitle:  issue.Title,
				IssueBody:   issue.Body,
				Stage:       StagePending,
			}
			if err := store.Create(ctx, p); err != nil {
				return created, fmt.Errorf("create pipeline for %s#%d: %w", repoSlug, issue.Number, err)
			}

			// Remove intake label so the issue is not re-processed next run.
			if _, err := ex.Run(ctx, repo.Path, "gh", "issue", "edit",
				fmt.Sprintf("%d", issue.Number),
				"--repo", repoSlug,
				"--remove-label", intakeLabel,
			); err != nil {
				// Non-fatal: log and continue. Pipeline is already created.
				log.Printf("devrouter: warning: remove label %q from %s#%d: %v",
					intakeLabel, repoSlug, issue.Number, err)
			}

			created++
		}
	}

	return created, nil
}
