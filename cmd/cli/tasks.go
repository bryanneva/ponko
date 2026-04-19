package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/bryanneva/ponko/internal/sqlite"
	"github.com/bryanneva/ponko/internal/task"
)

var tasksCmd = &cobra.Command{
	Use:   "tasks",
	Short: "List all tasks",
	RunE:  runTasks,
}

var tasksInspectCmd = &cobra.Command{
	Use:   "inspect <task-id>",
	Short: "Show full task details",
	Args:  cobra.ExactArgs(1),
	RunE:  runTasksInspect,
}

func init() {
	tasksCmd.Flags().String("status", "", "filter by status (queued, in_progress, blocked, awaiting_approval, completed, failed, cancelled)")
	tasksCmd.Flags().Bool("all", false, "show all tasks including completed and failed")
	tasksCmd.AddCommand(tasksInspectCmd)
	rootCmd.AddCommand(tasksCmd)
}

func runTasks(cmd *cobra.Command, _ []string) error {
	db, err := openDB(viper.GetString("db"))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	store := sqlite.NewTaskStore(db)
	ctx := context.Background()

	all, _ := cmd.Flags().GetBool("all")
	statusFilter, _ := cmd.Flags().GetString("status")

	var statuses []task.Status
	if statusFilter != "" {
		statuses = []task.Status{task.Status(statusFilter)}
	} else if all {
		statuses = []task.Status{
			task.StatusQueued, task.StatusInProgress, task.StatusBlocked,
			task.StatusAwaitingApproval, task.StatusCompleted, task.StatusFailed, task.StatusCancelled,
		}
	} else {
		statuses = []task.Status{task.StatusQueued, task.StatusInProgress, task.StatusBlocked, task.StatusAwaitingApproval}
	}

	tasks, err := store.ListByStatus(ctx, statuses...)
	if err != nil {
		return fmt.Errorf("list tasks: %w", err)
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks found.")
		return nil
	}

	for _, t := range tasks {
		fmt.Printf("%s  [%s]  %s\n", t.ID[:8], t.Status, t.Title)
	}
	return nil
}

func runTasksInspect(_ *cobra.Command, args []string) error {
	db, err := openDB(viper.GetString("db"))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	store := sqlite.NewTaskStore(db)
	t, err := store.Get(context.Background(), args[0])
	if err != nil {
		return err
	}
	if t == nil {
		return fmt.Errorf("task %q not found", args[0])
	}

	fmt.Printf("ID:          %s\n", t.ID)
	fmt.Printf("Issue URL:   %s\n", t.IssueURL)
	fmt.Printf("Repo:        %s\n", t.Repo)
	fmt.Printf("Title:       %s\n", t.Title)
	fmt.Printf("Workflow:    %s\n", t.Workflow)
	fmt.Printf("Status:      %s\n", t.Status)
	fmt.Printf("Phase:       %s\n", t.Phase)
	fmt.Printf("Attempts:    %d\n", t.Attempts)
	fmt.Printf("Cost USD:    $%.4f\n", t.CostUSD)
	fmt.Printf("Last Error:  %s\n", t.LastError)
	fmt.Printf("Created At:  %s\n", t.CreatedAt)
	fmt.Printf("Updated At:  %s\n", t.UpdatedAt)
	return nil
}
