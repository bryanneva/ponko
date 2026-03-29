package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type jobSummary struct {
	Available int `json:"available"`
	Running   int `json:"running"`
	Completed int `json:"completed"`
	Discarded int `json:"discarded"`
	Cancelled int `json:"cancelled"`
	Scheduled int `json:"scheduled"`
	Retryable int `json:"retryable"`
}

func handleJobsSummary(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := pool.Query(r.Context(),
			`SELECT state, count(*) FROM river_job GROUP BY state`)
		if err != nil {
			slog.Error("failed to query job summary", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to query job summary")
			return
		}
		defer rows.Close()

		var summary jobSummary
		for rows.Next() {
			var state string
			var count int
			if err := rows.Scan(&state, &count); err != nil {
				slog.Error("failed to scan job summary row", "error", err)
				writeError(w, http.StatusInternalServerError, "failed to query job summary")
				return
			}
			switch state {
			case "available":
				summary.Available = count
			case "running":
				summary.Running = count
			case "completed":
				summary.Completed = count
			case "discarded":
				summary.Discarded = count
			case "cancelled":
				summary.Cancelled = count
			case "scheduled":
				summary.Scheduled = count
			case "retryable":
				summary.Retryable = count
			}
		}
		if err := rows.Err(); err != nil {
			slog.Error("failed to iterate job summary rows", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to query job summary")
			return
		}

		writeJSON(w, http.StatusOK, summary)
	}
}

type recentJob struct {
	Kind        string   `json:"kind"`
	State       string   `json:"state"`
	CreatedAt   string   `json:"createdAt"`
	FinalizedAt *string  `json:"finalizedAt"`
	Errors      []string `json:"errors"`
	ID          int64    `json:"id"`
}

func handleRecentJobs(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 200 {
				limit = parsed
			}
		}

		rows, err := pool.Query(r.Context(),
			`SELECT id, kind, state, created_at, finalized_at, errors
			FROM river_job ORDER BY created_at DESC LIMIT $1`, limit)
		if err != nil {
			slog.Error("failed to query recent jobs", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to query recent jobs")
			return
		}
		defer rows.Close()

		jobs := make([]recentJob, 0)
		for rows.Next() {
			var j recentJob
			var createdAt time.Time
			var finalizedAt *time.Time
			var errorsJSON []byte

			if err := rows.Scan(&j.ID, &j.Kind, &j.State, &createdAt, &finalizedAt, &errorsJSON); err != nil {
				slog.Error("failed to scan job row", "error", err)
				writeError(w, http.StatusInternalServerError, "failed to query recent jobs")
				return
			}

			j.CreatedAt = createdAt.Format(time.RFC3339)
			if finalizedAt != nil {
				s := finalizedAt.Format(time.RFC3339)
				j.FinalizedAt = &s
			}
			j.Errors = parseJobErrors(errorsJSON)
			jobs = append(jobs, j)
		}
		if err := rows.Err(); err != nil {
			slog.Error("failed to iterate job rows", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to query recent jobs")
			return
		}

		writeJSON(w, http.StatusOK, jobs)
	}
}

func handleDiscardJob(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "invalid job id: must be a positive integer")
			return
		}

		var j recentJob
		var createdAt time.Time
		var finalizedAt *time.Time
		err = pool.QueryRow(r.Context(),
			`UPDATE river_job SET state = 'discarded', finalized_at = now()
			WHERE id = $1 AND state IN ('retryable', 'available', 'scheduled')
			RETURNING id, kind, state, created_at, finalized_at`, id).Scan(&j.ID, &j.Kind, &j.State, &createdAt, &finalizedAt)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "job not found or already in terminal state")
				return
			}
			slog.Error("failed to discard job", "job_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to discard job")
			return
		}

		j.CreatedAt = createdAt.Format(time.RFC3339)
		if finalizedAt != nil {
			s := finalizedAt.Format(time.RFC3339)
			j.FinalizedAt = &s
		}
		slog.Info("discarded job", "job_id", j.ID, "kind", j.Kind)
		writeJSON(w, http.StatusOK, j)
	}
}

func parseJobErrors(data []byte) []string {
	if len(data) == 0 || string(data) == "null" || string(data) == "[]" {
		return []string{}
	}

	// River stores errors as a JSONB array of objects with an "error" field
	// Parse minimally — extract just the error strings
	type riverError struct {
		Error string `json:"error"`
	}
	var errs []riverError
	if err := json.Unmarshal(data, &errs); err != nil {
		slog.Warn("failed to parse job errors", "error", err)
		return []string{}
	}

	result := make([]string, 0, len(errs))
	for _, e := range errs {
		if e.Error != "" {
			result = append(result, e.Error)
		}
	}
	return result
}
