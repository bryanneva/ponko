package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/bryanneva/ponko/internal/jobs"
	"github.com/bryanneva/ponko/internal/saga"
	"github.com/bryanneva/ponko/internal/slack"
	"github.com/bryanneva/ponko/internal/workflow"
)

type interactionPayload struct {
	Type    string              `json:"type"`
	User    interactionUser     `json:"user"`
	Channel interactionChannel  `json:"channel"`
	Message interactionMessage  `json:"message"`
	Actions []interactionAction `json:"actions"`
}

type interactionUser struct {
	ID string `json:"id"`
}

type interactionAction struct {
	ActionID string `json:"action_id"`
	Value    string `json:"value"`
}

type interactionChannel struct {
	ID string `json:"id"`
}

type interactionMessage struct {
	TS string `json:"ts"`
}

func handleSlackInteractions(signingSecret string, pool *pgxpool.Pool, riverClient *river.Client[pgx.Tx], slackClient *slack.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
		if err != nil {
			slog.Error("failed to read interaction request body", "error", err)
			writeError(w, http.StatusBadRequest, "failed to read request body")
			return
		}

		if !verifySlackSignature(signingSecret, r.Header, body) {
			writeError(w, http.StatusUnauthorized, "invalid signature")
			return
		}

		form, parseErr := url.ParseQuery(string(body))
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid form body")
			return
		}

		payloadJSON := form.Get("payload")
		if payloadJSON == "" {
			writeError(w, http.StatusBadRequest, "missing payload")
			return
		}

		var payload interactionPayload
		if unmarshalErr := json.Unmarshal([]byte(payloadJSON), &payload); unmarshalErr != nil {
			slog.Error("failed to parse interaction payload", "error", unmarshalErr)
			writeError(w, http.StatusBadRequest, "invalid payload")
			return
		}

		if len(payload.Actions) == 0 {
			w.WriteHeader(http.StatusOK)
			return
		}

		action := payload.Actions[0]
		ctx := r.Context()

		if action.Value != "" && !isValidUUID(action.Value) {
			slog.Warn("invalid workflow_id in interaction", "action_id", action.ActionID, "value", action.Value)
			w.WriteHeader(http.StatusOK)
			return
		}

		switch action.ActionID {
		case "approve_plan":
			if handleErr := handleApprove(ctx, pool, riverClient, slackClient, action.Value, payload.Channel.ID, payload.Message.TS); handleErr != nil {
				slog.Error("failed to handle approval", "workflow_id", action.Value, "error", handleErr)
			}
		case "reject_plan":
			if handleErr := handleReject(ctx, pool, slackClient, action.Value, payload.Channel.ID, payload.Message.TS); handleErr != nil {
				slog.Error("failed to handle rejection", "workflow_id", action.Value, "error", handleErr)
			}
		default:
			slog.Warn("unknown interaction action", "action_id", action.ActionID)
		}

		w.WriteHeader(http.StatusOK)
	}
}

func handleApprove(ctx context.Context, pool *pgxpool.Pool, riverClient *river.Client[pgx.Tx], slackClient *slack.Client, workflowID, channelID, messageTS string) error {
	convID, convErr := workflow.GetConversationID(ctx, pool, workflowID)
	if convErr != nil {
		return fmt.Errorf("getting conversation_id: %w", convErr)
	}
	if convID == "" {
		return fmt.Errorf("workflow %s has no conversation_id", workflowID)
	}

	planData, planErr := workflow.GetOutput(ctx, pool, workflowID, "plan")
	if planErr != nil {
		return fmt.Errorf("getting plan output: %w", planErr)
	}

	var planOutput struct {
		Tasks []struct {
			Instruction string `json:"instruction"`
		} `json:"tasks"`
	}
	if unmarshalErr := json.Unmarshal(planData, &planOutput); unmarshalErr != nil {
		return fmt.Errorf("unmarshaling plan output: %w", unmarshalErr)
	}

	if transErr := saga.TransitionStatus(ctx, pool, convID, "awaiting_approval", "active"); transErr != nil {
		return fmt.Errorf("transitioning to active: %w", transErr)
	}

	if turnErr := saga.AddTurn(ctx, pool, convID, workflowID, "approval"); turnErr != nil {
		return fmt.Errorf("adding approval turn: %w", turnErr)
	}

	for i, task := range planOutput.Tasks {
		_, insertErr := riverClient.Insert(ctx, jobs.ExecuteArgs{
			WorkflowID:  workflowID,
			TaskIndex:   i,
			Instruction: task.Instruction,
		}, &river.InsertOpts{
			MaxAttempts: jobs.ExecuteMaxAttempts,
		})
		if insertErr != nil {
			return fmt.Errorf("enqueuing execute job %d: %w", i, insertErr)
		}
	}

	slog.Info("plan approved", "workflow_id", workflowID, "conversation_id", convID, "tasks", len(planOutput.Tasks))

	if slackClient != nil {
		updateErr := slackClient.UpdateMessage(ctx, channelID, messageTS, "Approved \u2713", nil)
		if updateErr != nil {
			slog.Warn("failed to update approval message", "error", updateErr)
		}
	}

	return nil
}

func handleReject(ctx context.Context, pool *pgxpool.Pool, slackClient *slack.Client, workflowID, channelID, messageTS string) error {
	convID, convErr := workflow.GetConversationID(ctx, pool, workflowID)
	if convErr != nil {
		return fmt.Errorf("getting conversation_id: %w", convErr)
	}
	if convID == "" {
		return fmt.Errorf("workflow %s has no conversation_id", workflowID)
	}

	conv, getErr := saga.GetConversation(ctx, pool, convID)
	if getErr != nil {
		return fmt.Errorf("getting conversation: %w", getErr)
	}

	if transErr := saga.TransitionStatus(ctx, pool, convID, "awaiting_approval", "completed"); transErr != nil {
		return fmt.Errorf("transitioning to completed: %w", transErr)
	}

	rejectionMsg, _ := json.Marshal("Plan rejected. Let me know if you'd like to try a different approach.")
	if _, enqueueErr := saga.Enqueue(ctx, pool, saga.OutboxEntry{
		ConversationID: convID,
		WorkflowID:     workflowID,
		ChannelID:      channelID,
		ThreadTS:       conv.ThreadTS,
		MessageType:    "text",
		Content:        rejectionMsg,
	}); enqueueErr != nil {
		return fmt.Errorf("enqueuing rejection message: %w", enqueueErr)
	}

	slog.Info("plan rejected", "workflow_id", workflowID, "conversation_id", convID)

	if slackClient != nil {
		updateErr := slackClient.UpdateMessage(ctx, channelID, messageTS, "Rejected \u2717", nil)
		if updateErr != nil {
			slog.Warn("failed to update rejection message", "error", updateErr)
		}
	}

	return nil
}
