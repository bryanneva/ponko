package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bryanneva/ponko/internal/saga"
)

type turnSummary struct {
	CreatedAt   time.Time `json:"created_at"`
	TurnID      string    `json:"turn_id"`
	WorkflowID  string    `json:"workflow_id"`
	TriggerType string    `json:"trigger_type"`
	TurnIndex   int       `json:"turn_index"`
}

type outboxSummary struct {
	CreatedAt   time.Time  `json:"created_at"`
	DeliveredAt *time.Time `json:"delivered_at"`
	OutboxID    string     `json:"outbox_id"`
	MessageType string     `json:"message_type"`
	Status      string     `json:"status"`
	Attempts    int        `json:"attempts"`
}

type conversationDetail struct {
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	ConversationID string          `json:"conversation_id"`
	ChannelID      string          `json:"channel_id"`
	Status         string          `json:"status"`
	Turns          []turnSummary   `json:"turns"`
	OutboxEntries  []outboxSummary `json:"outbox_entries"`
}

func handleRecentConversations(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 200 {
				limit = parsed
			}
		}

		convs, err := saga.ListRecentConversations(r.Context(), pool, limit)
		if err != nil {
			slog.Error("failed to list conversations", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list conversations")
			return
		}

		writeJSON(w, http.StatusOK, convs)
	}
}

func handleGetConversation(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !isValidUUID(id) {
			writeError(w, http.StatusBadRequest, "invalid conversation_id: must be a valid UUID")
			return
		}

		conv, err := saga.GetConversation(r.Context(), pool, id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "conversation not found")
				return
			}
			slog.Error("failed to get conversation", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to get conversation")
			return
		}

		outboxEntries, err := saga.ListOutboxByConversation(r.Context(), pool, id)
		if err != nil {
			slog.Error("failed to list outbox entries", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to get conversation")
			return
		}

		turns := make([]turnSummary, len(conv.Turns))
		for i, t := range conv.Turns {
			turns[i] = turnSummary{
				CreatedAt:   t.CreatedAt,
				TurnID:      t.TurnID,
				WorkflowID:  t.WorkflowID,
				TriggerType: t.TriggerType,
				TurnIndex:   t.TurnIndex,
			}
		}

		entries := make([]outboxSummary, len(outboxEntries))
		for i, e := range outboxEntries {
			entries[i] = outboxSummary{
				CreatedAt:   e.CreatedAt,
				DeliveredAt: e.DeliveredAt,
				OutboxID:    e.OutboxID,
				MessageType: e.MessageType,
				Status:      e.Status,
				Attempts:    e.Attempts,
			}
		}

		detail := conversationDetail{
			CreatedAt:      conv.CreatedAt,
			UpdatedAt:      conv.UpdatedAt,
			ConversationID: conv.ConversationID,
			ChannelID:      conv.ChannelID,
			Status:         conv.Status,
			Turns:          turns,
			OutboxEntries:  entries,
		}

		writeJSON(w, http.StatusOK, detail)
	}
}
