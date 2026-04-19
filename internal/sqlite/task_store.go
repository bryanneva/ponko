package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bryanneva/ponko/internal/task"
)

// TaskStore implements task.Store using SQLite.
type TaskStore struct {
	db *sql.DB
}

// NewTaskStore returns a TaskStore backed by the given database.
func NewTaskStore(db *sql.DB) *TaskStore {
	return &TaskStore{db: db}
}

// Create inserts a new task row. Auto-generates ID if empty, sets timestamps.
func (s *TaskStore) Create(ctx context.Context, t *task.Task) error {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now

	labels, err := json.Marshal(t.Labels)
	if err != nil {
		return fmt.Errorf("marshal labels: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO tasks
			(id, issue_url, repo, issue_number, title, labels, body, workflow, status,
			 phase, block_reason, attempts, last_error, cost_usd, locked_by, locked_at,
			 created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.IssueURL, t.Repo, t.IssueNumber, t.Title, string(labels), t.Body,
		t.Workflow, string(t.Status), t.Phase, t.BlockReason, t.Attempts, t.LastError,
		t.CostUSD, t.LockedBy, t.LockedAt, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}
	return nil
}

// Get returns the task with the given ID, or nil if not found.
func (s *TaskStore) Get(ctx context.Context, id string) (*task.Task, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id = ?`, id)
	return scanTask(row)
}

// GetByIssueURL returns the task for the given GitHub issue URL, or nil if not found.
func (s *TaskStore) GetByIssueURL(ctx context.Context, issueURL string) (*task.Task, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE issue_url = ?`, issueURL)
	return scanTask(row)
}

// ListByStatus returns tasks matching any of the given statuses, ordered by created_at ASC.
func (s *TaskStore) ListByStatus(ctx context.Context, statuses ...task.Status) ([]*task.Task, error) {
	if len(statuses) == 0 {
		return nil, nil
	}

	placeholders := strings.Repeat("?,", len(statuses))
	placeholders = placeholders[:len(placeholders)-1] // trim trailing comma

	args := make([]any, len(statuses))
	for i, s := range statuses {
		args[i] = string(s)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskColumns+` FROM tasks WHERE status IN (`+placeholders+`) ORDER BY created_at ASC`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tasks []*task.Task
	for rows.Next() {
		t, err := scanTaskRow(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// UpdateStatus updates a task's status and optionally sets last_error.
func (s *TaskStore) UpdateStatus(ctx context.Context, id string, status task.Status, lastError string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, last_error = ?, updated_at = ? WHERE id = ?`,
		string(status), lastError, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	return nil
}

// UpdatePhase sets the current phase for the task.
func (s *TaskStore) UpdatePhase(ctx context.Context, id string, phase string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET phase = ?, updated_at = ? WHERE id = ?`,
		phase, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("update phase: %w", err)
	}
	return nil
}

// IncrAttempts atomically increments the task's attempts counter by one.
func (s *TaskStore) IncrAttempts(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET attempts = attempts + 1, updated_at = ? WHERE id = ?`,
		time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("incr attempts: %w", err)
	}
	return nil
}

// AddCost atomically increments the task's cost_usd by delta.
func (s *TaskStore) AddCost(ctx context.Context, id string, delta float64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET cost_usd = cost_usd + ?, updated_at = ? WHERE id = ?`,
		delta, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("add cost: %w", err)
	}
	return nil
}

// CountActive returns the number of tasks in queued, in_progress, or blocked state.
func (s *TaskStore) CountActive(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE status IN ('queued','in_progress','blocked')`,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active: %w", err)
	}
	return count, nil
}

// taskColumns is the column list for SELECT queries.
const taskColumns = `id, issue_url, repo, issue_number, title, labels, body, workflow, status,
	phase, block_reason, attempts, last_error, cost_usd, locked_by, locked_at,
	created_at, updated_at`

type scanner interface {
	Scan(dest ...any) error
}

func scanTask(s scanner) (*task.Task, error) {
	t, err := scanTaskRow(s)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

func scanTaskRow(s scanner) (*task.Task, error) {
	var t task.Task
	var status string
	var labelsJSON string
	var lockedAt sql.NullTime

	err := s.Scan(
		&t.ID, &t.IssueURL, &t.Repo, &t.IssueNumber, &t.Title, &labelsJSON, &t.Body,
		&t.Workflow, &status, &t.Phase, &t.BlockReason, &t.Attempts, &t.LastError,
		&t.CostUSD, &t.LockedBy, &lockedAt, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan task: %w", err)
	}

	t.Status = task.Status(status)
	if lockedAt.Valid {
		t.LockedAt = &lockedAt.Time
	}
	if err := json.Unmarshal([]byte(labelsJSON), &t.Labels); err != nil {
		t.Labels = nil
	}
	return &t, nil
}
