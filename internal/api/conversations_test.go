package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bryanneva/ponko/internal/saga"
	"github.com/bryanneva/ponko/internal/testutil"
	"github.com/bryanneva/ponko/internal/workflow"
)

func cleanupTestConversations(t *testing.T, pool *pgxpool.Pool, channelID string) {
	t.Helper()
	ctx := context.Background()
	_, _ = pool.Exec(ctx,
		`DELETE FROM outbox WHERE conversation_id IN (SELECT conversation_id FROM conversations WHERE channel_id = $1)`,
		channelID)
	_, _ = pool.Exec(ctx,
		`DELETE FROM conversation_turns WHERE conversation_id IN (SELECT conversation_id FROM conversations WHERE channel_id = $1)`,
		channelID)
	_, _ = pool.Exec(ctx,
		`DELETE FROM conversations WHERE channel_id = $1`,
		channelID)
}

func TestHandleRecentConversations(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	channelID := "C_TEST_CONV_LIST"
	cleanupTestConversations(t, pool, channelID)
	t.Cleanup(func() { cleanupTestConversations(t, pool, channelID) })

	// Create two conversations with turns
	convID1, _, err := saga.FindOrCreateConversation(ctx, pool, channelID, "2000.0001")
	if err != nil {
		t.Fatalf("FindOrCreateConversation 1: %v", err)
	}
	wfID1, err := workflow.CreateWorkflow(ctx, pool, "echo")
	if err != nil {
		t.Fatalf("CreateWorkflow 1: %v", err)
	}
	addErr := saga.AddTurn(ctx, pool, convID1, wfID1, "message")
	if addErr != nil {
		t.Fatalf("AddTurn 1: %v", addErr)
	}

	convID2, _, err := saga.FindOrCreateConversation(ctx, pool, channelID, "2000.0002")
	if err != nil {
		t.Fatalf("FindOrCreateConversation 2: %v", err)
	}
	wfID2, err := workflow.CreateWorkflow(ctx, pool, "echo")
	if err != nil {
		t.Fatalf("CreateWorkflow 2: %v", err)
	}
	addErr2 := saga.AddTurn(ctx, pool, convID2, wfID2, "message")
	if addErr2 != nil {
		t.Fatalf("AddTurn 2a: %v", addErr2)
	}
	wfID3, err := workflow.CreateWorkflow(ctx, pool, "echo")
	if err != nil {
		t.Fatalf("CreateWorkflow 3: %v", err)
	}
	if err := saga.AddTurn(ctx, pool, convID2, wfID3, "approval"); err != nil {
		t.Fatalf("AddTurn 2b: %v", err)
	}

	handler := handleRecentConversations(pool)
	req := httptest.NewRequest(http.MethodGet, "/api/conversations/recent", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var convs []saga.ConversationSummary
	if err := json.NewDecoder(w.Body).Decode(&convs); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(convs) < 2 {
		t.Fatalf("expected at least 2 conversations, got %d", len(convs))
	}

	// Find our conversations in results
	found := map[string]saga.ConversationSummary{}
	for _, c := range convs {
		if c.ConversationID == convID1 || c.ConversationID == convID2 {
			found[c.ConversationID] = c
		}
	}

	if len(found) != 2 {
		t.Fatalf("expected to find 2 test conversations, found %d", len(found))
	}

	c1 := found[convID1]
	if c1.TurnCount != 1 {
		t.Errorf("conv1 TurnCount = %d, want 1", c1.TurnCount)
	}
	if c1.ChannelID != channelID {
		t.Errorf("conv1 ChannelID = %q, want %q", c1.ChannelID, channelID)
	}
	if c1.Status != "active" {
		t.Errorf("conv1 Status = %q, want active", c1.Status)
	}

	c2 := found[convID2]
	if c2.TurnCount != 2 {
		t.Errorf("conv2 TurnCount = %d, want 2", c2.TurnCount)
	}
}

func TestHandleRecentConversations_WithLimit(t *testing.T) {
	pool := testutil.TestDB(t)

	handler := handleRecentConversations(pool)
	req := httptest.NewRequest(http.MethodGet, "/api/conversations/recent?limit=1", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var convs []saga.ConversationSummary
	if err := json.NewDecoder(w.Body).Decode(&convs); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(convs) > 1 {
		t.Errorf("expected at most 1 conversation with limit=1, got %d", len(convs))
	}
}

func TestHandleGetConversation(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	channelID := "C_TEST_CONV_DETAIL"
	cleanupTestConversations(t, pool, channelID)
	t.Cleanup(func() { cleanupTestConversations(t, pool, channelID) })

	convID, _, err := saga.FindOrCreateConversation(ctx, pool, channelID, "3000.0001")
	if err != nil {
		t.Fatalf("FindOrCreateConversation: %v", err)
	}
	wfID, err := workflow.CreateWorkflow(ctx, pool, "echo")
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if err := saga.AddTurn(ctx, pool, convID, wfID, "message"); err != nil {
		t.Fatalf("AddTurn: %v", err)
	}

	// Create an outbox entry for this conversation
	content, _ := json.Marshal("hello")
	_, enqueueErr := saga.Enqueue(ctx, pool, saga.OutboxEntry{
		ConversationID: convID,
		WorkflowID:     wfID,
		ChannelID:      channelID,
		ThreadTS:       "3000.0001",
		MessageType:    "text",
		Content:        content,
	})
	if enqueueErr != nil {
		t.Fatalf("Enqueue: %v", enqueueErr)
	}

	handler := handleGetConversation(pool)
	req := httptest.NewRequest(http.MethodGet, "/api/conversations/"+convID, nil)
	req.SetPathValue("id", convID)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var detail conversationDetail
	if err := json.NewDecoder(w.Body).Decode(&detail); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if detail.ConversationID != convID {
		t.Errorf("ConversationID = %q, want %q", detail.ConversationID, convID)
	}
	if detail.ChannelID != channelID {
		t.Errorf("ChannelID = %q, want %q", detail.ChannelID, channelID)
	}
	if detail.Status != "active" {
		t.Errorf("Status = %q, want active", detail.Status)
	}
	if len(detail.Turns) != 1 {
		t.Fatalf("len(Turns) = %d, want 1", len(detail.Turns))
	}
	if detail.Turns[0].WorkflowID != wfID {
		t.Errorf("Turn WorkflowID = %q, want %q", detail.Turns[0].WorkflowID, wfID)
	}
	if detail.Turns[0].TriggerType != "message" {
		t.Errorf("Turn TriggerType = %q, want message", detail.Turns[0].TriggerType)
	}
	if len(detail.OutboxEntries) != 1 {
		t.Fatalf("len(OutboxEntries) = %d, want 1", len(detail.OutboxEntries))
	}
	if detail.OutboxEntries[0].MessageType != "text" {
		t.Errorf("OutboxEntry MessageType = %q, want text", detail.OutboxEntries[0].MessageType)
	}
	if detail.OutboxEntries[0].Status != "pending" {
		t.Errorf("OutboxEntry Status = %q, want pending", detail.OutboxEntries[0].Status)
	}
}

func TestHandleGetConversation_InvalidUUID(t *testing.T) {
	handler := handleGetConversation(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/conversations/not-a-uuid", nil)
	req.SetPathValue("id", "not-a-uuid")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestHandleGetConversation_NotFound(t *testing.T) {
	pool := testutil.TestDB(t)

	handler := handleGetConversation(pool)
	req := httptest.NewRequest(http.MethodGet, "/api/conversations/00000000-0000-0000-0000-000000000000", nil)
	req.SetPathValue("id", "00000000-0000-0000-0000-000000000000")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", w.Code)
	}
}
