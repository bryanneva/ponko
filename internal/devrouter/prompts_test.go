package devrouter_test

import (
	"strings"
	"testing"

	"github.com/bryanneva/ponko/internal/devrouter"
)

func TestClassificationPrompt_ContainsRequiredFields(t *testing.T) {
	title := "Fix the login bug"
	body := "Users can't log in with special characters"
	labels := []string{"bug", "ponko-runner:dev-router"}

	prompt := devrouter.ClassificationPrompt(title, body, labels)

	checks := []string{
		title,
		body,
		"bug", // label
		"fix",
		"rpi",
		"ralph",
		`"track"`,
		`"rationale"`,
		"JSON",
	}
	for _, want := range checks {
		if !strings.Contains(prompt, want) {
			t.Errorf("ClassificationPrompt missing %q", want)
		}
	}
}

func TestPlanningPrompt_ContainsRequiredFields(t *testing.T) {
	title := "Add dark mode"
	body := "Users want dark mode support"
	rationale := "Multi-step UI feature"

	for _, track := range []devrouter.Track{devrouter.TrackFix, devrouter.TrackRPI, devrouter.TrackRalph} {
		prompt := devrouter.PlanningPrompt(track, title, body, rationale)

		checks := []string{
			title,
			body,
			rationale,
			`"stories"`,
			`"title"`,
			`"description"`,
			`"success_criteria"`,
			"JSON",
		}
		for _, want := range checks {
			if !strings.Contains(prompt, want) {
				t.Errorf("PlanningPrompt(%s) missing %q", track, want)
			}
		}
	}
}

func TestStoryPrompt_ContainsRequiredFields(t *testing.T) {
	story := devrouter.Story{
		Title:           "Implement login form",
		Description:     "Build the login UI component",
		SuccessCriteria: []string{"Form renders", "Submit works"},
	}
	issueTitle := "Build login feature"
	issueBody := "We need a login form"
	previousResults := []string{"story 1 completed"}

	prompt := devrouter.StoryPrompt(story, issueTitle, issueBody, previousResults)

	checks := []string{
		story.Title,
		story.Description,
		story.SuccessCriteria[0],
		issueTitle,
		issueBody,
		previousResults[0],
	}
	for _, want := range checks {
		if !strings.Contains(prompt, want) {
			t.Errorf("StoryPrompt missing %q", want)
		}
	}
}

func TestStoryPrompt_NoPreviousResults(t *testing.T) {
	story := devrouter.Story{
		Title:           "First story",
		Description:     "Do something",
		SuccessCriteria: []string{"It works"},
	}
	prompt := devrouter.StoryPrompt(story, "Issue", "Body", nil)
	if prompt == "" {
		t.Error("StoryPrompt returned empty string")
	}
}
