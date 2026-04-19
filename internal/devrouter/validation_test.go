package devrouter_test

import (
	"context"
	"testing"

	"github.com/bryanneva/ponko/internal/approval"
	"github.com/bryanneva/ponko/internal/devrouter"
	"github.com/bryanneva/ponko/internal/devrouter/testutil"
)

func makeValidatingPipeline(store devrouter.PipelineStore) (*devrouter.Pipeline, error) {
	p := &devrouter.Pipeline{
		ID:               "validation-test",
		Stage:            devrouter.StageValidating,
		Track:            devrouter.TrackFix,
		StoryCount:       1,
		StoriesCompleted: 1,
		IssueURL:         "https://github.com/owner/repo/issues/10",
	}
	return p, store.Create(context.Background(), p)
}

func TestValidationJob_GreenCITransitionsToAwaitingApproval(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	bus := &testutil.DiscardBus{}
	commenter := &testutil.NoOpCommenter{}
	ciChecker := &testutil.MockCIChecker{Status: "green"}
	approvalChecker := &testutil.MockApprovalChecker{Status: approval.Pending}

	p, err := makeValidatingPipeline(store)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	job := devrouter.NewValidationJob()
	if err := job.Run(ctx, p, store, bus, commenter, ciChecker, approvalChecker); err != nil {
		t.Fatalf("ValidationJob.Run: %v", err)
	}

	got, _ := store.Get(ctx, p.ID)
	if got.Stage != devrouter.StageAwaitingApproval {
		t.Errorf("expected stage awaiting_approval, got %s", got.Stage)
	}
	if !commenter.WasCalled() {
		t.Error("expected comment to be posted on issue")
	}
}

func TestValidationJob_PendingCIStaysInValidating(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	bus := &testutil.DiscardBus{}
	commenter := &testutil.NoOpCommenter{}
	ciChecker := &testutil.MockCIChecker{Status: "pending"}
	approvalChecker := &testutil.MockApprovalChecker{Status: approval.Pending}

	p, err := makeValidatingPipeline(store)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	job := devrouter.NewValidationJob()
	if err := job.Run(ctx, p, store, bus, commenter, ciChecker, approvalChecker); err != nil {
		t.Fatalf("ValidationJob.Run: %v", err)
	}

	got, _ := store.Get(ctx, p.ID)
	if got.Stage != devrouter.StageValidating {
		t.Errorf("expected stage validating, got %s", got.Stage)
	}
}

func TestValidationJob_FailedCITransitionsToFailed(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	bus := &testutil.DiscardBus{}
	commenter := &testutil.NoOpCommenter{}
	ciChecker := &testutil.MockCIChecker{Status: "failed"}
	approvalChecker := &testutil.MockApprovalChecker{Status: approval.Pending}

	p, err := makeValidatingPipeline(store)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	job := devrouter.NewValidationJob()
	if err := job.Run(ctx, p, store, bus, commenter, ciChecker, approvalChecker); err != nil {
		t.Fatalf("ValidationJob.Run: %v", err)
	}

	got, _ := store.Get(ctx, p.ID)
	if got.Stage != devrouter.StageFailed {
		t.Errorf("expected stage failed on CI failure, got %s", got.Stage)
	}
}

func TestValidationJob_ApprovedTransitionsToCompleted(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	bus := &testutil.DiscardBus{}
	commenter := &testutil.NoOpCommenter{}
	ciChecker := &testutil.MockCIChecker{Status: "green"}
	approvalChecker := &testutil.MockApprovalChecker{Status: approval.Approved}

	// Start in awaiting_approval (CI already green, approval already given)
	p := &devrouter.Pipeline{
		ID:       "approval-test",
		Stage:    devrouter.StageAwaitingApproval,
		IssueURL: "https://github.com/owner/repo/issues/11",
	}
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	job := devrouter.NewValidationJob()
	if err := job.Run(ctx, p, store, bus, commenter, ciChecker, approvalChecker); err != nil {
		t.Fatalf("ValidationJob.Run: %v", err)
	}

	got, _ := store.Get(ctx, p.ID)
	if got.Stage != devrouter.StageCompleted {
		t.Errorf("expected stage completed on approval, got %s", got.Stage)
	}
}

func TestValidationJob_RejectedTransitionsToFailed(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewMemoryPipelineStore()
	bus := &testutil.DiscardBus{}
	commenter := &testutil.NoOpCommenter{}
	ciChecker := &testutil.MockCIChecker{Status: "green"}
	approvalChecker := &testutil.MockApprovalChecker{Status: approval.Rejected}

	p := &devrouter.Pipeline{
		ID:       "rejection-test",
		Stage:    devrouter.StageAwaitingApproval,
		IssueURL: "https://github.com/owner/repo/issues/12",
	}
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	job := devrouter.NewValidationJob()
	if err := job.Run(ctx, p, store, bus, commenter, ciChecker, approvalChecker); err != nil {
		t.Fatalf("ValidationJob.Run: %v", err)
	}

	got, _ := store.Get(ctx, p.ID)
	if got.Stage != devrouter.StageFailed {
		t.Errorf("expected stage failed on rejection, got %s", got.Stage)
	}
}
