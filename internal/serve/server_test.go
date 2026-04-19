package serve_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bryanneva/ponko/internal/config"
	"github.com/bryanneva/ponko/internal/devrouter"
	"github.com/bryanneva/ponko/internal/serve"
	"github.com/bryanneva/ponko/internal/sqlite"
)

// TODO: serve handler tests (#48 + follow-up from PR #47 retro)
//
// Priority test cases (3 of 5 slim-pr-review findings were testable):
//
//  1. TestHandleJobLog_PathTraversal — runIDs containing "/" or "\" should return 404
//  2. TestHandleJobLog_UnknownRunID — non-existent run_id should return 404 (currently returns 200, see #48)
//  3. TestHandleJobLog_LargeFile — response should be capped at 512KB
//  4. TestHandleJobs_EmptyEvents — /api/jobs with no events.jsonl should return {runs:[], projects:[]}
//  5. TestHandleJobs_RunGrouping — job.started/completed pairs should group into a single run

func newTestServer(t *testing.T) (*serve.Server, *sqlite.PipelineStore) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := sqlite.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	taskStore := sqlite.NewTaskStore(db)
	pipelineStore := sqlite.NewPipelineStore(db)

	srv := serve.New(
		taskStore,
		nil, // budget.Controller not needed for pipeline tests
		config.Budget{},
		t.TempDir()+"/events.jsonl",
		nil, nil,
		t.TempDir(),
		nil,
	)
	srv.SetPipelineStore(pipelineStore)
	return srv, pipelineStore
}

func TestHandlePipelines_EmptyList(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/pipelines", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}

	var result []any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if result == nil || len(result) != 0 {
		t.Errorf("expected [] not null, got %v", result)
	}
}

func TestHandlePipelines_ReturnsList(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	p := &devrouter.Pipeline{
		IssueURL:                "https://github.com/owner/repo/issues/1",
		Repo:                    "owner/repo",
		IssueNumber:             1,
		Track:                   devrouter.TrackFix,
		Stage:                   devrouter.StagePending,
		ClassificationRationale: "simple fix",
	}
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/pipelines", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var result []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 pipeline, got %d", len(result))
	}

	pipeline := result[0]
	checkField := func(key string, want any) {
		t.Helper()
		got, ok := pipeline[key]
		if !ok {
			t.Errorf("missing field %q", key)
			return
		}
		if fmt := "%v"; fmt != "" {
			_ = fmt
		}
		if got != want {
			t.Errorf("field %q: got %v, want %v", key, got, want)
		}
	}
	checkField("issue_url", "https://github.com/owner/repo/issues/1")
	checkField("track", "fix")
	checkField("stage", "pending")
}

func TestHandlePipelineByID_Found(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	p := &devrouter.Pipeline{
		IssueURL:   "https://github.com/owner/repo/issues/2",
		Stage:      devrouter.StagePlanning,
		PlanOutput: `{"stories":[]}`,
	}
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/pipelines/"+p.ID, nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if result["id"] != p.ID {
		t.Errorf("id mismatch: got %v, want %v", result["id"], p.ID)
	}
	// plan_output should be parsed as JSON object
	if result["plan_output"] == nil {
		t.Error("expected plan_output to be present")
	}
}

func TestHandlePipelineByID_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/pipelines/nonexistent-id", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}
