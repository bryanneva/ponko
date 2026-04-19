package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/bryanneva/ponko/internal/budget"
	"github.com/bryanneva/ponko/internal/config"
	"github.com/bryanneva/ponko/internal/event"
	"github.com/bryanneva/ponko/internal/groom"
	"github.com/bryanneva/ponko/internal/runtime"
	"github.com/bryanneva/ponko/internal/sqlite"
)

var groomCmd = &cobra.Command{
	Use:   "groom [project-name]",
	Short: "Run one grooming step per configured project",
	Long: `Reads groom-consume state files and invokes /groom-consume for the next
non-terminal issue. If project-name is given, only that project is processed.
Otherwise all configured groom projects are processed.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runGroom,
}

func init() {
	groomCmd.Flags().Bool("dry-run", false, "show planned actions without executing agents")
	rootCmd.AddCommand(groomCmd)
}

// jobDeps carries observable infrastructure for a single job execution.
// Named generically for forward-compatibility with the AutomatedTask interface (#46).
type jobDeps struct {
	bus     event.Bus
	budget  budget.Controller
	runID   string
	logFile string // ~/.ponko-runner/logs/{runID}.log
	dryRun  bool
}

func runGroom(cmd *cobra.Command, args []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	cfgPath := viper.GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if len(cfg.Groom.Projects) == 0 {
		fmt.Println("No groom projects configured.")
		return nil
	}

	projects := cfg.Groom.Projects
	if len(args) == 1 {
		name := args[0]
		projects = filterProject(cfg.Groom.Projects, name)
		if len(projects) == 0 {
			return fmt.Errorf("project %q not found in groom config", name)
		}
	}

	db, err := openDB(viper.GetString("db"))
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = db.Close() }()

	err = sqlite.Migrate(context.Background(), db)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	eventsPath := expandHome(viper.GetString("events"))
	bus, err := event.NewJSONLBus(eventsPath)
	if err != nil {
		return fmt.Errorf("open event bus: %w", err)
	}

	logDir := expandHome("~/.ponko-runner/logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	ctrl := budget.NewController(db, cfg.Budget)
	exec := &osExec{}
	rt := runtime.NewClaudeRuntime(exec)
	ctx := context.Background()

	runGroomProjects(ctx, cfg, projects, rt, bus, ctrl, logDir, dryRun)
	return nil
}

// runGroomProjects executes one groom step per project. Per-project errors are
// logged but do not abort remaining projects. Used by both the CLI groom command
// and the daemon GroomTask.
func runGroomProjects(ctx context.Context, cfg *config.Config, projects []config.GroomProject, rt runtime.AgentRuntime, bus event.Bus, ctrl budget.Controller, logDir string, dryRun bool) {
	for _, proj := range projects {
		runID := uuid.NewString()
		deps := jobDeps{
			bus:     bus,
			budget:  ctrl,
			runID:   runID,
			logFile: filepath.Join(logDir, runID+".log"),
			dryRun:  dryRun,
		}
		if err := groomProject(ctx, cfg, proj, rt, deps); err != nil {
			log.Printf("groom %q: %v", proj.Name, err)
		}
	}
}

func groomProject(ctx context.Context, cfg *config.Config, proj config.GroomProject, rt runtime.AgentRuntime, deps jobDeps) error {
	repoPath := expandHome(cfg.RepoPath(proj.Repo))
	if repoPath == "" {
		return fmt.Errorf("repo %q has no path configured", proj.Repo)
	}

	states, err := groom.ReadIssueStates(repoPath)
	if err != nil {
		return err
	}

	var prompt string
	var model string
	var issueNumber int
	var pipelineState string

	next := groom.NextActionableIssue(states)
	if next != nil {
		model = cfg.Groom.DefaultModel
		if next.NeedsSynthesis() {
			model = cfg.Groom.SynthesisModel
		}
		issueNumber = next.Issue.Number
		pipelineState = string(next.Pipeline.State)
		prompt = fmt.Sprintf("Skill: /groom-consume %d --headless", next.Issue.Number)
		log.Printf("groom %q: processing issue #%d (state=%s, model=%s)",
			proj.Name, next.Issue.Number, next.Pipeline.State, model)
	} else if len(states) == 0 {
		model = cfg.Groom.DefaultModel
		prompt = fmt.Sprintf("Skill: /groom-consume project %q --headless", proj.Name)
		log.Printf("groom %q: starting batch intake (model=%s)", proj.Name, model)
	} else {
		cursor, err := groom.ReadBatchCursor(repoPath, proj.Name)
		if err != nil {
			return err
		}
		if cursor != nil && cursor.HasRemainingItems() {
			model = cfg.Groom.DefaultModel
			prompt = fmt.Sprintf("Skill: /groom-consume project %q --headless", proj.Name)
			log.Printf("groom %q: batch has remaining items, continuing (model=%s)", proj.Name, model)
		} else {
			log.Printf("groom %q: all issues terminal, nothing to do", proj.Name)
			return nil
		}
	}

	if deps.dryRun {
		if issueNumber > 0 {
			log.Printf("[dry-run] groom %q: would execute (issue=#%d, model=%s)", proj.Name, issueNumber, model)
		} else {
			log.Printf("[dry-run] groom %q: would execute batch intake (model=%s)", proj.Name, model)
		}
		return nil
	}

	_ = deps.bus.Emit(event.Event{
		Type:          event.JobStarted,
		CorrelationID: deps.runID,
		Payload: map[string]any{
			"job_type":       "groom",
			"project":        proj.Name,
			"issue_number":   issueNumber,
			"pipeline_state": pipelineState,
			"model":          model,
		},
	})

	stepCtx, cancel := context.WithTimeout(ctx, cfg.Groom.StepTimeout.TimeDuration())
	defer cancel()

	result, execErr := rt.Execute(stepCtx, runtime.ExecuteRequest{
		WorkingDir:   repoPath,
		Prompt:       prompt,
		MaxBudgetUSD: cfg.Groom.MaxBudgetUSD,
		Model:        model,
		MaxTurns:     cfg.Groom.MaxTurns,
	})

	if result != nil && result.Output != "" {
		if writeErr := appendToFile(deps.logFile, result.Output); writeErr != nil {
			log.Printf("groom %q: write log: %v", proj.Name, writeErr)
		}
	}

	if execErr != nil {
		if errors.Is(execErr, context.DeadlineExceeded) {
			log.Printf("groom %q: step timed out (issue=#%d, timeout=%s)", proj.Name, issueNumber, cfg.Groom.StepTimeout.TimeDuration())
		}
		_ = deps.bus.Emit(event.Event{
			Type:          event.JobFailed,
			CorrelationID: deps.runID,
			Payload: map[string]any{
				"job_type": "groom",
				"project":  proj.Name,
				"error":    execErr.Error(),
			},
		})
		return fmt.Errorf("claude execution failed: %w", execErr)
	}
	if result == nil {
		// Should not happen given ClaudeRuntime's contract, but guard defensively.
		_ = deps.bus.Emit(event.Event{
			Type:          event.JobFailed,
			CorrelationID: deps.runID,
			Payload:       map[string]any{"job_type": "groom", "project": proj.Name, "error": "nil result"},
		})
		return fmt.Errorf("runtime returned nil result without error")
	}

	_ = deps.bus.Emit(event.Event{
		Type:          event.JobCompleted,
		CorrelationID: deps.runID,
		Payload: map[string]any{
			"job_type":      "groom",
			"project":       proj.Name,
			"cost_usd":      result.CostUSD,
			"duration_secs": result.DurationSecs,
		},
	})

	if err := deps.budget.Record(ctx, "groom:"+proj.Name, deps.runID, result.CostUSD); err != nil {
		log.Printf("groom %q: record budget: %v", proj.Name, err)
	}

	log.Printf("groom %q: completed (duration=%.1fs, notional_cost=$%.4f)", proj.Name, result.DurationSecs, result.CostUSD)
	return nil
}

func filterProject(projects []config.GroomProject, name string) []config.GroomProject {
	for _, p := range projects {
		if p.Name == name {
			return []config.GroomProject{p}
		}
	}
	return nil
}

// appendToFile appends content to path, creating the file if needed.
func appendToFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.WriteString(content)
	return err
}
