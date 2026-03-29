package jobs

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertest"

	"github.com/bryanneva/ponko/internal/testutil"
	"github.com/bryanneva/ponko/internal/workflow"
)

func TestReceiveArgs_Kind(t *testing.T) {
	args := ReceiveArgs{}
	if args.Kind() != "receive" {
		t.Errorf("Kind() = %q, want %q", args.Kind(), "receive")
	}
}

func TestReceiveArgs_OutputIncludesAllFields(t *testing.T) {
	args := ReceiveArgs{
		ThreadKey:   "C123:1234567890.123456",
		Channel:     "C123",
		ThreadTS:    "1234567890.123456",
		EventTS:     "1234567890.123457",
		ChannelType: "channel",
		Message:     "hello",
	}

	data, err := json.Marshal(map[string]string{
		"message":      args.Message,
		"thread_key":   args.ThreadKey,
		"channel":      args.Channel,
		"thread_ts":    args.ThreadTS,
		"event_ts":     args.EventTS,
		"channel_type": args.ChannelType,
	})
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	var output map[string]string
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}

	if output["message"] != "hello" {
		t.Errorf("message = %q, want %q", output["message"], "hello")
	}

	if output["thread_key"] != "C123:1234567890.123456" {
		t.Errorf("thread_key = %q, want %q", output["thread_key"], "C123:1234567890.123456")
	}

	if output["channel"] != "C123" {
		t.Errorf("channel = %q, want %q", output["channel"], "C123")
	}

	if output["thread_ts"] != "1234567890.123456" {
		t.Errorf("thread_ts = %q, want %q", output["thread_ts"], "1234567890.123456")
	}

	if output["event_ts"] != "1234567890.123457" {
		t.Errorf("event_ts = %q, want %q", output["event_ts"], "1234567890.123457")
	}

	if output["channel_type"] != "channel" {
		t.Errorf("channel_type = %q, want %q", output["channel_type"], "channel")
	}
}

func TestReceiveArgs_OutputWithEmptyThreadKey(t *testing.T) {
	args := ReceiveArgs{
		Message: "hello",
	}

	data, err := json.Marshal(map[string]string{"message": args.Message, "thread_key": args.ThreadKey})
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	var output map[string]string
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}

	if output["thread_key"] != "" {
		t.Errorf("thread_key = %q, want empty string", output["thread_key"])
	}
}

func newTestRiverClient(t *testing.T, pool *pgxpool.Pool) *river.Client[pgx.Tx] {
	t.Helper()

	workers := river.NewWorkers()
	river.AddWorker(workers, &ReceiveWorker{Pool: pool})
	river.AddWorker(workers, &PlanWorker{Pool: pool})
	river.AddWorker(workers, &ProcessWorker{Pool: pool})
	river.AddWorker(workers, &ExecuteWorker{Pool: pool})
	river.AddWorker(workers, &SynthesizeWorker{Pool: pool})
	river.AddWorker(workers, &RespondWorker{Pool: pool})
	river.AddWorker(workers, &SlackReplyWorker{})

	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 1},
		},
		Workers: workers,
	})
	if err != nil {
		t.Fatalf("creating river client: %v", err)
	}
	return client
}

func TestReceiveWorker_Work(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()
	client := newTestRiverClient(t, pool)

	t.Run("creates receive step and enqueues process job", func(t *testing.T) {
		wfID, err := workflow.CreateWorkflow(ctx, pool, "test")
		if err != nil {
			t.Fatalf("creating workflow: %v", err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM workflow_outputs WHERE workflow_id = $1", wfID)
			_, _ = pool.Exec(ctx, "DELETE FROM workflow_steps WHERE workflow_id = $1", wfID)
			_, _ = pool.Exec(ctx, "DELETE FROM workflows WHERE workflow_id = $1", wfID)
		})

		worker := &ReceiveWorker{Pool: pool}
		job := &river.Job[ReceiveArgs]{
			Args: ReceiveArgs{
				WorkflowID:  wfID,
				ThreadKey:   "C123:ts123",
				Channel:     "C123",
				ThreadTS:    "ts123",
				EventTS:     "ev123",
				ChannelType: "channel",
				Message:     "hello world",
				UserID:      "U999",
			},
		}

		workCtx := rivertest.WorkContext(ctx, client)
		if workErr := worker.Work(workCtx, job); workErr != nil {
			t.Fatalf("Work() returned error: %v", workErr)
		}

		// Verify receive step was created and completed
		wf, err := workflow.GetWorkflow(ctx, pool, wfID)
		if err != nil {
			t.Fatalf("getting workflow: %v", err)
		}
		if len(wf.Steps) != 1 {
			t.Fatalf("expected 1 step, got %d", len(wf.Steps))
		}
		if wf.Steps[0].StepName != "receive" {
			t.Errorf("step name = %q, want %q", wf.Steps[0].StepName, "receive")
		}
		if wf.Steps[0].Status != "complete" {
			t.Errorf("step status = %q, want %q", wf.Steps[0].Status, "complete")
		}

		// Verify output was saved
		if len(wf.Outputs) != 1 {
			t.Fatalf("expected 1 output, got %d", len(wf.Outputs))
		}
		if wf.Outputs[0].StepName != "receive" {
			t.Errorf("output step name = %q, want %q", wf.Outputs[0].StepName, "receive")
		}

		var outputData map[string]string
		if err := json.Unmarshal(wf.Outputs[0].Data, &outputData); err != nil {
			t.Fatalf("unmarshaling output: %v", err)
		}
		if outputData["message"] != "hello world" {
			t.Errorf("output message = %q, want %q", outputData["message"], "hello world")
		}
		if outputData["user_id"] != "U999" {
			t.Errorf("output user_id = %q, want %q", outputData["user_id"], "U999")
		}
		if outputData["channel"] != "C123" {
			t.Errorf("output channel = %q, want %q", outputData["channel"], "C123")
		}
	})

	t.Run("returns error when workflow does not exist", func(t *testing.T) {
		worker := &ReceiveWorker{Pool: pool}
		job := &river.Job[ReceiveArgs]{
			Args: ReceiveArgs{
				WorkflowID: "nonexistent-workflow-id",
				Message:    "hello",
			},
		}

		workCtx := rivertest.WorkContext(ctx, client)
		err := worker.Work(workCtx, job)
		if err == nil {
			t.Fatal("expected error for nonexistent workflow")
		}
	})
}
