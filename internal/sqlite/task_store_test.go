package sqlite_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/bryanneva/ponko/internal/sqlite"
	"github.com/bryanneva/ponko/internal/task"
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

func newTestTask() *task.Task {
	return &task.Task{
		IssueURL:    "https://github.com/owner/repo/issues/42",
		Repo:        "owner/repo",
		IssueNumber: 42,
		Title:       "Test issue",
		Labels:      []string{"bug", "ponko-runner:ready"},
		Body:        "Issue body",
		Workflow:    "fix",
		Status:      task.StatusQueued,
	}
}

func TestTaskStore_Create(t *testing.T) {
	db := openTestDB(t)
	store := sqlite.NewTaskStore(db)
	ctx := context.Background()

	tsk := newTestTask()
	if err := store.Create(ctx, tsk); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if tsk.ID == "" {
		t.Error("Create should auto-generate ID")
	}
	if tsk.CreatedAt.IsZero() {
		t.Error("Create should set CreatedAt")
	}
}

func TestTaskStore_Get(t *testing.T) {
	db := openTestDB(t)
	store := sqlite.NewTaskStore(db)
	ctx := context.Background()

	tsk := newTestTask()
	if err := store.Create(ctx, tsk); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.Get(ctx, tsk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil for existing task")
	}
	if got.ID != tsk.ID {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, tsk.ID)
	}
	if got.Title != tsk.Title {
		t.Errorf("Title mismatch: got %q, want %q", got.Title, tsk.Title)
	}
}

func TestTaskStore_Get_NotFound(t *testing.T) {
	db := openTestDB(t)
	store := sqlite.NewTaskStore(db)
	ctx := context.Background()

	got, err := store.Get(ctx, "nonexistent-id")
	if err != nil {
		t.Fatalf("Get for missing ID should not error, got: %v", err)
	}
	if got != nil {
		t.Error("Get for missing ID should return nil")
	}
}

func TestTaskStore_GetByIssueURL(t *testing.T) {
	db := openTestDB(t)
	store := sqlite.NewTaskStore(db)
	ctx := context.Background()

	tsk := newTestTask()
	if err := store.Create(ctx, tsk); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.GetByIssueURL(ctx, tsk.IssueURL)
	if err != nil {
		t.Fatalf("GetByIssueURL: %v", err)
	}
	if got == nil {
		t.Fatal("GetByIssueURL returned nil for existing URL")
	}
	if got.ID != tsk.ID {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, tsk.ID)
	}

	// Not found returns nil, nil
	missing, err := store.GetByIssueURL(ctx, "https://github.com/x/y/issues/999")
	if err != nil {
		t.Fatalf("GetByIssueURL (missing) error: %v", err)
	}
	if missing != nil {
		t.Error("GetByIssueURL (missing) should return nil")
	}
}

func TestTaskStore_ListByStatus(t *testing.T) {
	db := openTestDB(t)
	store := sqlite.NewTaskStore(db)
	ctx := context.Background()

	// Create two queued and one in_progress
	t1 := newTestTask()
	t1.IssueURL = "https://github.com/o/r/issues/1"
	t1.IssueNumber = 1
	t2 := newTestTask()
	t2.IssueURL = "https://github.com/o/r/issues/2"
	t2.IssueNumber = 2
	t3 := newTestTask()
	t3.IssueURL = "https://github.com/o/r/issues/3"
	t3.IssueNumber = 3
	t3.Status = task.StatusInProgress

	for _, tsk := range []*task.Task{t1, t2, t3} {
		if err := store.Create(ctx, tsk); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	queued, err := store.ListByStatus(ctx, task.StatusQueued)
	if err != nil {
		t.Fatalf("ListByStatus(queued): %v", err)
	}
	if len(queued) != 2 {
		t.Errorf("expected 2 queued tasks, got %d", len(queued))
	}

	// Multi-status query
	active, err := store.ListByStatus(ctx, task.StatusQueued, task.StatusInProgress)
	if err != nil {
		t.Fatalf("ListByStatus(queued,in_progress): %v", err)
	}
	if len(active) != 3 {
		t.Errorf("expected 3 active tasks, got %d", len(active))
	}
}

func TestTaskStore_UpdateStatus(t *testing.T) {
	db := openTestDB(t)
	store := sqlite.NewTaskStore(db)
	ctx := context.Background()

	tsk := newTestTask()
	if err := store.Create(ctx, tsk); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.UpdateStatus(ctx, tsk.ID, task.StatusInProgress, ""); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, _ := store.Get(ctx, tsk.ID)
	if got.Status != task.StatusInProgress {
		t.Errorf("expected in_progress, got %s", got.Status)
	}
}

func TestTaskStore_UpdatePhase(t *testing.T) {
	db := openTestDB(t)
	store := sqlite.NewTaskStore(db)
	ctx := context.Background()

	tsk := newTestTask()
	if err := store.Create(ctx, tsk); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.UpdatePhase(ctx, tsk.ID, "implement"); err != nil {
		t.Fatalf("UpdatePhase: %v", err)
	}

	got, _ := store.Get(ctx, tsk.ID)
	if got.Phase != "implement" {
		t.Errorf("expected phase 'implement', got %q", got.Phase)
	}
}

func TestTaskStore_AddCost(t *testing.T) {
	db := openTestDB(t)
	store := sqlite.NewTaskStore(db)
	ctx := context.Background()

	tsk := newTestTask()
	if err := store.Create(ctx, tsk); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.AddCost(ctx, tsk.ID, 1.50); err != nil {
		t.Fatalf("AddCost(1.50): %v", err)
	}
	if err := store.AddCost(ctx, tsk.ID, 0.75); err != nil {
		t.Fatalf("AddCost(0.75): %v", err)
	}

	got, _ := store.Get(ctx, tsk.ID)
	const want = 2.25
	if got.CostUSD < want-0.001 || got.CostUSD > want+0.001 {
		t.Errorf("CostUSD: got %f, want %f", got.CostUSD, want)
	}
}

func TestTaskStore_CountActive(t *testing.T) {
	db := openTestDB(t)
	store := sqlite.NewTaskStore(db)
	ctx := context.Background()

	makeTask := func(url string, n int, status task.Status) {
		tsk := &task.Task{
			IssueURL:    url,
			IssueNumber: n,
			Status:      status,
			CreatedAt:   time.Now(),
		}
		if err := store.Create(ctx, tsk); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	makeTask("https://github.com/o/r/issues/1", 1, task.StatusQueued)
	makeTask("https://github.com/o/r/issues/2", 2, task.StatusInProgress)
	makeTask("https://github.com/o/r/issues/3", 3, task.StatusBlocked)
	makeTask("https://github.com/o/r/issues/4", 4, task.StatusCompleted)
	makeTask("https://github.com/o/r/issues/5", 5, task.StatusFailed)

	count, err := store.CountActive(ctx)
	if err != nil {
		t.Fatalf("CountActive: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 active tasks, got %d", count)
	}
}
