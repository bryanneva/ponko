package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bryanneva/ponko/internal/config"
)

func writeYAML(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "routing-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	_ = f.Close()
	return f.Name()
}

const validYAML = `
repos:
  - owner: acme
    name: myapp
    path: /home/user/repos/myapp

workflows:
  fix:
    runtime: claude-code
    phases:
      - name: implement
        skill: fix
      - name: wrap-up
        skill: wrap-up

rules:
  - match:
      labels: [workflow:fix]
    workflow: fix
`

func TestLoad_Valid(t *testing.T) {
	path := writeYAML(t, validYAML)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load valid config: %v", err)
	}
	if len(cfg.Repos) != 1 {
		t.Errorf("expected 1 repo, got %d", len(cfg.Repos))
	}
	if cfg.Repos[0].Owner != "acme" {
		t.Errorf("expected owner 'acme', got %q", cfg.Repos[0].Owner)
	}
}

func TestLoad_MissingRepos(t *testing.T) {
	path := writeYAML(t, `
workflows:
  fix:
    runtime: claude-code
    phases:
      - name: wrap-up
        skill: wrap-up
rules: []
`)
	_, err := config.Load(path)
	if err == nil {
		t.Error("expected error for missing repos, got nil")
	}
}

func TestLoad_WorkflowMissingWrapUp(t *testing.T) {
	path := writeYAML(t, `
repos:
  - owner: acme
    name: myapp
    path: /some/path
workflows:
  fix:
    runtime: claude-code
    phases:
      - name: implement
        skill: fix
rules:
  - match:
      labels: [workflow:fix]
    workflow: fix
`)
	_, err := config.Load(path)
	if err == nil {
		t.Error("expected error for workflow missing wrap-up phase, got nil")
	}
}

func TestLoad_UnknownWorkflowInRule(t *testing.T) {
	path := writeYAML(t, `
repos:
  - owner: acme
    name: myapp
    path: /some/path
workflows:
  fix:
    runtime: claude-code
    phases:
      - name: wrap-up
        skill: wrap-up
rules:
  - match:
      labels: [workflow:rpi]
    workflow: rpi
`)
	_, err := config.Load(path)
	if err == nil {
		t.Error("expected error for unknown workflow in rule, got nil")
	}
}

func TestLoad_BudgetDefaults(t *testing.T) {
	path := writeYAML(t, validYAML)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Budget.PerRunUSD != 5.00 {
		t.Errorf("PerRunUSD default: got %f, want 5.00", cfg.Budget.PerRunUSD)
	}
	if cfg.Budget.PerDayUSD != 20.00 {
		t.Errorf("PerDayUSD default: got %f, want 20.00", cfg.Budget.PerDayUSD)
	}
	if cfg.Budget.PerTaskUSD != 3.00 {
		t.Errorf("PerTaskUSD default: got %f, want 3.00", cfg.Budget.PerTaskUSD)
	}
}

func TestLoad_MaxActiveTasksDefault(t *testing.T) {
	path := writeYAML(t, validYAML)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MaxActiveTasks != 3 {
		t.Errorf("MaxActiveTasks default: got %d, want 3", cfg.MaxActiveTasks)
	}
}

func TestLoad_IntakeLabelDefault(t *testing.T) {
	path := writeYAML(t, validYAML)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.IntakeLabel != "ponko-runner:ready" {
		t.Errorf("IntakeLabel default: got %q, want 'ponko-runner:ready'", cfg.IntakeLabel)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestLoad_GroomDefaults(t *testing.T) {
	path := writeYAML(t, validYAML)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Groom.MaxBudgetUSD != 1.00 {
		t.Errorf("MaxBudgetUSD default: got %f, want 1.00", cfg.Groom.MaxBudgetUSD)
	}
	if cfg.Groom.DefaultModel != "haiku" {
		t.Errorf("DefaultModel default: got %q, want 'haiku'", cfg.Groom.DefaultModel)
	}
	if cfg.Groom.SynthesisModel != "sonnet" {
		t.Errorf("SynthesisModel default: got %q, want 'sonnet'", cfg.Groom.SynthesisModel)
	}
	if cfg.Groom.StepTimeout.TimeDuration() != 5*time.Minute {
		t.Errorf("StepTimeout default: got %v, want 5m", cfg.Groom.StepTimeout.TimeDuration())
	}
	if cfg.Groom.Interval.TimeDuration() != 15*time.Minute {
		t.Errorf("Interval default: got %v, want 15m", cfg.Groom.Interval.TimeDuration())
	}
	if cfg.Groom.MaxTurns != 10 {
		t.Errorf("MaxTurns default: got %d, want 10", cfg.Groom.MaxTurns)
	}
}

func TestLoad_GroomStepTimeout(t *testing.T) {
	path := writeYAML(t, validYAML+`
groom:
  step_timeout: "10m"
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load with step_timeout: %v", err)
	}
	if cfg.Groom.StepTimeout.TimeDuration() != 10*time.Minute {
		t.Errorf("StepTimeout: got %v, want 10m", cfg.Groom.StepTimeout.TimeDuration())
	}
}

func TestLoad_GroomConfig(t *testing.T) {
	path := writeYAML(t, `
repos:
  - owner: acme
    name: myapp
    path: /home/user/repos/myapp

workflows:
  fix:
    runtime: claude-code
    phases:
      - name: implement
        skill: fix
      - name: wrap-up
        skill: wrap-up

rules:
  - match:
      labels: [workflow:fix]
    workflow: fix

groom:
  projects:
    - name: "Daily Driver"
      repo: myapp
  default_model: sonnet
  synthesis_model: opus
  max_budget_usd: 0.05
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Groom.Projects) != 1 {
		t.Fatalf("expected 1 groom project, got %d", len(cfg.Groom.Projects))
	}
	if cfg.Groom.Projects[0].Name != "Daily Driver" {
		t.Errorf("project name: got %q", cfg.Groom.Projects[0].Name)
	}
	if cfg.Groom.DefaultModel != "sonnet" {
		t.Errorf("default model: got %q", cfg.Groom.DefaultModel)
	}
	if cfg.Groom.MaxBudgetUSD != 0.05 {
		t.Errorf("max budget: got %f", cfg.Groom.MaxBudgetUSD)
	}
}

func TestLoad_GroomRepoSlug(t *testing.T) {
	path := writeYAML(t, `
repos:
  - owner: acme
    name: myapp
    path: /some/path

workflows:
  fix:
    runtime: claude-code
    phases:
      - name: wrap-up
        skill: wrap-up

rules: []

groom:
  projects:
    - name: "My Project"
      repo: acme/myapp
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Errorf("expected no error for groom project with owner/name slug, got: %v", err)
	}
	if cfg != nil && len(cfg.Groom.Projects) != 1 {
		t.Errorf("expected 1 groom project, got %d", len(cfg.Groom.Projects))
	}
}

func TestLoad_GroomInvalidRepo(t *testing.T) {
	path := writeYAML(t, `
repos:
  - owner: acme
    name: myapp
    path: /some/path

workflows:
  fix:
    runtime: claude-code
    phases:
      - name: wrap-up
        skill: wrap-up

rules: []

groom:
  projects:
    - name: "Bad Project"
      repo: nonexistent
`)
	_, err := config.Load(path)
	if err == nil {
		t.Error("expected error for groom project with unknown repo")
	}
}

func TestLoad_PhaseModel(t *testing.T) {
	path := writeYAML(t, `
repos:
  - owner: acme
    name: myapp
    path: /some/path

workflows:
  fix:
    runtime: claude-code
    phases:
      - name: implement
        skill: fix
        model: haiku
      - name: wrap-up
        skill: wrap-up
        model: sonnet

rules:
  - match:
      labels: [workflow:fix]
    workflow: fix
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	phases := cfg.Workflows["fix"].Phases
	if phases[0].Model != "haiku" {
		t.Errorf("phase[0].Model: got %q, want 'haiku'", phases[0].Model)
	}
	if phases[1].Model != "sonnet" {
		t.Errorf("phase[1].Model: got %q, want 'sonnet'", phases[1].Model)
	}
}

func TestLoad_DevRouterDefaults(t *testing.T) {
	path := writeYAML(t, validYAML)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DevRouter.ClassificationModel != "sonnet" {
		t.Errorf("ClassificationModel default: got %q, want 'sonnet'", cfg.DevRouter.ClassificationModel)
	}
	if cfg.DevRouter.PlanningModel != "sonnet" {
		t.Errorf("PlanningModel default: got %q, want 'sonnet'", cfg.DevRouter.PlanningModel)
	}
	if cfg.DevRouter.ExecutionModel != "sonnet" {
		t.Errorf("ExecutionModel default: got %q, want 'sonnet'", cfg.DevRouter.ExecutionModel)
	}
	if cfg.DevRouter.MaxStories != 10 {
		t.Errorf("MaxStories default: got %d, want 10", cfg.DevRouter.MaxStories)
	}
	if cfg.DevRouter.MaxRetries != 3 {
		t.Errorf("MaxRetries default: got %d, want 3", cfg.DevRouter.MaxRetries)
	}
	if cfg.DevRouter.IntakeLabel != "ponko-runner:dev-router" {
		t.Errorf("IntakeLabel default: got %q, want 'ponko-runner:dev-router'", cfg.DevRouter.IntakeLabel)
	}
}

func TestLoad_DevRouterConfig(t *testing.T) {
	path := writeYAML(t, validYAML+`
dev_router:
  classification_model: haiku
  planning_model: opus
  execution_model: haiku
  max_stories: 5
  max_retries: 2
  intake_label: ponko-runner:custom
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DevRouter.ClassificationModel != "haiku" {
		t.Errorf("ClassificationModel: got %q, want 'haiku'", cfg.DevRouter.ClassificationModel)
	}
	if cfg.DevRouter.MaxStories != 5 {
		t.Errorf("MaxStories: got %d, want 5", cfg.DevRouter.MaxStories)
	}
	if cfg.DevRouter.IntakeLabel != "ponko-runner:custom" {
		t.Errorf("IntakeLabel: got %q, want 'ponko-runner:custom'", cfg.DevRouter.IntakeLabel)
	}
}

func TestRepoPath(t *testing.T) {
	cfg := &config.Config{
		Repos: []config.Repo{
			{Owner: "acme", Name: "myapp", Path: "/repos/myapp"},
			{Owner: "acme", Name: "other", Path: "/repos/other"},
		},
	}
	if got := cfg.RepoPath("myapp"); got != "/repos/myapp" {
		t.Errorf("RepoPath(myapp): got %q", got)
	}
	if got := cfg.RepoPath("missing"); got != "" {
		t.Errorf("RepoPath(missing): got %q, want empty", got)
	}
	if got := cfg.RepoPath("acme/myapp"); got != "/repos/myapp" {
		t.Errorf("RepoPath(acme/myapp): got %q, want /repos/myapp", got)
	}
	if got := cfg.RepoPath("acme/missing"); got != "" {
		t.Errorf("RepoPath(acme/missing): got %q, want empty", got)
	}
}
