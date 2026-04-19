package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/bryanneva/ponko/internal/budget"
	"github.com/bryanneva/ponko/internal/config"
	"github.com/bryanneva/ponko/internal/event"
	"github.com/bryanneva/ponko/internal/runtime"
	"github.com/bryanneva/ponko/internal/scheduler"
	"github.com/bryanneva/ponko/internal/serve"
	"github.com/bryanneva/ponko/internal/sqlite"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start observability web UI with built-in groom scheduler",
	RunE:  runServe,
}

func init() {
	serveCmd.Flags().Int("port", 8765, "port to listen on")
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, _ []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	port, err := cmd.Flags().GetInt("port")
	if err != nil {
		return err
	}

	db, err := openDB(viper.GetString("db"))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	cfgPath := viper.GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Printf("serve: config load failed (%v) — repos and groom projects will be empty", err)
		cfg = &config.Config{Budget: config.DefaultBudget(), Groom: config.GroomConfig{Interval: config.Duration(15 * time.Minute)}}
	}

	err = sqlite.Migrate(ctx, db)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	store := sqlite.NewTaskStore(db)
	ctrl := budget.NewController(db, cfg.Budget)
	eventsPath := expandHome(viper.GetString("events"))
	logDir := expandHome("~/.ponko-runner/logs")

	err = os.MkdirAll(logDir, 0o755)
	if err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	bus, err := event.NewJSONLBus(eventsPath)
	if err != nil {
		return fmt.Errorf("open event bus: %w", err)
	}

	rt := runtime.NewClaudeRuntime(&osExec{})

	tasks := []scheduler.AutomatedTask{
		NewGroomTask(cfg, rt, bus, ctrl, logDir),
		&StaleIssueCleanupTask{},
	}
	sched := scheduler.New(cfg.Groom.Interval.TimeDuration(), tasks)
	go sched.Start(ctx)

	srv := serve.New(store, ctrl, cfg.Budget, eventsPath, cfg.Repos, cfg.Groom.Projects, logDir, sched)
	srv.SetPipelineStore(sqlite.NewPipelineStore(db))
	return srv.ListenAndServe(ctx, fmt.Sprintf(":%d", port))
}
