package sqlite_test

import (
	"context"
	"testing"

	"github.com/bryanneva/ponko/internal/devrouter"
	"github.com/bryanneva/ponko/internal/sqlite"
)

func newTestPipeline() *devrouter.Pipeline {
	return &devrouter.Pipeline{
		IssueURL:    "https://github.com/owner/repo/issues/1",
		Repo:        "owner/repo",
		IssueNumber: 1,
		IssueTitle:  "Test issue title",
		IssueBody:   "Test issue body",
		Stage:       devrouter.StagePending,
	}
}

func TestPipelineStore_Create(t *testing.T) {
	db := openTestDB(t)
	store := sqlite.NewPipelineStore(db)
	ctx := context.Background()

	p := newTestPipeline()
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p.ID == "" {
		t.Error("Create should auto-generate ID")
	}
	if p.CreatedAt.IsZero() {
		t.Error("Create should set CreatedAt")
	}
}

func TestPipelineStore_Get(t *testing.T) {
	db := openTestDB(t)
	store := sqlite.NewPipelineStore(db)
	ctx := context.Background()

	p := newTestPipeline()
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.IssueURL != p.IssueURL {
		t.Errorf("IssueURL: got %q, want %q", got.IssueURL, p.IssueURL)
	}
}

func TestPipelineStore_Get_NotFound(t *testing.T) {
	db := openTestDB(t)
	store := sqlite.NewPipelineStore(db)
	ctx := context.Background()

	got, err := store.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Error("expected nil for unknown ID")
	}
}

func TestPipelineStore_UpdateStage(t *testing.T) {
	db := openTestDB(t)
	store := sqlite.NewPipelineStore(db)
	ctx := context.Background()

	p := newTestPipeline()
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.UpdateStage(ctx, p.ID, devrouter.StagePending, devrouter.StageClassifying); err != nil {
		t.Fatalf("UpdateStage: %v", err)
	}

	got, _ := store.Get(ctx, p.ID)
	if got.Stage != devrouter.StageClassifying {
		t.Errorf("expected stage %s, got %s", devrouter.StageClassifying, got.Stage)
	}
}

func TestPipelineStore_UpdateStage_InvalidTransition(t *testing.T) {
	db := openTestDB(t)
	store := sqlite.NewPipelineStore(db)
	ctx := context.Background()

	p := newTestPipeline()
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	err := store.UpdateStage(ctx, p.ID, devrouter.StagePending, devrouter.StageCompleted)
	if err == nil {
		t.Error("expected error for invalid transition")
	}
}

func TestPipelineStore_SetTrack(t *testing.T) {
	db := openTestDB(t)
	store := sqlite.NewPipelineStore(db)
	ctx := context.Background()

	p := newTestPipeline()
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.SetTrack(ctx, p.ID, devrouter.TrackFix); err != nil {
		t.Fatalf("SetTrack: %v", err)
	}

	got, _ := store.Get(ctx, p.ID)
	if got.Track != devrouter.TrackFix {
		t.Errorf("expected track %s, got %s", devrouter.TrackFix, got.Track)
	}
}

func TestPipelineStore_SetPlanOutput(t *testing.T) {
	db := openTestDB(t)
	store := sqlite.NewPipelineStore(db)
	ctx := context.Background()

	p := newTestPipeline()
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	plan := `{"stories":[]}`
	if err := store.SetPlanOutput(ctx, p.ID, plan); err != nil {
		t.Fatalf("SetPlanOutput: %v", err)
	}

	got, _ := store.Get(ctx, p.ID)
	if got.PlanOutput != plan {
		t.Errorf("PlanOutput: got %q, want %q", got.PlanOutput, plan)
	}
}

func TestPipelineStore_IncrStoriesCompleted(t *testing.T) {
	db := openTestDB(t)
	store := sqlite.NewPipelineStore(db)
	ctx := context.Background()

	p := newTestPipeline()
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	count, err := store.IncrStoriesCompleted(ctx, p.ID)
	if err != nil {
		t.Fatalf("IncrStoriesCompleted: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1, got %d", count)
	}

	count, err = store.IncrStoriesCompleted(ctx, p.ID)
	if err != nil {
		t.Fatalf("IncrStoriesCompleted: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}
}

func TestPipelineStore_List(t *testing.T) {
	db := openTestDB(t)
	store := sqlite.NewPipelineStore(db)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		p := newTestPipeline()
		if err := store.Create(ctx, p); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	pipelines, err := store.List(ctx, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(pipelines) != 3 {
		t.Errorf("expected 3 pipelines, got %d", len(pipelines))
	}

	limited, err := store.List(ctx, 2)
	if err != nil {
		t.Fatalf("List limited: %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("expected 2 pipelines with limit=2, got %d", len(limited))
	}
}

func TestPipelineStore_CreateRoundTrips_IssueContent(t *testing.T) {
	db := openTestDB(t)
	store := sqlite.NewPipelineStore(db)
	ctx := context.Background()

	p := newTestPipeline()
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.IssueTitle != "Test issue title" {
		t.Errorf("IssueTitle: got %q, want %q", got.IssueTitle, "Test issue title")
	}
	if got.IssueBody != "Test issue body" {
		t.Errorf("IssueBody: got %q, want %q", got.IssueBody, "Test issue body")
	}
}

func TestPipelineStore_GetByIssueURL(t *testing.T) {
	db := openTestDB(t)
	store := sqlite.NewPipelineStore(db)
	ctx := context.Background()

	p := newTestPipeline()
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Found case
	got, err := store.GetByIssueURL(ctx, p.IssueURL)
	if err != nil {
		t.Fatalf("GetByIssueURL: %v", err)
	}
	if got == nil {
		t.Fatal("expected pipeline, got nil")
	}
	if got.ID != p.ID {
		t.Errorf("ID: got %q, want %q", got.ID, p.ID)
	}

	// Not found case
	notFound, err := store.GetByIssueURL(ctx, "https://github.com/owner/repo/issues/9999")
	if err != nil {
		t.Fatalf("GetByIssueURL not-found: %v", err)
	}
	if notFound != nil {
		t.Error("expected nil for unknown issue URL")
	}
}

func TestPipelineStore_SetIssueContent(t *testing.T) {
	db := openTestDB(t)
	store := sqlite.NewPipelineStore(db)
	ctx := context.Background()

	p := newTestPipeline()
	p.IssueTitle = ""
	p.IssueBody = ""
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.SetIssueContent(ctx, p.ID, "Updated title", "Updated body"); err != nil {
		t.Fatalf("SetIssueContent: %v", err)
	}

	got, _ := store.Get(ctx, p.ID)
	if got.IssueTitle != "Updated title" {
		t.Errorf("IssueTitle: got %q, want %q", got.IssueTitle, "Updated title")
	}
	if got.IssueBody != "Updated body" {
		t.Errorf("IssueBody: got %q, want %q", got.IssueBody, "Updated body")
	}
}
