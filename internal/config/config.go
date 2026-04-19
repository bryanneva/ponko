// Package config loads and validates the ponko-runner routing configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration wraps time.Duration to support YAML string parsing (e.g., "5m", "30s").
type Duration time.Duration

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	dur, err := time.ParseDuration(value.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", value.Value, err)
	}
	*d = Duration(dur)
	return nil
}

// TimeDuration returns the underlying time.Duration.
func (d Duration) TimeDuration() time.Duration {
	return time.Duration(d)
}

// Config is the root configuration structure for ponko-runner.
type Config struct {
	Workflows      map[string]Workflow `yaml:"workflows"`
	Gates          map[string]string   `yaml:"gates"`
	DevRouter      DevRouterConfig     `yaml:"dev_router"`
	StateLabels    StateLabels         `yaml:"state_labels"`
	IntakeLabel    string              `yaml:"intake_label"`
	Project        string              `yaml:"project"`
	Repos          []Repo              `yaml:"repos"`
	Rules          []Rule              `yaml:"rules"`
	Groom          GroomConfig         `yaml:"groom"`
	Budget         Budget              `yaml:"budget"`
	MaxActiveTasks int                 `yaml:"max_active_tasks"`
	LockTTLSeconds int                 `yaml:"lock_ttl_seconds"`
}

// Repo is a GitHub repository to poll for issues.
type Repo struct {
	Owner string `yaml:"owner"`
	Name  string `yaml:"name"`
	Path  string `yaml:"path"`
}

// Rule maps a label-set to a workflow name.
type Rule struct {
	Workflow string `yaml:"workflow"`
	Match    Match  `yaml:"match"`
}

// Match holds the labels that must ALL be present for the rule to fire.
type Match struct {
	Labels []string `yaml:"labels"`
}

// Workflow defines a sequence of phases for processing a task.
type Workflow struct {
	Runtime string  `yaml:"runtime"`
	Phases  []Phase `yaml:"phases"`
}

// Phase is one step within a workflow.
type Phase struct {
	Name     string `yaml:"name"`
	Skill    string `yaml:"skill"`
	Gate     string `yaml:"gate"`
	Model    string `yaml:"model"`
	Provider string `yaml:"provider"`
	Approval bool   `yaml:"approval"`
}

// GroomConfig defines settings for the groom command and daemon scheduler.
type GroomConfig struct {
	DefaultModel   string         `yaml:"default_model"`
	SynthesisModel string         `yaml:"synthesis_model"`
	Projects       []GroomProject `yaml:"projects"`
	MaxBudgetUSD   float64        `yaml:"max_budget_usd"`
	StepTimeout    Duration       `yaml:"step_timeout"`
	MaxTurns       int            `yaml:"max_turns"`
	Interval       Duration       `yaml:"interval"`
}

// GroomProject maps a project name to a configured repo.
type GroomProject struct {
	Name string `yaml:"name"`
	Repo string `yaml:"repo"`
}

// DevRouterConfig defines settings for the autonomous issue execution pipeline.
type DevRouterConfig struct {
	ClassificationModel string `yaml:"classification_model"`
	PlanningModel       string `yaml:"planning_model"`
	ExecutionModel      string `yaml:"execution_model"`
	IntakeLabel         string `yaml:"intake_label"`
	MaxStories          int    `yaml:"max_stories"`
	MaxRetries          int    `yaml:"max_retries"`
}

// RepoPath looks up the local filesystem path for a repo by name or owner/name slug.
func (c *Config) RepoPath(nameOrSlug string) string {
	for _, r := range c.Repos {
		if r.Name == nameOrSlug || r.Owner+"/"+r.Name == nameOrSlug {
			return expandHome(r.Path)
		}
	}
	return ""
}

func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

// Budget defines spending limits for the orchestrator.
type Budget struct {
	PerRunUSD  float64 `yaml:"per_run_usd"`
	PerDayUSD  float64 `yaml:"per_day_usd"`
	PerTaskUSD float64 `yaml:"per_task_usd"`
}

// StateLabels maps task states to GitHub label names.
type StateLabels struct {
	InProgress string `yaml:"in_progress"`
	Blocked    string `yaml:"blocked"`
	Done       string `yaml:"done"`
}

// Gates maps gate names to required labels or conditions.
type Gates map[string]string

// Load reads and validates a routing YAML config from path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	applyDefaults(&cfg)

	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// DefaultBudget returns the budget limits used when no config is available.
func DefaultBudget() Budget {
	return Budget{PerRunUSD: 5.00, PerDayUSD: 20.00, PerTaskUSD: 3.00}
}

func applyDefaults(cfg *Config) {
	if cfg.Budget.PerRunUSD == 0 {
		cfg.Budget.PerRunUSD = 5.00
	}
	if cfg.Budget.PerDayUSD == 0 {
		cfg.Budget.PerDayUSD = 20.00
	}
	if cfg.Budget.PerTaskUSD == 0 {
		cfg.Budget.PerTaskUSD = 3.00
	}
	if cfg.MaxActiveTasks == 0 {
		cfg.MaxActiveTasks = 3
	}
	if cfg.IntakeLabel == "" {
		cfg.IntakeLabel = "ponko-runner:ready"
	}
	if cfg.LockTTLSeconds == 0 {
		cfg.LockTTLSeconds = 300
	}
	if cfg.Groom.MaxBudgetUSD == 0 {
		cfg.Groom.MaxBudgetUSD = 1.00
	}
	if cfg.Groom.DefaultModel == "" {
		cfg.Groom.DefaultModel = "haiku"
	}
	if cfg.Groom.SynthesisModel == "" {
		cfg.Groom.SynthesisModel = "sonnet"
	}
	if cfg.Groom.MaxTurns == 0 {
		cfg.Groom.MaxTurns = 10
	}
	if cfg.Groom.StepTimeout == 0 {
		cfg.Groom.StepTimeout = Duration(5 * time.Minute)
	}
	if cfg.Groom.Interval == 0 {
		cfg.Groom.Interval = Duration(15 * time.Minute)
	}
	if cfg.DevRouter.ClassificationModel == "" {
		cfg.DevRouter.ClassificationModel = "sonnet"
	}
	if cfg.DevRouter.PlanningModel == "" {
		cfg.DevRouter.PlanningModel = "sonnet"
	}
	if cfg.DevRouter.ExecutionModel == "" {
		cfg.DevRouter.ExecutionModel = "sonnet"
	}
	if cfg.DevRouter.MaxStories == 0 {
		cfg.DevRouter.MaxStories = 10
	}
	if cfg.DevRouter.MaxRetries == 0 {
		cfg.DevRouter.MaxRetries = 3
	}
	if cfg.DevRouter.IntakeLabel == "" {
		cfg.DevRouter.IntakeLabel = "ponko-runner:dev-router"
	}
}

func validate(cfg *Config) error {
	if len(cfg.Repos) == 0 {
		return fmt.Errorf("config: repos must not be empty")
	}

	for i, r := range cfg.Repos {
		if r.Owner == "" || r.Name == "" || r.Path == "" {
			return fmt.Errorf("config: repo[%d] must have owner, name, and path", i)
		}
	}

	for name, wf := range cfg.Workflows {
		hasWrapUp := false
		for _, p := range wf.Phases {
			if p.Name == "wrap-up" {
				hasWrapUp = true
				break
			}
		}
		if !hasWrapUp {
			return fmt.Errorf("config: workflow %q must have a final phase named 'wrap-up'", name)
		}
	}

	for i, rule := range cfg.Rules {
		if _, ok := cfg.Workflows[rule.Workflow]; !ok {
			return fmt.Errorf("config: rule[%d] references unknown workflow %q", i, rule.Workflow)
		}
	}

	repoNames := make(map[string]bool, len(cfg.Repos))
	for _, r := range cfg.Repos {
		repoNames[r.Name] = true
		repoNames[r.Owner+"/"+r.Name] = true
	}
	for i, gp := range cfg.Groom.Projects {
		if gp.Name == "" {
			return fmt.Errorf("config: groom.projects[%d] must have a name", i)
		}
		if !repoNames[gp.Repo] {
			return fmt.Errorf("config: groom.projects[%d] repo %q does not match any repos[].name or repos[].owner/name", i, gp.Repo)
		}
	}

	if cfg.DevRouter.MaxStories <= 0 {
		return fmt.Errorf("config: dev_router.max_stories must be > 0")
	}
	if cfg.DevRouter.MaxRetries <= 0 {
		return fmt.Errorf("config: dev_router.max_retries must be > 0")
	}

	return nil
}
