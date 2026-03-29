package workflow

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Workflow struct {
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	WorkflowID     string    `json:"workflow_id"`
	WorkflowType   string    `json:"workflow_type"`
	Status         string    `json:"status"`
	TotalTasks     int       `json:"total_tasks"`
	CompletedTasks int       `json:"completed_tasks"`
}

type Step struct {
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
	StepID      string     `json:"step_id"`
	WorkflowID  string     `json:"workflow_id"`
	StepName    string     `json:"step_name"`
	Status      string     `json:"status"`
}

type Output struct {
	CreatedAt  time.Time       `json:"created_at"`
	WorkflowID string          `json:"workflow_id"`
	StepName   string          `json:"step_name"`
	Data       json.RawMessage `json:"data"`
}

type WorkflowWithDetails struct {
	Steps   []Step   `json:"steps"`
	Outputs []Output `json:"outputs"`
	Workflow
}

func CreateWorkflow(ctx context.Context, pool *pgxpool.Pool, workflowType string) (string, error) {
	var workflowID string
	err := pool.QueryRow(ctx,
		`INSERT INTO workflows (workflow_type) VALUES ($1) RETURNING workflow_id`,
		workflowType,
	).Scan(&workflowID)
	return workflowID, err
}

func GetWorkflow(ctx context.Context, pool *pgxpool.Pool, workflowID string) (*WorkflowWithDetails, error) {
	w := &WorkflowWithDetails{}
	err := pool.QueryRow(ctx,
		`SELECT workflow_id, workflow_type, status, created_at, updated_at, total_tasks, completed_tasks FROM workflows WHERE workflow_id = $1`,
		workflowID,
	).Scan(&w.WorkflowID, &w.WorkflowType, &w.Status, &w.CreatedAt, &w.UpdatedAt, &w.TotalTasks, &w.CompletedTasks)
	if err != nil {
		return nil, err
	}

	stepRows, err := pool.Query(ctx,
		`SELECT started_at, completed_at, step_id, workflow_id, step_name, status FROM workflow_steps WHERE workflow_id = $1 ORDER BY started_at, step_name`,
		workflowID,
	)
	if err != nil {
		return nil, err
	}
	w.Steps, err = pgx.CollectRows(stepRows, pgx.RowToStructByPos[Step])
	if err != nil {
		return nil, err
	}

	outputRows, err := pool.Query(ctx,
		`SELECT created_at, workflow_id, step_name, data FROM workflow_outputs WHERE workflow_id = $1 ORDER BY created_at`,
		workflowID,
	)
	if err != nil {
		return nil, err
	}
	w.Outputs, err = pgx.CollectRows(outputRows, pgx.RowToStructByPos[Output])
	if err != nil {
		return nil, err
	}

	return w, nil
}

func ListRecent(ctx context.Context, pool *pgxpool.Pool, limit int) ([]Workflow, error) {
	rows, err := pool.Query(ctx,
		`SELECT created_at, updated_at, workflow_id, workflow_type, status, total_tasks, completed_tasks
		FROM workflows ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Workflow])
}

func CreateStep(ctx context.Context, pool *pgxpool.Pool, workflowID string, stepName string) (string, error) {
	var stepID string
	err := pool.QueryRow(ctx,
		`INSERT INTO workflow_steps (workflow_id, step_name, started_at) VALUES ($1, $2, now()) RETURNING step_id`,
		workflowID, stepName,
	).Scan(&stepID)
	return stepID, err
}

// UpdateStepStatus sets completed_at when status is 'complete'.
func UpdateStepStatus(ctx context.Context, pool *pgxpool.Pool, stepID string, status string) error {
	var query string
	if status == "complete" {
		query = `UPDATE workflow_steps SET status = $1, completed_at = now() WHERE step_id = $2`
	} else {
		query = `UPDATE workflow_steps SET status = $1 WHERE step_id = $2`
	}
	_, err := pool.Exec(ctx, query, status, stepID)
	return err
}

// UpdateStepStatusByName updates a step's status using workflow_id + step_name.
func UpdateStepStatusByName(ctx context.Context, pool *pgxpool.Pool, workflowID string, stepName string, status string) error {
	var query string
	if status == "complete" {
		query = `UPDATE workflow_steps SET status = $1, completed_at = now() WHERE workflow_id = $2 AND step_name = $3`
	} else {
		query = `UPDATE workflow_steps SET status = $1 WHERE workflow_id = $2 AND step_name = $3`
	}
	_, err := pool.Exec(ctx, query, status, workflowID, stepName)
	return err
}

func SaveOutput(ctx context.Context, pool *pgxpool.Pool, workflowID string, stepName string, data json.RawMessage) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO workflow_outputs (workflow_id, step_name, data) VALUES ($1, $2, $3)`,
		workflowID, stepName, data,
	)
	return err
}

func GetOutput(ctx context.Context, pool *pgxpool.Pool, workflowID string, stepName string) (json.RawMessage, error) {
	var data json.RawMessage
	err := pool.QueryRow(ctx,
		`SELECT data FROM workflow_outputs WHERE workflow_id = $1 AND step_name = $2`,
		workflowID, stepName,
	).Scan(&data)
	return data, err
}

func GetConversationID(ctx context.Context, pool *pgxpool.Pool, workflowID string) (string, error) {
	var convID *string
	err := pool.QueryRow(ctx,
		`SELECT conversation_id FROM workflows WHERE workflow_id = $1`,
		workflowID,
	).Scan(&convID)
	if err != nil {
		return "", err
	}
	if convID == nil {
		return "", nil
	}
	return *convID, nil
}

func SetConversationID(ctx context.Context, pool *pgxpool.Pool, workflowID, conversationID string) error {
	_, err := pool.Exec(ctx,
		`UPDATE workflows SET conversation_id = $1, updated_at = now() WHERE workflow_id = $2`,
		conversationID, workflowID,
	)
	return err
}

func UpdateWorkflowStatus(ctx context.Context, pool *pgxpool.Pool, workflowID string, status string) error {
	_, err := pool.Exec(ctx,
		`UPDATE workflows SET status = $1, updated_at = now() WHERE workflow_id = $2`,
		status, workflowID,
	)
	return err
}

func GetOutputsByPrefix(ctx context.Context, pool *pgxpool.Pool, workflowID string, prefix string) ([]Output, error) {
	rows, err := pool.Query(ctx,
		`SELECT created_at, workflow_id, step_name, data FROM workflow_outputs WHERE workflow_id = $1 AND step_name LIKE $2 ORDER BY step_name`,
		workflowID, prefix+"%",
	)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Output])
}

func SetTotalTasks(ctx context.Context, pool *pgxpool.Pool, workflowID string, total int) error {
	_, err := pool.Exec(ctx,
		`UPDATE workflows SET total_tasks = $1, updated_at = now() WHERE workflow_id = $2`,
		total, workflowID,
	)
	return err
}

func IncrementCompletedTasks(ctx context.Context, pool *pgxpool.Pool, workflowID string) (completedTasks int, totalTasks int, err error) {
	err = pool.QueryRow(ctx,
		`UPDATE workflows SET completed_tasks = completed_tasks + 1, updated_at = now() WHERE workflow_id = $1 RETURNING completed_tasks, total_tasks`,
		workflowID,
	).Scan(&completedTasks, &totalTasks)
	return
}
