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

	appOtel "github.com/bryanneva/ponko/internal/otel"
	"github.com/bryanneva/ponko/internal/conversation"
	"github.com/bryanneva/ponko/internal/llm"
	"github.com/bryanneva/ponko/internal/workflow"
)

type SynthesizeArgs struct {
	WorkflowID   string `json:"workflow_id"`
	TraceContext string `json:"trace_context,omitempty"`
}

func (SynthesizeArgs) Kind() string { return "synthesize" }

type SynthesizeWorker struct {
	river.WorkerDefaults[SynthesizeArgs]
	Pool   *pgxpool.Pool
	Claude LLMClient
}

func (w *SynthesizeWorker) Timeout(_ *river.Job[SynthesizeArgs]) time.Duration {
	return singleCallTimeout
}

func (w *SynthesizeWorker) Work(ctx context.Context, job *river.Job[SynthesizeArgs]) error {
	args := job.Args

	ctx, span := startJobSpan(ctx, "synthesize", args.WorkflowID, args.TraceContext)
	defer span.End()

	receiveData, err := workflow.GetOutput(ctx, w.Pool, args.WorkflowID, "receive")
	if err != nil {
		return fmt.Errorf("getting receive output: %w", err)
	}

	var payload receivePayload
	if unmarshalErr := json.Unmarshal(receiveData, &payload); unmarshalErr != nil {
		return fmt.Errorf("unmarshaling receive output: %w", unmarshalErr)
	}

	outputs, err := workflow.GetOutputsByPrefix(ctx, w.Pool, args.WorkflowID, "execute-")
	if err != nil {
		return fmt.Errorf("getting execute outputs: %w", err)
	}

	if len(outputs) == 0 {
		return fmt.Errorf("no execute outputs found for workflow %s", args.WorkflowID)
	}

	synthesisPrompt, userMessage := buildSynthesisInputs(payload.Message, outputs)

	response, err := w.Claude.SendConversation(ctx, synthesisPrompt,
		[]llm.Message{{Role: llm.RoleUser, Content: userMessage}},
		llm.ModelHaiku)
	if err != nil {
		return fmt.Errorf("synthesizing response: %w", err)
	}

	stepID, err := workflow.CreateStep(ctx, w.Pool, args.WorkflowID, "synthesize")
	if err != nil {
		return fmt.Errorf("creating synthesize step: %w", err)
	}

	if updateErr := workflow.UpdateStepStatus(ctx, w.Pool, stepID, "complete"); updateErr != nil {
		return fmt.Errorf("updating synthesize step status: %w", updateErr)
	}

	outputData, err := json.Marshal(map[string]string{
		"response":     response,
		"channel":      payload.Channel,
		"thread_ts":    payload.ThreadTS,
		"event_ts":     payload.EventTS,
		"channel_type": payload.ChannelType,
	})
	if err != nil {
		return fmt.Errorf("marshaling synthesize output: %w", err)
	}

	if saveErr := workflow.SaveOutput(ctx, w.Pool, args.WorkflowID, "synthesize", outputData); saveErr != nil {
		return fmt.Errorf("saving synthesize output: %w", saveErr)
	}

	if payload.ThreadKey != "" {
		if saveErr := conversation.SaveThreadMessages(ctx, w.Pool, payload.ThreadKey,
			llm.RoleUser, payload.Message, llm.RoleAssistant, response, payload.UserID); saveErr != nil {
			return fmt.Errorf("saving thread messages: %w", saveErr)
		}
	}

	client := river.ClientFromContext[pgx.Tx](ctx)
	_, err = client.Insert(ctx, RespondArgs{
		WorkflowID:   args.WorkflowID,
		ResponseStep: "synthesize",
		TraceContext: appOtel.InjectTraceContext(ctx),
	}, nil)
	if err != nil {
		return fmt.Errorf("enqueuing respond job: %w", err)
	}

	return nil
}

type execOutput struct {
	Instruction string `json:"instruction"`
	Result      string `json:"result"`
	Error       string `json:"error"`
}

func buildSynthesisInputs(originalMessage string, outputs []workflow.Output) (prompt string, userMessage string) {
	synthesisPrompt := "You are composing a final response from multiple sub-task results. Combine them into a single cohesive, natural response for the user. Do not mention that the work was split into sub-tasks. Be concise and direct."

	successParts := make([]string, 0, len(outputs))
	failureParts := make([]string, 0)

	for _, output := range outputs {
		var result execOutput
		if unmarshalErr := json.Unmarshal(output.Data, &result); unmarshalErr != nil {
			slog.Warn("failed to parse execute output", "step", output.StepName, "error", unmarshalErr)
			continue
		}
		if result.Error != "" {
			failureParts = append(failureParts, fmt.Sprintf("Task: %s\nFailed: %s", result.Instruction, result.Error))
		} else {
			successParts = append(successParts, fmt.Sprintf("Task: %s\nResult: %s", result.Instruction, result.Result))
		}
	}

	var userMessageParts []string
	userMessageParts = append(userMessageParts, fmt.Sprintf("Original question: %s", originalMessage))

	if len(successParts) > 0 {
		userMessageParts = append(userMessageParts,
			fmt.Sprintf("Sub-task results:\n\n%s", strings.Join(successParts, "\n\n---\n\n")))
	}

	if len(failureParts) > 0 {
		synthesisPrompt += " Some sub-tasks failed. Acknowledge what you couldn't complete and why, briefly and naturally, without apologizing excessively."
		userMessageParts = append(userMessageParts,
			fmt.Sprintf("Failed sub-tasks:\n\n%s", strings.Join(failureParts, "\n\n---\n\n")))
	}

	return synthesisPrompt, strings.Join(userMessageParts, "\n\n")
}
