package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/bryanneva/ponko/internal/channel"
	appOtel "github.com/bryanneva/ponko/internal/otel"
	"github.com/bryanneva/ponko/internal/llm"
	"github.com/bryanneva/ponko/internal/saga"
	"github.com/bryanneva/ponko/internal/slack"
	"github.com/bryanneva/ponko/internal/workflow"
)

type PlanArgs struct {
	WorkflowID   string `json:"workflow_id"`
	TraceContext string `json:"trace_context,omitempty"`
}

func (PlanArgs) Kind() string { return "plan" }

type PlanWorker struct {
	river.WorkerDefaults[PlanArgs]
	Pool       *pgxpool.Pool
	Claude     LLMClient
	Slack      *slack.Client
	AppBaseURL string
}

func (w *PlanWorker) Timeout(_ *river.Job[PlanArgs]) time.Duration {
	return singleCallTimeout
}

func (w *PlanWorker) Work(ctx context.Context, job *river.Job[PlanArgs]) error {
	args := job.Args

	ctx, span := startJobSpan(ctx, "plan", args.WorkflowID, args.TraceContext)
	defer span.End()

	data, err := workflow.GetOutput(ctx, w.Pool, args.WorkflowID, "receive")
	if err != nil {
		return fmt.Errorf("getting receive output: %w", err)
	}

	var payload receivePayload
	if unmarshalErr := json.Unmarshal(data, &payload); unmarshalErr != nil {
		return fmt.Errorf("unmarshaling receive output: %w", unmarshalErr)
	}

	stepID, err := workflow.CreateStep(ctx, w.Pool, args.WorkflowID, "plan")
	if err != nil {
		return fmt.Errorf("creating plan step: %w", err)
	}

	tasks, err := w.decompose(ctx, payload)
	if err != nil {
		return fmt.Errorf("decomposing message: %w", err)
	}

	if updateErr := workflow.UpdateStepStatus(ctx, w.Pool, stepID, "complete"); updateErr != nil {
		return fmt.Errorf("updating plan step status: %w", updateErr)
	}

	client := river.ClientFromContext[pgx.Tx](ctx)

	if tasks == nil {
		slog.Info("plan bypass: routing to process", "workflow_id", args.WorkflowID)
		_, err = client.Insert(ctx, ProcessArgs{
			WorkflowID:   args.WorkflowID,
			TraceContext: appOtel.InjectTraceContext(ctx),
		}, nil)
		if err != nil {
			return fmt.Errorf("enqueuing process job: %w", err)
		}
		return nil
	}

	slog.Info("plan fan-out", "workflow_id", args.WorkflowID, "task_count", len(tasks))

	planOutput, err := json.Marshal(map[string]any{
		"tasks":       tasks,
		"total_tasks": len(tasks),
	})
	if err != nil {
		return fmt.Errorf("marshaling plan output: %w", err)
	}
	if err := workflow.SaveOutput(ctx, w.Pool, args.WorkflowID, "plan", planOutput); err != nil {
		return fmt.Errorf("saving plan output: %w", err)
	}

	if err := workflow.SetTotalTasks(ctx, w.Pool, args.WorkflowID, len(tasks)); err != nil {
		return fmt.Errorf("setting total tasks: %w", err)
	}

	if w.shouldRequireApproval(ctx, payload.Channel) {
		return w.pauseForApproval(ctx, args.WorkflowID, payload, tasks)
	}

	for i, task := range tasks {
		_, err := client.Insert(ctx, ExecuteArgs{
			WorkflowID:   args.WorkflowID,
			Instruction:  task.Instruction,
			TraceContext: appOtel.InjectTraceContext(ctx),
			TaskIndex:    i,
		}, &river.InsertOpts{
			MaxAttempts: ExecuteMaxAttempts,
		})
		if err != nil {
			return fmt.Errorf("enqueuing execute job %d: %w", i, err)
		}
	}

	if w.Slack != nil {
		ackMsg := buildAckMessage(tasks, args.WorkflowID, w.AppBaseURL)
		if ackErr := w.Slack.PostMessage(ctx, payload.Channel, ackMsg, threadTSForReply(payload.ThreadTS, payload.ChannelType)); ackErr != nil {
			slog.Warn("failed to post ack message", "workflow_id", args.WorkflowID, "error", ackErr)
		}
	}

	return nil
}

func (w *PlanWorker) shouldRequireApproval(ctx context.Context, channelID string) bool {
	cfg, err := channel.GetConfig(ctx, w.Pool, channelID)
	if err != nil {
		slog.Warn("failed to load channel config for approval check", "channel_id", channelID, "error", err)
		return false
	}
	return cfg != nil && cfg.ApprovalRequired
}

func (w *PlanWorker) pauseForApproval(ctx context.Context, workflowID string, payload receivePayload, tasks []taskPlan) error {
	conversationID, err := workflow.GetConversationID(ctx, w.Pool, workflowID)
	if err != nil {
		return fmt.Errorf("getting conversation_id for approval: %w", err)
	}
	if conversationID == "" {
		slog.Info("approval: no conversation_id, creating one", "workflow_id", workflowID)
		var createErr error
		conversationID, _, createErr = saga.FindOrCreateConversation(ctx, w.Pool, payload.Channel, payload.ThreadTS)
		if createErr != nil {
			return fmt.Errorf("creating conversation for approval: %w", createErr)
		}
		if setErr := workflow.SetConversationID(ctx, w.Pool, workflowID, conversationID); setErr != nil {
			return fmt.Errorf("setting conversation_id for approval: %w", setErr)
		}
	}

	if transErr := saga.TransitionStatus(ctx, w.Pool, conversationID, "active", "awaiting_approval"); transErr != nil {
		return fmt.Errorf("transitioning to awaiting_approval: %w", transErr)
	}

	blocks := buildApprovalBlocks(tasks, workflowID)
	blocksJSON, marshalErr := json.Marshal(blocks)
	if marshalErr != nil {
		return fmt.Errorf("marshaling approval blocks: %w", marshalErr)
	}

	_, enqueueErr := saga.Enqueue(ctx, w.Pool, saga.OutboxEntry{
		ConversationID: conversationID,
		WorkflowID:     workflowID,
		ChannelID:      payload.Channel,
		ThreadTS:       payload.ThreadTS,
		MessageType:    "blocks",
		Content:        blocksJSON,
	})
	if enqueueErr != nil {
		return fmt.Errorf("enqueuing approval blocks: %w", enqueueErr)
	}

	slog.Info("plan paused for approval", "workflow_id", workflowID, "conversation_id", conversationID)

	if w.Slack != nil {
		ackMsg := buildAckMessage(tasks, workflowID, w.AppBaseURL)
		if ackErr := w.Slack.PostMessage(ctx, payload.Channel, ackMsg, threadTSForReply(payload.ThreadTS, payload.ChannelType)); ackErr != nil {
			slog.Warn("failed to post ack message", "workflow_id", workflowID, "error", ackErr)
		}
	}

	return nil
}

func buildApprovalBlocks(tasks []taskPlan, workflowID string) []slack.Block {
	var planText strings.Builder
	planText.WriteString("*Here's my plan:*\n")
	for i, task := range tasks {
		fmt.Fprintf(&planText, "%d. %s\n", i+1, task.Instruction)
	}

	return []slack.Block{
		slack.SectionBlock{
			Text: slack.TextObject{Type: "mrkdwn", Text: planText.String()},
		},
		slack.ActionsBlock{
			Elements: []slack.ButtonElement{
				{
					Text:     slack.TextObject{Type: "plain_text", Text: "Approve"},
					ActionID: "approve_plan",
					Value:    workflowID,
				},
				{
					Text:     slack.TextObject{Type: "plain_text", Text: "Reject"},
					ActionID: "reject_plan",
					Value:    workflowID,
					Style:    "danger",
				},
			},
		},
	}
}

type taskPlan struct {
	Instruction string `json:"instruction"`
}

var decomposePrompt = `You are a task planner. Given a user message, decide if it requires multiple independent steps or can be answered directly.

If the message is simple (greeting, single question, casual chat, single lookup), respond with exactly:
{"action":"direct"}

If the message requires multiple independent research tasks or actions, respond with:
{"action":"fan_out","tasks":[{"instruction":"..."},{"instruction":"..."}]}

Each task instruction should be self-contained and independently executable. Keep to 2-5 tasks max.

Respond with ONLY valid JSON, no other text.`

// decompose asks Claude whether the message needs fan-out.
// Returns nil for simple/bypass requests, or a task list for complex ones.
func (w *PlanWorker) decompose(ctx context.Context, payload receivePayload) ([]taskPlan, error) {
	if payload.ThreadKey == "" || w.Claude == nil {
		return nil, nil
	}

	response, err := w.Claude.SendMessage(ctx, fmt.Sprintf("%s\n\nUser message: %s", decomposePrompt, payload.Message), llm.ModelHaiku)
	if err != nil {
		if llm.IsPermanentError(err) {
			return nil, err
		}
		slog.Warn("plan decomposition failed, falling back to bypass", "error", err)
		return nil, nil
	}

	var result struct {
		Action string     `json:"action"`
		Tasks  []taskPlan `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(stripMarkdownFences(response)), &result); err != nil {
		slog.Warn("plan response not valid JSON, falling back to bypass", "response", response)
		return nil, nil
	}

	if result.Action != "fan_out" || len(result.Tasks) == 0 {
		return nil, nil
	}

	return result.Tasks, nil
}

func buildAckMessage(tasks []taskPlan, workflowID string, appBaseURL string) string {
	var b strings.Builder
	b.WriteString("Working on this — here's my plan:\n")
	for i, task := range tasks {
		fmt.Fprintf(&b, "%d. %s\n", i+1, task.Instruction)
	}
	b.WriteString("\nI'll reply when everything is done.")
	if appBaseURL != "" {
		fmt.Fprintf(&b, "\n<%s/workflows/%s|Track progress>", strings.TrimRight(appBaseURL, "/"), workflowID)
	}
	return b.String()
}

const channelTypeDM = "im"

func threadTSForReply(threadTS, channelType string) string {
	if channelType == channelTypeDM {
		return ""
	}
	return threadTS
}

func stripMarkdownFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Remove opening fence line (```json or ```)
	if i := strings.Index(s, "\n"); i != -1 {
		s = s[i+1:]
	}
	// Remove closing fence
	if strings.HasSuffix(s, "```") {
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimRight(s, "\n")
	}
	return s
}

// receivePayload is the shared struct for parsing receive output.
type receivePayload struct {
	Message     string `json:"message"`
	ThreadKey   string `json:"thread_key"`
	Channel     string `json:"channel"`
	ThreadTS    string `json:"thread_ts"`
	EventTS     string `json:"event_ts"`
	ChannelType string `json:"channel_type"`
	UserID      string `json:"user_id"`
}
