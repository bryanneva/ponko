package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/bryanneva/ponko/internal/approval"
	"github.com/bryanneva/ponko/internal/budget"
	"github.com/bryanneva/ponko/internal/config"
	"github.com/bryanneva/ponko/internal/devrouter"
	"github.com/bryanneva/ponko/internal/event"
	"github.com/bryanneva/ponko/internal/intake"
	"github.com/bryanneva/ponko/internal/runtime"
	"github.com/bryanneva/ponko/internal/sqlite"
)

var devRouterCmd = &cobra.Command{
	Use:   "dev-router",
	Short: "Advance active dev-router pipelines",
	Long: `Classifies GitHub issues, runs planning, executes stories sequentially,
and gates on CI + human approval before completion.

With no flags, advances all non-terminal pipelines by one step each.`,
	RunE: runDevRouter,
}

func init() {
	devRouterCmd.Flags().Bool("dry-run", false, "show planned actions without executing agents")
	devRouterCmd.Flags().String("pipeline-id", "", "process only the given pipeline ID")
	rootCmd.AddCommand(devRouterCmd)
}

func runDevRouter(cmd *cobra.Command, _ []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	pipelineID, _ := cmd.Flags().GetString("pipeline-id")

	cfgPath := viper.GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := openDB(viper.GetString("db"))
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	err = sqlite.Migrate(ctx, db)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	eventsPath := expandHome(viper.GetString("events"))
	bus, err := event.NewJSONLBus(eventsPath)
	if err != nil {
		return fmt.Errorf("open event bus: %w", err)
	}

	ctrl := budget.NewController(db, cfg.Budget)
	exec := &osExec{}
	rt := runtime.NewClaudeRuntime(exec)
	store := sqlite.NewPipelineStore(db)

	// Reuse intake.Intake for Commenter and ApprovalChecker since it already wraps gh CLI.
	intaker := intake.NewIntake(*cfg, exec)
	ciChecker := newGHCIChecker(exec)

	if pipelineID == "" {
		created, err := devrouter.PollNewIssues(ctx, cfg.Repos, cfg.DevRouter.IntakeLabel, store, exec, dryRun)
		if err != nil {
			return fmt.Errorf("dev-router intake: %w", err)
		}
		if created > 0 {
			log.Printf("dev-router: created %d new pipeline(s)", created)
		}
	}

	runner := devrouter.NewPipelineRunner(
		store,
		rt,
		bus,
		ctrl,
		intaker,
		ciChecker,
		intaker,
		cfg.DevRouter,
		exec,
	)
	runner.SetDryRun(dryRun)

	if pipelineID != "" {
		log.Printf("dev-router: processing pipeline %s (dry-run=%v)", pipelineID, dryRun)
		if err := runner.RunOnce(ctx, pipelineID); err != nil {
			return fmt.Errorf("run pipeline %s: %w", pipelineID, err)
		}
	} else {
		log.Printf("dev-router: processing all active pipelines (dry-run=%v)", dryRun)
		if err := runner.ProcessAll(ctx); err != nil {
			return fmt.Errorf("process all pipelines: %w", err)
		}
	}

	log.Printf("dev-router: done")
	return nil
}

// ghCIChecker checks CI status for a branch using gh run list.
type ghCIChecker struct {
	exec interface {
		Run(ctx context.Context, dir, name string, args ...string) ([]byte, error)
	}
}

func newGHCIChecker(exec interface {
	Run(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}) *ghCIChecker {
	return &ghCIChecker{exec: exec}
}

func (c *ghCIChecker) CheckCI(ctx context.Context, repo, branch string) (string, error) {
	args := []string{"run", "list", "--limit", "1", "--json", "conclusion,status"}
	if repo != "" {
		args = append(args, "--repo", repo)
	}
	if branch != "" {
		args = append(args, "--branch", branch)
	}

	out, err := c.exec.Run(ctx, "", "gh", args...)
	if err != nil {
		// Treat exec errors as pending (CI not yet registered)
		return "pending", nil
	}

	return parseCIStatusJSON(out), nil
}

// parseCIStatusJSON interprets gh run list JSON output into pending/green/failed.
func parseCIStatusJSON(raw []byte) string {
	var runs []struct {
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	}
	if err := json.Unmarshal(raw, &runs); err != nil || len(runs) == 0 {
		return "pending"
	}
	r := runs[0]
	switch {
	case r.Status != "completed":
		return "pending"
	case r.Conclusion == "success":
		return "green"
	case strings.Contains(r.Conclusion, "failure") || r.Conclusion == "cancelled":
		return "failed"
	default:
		return "pending"
	}
}

// Verify interfaces are satisfied at compile time.
var _ devrouter.Commenter = (*intake.Intake)(nil)
var _ devrouter.ApprovalChecker = (*intake.Intake)(nil)
var _ devrouter.CIChecker = (*ghCIChecker)(nil)

// approvalStatusAdapter satisfies devrouter.ApprovalChecker using intake.Intake.
// intake.Intake returns approval.Status; devrouter.ApprovalChecker also uses approval.Status.
var _ interface {
	CheckApproval(ctx context.Context, issueURL string) (approval.Status, error)
} = (*intake.Intake)(nil)
