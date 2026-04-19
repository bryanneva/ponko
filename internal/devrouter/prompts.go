package devrouter

import (
	"fmt"
	"strings"
)

// ClassificationPrompt builds a prompt asking the LLM to classify a GitHub issue into a track.
// The LLM should return JSON: {"track": "fix|rpi|ralph", "rationale": "..."}
func ClassificationPrompt(title, body string, labels []string) string {
	labelsStr := strings.Join(labels, ", ")
	if labelsStr == "" {
		labelsStr = "(none)"
	}
	return fmt.Sprintf(`You are classifying a GitHub issue into one of three execution tracks.

## Issue

Title: %s
Labels: %s

Body:
%s

## Tracks

- **fix**: A small, well-scoped bug fix or simple change. A single implementation step is sufficient. Examples: typos, one-line fixes, obvious bugs.
- **rpi**: A medium-sized feature or refactor requiring research, planning, and multi-step implementation. Examples: new API endpoint, database migration, multi-file refactor.
- **ralph**: A large, complex feature requiring many stories and autonomous iteration. Examples: new product feature, architectural overhaul, multi-component integration.

## Output

Respond with ONLY valid JSON in this exact format:
{"track": "fix|rpi|ralph", "rationale": "brief explanation of classification"}

Do not include any other text.`, title, labelsStr, body)
}

// PlanningPrompt builds a prompt asking the LLM to produce a structured plan for the given track.
// The LLM should return JSON: {"stories": [{"title": "...", "description": "...", "success_criteria": ["..."]}]}
func PlanningPrompt(track Track, title, body, rationale string) string {
	trackGuidance := planningGuidanceForTrack(track)
	return fmt.Sprintf(`You are creating an implementation plan for a GitHub issue.

## Issue

Title: %s

Body:
%s

## Classification

Track: %s
Rationale: %s

## Planning Guidance

%s

## Output

Respond with ONLY valid JSON in this exact format:
{
  "stories": [
    {
      "title": "story title",
      "description": "what to implement",
      "success_criteria": ["criterion 1", "criterion 2"]
    }
  ]
}

Each story must have a title, description, and at least one success_criteria entry. Do not include any other text.`,
		title, body, track, rationale, trackGuidance)
}

func planningGuidanceForTrack(track Track) string {
	switch track {
	case TrackFix:
		return "This is a fix track issue. Produce exactly 1 story covering the complete fix."
	case TrackRPI:
		return "This is an RPI track issue. Produce 2-5 stories: research/design first, then implementation steps, then verification."
	case TrackRalph:
		return "This is a Ralph track issue. Produce 3-10 stories as independent units of work that can be implemented sequentially."
	default:
		return "Produce a minimal set of stories to implement this issue."
	}
}

// StoryPrompt builds a prompt for executing a single story within a pipeline.
func StoryPrompt(story Story, issueTitle, issueBody string, previousResults []string) string {
	criteria := strings.Join(story.SuccessCriteria, "\n- ")
	if criteria != "" {
		criteria = "- " + criteria
	}

	prevSection := ""
	if len(previousResults) > 0 {
		prevSection = "\n## Previous Story Results\n\n" + strings.Join(previousResults, "\n\n")
	}

	return fmt.Sprintf(`You are implementing a story as part of a larger GitHub issue.

## Issue Context

Title: %s

Body:
%s
%s
## Current Story

Title: %s

Description:
%s

Success Criteria:
%s

## Instructions

Implement this story completely. Ensure all success criteria are met before finishing.
Run tests and quality checks. Commit your changes with a descriptive message.`,
		issueTitle, issueBody, prevSection,
		story.Title, story.Description, criteria)
}
