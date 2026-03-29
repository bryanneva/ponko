package saga_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bryanneva/ponko/internal/saga"
	"github.com/bryanneva/ponko/internal/slack"
	"github.com/bryanneva/ponko/internal/testutil"
	"github.com/bryanneva/ponko/internal/workflow"
)

type postMessageCall struct {
	Channel  string
	Text     string
	ThreadTS string
}

type postBlocksCall struct {
	Channel  string
	Text     string
	ThreadTS string
	Blocks   []slack.Block
}

type fakeSlackClient struct {
	PostMessageErr   error
	PostBlocksErr    error
	PostBlocksTS     string
	PostMessageCalls []postMessageCall
	PostBlocksCalls  []postBlocksCall
}

func (f *fakeSlackClient) PostMessage(_ context.Context, channel, text, threadTS string) error {
	f.PostMessageCalls = append(f.PostMessageCalls, postMessageCall{Channel: channel, Text: text, ThreadTS: threadTS})
	return f.PostMessageErr
}

func (f *fakeSlackClient) PostBlocks(_ context.Context, channel, text string, blocks []slack.Block, threadTS string) (string, error) {
	f.PostBlocksCalls = append(f.PostBlocksCalls, postBlocksCall{Channel: channel, Text: text, Blocks: blocks, ThreadTS: threadTS})
	return f.PostBlocksTS, f.PostBlocksErr
}

func seedWorkflow(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	wfID, err := workflow.CreateWorkflow(context.Background(), pool, "test")
	if err != nil {
		t.Fatalf("seed workflow: %v", err)
	}
	return wfID
}

func cleanupDeliverTest(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, _ = pool.Exec(context.Background(), `DELETE FROM outbox`)
}

func TestOutboxDeliverWorker_TextDelivery(t *testing.T) {
	pool := testutil.TestDB(t)
	cleanupDeliverTest(t, pool)
	t.Cleanup(func() { cleanupDeliverTest(t, pool) })
	ctx := context.Background()

	wfID := seedWorkflow(t, pool)
	entry := saga.OutboxEntry{
		WorkflowID:  wfID,
		ChannelID:   "C_DELIVER_TEXT",
		ThreadTS:    "1234567890.123456",
		MessageType: "text",
		Content:     json.RawMessage(`"hello world"`),
	}
	outboxID, enqErr := saga.Enqueue(ctx, pool, entry)
	if enqErr != nil {
		t.Fatalf("enqueue: %v", enqErr)
	}

	fake := &fakeSlackClient{}
	worker := saga.OutboxDeliverWorker{
		Pool:  pool,
		Slack: fake,
	}

	deliverErr := worker.DeliverPending(ctx)
	if deliverErr != nil {
		t.Fatalf("deliver: %v", deliverErr)
	}

	if len(fake.PostMessageCalls) != 1 {
		t.Fatalf("expected 1 PostMessage call, got %d", len(fake.PostMessageCalls))
	}
	call := fake.PostMessageCalls[0]
	if call.Channel != "C_DELIVER_TEXT" {
		t.Errorf("expected channel C_DELIVER_TEXT, got %q", call.Channel)
	}
	if call.ThreadTS != "1234567890.123456" {
		t.Errorf("expected thread_ts, got %q", call.ThreadTS)
	}

	delivered, getErr := saga.GetOutboxEntry(ctx, pool, outboxID)
	if getErr != nil {
		t.Fatalf("get entry: %v", getErr)
	}
	if delivered.Status != "delivered" {
		t.Errorf("expected status delivered, got %q", delivered.Status)
	}
}

func TestOutboxDeliverWorker_BlocksDelivery(t *testing.T) {
	pool := testutil.TestDB(t)
	cleanupDeliverTest(t, pool)
	t.Cleanup(func() { cleanupDeliverTest(t, pool) })
	ctx := context.Background()

	wfID := seedWorkflow(t, pool)
	blocksJSON := json.RawMessage(`[{"type":"section","text":{"type":"mrkdwn","text":"plan"}}]`)
	entry := saga.OutboxEntry{
		WorkflowID:  wfID,
		ChannelID:   "C_DELIVER_BLOCKS",
		ThreadTS:    "1234567890.123456",
		MessageType: "blocks",
		Content:     blocksJSON,
	}
	outboxID, enqErr := saga.Enqueue(ctx, pool, entry)
	if enqErr != nil {
		t.Fatalf("enqueue: %v", enqErr)
	}

	fake := &fakeSlackClient{PostBlocksTS: "9999.8888"}
	worker := saga.OutboxDeliverWorker{
		Pool:  pool,
		Slack: fake,
	}

	deliverErr := worker.DeliverPending(ctx)
	if deliverErr != nil {
		t.Fatalf("deliver: %v", deliverErr)
	}

	if len(fake.PostBlocksCalls) != 1 {
		t.Fatalf("expected 1 PostBlocks call, got %d", len(fake.PostBlocksCalls))
	}

	delivered, getErr := saga.GetOutboxEntry(ctx, pool, outboxID)
	if getErr != nil {
		t.Fatalf("get entry: %v", getErr)
	}
	if delivered.Status != "delivered" {
		t.Errorf("expected status delivered, got %q", delivered.Status)
	}
	if delivered.SlackMessageTS == nil || *delivered.SlackMessageTS != "9999.8888" {
		t.Errorf("expected slack_message_ts 9999.8888, got %v", delivered.SlackMessageTS)
	}
}

func TestOutboxDeliverWorker_RetryOnFailure(t *testing.T) {
	pool := testutil.TestDB(t)
	cleanupDeliverTest(t, pool)
	t.Cleanup(func() { cleanupDeliverTest(t, pool) })
	ctx := context.Background()

	wfID := seedWorkflow(t, pool)
	entry := saga.OutboxEntry{
		WorkflowID:  wfID,
		ChannelID:   "C_DELIVER_RETRY",
		MessageType: "text",
		Content:     json.RawMessage(`"hello"`),
	}
	outboxID, enqErr := saga.Enqueue(ctx, pool, entry)
	if enqErr != nil {
		t.Fatalf("enqueue: %v", enqErr)
	}

	fake := &fakeSlackClient{
		PostMessageErr: fmt.Errorf("network timeout"),
	}
	worker := saga.OutboxDeliverWorker{
		Pool:  pool,
		Slack: fake,
	}

	deliverErr := worker.DeliverPending(ctx)
	if deliverErr != nil {
		t.Fatalf("deliver: %v", deliverErr)
	}

	retried, getErr := saga.GetOutboxEntry(ctx, pool, outboxID)
	if getErr != nil {
		t.Fatalf("get entry: %v", getErr)
	}
	if retried.Status != "pending" {
		t.Errorf("expected status pending (reset for retry), got %q", retried.Status)
	}
}

func TestOutboxDeliverWorker_MaxAttemptsExceeded(t *testing.T) {
	pool := testutil.TestDB(t)
	cleanupDeliverTest(t, pool)
	t.Cleanup(func() { cleanupDeliverTest(t, pool) })
	ctx := context.Background()

	wfID := seedWorkflow(t, pool)
	entry := saga.OutboxEntry{
		WorkflowID:  wfID,
		ChannelID:   "C_DELIVER_MAX",
		MessageType: "text",
		Content:     json.RawMessage(`"hello"`),
	}
	outboxID, enqErr := saga.Enqueue(ctx, pool, entry)
	if enqErr != nil {
		t.Fatalf("enqueue: %v", enqErr)
	}

	// Simulate previous failed attempts
	_, execErr := pool.Exec(ctx, "UPDATE outbox SET attempts = $1 WHERE outbox_id = $2", saga.MaxDeliveryAttempts-1, outboxID)
	if execErr != nil {
		t.Fatalf("update attempts: %v", execErr)
	}

	fake := &fakeSlackClient{
		PostMessageErr: fmt.Errorf("network timeout"),
	}
	worker := saga.OutboxDeliverWorker{
		Pool:  pool,
		Slack: fake,
	}

	deliverErr := worker.DeliverPending(ctx)
	if deliverErr != nil {
		t.Fatalf("deliver: %v", deliverErr)
	}

	failed, getErr := saga.GetOutboxEntry(ctx, pool, outboxID)
	if getErr != nil {
		t.Fatalf("get entry: %v", getErr)
	}
	if failed.Status != "failed" {
		t.Errorf("expected status failed, got %q", failed.Status)
	}
}

func TestOutboxDeliverWorker_PermanentSlackError(t *testing.T) {
	pool := testutil.TestDB(t)
	cleanupDeliverTest(t, pool)
	t.Cleanup(func() { cleanupDeliverTest(t, pool) })
	ctx := context.Background()

	wfID := seedWorkflow(t, pool)
	entry := saga.OutboxEntry{
		WorkflowID:  wfID,
		ChannelID:   "C_DELIVER_PERM",
		MessageType: "text",
		Content:     json.RawMessage(`"hello"`),
	}
	outboxID, enqErr := saga.Enqueue(ctx, pool, entry)
	if enqErr != nil {
		t.Fatalf("enqueue: %v", enqErr)
	}

	fake := &fakeSlackClient{
		PostMessageErr: fmt.Errorf("slack API error: channel_not_found"),
	}
	worker := saga.OutboxDeliverWorker{
		Pool:  pool,
		Slack: fake,
	}

	deliverErr := worker.DeliverPending(ctx)
	if deliverErr != nil {
		t.Fatalf("deliver: %v", deliverErr)
	}

	failed, getErr := saga.GetOutboxEntry(ctx, pool, outboxID)
	if getErr != nil {
		t.Fatalf("get entry: %v", getErr)
	}
	if failed.Status != "failed" {
		t.Errorf("expected status failed (permanent error), got %q", failed.Status)
	}
}

func TestOutboxDeliverWorker_EmptyOutbox(t *testing.T) {
	pool := testutil.TestDB(t)
	cleanupDeliverTest(t, pool)
	t.Cleanup(func() { cleanupDeliverTest(t, pool) })

	fake := &fakeSlackClient{}
	worker := saga.OutboxDeliverWorker{
		Pool:  pool,
		Slack: fake,
	}

	deliverErr := worker.DeliverPending(context.Background())
	if deliverErr != nil {
		t.Fatalf("deliver: %v", deliverErr)
	}

	if len(fake.PostMessageCalls) != 0 {
		t.Errorf("expected no PostMessage calls, got %d", len(fake.PostMessageCalls))
	}
}
