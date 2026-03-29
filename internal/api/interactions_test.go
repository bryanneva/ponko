package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	riverpgxv5 "github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/bryanneva/ponko/internal/channel"
	"github.com/bryanneva/ponko/internal/jobs"
	"github.com/bryanneva/ponko/internal/saga"
	"github.com/bryanneva/ponko/internal/slack"
	"github.com/bryanneva/ponko/internal/testutil"
	"github.com/bryanneva/ponko/internal/workflow"
)

func newInteractionRiverClient(t *testing.T, pool *pgxpool.Pool) *river.Client[pgx.Tx] {
	t.Helper()
	workers := river.NewWorkers()
	river.AddWorker(workers, &jobs.ExecuteWorker{Pool: pool})
	river.AddWorker(workers, &jobs.SynthesizeWorker{Pool: pool})
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

func buildInteractionPayload(actionID, workflowID, channelID, messageTS string) string {
	payload := map[string]any{
		"type": "block_actions",
		"user": map[string]string{"id": "U_TEST_USER"},
		"actions": []map[string]string{
			{"action_id": actionID, "value": workflowID},
		},
		"channel": map[string]string{"id": channelID},
		"message": map[string]string{"ts": messageTS},
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

func postInteraction(t *testing.T, handler http.HandlerFunc, payloadJSON string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"payload": {payloadJSON}}
	body := form.Encode()
	ts, sig := signRequest(t, body, time.Now().Unix())
	req := httptest.NewRequest(http.MethodPost, "/slack/interactions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", sig)
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

func setupApprovalWorkflow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, channelID, threadTS string) (string, string) {
	t.Helper()
	wfID, err := workflow.CreateWorkflow(ctx, pool, "test")
	if err != nil {
		t.Fatalf("creating workflow: %v", err)
	}

	convID, _, convErr := saga.FindOrCreateConversation(ctx, pool, channelID, threadTS)
	if convErr != nil {
		t.Fatalf("creating conversation: %v", convErr)
	}
	if setErr := workflow.SetConversationID(ctx, pool, wfID, convID); setErr != nil {
		t.Fatalf("setting conversation_id: %v", setErr)
	}
	if turnErr := saga.AddTurn(ctx, pool, convID, wfID, "message"); turnErr != nil {
		t.Fatalf("adding turn: %v", turnErr)
	}

	receiveOutput, _ := json.Marshal(map[string]string{
		"message":      "Research A and B",
		"thread_key":   fmt.Sprintf("%s:%s", channelID, threadTS),
		"channel":      channelID,
		"thread_ts":    threadTS,
		"event_ts":     "ev-test",
		"channel_type": "channel",
	})
	if saveErr := workflow.SaveOutput(ctx, pool, wfID, "receive", receiveOutput); saveErr != nil {
		t.Fatalf("saving receive output: %v", saveErr)
	}

	planOutput, _ := json.Marshal(map[string]any{
		"tasks": []map[string]string{
			{"instruction": "Research company A"},
			{"instruction": "Research company B"},
		},
		"total_tasks": 2,
	})
	if saveErr := workflow.SaveOutput(ctx, pool, wfID, "plan", planOutput); saveErr != nil {
		t.Fatalf("saving plan output: %v", saveErr)
	}
	if setErr := workflow.SetTotalTasks(ctx, pool, wfID, 2); setErr != nil {
		t.Fatalf("setting total tasks: %v", setErr)
	}

	if transErr := saga.TransitionStatus(ctx, pool, convID, "active", "awaiting_approval"); transErr != nil {
		t.Fatalf("transitioning to awaiting_approval: %v", transErr)
	}

	return wfID, convID
}

func cleanupInteraction(t *testing.T, ctx context.Context, pool *pgxpool.Pool, wfID, convID, channelID string) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM river_job WHERE args::text LIKE $1", "%"+wfID+"%")
		_, _ = pool.Exec(ctx, "DELETE FROM outbox WHERE conversation_id = $1", convID)
		_, _ = pool.Exec(ctx, "DELETE FROM conversation_turns WHERE conversation_id = $1", convID)
		_, _ = pool.Exec(ctx, "DELETE FROM conversations WHERE conversation_id = $1", convID)
		_, _ = pool.Exec(ctx, "DELETE FROM workflow_outputs WHERE workflow_id = $1", wfID)
		_, _ = pool.Exec(ctx, "DELETE FROM workflow_steps WHERE workflow_id = $1", wfID)
		_, _ = pool.Exec(ctx, "DELETE FROM workflows WHERE workflow_id = $1", wfID)
		_, _ = pool.Exec(ctx, "DELETE FROM channel_configs WHERE channel_id = $1", channelID)
	})
}

func TestHandleSlackInteractions_Approve(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()
	riverClient := newInteractionRiverClient(t, pool)

	channelID := "C-INT-APPROVE"
	wfID, convID := setupApprovalWorkflow(t, ctx, pool, channelID, "ts-int-approve")
	cleanupInteraction(t, ctx, pool, wfID, convID, channelID)

	var updateCalled bool
	slackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "chat.update") {
			updateCalled = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true,"ts":"msg-ts"}`)
	}))
	t.Cleanup(slackSrv.Close)
	slackClient := slack.NewClient("test-token", slackSrv.URL)

	handler := handleSlackInteractions(testSigningSecret, pool, riverClient, slackClient)
	payloadJSON := buildInteractionPayload("approve_plan", wfID, channelID, "approval-msg-ts")
	w := postInteraction(t, handler, payloadJSON)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	conv, convErr := saga.GetConversation(ctx, pool, convID)
	if convErr != nil {
		t.Fatalf("getting conversation: %v", convErr)
	}
	if conv.Status != "active" {
		t.Errorf("conversation status = %q, want %q", conv.Status, "active")
	}

	foundApprovalTurn := false
	for _, turn := range conv.Turns {
		if turn.TriggerType == "approval" {
			foundApprovalTurn = true
		}
	}
	if !foundApprovalTurn {
		t.Error("expected an approval turn in conversation")
	}

	var executeCount int
	if countErr := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM river_job WHERE kind = 'execute' AND args::text LIKE $1`,
		"%"+wfID+"%",
	).Scan(&executeCount); countErr != nil {
		t.Fatalf("querying execute jobs: %v", countErr)
	}
	if executeCount != 2 {
		t.Errorf("expected 2 execute jobs, got %d", executeCount)
	}

	if !updateCalled {
		t.Error("expected Slack UpdateMessage to be called")
	}
}

func TestHandleSlackInteractions_Reject(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	channelID := "C-INT-REJECT"
	wfID, convID := setupApprovalWorkflow(t, ctx, pool, channelID, "ts-int-reject")
	cleanupInteraction(t, ctx, pool, wfID, convID, channelID)

	var updateCalled bool
	slackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "chat.update") {
			updateCalled = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true,"ts":"msg-ts"}`)
	}))
	t.Cleanup(slackSrv.Close)
	slackClient := slack.NewClient("test-token", slackSrv.URL)

	handler := handleSlackInteractions(testSigningSecret, pool, nil, slackClient)
	payloadJSON := buildInteractionPayload("reject_plan", wfID, channelID, "reject-msg-ts")
	w := postInteraction(t, handler, payloadJSON)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	conv, convErr := saga.GetConversation(ctx, pool, convID)
	if convErr != nil {
		t.Fatalf("getting conversation: %v", convErr)
	}
	if conv.Status != "completed" {
		t.Errorf("conversation status = %q, want %q", conv.Status, "completed")
	}

	var outboxCount int
	if countErr := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM outbox WHERE conversation_id = $1 AND message_type = 'text'`,
		convID,
	).Scan(&outboxCount); countErr != nil {
		t.Fatalf("querying outbox: %v", countErr)
	}
	if outboxCount != 1 {
		t.Errorf("expected 1 rejection outbox entry, got %d", outboxCount)
	}

	if !updateCalled {
		t.Error("expected Slack UpdateMessage to be called")
	}
}

func TestHandleSlackInteractions_InvalidSignature(t *testing.T) {
	handler := handleSlackInteractions(testSigningSecret, nil, nil, nil)
	form := url.Values{"payload": {`{"type":"block_actions","actions":[]}`}}
	body := form.Encode()
	req := httptest.NewRequest(http.MethodPost, "/slack/interactions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", fmt.Sprintf("%d", time.Now().Unix()))
	req.Header.Set("X-Slack-Signature", "v0=invalidsignature")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", w.Code)
	}
}

func TestHandleSlackInteractions_UnknownAction(t *testing.T) {
	pool := testutil.TestDB(t)
	handler := handleSlackInteractions(testSigningSecret, pool, nil, nil)
	payloadJSON := buildInteractionPayload("unknown_action", "wf-doesnt-matter", "C-UNK", "msg-ts")
	w := postInteraction(t, handler, payloadJSON)

	// Slack requires 200 OK even for unrecognized actions
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for unknown action, got %d", w.Code)
	}
}

func TestHandleSlackInteractions_ApproveWithChannelConfig(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()
	riverClient := newInteractionRiverClient(t, pool)

	channelID := "C-INT-APPROVE-CFG"
	wfID, convID := setupApprovalWorkflow(t, ctx, pool, channelID, "ts-int-approve-cfg")
	cleanupInteraction(t, ctx, pool, wfID, convID, channelID)

	if err := channel.UpsertConfig(ctx, pool, &channel.Config{
		ChannelID:        channelID,
		ApprovalRequired: true,
	}); err != nil {
		t.Fatalf("upserting channel config: %v", err)
	}

	slackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true,"ts":"msg-ts"}`)
	}))
	t.Cleanup(slackSrv.Close)
	slackClient := slack.NewClient("test-token", slackSrv.URL)

	handler := handleSlackInteractions(testSigningSecret, pool, riverClient, slackClient)
	payloadJSON := buildInteractionPayload("approve_plan", wfID, channelID, "approval-cfg-msg-ts")
	w := postInteraction(t, handler, payloadJSON)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var executeCount int
	if countErr := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM river_job WHERE kind = 'execute' AND args::text LIKE $1`,
		"%"+wfID+"%",
	).Scan(&executeCount); countErr != nil {
		t.Fatalf("querying execute jobs: %v", countErr)
	}
	if executeCount != 2 {
		t.Errorf("expected 2 execute jobs, got %d", executeCount)
	}
}
