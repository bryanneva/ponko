package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/bryanneva/ponko/internal/sqlite"
	"github.com/bryanneva/ponko/internal/task"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show active tasks",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(_ *cobra.Command, _ []string) error {
	db, err := openDB(viper.GetString("db"))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	store := sqlite.NewTaskStore(db)
	ctx := context.Background()

	tasks, err := store.ListByStatus(ctx, task.StatusQueued, task.StatusInProgress, task.StatusBlocked, task.StatusAwaitingApproval)
	if err != nil {
		return fmt.Errorf("list tasks: %w", err)
	}

	if len(tasks) == 0 {
		fmt.Println("No active tasks.")
		return nil
	}

	fmt.Printf("%-8s  %-40s  %-10s  %-12s  %-10s  %s\n",
		"ID", "Issue", "Workflow", "Phase", "Status", "Cost")
	fmt.Println(strings.Repeat("-", 100))
	for _, t := range tasks {
		id := t.ID
		if len(id) > 8 {
			id = id[:8]
		}
		issue := t.IssueURL
		if len(issue) > 40 {
			issue = "..." + issue[len(issue)-37:]
		}
		fmt.Printf("%-8s  %-40s  %-10s  %-12s  %-10s  $%.4f\n",
			id, issue, t.Workflow, t.Phase, string(t.Status), t.CostUSD)
	}
	return nil
}

