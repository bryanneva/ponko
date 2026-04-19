package devrouter

import "context"

// PipelineStore persists pipeline state across process restarts.
type PipelineStore interface {
	Create(ctx context.Context, p *Pipeline) error
	Get(ctx context.Context, id string) (*Pipeline, error)
	UpdateStage(ctx context.Context, id string, from, to Stage) error
	SetTrack(ctx context.Context, id string, track Track) error
	SetClassificationRationale(ctx context.Context, id string, rationale string) error
	SetPlanOutput(ctx context.Context, id string, planOutput string) error
	SetStoryCount(ctx context.Context, id string, count int) error
	IncrStoriesCompleted(ctx context.Context, id string) (int, error)
	SetCurrentStoryIndex(ctx context.Context, id string, index int) error
	SetPRNumber(ctx context.Context, id string, prNumber int) error
	AddCost(ctx context.Context, id string, delta float64) error
	List(ctx context.Context, limit int) ([]*Pipeline, error)
	GetByIssueURL(ctx context.Context, issueURL string) (*Pipeline, error)
	SetIssueContent(ctx context.Context, id, title, body string) error
}
