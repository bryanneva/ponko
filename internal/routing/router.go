// Package routing implements the rule engine that maps issue labels to workflow names.
package routing

import (
	"errors"

	"github.com/bryanneva/ponko/internal/config"
)

// ErrNoMatch is returned when no rule matches the given labels.
var ErrNoMatch = errors.New("no matching routing rule")

// Router evaluates rules top-to-bottom and returns the first matching workflow name.
type Router struct {
	rules []config.Rule
}

// NewRouter creates a Router with the given ordered rules.
func NewRouter(rules []config.Rule) *Router {
	return &Router{rules: rules}
}

// Match returns the workflow name for the first rule whose labels are all present
// in the issue's label set. Returns ErrNoMatch if no rule matches.
func (r *Router) Match(issueLabels []string) (string, error) {
	labelSet := make(map[string]struct{}, len(issueLabels))
	for _, l := range issueLabels {
		labelSet[l] = struct{}{}
	}

	for _, rule := range r.rules {
		if matchesAll(rule.Match.Labels, labelSet) {
			return rule.Workflow, nil
		}
	}
	return "", ErrNoMatch
}

// matchesAll returns true when every required label is in the set.
// An empty required slice (catch-all) always returns true.
func matchesAll(required []string, set map[string]struct{}) bool {
	for _, l := range required {
		if _, ok := set[l]; !ok {
			return false
		}
	}
	return true
}
