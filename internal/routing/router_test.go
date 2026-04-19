package routing_test

import (
	"errors"
	"testing"

	"github.com/bryanneva/ponko/internal/config"
	"github.com/bryanneva/ponko/internal/routing"
)

func rules(pairs ...any) []config.Rule {
	var out []config.Rule
	for i := 0; i < len(pairs); i += 2 {
		labels := pairs[i].([]string)
		workflow := pairs[i+1].(string)
		out = append(out, config.Rule{
			Match:    config.Match{Labels: labels},
			Workflow: workflow,
		})
	}
	return out
}

func TestRouter_SingleLabelMatch(t *testing.T) {
	r := routing.NewRouter(rules([]string{"workflow:fix"}, "fix"))
	got, err := r.Match([]string{"workflow:fix", "size:small"})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if got != "fix" {
		t.Errorf("got workflow %q, want fix", got)
	}
}

func TestRouter_MultiLabelAND(t *testing.T) {
	r := routing.NewRouter(rules([]string{"workflow:fix", "size:small"}, "fix"))

	// Both present — should match
	got, err := r.Match([]string{"workflow:fix", "size:small", "bug"})
	if err != nil {
		t.Fatalf("Match (both): %v", err)
	}
	if got != "fix" {
		t.Errorf("got %q, want fix", got)
	}

	// Only one present — should not match
	_, err = r.Match([]string{"workflow:fix"})
	if !errors.Is(err, routing.ErrNoMatch) {
		t.Errorf("expected ErrNoMatch when only one label present, got %v", err)
	}
}

func TestRouter_FirstMatchWins(t *testing.T) {
	r := routing.NewRouter(rules(
		[]string{"workflow:fix"}, "fix",
		[]string{"workflow:fix"}, "rpi", // same label, second rule
	))
	got, err := r.Match([]string{"workflow:fix"})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if got != "fix" {
		t.Errorf("expected first match 'fix', got %q", got)
	}
}

func TestRouter_CatchAll(t *testing.T) {
	r := routing.NewRouter(rules(
		[]string{"workflow:fix"}, "fix",
		[]string{}, "rpi", // catch-all: empty labels
	))
	got, err := r.Match([]string{"some-random-label"})
	if err != nil {
		t.Fatalf("Match (catch-all): %v", err)
	}
	if got != "rpi" {
		t.Errorf("expected catch-all 'rpi', got %q", got)
	}
}

func TestRouter_NoMatch(t *testing.T) {
	r := routing.NewRouter(rules([]string{"workflow:fix"}, "fix"))
	_, err := r.Match([]string{"bug"})
	if !errors.Is(err, routing.ErrNoMatch) {
		t.Errorf("expected ErrNoMatch, got %v", err)
	}
}
