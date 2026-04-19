package budget_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/bryanneva/ponko/internal/budget"
	"github.com/bryanneva/ponko/internal/config"
	"github.com/bryanneva/ponko/internal/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := sqlite.Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

func defaultLimits() config.Budget {
	return config.Budget{
		PerRunUSD:  10.00,
		PerDayUSD:  50.00,
		PerTaskUSD: 5.00,
	}
}

func TestController_Record(t *testing.T) {
	db := openTestDB(t)
	ctrl := budget.NewController(db, defaultLimits())
	ctx := context.Background()

	if err := ctrl.Record(ctx, "task-1", "run-1", 1.50); err != nil {
		t.Fatalf("Record: %v", err)
	}
}

func TestController_RunSpent(t *testing.T) {
	db := openTestDB(t)
	ctrl := budget.NewController(db, defaultLimits())
	ctx := context.Background()

	_ = ctrl.Record(ctx, "task-1", "run-1", 2.00)
	_ = ctrl.Record(ctx, "task-2", "run-1", 1.50)
	_ = ctrl.Record(ctx, "task-3", "run-2", 9.00) // different run

	spent, err := ctrl.RunSpent(ctx, "run-1")
	if err != nil {
		t.Fatalf("RunSpent: %v", err)
	}
	const want = 3.50
	if spent < want-0.001 || spent > want+0.001 {
		t.Errorf("RunSpent: got %f, want %f", spent, want)
	}
}

func TestController_DaySpent(t *testing.T) {
	db := openTestDB(t)
	ctrl := budget.NewController(db, defaultLimits())
	ctx := context.Background()

	_ = ctrl.Record(ctx, "task-1", "run-1", 3.00)
	_ = ctrl.Record(ctx, "task-2", "run-2", 4.00)

	spent, err := ctrl.DaySpent(ctx, time.Now().UTC().Format("2006-01-02"))
	if err != nil {
		t.Fatalf("DaySpent: %v", err)
	}
	// Both should be recorded for today (the impl sets today's date)
	if spent < 6.999 {
		t.Errorf("DaySpent: got %f, want >= 7.00", spent)
	}
}

func TestController_CanAffordRun(t *testing.T) {
	db := openTestDB(t)
	limits := config.Budget{PerRunUSD: 5.00, PerDayUSD: 100.00, PerTaskUSD: 100.00}
	ctrl := budget.NewController(db, limits)
	ctx := context.Background()

	_ = ctrl.Record(ctx, "task-1", "run-1", 3.00) // 3.00 spent on run-1

	// 3.00 spent + 1.50 estimated = 4.50 <= 5.00 — should afford
	if !ctrl.CanAffordRun(ctx, "run-1", 1.50) {
		t.Error("CanAffordRun: expected true (4.50 <= 5.00)")
	}

	// 3.00 spent + 3.00 estimated = 6.00 > 5.00 — should not afford
	if ctrl.CanAffordRun(ctx, "run-1", 3.00) {
		t.Error("CanAffordRun: expected false (6.00 > 5.00)")
	}
}

func TestController_CanAffordDay(t *testing.T) {
	db := openTestDB(t)
	limits := config.Budget{PerRunUSD: 100.00, PerDayUSD: 10.00, PerTaskUSD: 100.00}
	ctrl := budget.NewController(db, limits)
	ctx := context.Background()

	_ = ctrl.Record(ctx, "task-1", "run-1", 8.00) // 8.00 spent today

	if !ctrl.CanAffordDay(ctx, 1.50) {
		t.Error("CanAffordDay: expected true (9.50 <= 10.00)")
	}
	if ctrl.CanAffordDay(ctx, 3.00) {
		t.Error("CanAffordDay: expected false (11.00 > 10.00)")
	}
}

func TestController_CanAffordTask(t *testing.T) {
	db := openTestDB(t)
	limits := config.Budget{PerRunUSD: 100.00, PerDayUSD: 100.00, PerTaskUSD: 5.00}
	ctrl := budget.NewController(db, limits)
	ctx := context.Background()

	_ = ctrl.Record(ctx, "task-1", "run-1", 3.00) // 3.00 spent on task-1

	if !ctrl.CanAffordTask(ctx, "task-1", 1.50) {
		t.Error("CanAffordTask: expected true (4.50 <= 5.00)")
	}
	if ctrl.CanAffordTask(ctx, "task-1", 3.00) {
		t.Error("CanAffordTask: expected false (6.00 > 5.00)")
	}
}
