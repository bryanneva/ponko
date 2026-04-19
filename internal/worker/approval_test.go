package worker_test

import (
	"context"
	"errors"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertest"
	"github.com/riverqueue/river/rivertype"

	"github.com/bryanneva/ponko/internal/approval"
	"github.com/bryanneva/ponko/internal/worker"
)

type approvalChecker struct {
	err    error
	status approval.Status
}

func (a *approvalChecker) CheckApproval(context.Context, string) (approval.Status, error) {
	if a.err != nil {
		return approval.Pending, a.err
	}
	return a.status, nil
}

type approvalCommenter struct {
	comments []string
}

func (c *approvalCommenter) Comment(_ context.Context, _ string, body string) error {
	c.comments = append(c.comments, body)
	return nil
}

func approvalPollerJob(args worker.ApprovalPollerArgs) *river.Job[worker.ApprovalPollerArgs] {
	return &river.Job[worker.ApprovalPollerArgs]{
		JobRow: &rivertype.JobRow{ID: 456},
		Args:   args,
	}
}

func approvalPollerArgs() worker.ApprovalPollerArgs {
	return worker.ApprovalPollerArgs{
		IssueURL: "https://github.com/x/y/issues/1",
		NextPhaseArgs: worker.TaskPhaseArgs{
			IssueURL:    "https://github.com/x/y/issues/1",
			Repo:        "x/y",
			IssueNumber: 1,
			Title:       "Fix issue",
			Workflow:    "fix",
			Phase:       "wrap-up",
		},
	}
}

func TestApprovalPollerWorkerApprovedInsertsNextPhase(t *testing.T) {
	db, client := testWorkerDB(t)
	w := worker.NewApprovalPollerWorker(worker.ApprovalPollerWorkerDeps{
		ApprovalChecker: &approvalChecker{status: approval.Approved},
		Bus:             &taskWorkerBus{},
	})

	err := w.Work(rivertest.WorkContext(context.Background(), client), approvalPollerJob(approvalPollerArgs()))
	if err != nil {
		t.Fatalf("Work: %v", err)
	}
	if riverJobCount(t, db, "task_phase") != 1 {
		t.Fatalf("expected one task_phase job")
	}
}

func TestApprovalPollerWorkerRejectedCommentsAndCompletes(t *testing.T) {
	db, client := testWorkerDB(t)
	commenter := &approvalCommenter{}
	w := worker.NewApprovalPollerWorker(worker.ApprovalPollerWorkerDeps{
		ApprovalChecker: &approvalChecker{status: approval.Rejected},
		Commenter:       commenter,
		Bus:             &taskWorkerBus{},
	})

	err := w.Work(rivertest.WorkContext(context.Background(), client), approvalPollerJob(approvalPollerArgs()))
	if err != nil {
		t.Fatalf("Work: %v", err)
	}
	if riverJobCount(t, db, "task_phase") != 0 {
		t.Fatalf("expected no task_phase jobs")
	}
	if len(commenter.comments) != 1 {
		t.Fatalf("comments = %d, want 1", len(commenter.comments))
	}
}

func TestApprovalPollerWorkerPendingSnoozes(t *testing.T) {
	_, client := testWorkerDB(t)
	w := worker.NewApprovalPollerWorker(worker.ApprovalPollerWorkerDeps{
		ApprovalChecker: &approvalChecker{status: approval.Pending},
		Bus:             &taskWorkerBus{},
	})

	err := w.Work(rivertest.WorkContext(context.Background(), client), approvalPollerJob(approvalPollerArgs()))
	var snoozeErr *river.JobSnoozeError
	if !errors.As(err, &snoozeErr) {
		t.Fatalf("Work error = %v, want JobSnoozeError", err)
	}
}
