package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bryanneva/ponko/internal/testutil"
)

func TestHandleDiscardJob(t *testing.T) {
	pool := testutil.TestDB(t)

	// Seed a retryable job
	var retryableID int64
	err := pool.QueryRow(context.Background(),
		`INSERT INTO river_job (kind, state, args, queue, priority, max_attempts)
		VALUES ('test_kind', 'retryable', '{}', 'default', 1, 25)
		RETURNING id`).Scan(&retryableID)
	if err != nil {
		t.Fatalf("seeding retryable job: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE id = $1`, retryableID)
	})

	// Seed an available job
	var availableID int64
	err = pool.QueryRow(context.Background(),
		`INSERT INTO river_job (kind, state, args, queue, priority, max_attempts)
		VALUES ('test_kind', 'available', '{}', 'default', 1, 25)
		RETURNING id`).Scan(&availableID)
	if err != nil {
		t.Fatalf("seeding available job: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE id = $1`, availableID)
	})

	// Seed a completed job (terminal state)
	var completedID int64
	err = pool.QueryRow(context.Background(),
		`INSERT INTO river_job (kind, state, args, queue, priority, max_attempts, finalized_at)
		VALUES ('test_kind', 'completed', '{}', 'default', 1, 25, now())
		RETURNING id`).Scan(&completedID)
	if err != nil {
		t.Fatalf("seeding completed job: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE id = $1`, completedID)
	})

	handler := handleDiscardJob(pool)

	t.Run("discards retryable job", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/jobs/1/discard", nil)
		req.SetPathValue("id", fmt.Sprintf("%d", retryableID))
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var body recentJob
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("decoding response: %v", err)
		}
		if body.ID != retryableID {
			t.Errorf("expected job id %d, got %d", retryableID, body.ID)
		}
		if body.State != "discarded" {
			t.Errorf("expected state discarded, got %s", body.State)
		}
		if body.Kind != "test_kind" {
			t.Errorf("expected kind test_kind, got %s", body.Kind)
		}
		if body.FinalizedAt == nil {
			t.Error("expected finalizedAt to be set")
		}
	})

	t.Run("discards available job", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/jobs/1/discard", nil)
		req.SetPathValue("id", fmt.Sprintf("%d", availableID))
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var body recentJob
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("decoding response: %v", err)
		}
		if body.State != "discarded" {
			t.Errorf("expected state discarded, got %s", body.State)
		}
	})

	t.Run("404 for already-terminal job", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/jobs/1/discard", nil)
		req.SetPathValue("id", fmt.Sprintf("%d", completedID))
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("404 for non-existent job", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/jobs/999999999/discard", nil)
		req.SetPathValue("id", "999999999")
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("400 for invalid job id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/jobs/abc/discard", nil)
		req.SetPathValue("id", "abc")
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("400 for negative job id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/jobs/-1/discard", nil)
		req.SetPathValue("id", "-1")
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})
}

