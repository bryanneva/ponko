// Package budget tracks orchestrator spend and enforces per-run, per-day, and per-task limits.
package budget

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/bryanneva/ponko/internal/config"
)

// Controller defines the budget tracking interface.
type Controller interface {
	CanAffordRun(ctx context.Context, runID string, estimated float64) bool
	CanAffordDay(ctx context.Context, estimated float64) bool
	CanAffordTask(ctx context.Context, taskID string, estimated float64) bool
	Record(ctx context.Context, taskID, runID string, cost float64) error
	RunSpent(ctx context.Context, runID string) (float64, error)
	DaySpent(ctx context.Context, date string) (float64, error)
}

// SQLiteController implements Controller using the budget_usage table.
type SQLiteController struct {
	db     *sql.DB
	limits config.Budget
}

// NewController returns a SQLiteController with the given database and limits.
func NewController(db *sql.DB, limits config.Budget) *SQLiteController {
	return &SQLiteController{db: db, limits: limits}
}

// Record inserts a spend row for the given task, run, and cost.
func (c *SQLiteController) Record(ctx context.Context, taskID, runID string, cost float64) error {
	date := time.Now().UTC().Format("2006-01-02")
	_, err := c.db.ExecContext(ctx,
		`INSERT INTO budget_usage (task_id, run_id, cost_usd, date) VALUES (?, ?, ?, ?)`,
		taskID, runID, cost, date,
	)
	if err != nil {
		return fmt.Errorf("record budget: %w", err)
	}
	return nil
}

// RunSpent returns the total spend for the given run ID.
func (c *SQLiteController) RunSpent(ctx context.Context, runID string) (float64, error) {
	return c.sumWhere(ctx, "run_id = ?", runID)
}

// DaySpent returns the total spend for the given date (YYYY-MM-DD).
func (c *SQLiteController) DaySpent(ctx context.Context, date string) (float64, error) {
	return c.sumWhere(ctx, "date = ?", date)
}

// CanAffordRun returns true if running estimated additional cost won't exceed per_run_usd.
func (c *SQLiteController) CanAffordRun(ctx context.Context, runID string, estimated float64) bool {
	spent, err := c.RunSpent(ctx, runID)
	if err != nil {
		return false
	}
	return spent+estimated <= c.limits.PerRunUSD
}

// CanAffordDay returns true if today's spend + estimated won't exceed per_day_usd.
func (c *SQLiteController) CanAffordDay(ctx context.Context, estimated float64) bool {
	today := time.Now().UTC().Format("2006-01-02")
	spent, err := c.DaySpent(ctx, today)
	if err != nil {
		return false
	}
	return spent+estimated <= c.limits.PerDayUSD
}

// CanAffordTask returns true if the task's total spend + estimated won't exceed per_task_usd.
func (c *SQLiteController) CanAffordTask(ctx context.Context, taskID string, estimated float64) bool {
	spent, err := c.sumWhere(ctx, "task_id = ?", taskID)
	if err != nil {
		return false
	}
	return spent+estimated <= c.limits.PerTaskUSD
}

func (c *SQLiteController) sumWhere(ctx context.Context, where string, arg any) (float64, error) {
	var total float64
	err := c.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost_usd), 0) FROM budget_usage WHERE `+where,
		arg,
	).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("sum budget: %w", err)
	}
	return total, nil
}
