package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/bryanneva/ponko/internal/devrouter"
)

// PipelineStore implements devrouter.PipelineStore using SQLite.
type PipelineStore struct {
	db *sql.DB
}

// NewPipelineStore returns a PipelineStore backed by the given database.
func NewPipelineStore(db *sql.DB) *PipelineStore {
	return &PipelineStore{db: db}
}

// Create inserts a new pipeline row with auto-generated ID and timestamps.
func (s *PipelineStore) Create(ctx context.Context, p *devrouter.Pipeline) error {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pipelines
			(id, task_id, issue_url, repo, issue_number, issue_title, issue_body,
			 track, stage, plan_output,
			 story_count, stories_completed, current_story_index, pr_number,
			 classification_rationale, cost_usd, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.TaskID, p.IssueURL, p.Repo, p.IssueNumber, p.IssueTitle, p.IssueBody,
		string(p.Track), string(p.Stage), p.PlanOutput,
		p.StoryCount, p.StoriesCompleted, p.CurrentStoryIndex, p.PRNumber,
		p.ClassificationRationale, p.CostUSD, p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert pipeline: %w", err)
	}
	return nil
}

// Get returns the pipeline with the given ID, or nil if not found.
func (s *PipelineStore) Get(ctx context.Context, id string) (*devrouter.Pipeline, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+pipelineColumns+` FROM pipelines WHERE id = ?`, id)
	p, err := scanPipeline(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// UpdateStage validates and applies a stage transition.
func (s *PipelineStore) UpdateStage(ctx context.Context, id string, from, to devrouter.Stage) error {
	if err := devrouter.ValidateTransition(from, to); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE pipelines SET stage = ?, updated_at = ? WHERE id = ?`,
		string(to), time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("update stage: %w", err)
	}
	return nil
}

// SetTrack sets the pipeline's execution track.
func (s *PipelineStore) SetTrack(ctx context.Context, id string, track devrouter.Track) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE pipelines SET track = ?, updated_at = ? WHERE id = ?`,
		string(track), time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("set track: %w", err)
	}
	return nil
}

// SetClassificationRationale stores the classification rationale.
func (s *PipelineStore) SetClassificationRationale(ctx context.Context, id string, rationale string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE pipelines SET classification_rationale = ?, updated_at = ? WHERE id = ?`,
		rationale, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("set classification rationale: %w", err)
	}
	return nil
}

// SetPlanOutput stores the JSON plan output.
func (s *PipelineStore) SetPlanOutput(ctx context.Context, id string, planOutput string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE pipelines SET plan_output = ?, updated_at = ? WHERE id = ?`,
		planOutput, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("set plan output: %w", err)
	}
	return nil
}

// SetStoryCount sets the total story count.
func (s *PipelineStore) SetStoryCount(ctx context.Context, id string, count int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE pipelines SET story_count = ?, updated_at = ? WHERE id = ?`,
		count, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("set story count: %w", err)
	}
	return nil
}

// IncrStoriesCompleted atomically increments stories_completed and returns the new value.
func (s *PipelineStore) IncrStoriesCompleted(ctx context.Context, id string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`UPDATE pipelines SET stories_completed = stories_completed + 1, updated_at = ? WHERE id = ? RETURNING stories_completed`,
		time.Now().UTC(), id,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("incr stories completed: %w", err)
	}
	return count, nil
}

// SetCurrentStoryIndex updates the current story index.
func (s *PipelineStore) SetCurrentStoryIndex(ctx context.Context, id string, index int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE pipelines SET current_story_index = ?, updated_at = ? WHERE id = ?`,
		index, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("set current story index: %w", err)
	}
	return nil
}

// SetPRNumber stores the PR number associated with the pipeline.
func (s *PipelineStore) SetPRNumber(ctx context.Context, id string, prNumber int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE pipelines SET pr_number = ?, updated_at = ? WHERE id = ?`,
		prNumber, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("set pr number: %w", err)
	}
	return nil
}

// AddCost atomically adds delta to cost_usd.
func (s *PipelineStore) AddCost(ctx context.Context, id string, delta float64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE pipelines SET cost_usd = cost_usd + ?, updated_at = ? WHERE id = ?`,
		delta, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("add cost: %w", err)
	}
	return nil
}

// GetByIssueURL returns the pipeline with the given issue URL, or nil if not found.
func (s *PipelineStore) GetByIssueURL(ctx context.Context, issueURL string) (*devrouter.Pipeline, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+pipelineColumns+` FROM pipelines WHERE issue_url = ? LIMIT 1`, issueURL)
	p, err := scanPipeline(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// SetIssueContent updates the issue_title and issue_body for a pipeline.
func (s *PipelineStore) SetIssueContent(ctx context.Context, id, title, body string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE pipelines SET issue_title = ?, issue_body = ?, updated_at = ? WHERE id = ?`,
		title, body, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("set issue content: %w", err)
	}
	return nil
}

// List returns pipelines ordered by created_at DESC. Pass limit <= 0 for no limit.
func (s *PipelineStore) List(ctx context.Context, limit int) ([]*devrouter.Pipeline, error) {
	query := `SELECT ` + pipelineColumns + ` FROM pipelines ORDER BY created_at DESC`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list pipelines: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var pipelines []*devrouter.Pipeline
	for rows.Next() {
		p, err := scanPipeline(rows)
		if err != nil {
			return nil, err
		}
		pipelines = append(pipelines, p)
	}
	if pipelines == nil {
		pipelines = []*devrouter.Pipeline{}
	}
	return pipelines, rows.Err()
}

const pipelineColumns = `id, task_id, issue_url, repo, issue_number, issue_title, issue_body, track, stage,
	plan_output, story_count, stories_completed, current_story_index, pr_number,
	classification_rationale, cost_usd, created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanPipeline(s rowScanner) (*devrouter.Pipeline, error) {
	var p devrouter.Pipeline
	var track, stage string
	err := s.Scan(
		&p.ID, &p.TaskID, &p.IssueURL, &p.Repo, &p.IssueNumber,
		&p.IssueTitle, &p.IssueBody,
		&track, &stage,
		&p.PlanOutput, &p.StoryCount, &p.StoriesCompleted,
		&p.CurrentStoryIndex, &p.PRNumber,
		&p.ClassificationRationale, &p.CostUSD,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan pipeline: %w", err)
	}
	p.Track = devrouter.Track(track)
	p.Stage = devrouter.Stage(stage)
	return &p, nil
}
