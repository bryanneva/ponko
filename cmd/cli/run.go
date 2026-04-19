package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	osexec "os/exec"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riversqlite"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/bryanneva/ponko/internal/budget"
	"github.com/bryanneva/ponko/internal/config"
	"github.com/bryanneva/ponko/internal/event"
	"github.com/bryanneva/ponko/internal/intake"
	"github.com/bryanneva/ponko/internal/routing"
	"github.com/bryanneva/ponko/internal/runtime"
	"github.com/bryanneva/ponko/internal/sqlite"
	"github.com/bryanneva/ponko/internal/task"
	"github.com/bryanneva/ponko/internal/worker"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Execute one orchestration pass",
	RunE:  runOtto,
}

func init() {
	runCmd.Flags().Bool("dry-run", false, "show planned actions without executing agents")
	runCmd.Flags().String("project", "", "filter issues by GitHub Project title")
	rootCmd.AddCommand(runCmd)
}

func runOtto(cmd *cobra.Command, _ []string) error {
	cfgPath := viper.GetString("config")
	dbPath := expandHome(viper.GetString("db"))
	eventsPath := expandHome(viper.GetString("events"))
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	project, _ := cmd.Flags().GetString("project")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if project != "" {
		cfg.Project = project
	}

	err = sqlite.EnsureDir(dbPath)
	if err != nil {
		return err
	}
	db, err := sqlite.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	err = sqlite.MigrateRiver(ctx, db)
	if err != nil {
		return fmt.Errorf("river migrate: %w", err)
	}
	err = sqlite.Migrate(ctx, db)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	bus, err := event.NewJSONLBus(eventsPath)
	if err != nil {
		return fmt.Errorf("event bus: %w", err)
	}

	budgetCtrl := budget.NewController(db, cfg.Budget)
	exec := &osExec{}
	rt := runtime.NewClaudeRuntime(exec)
	intaker := intake.NewIntake(*cfg, exec)
	router := routing.NewRouter(cfg.Rules)

	if dryRun {
		return runDryRun(ctx, intaker, router)
	}

	workers := river.NewWorkers()
	worker.RegisterTaskPhase(workers, worker.TaskPhaseWorkerDeps{
		Cfg:         *cfg,
		Budget:      budgetCtrl,
		Bus:         bus,
		Runtime:     rt,
		GateChecker: intaker,
		Commenter:   intaker,
		Approval:    intaker,
	})
	worker.RegisterApprovalPoller(workers, worker.ApprovalPollerWorkerDeps{
		ApprovalChecker: intaker,
		Commenter:       intaker,
		Bus:             bus,
	})

	client, err := river.NewClient(riversqlite.New(db), &river.Config{
		Workers: workers,
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 1},
		},
	})
	if err != nil {
		return fmt.Errorf("create river client: %w", err)
	}
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("start river client: %w", err)
	}
	defer func() {
		if err := client.Stop(ctx); err != nil {
			log.Printf("stop river client: %v", err)
		}
	}()

	if err := doIntake(ctx, cfg, intaker, router, client, db, bus); err != nil {
		return fmt.Errorf("intake: %w", err)
	}
	if err := sqlite.DrainRiverQueue(ctx, db); err != nil {
		return fmt.Errorf("drain: %w", err)
	}

	return nil
}

func runDryRun(ctx context.Context, intaker *intake.Intake, router *routing.Router) error {
	issues, err := intaker.PollRaw(ctx)
	if err != nil {
		return err
	}
	for _, issue := range issues {
		workflow, err := router.Match(issue.Labels)
		if err != nil {
			log.Printf("[dry-run] no matching workflow for %s: %v", issue.IssueURL, err)
			continue
		}
		log.Printf("[dry-run] would enqueue task_phase job: issue=%s workflow=%s", issue.IssueURL, workflow)
	}
	return nil
}

func doIntake(
	ctx context.Context,
	cfg *config.Config,
	intaker *intake.Intake,
	router *routing.Router,
	client *river.Client[*sql.Tx],
	db *sql.DB,
	bus event.Bus,
) error {
	active, err := countActiveRiverTasks(ctx, db)
	if err != nil {
		return err
	}
	issues, err := intaker.PollRaw(ctx)
	if err != nil {
		return err
	}

	inserted := 0
	for _, issue := range issues {
		if cfg.MaxActiveTasks > 0 && active+inserted >= cfg.MaxActiveTasks {
			break
		}

		workflow, err := router.Match(issue.Labels)
		if err != nil {
			log.Printf("no matching workflow for issue %s: %v", issue.IssueURL, err)
			continue
		}
		issue.Workflow = workflow

		result, err := client.Insert(ctx, taskPhaseArgs(issue), &river.InsertOpts{
			MaxAttempts: worker.TaskPhaseMaxAttempts,
			UniqueOpts:  river.UniqueOpts{ByArgs: true},
		})
		if err != nil {
			return fmt.Errorf("insert job for %s: %w", issue.IssueURL, err)
		}
		if result.UniqueSkippedAsDuplicate {
			continue
		}

		inserted++
		if err := intaker.MarkInProgress(ctx, issue); err != nil {
			log.Printf("%v", err)
		}
		_ = bus.Emit(event.Event{
			Type:   event.TaskCreated,
			TaskID: issue.IssueURL,
			Payload: map[string]any{
				"issue_url": issue.IssueURL,
				"workflow":  issue.Workflow,
			},
		})
	}

	return nil
}

func countActiveRiverTasks(ctx context.Context, db *sql.DB) (int, error) {
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM river_job
		WHERE kind IN ('task_phase', 'approval_poller')
		  AND state IN ('available', 'running', 'pending', 'retryable', 'scheduled')
	`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active river tasks: %w", err)
	}
	return count, nil
}

func taskPhaseArgs(issue *task.Task) worker.TaskPhaseArgs {
	return worker.TaskPhaseArgs{
		IssueURL:    issue.IssueURL,
		Repo:        issue.Repo,
		IssueNumber: issue.IssueNumber,
		Title:       issue.Title,
		Labels:      issue.Labels,
		Body:        issue.Body,
		Workflow:    issue.Workflow,
	}
}

// osExec implements CommandExecutor using os/exec.
type osExec struct{}

func (e *osExec) Run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := osexec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	return cmd.Output()
}
