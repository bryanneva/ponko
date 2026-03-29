package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	appOtel "github.com/bryanneva/ponko/internal/otel"
	"github.com/bryanneva/ponko/internal/channel"
	"github.com/bryanneva/ponko/internal/llm"
	"github.com/bryanneva/ponko/internal/user"
	"github.com/bryanneva/ponko/internal/workflow"
)

type ExecuteArgs struct {
	WorkflowID   string `json:"workflow_id"`
	Instruction  string `json:"instruction"`
	TraceContext string `json:"trace_context,omitempty"`
	TaskIndex    int    `json:"task_index"`
}

func (ExecuteArgs) Kind() string { return "execute" }

type ExecuteWorker struct {
	river.WorkerDefaults[ExecuteArgs]
	MCPClient    llm.ToolCaller
	Pool         *pgxpool.Pool
	Claude       *llm.Client
	UserStore    *user.Store
	Timezone     *time.Location
	SystemPrompt string
	BotName      string
	Tools        []llm.Tool
}

const ExecuteMaxAttempts = 3

const (
	toolUseJobTimeout = 25 * time.Minute
	singleCallTimeout = 3 * time.Minute
)

func (w *ExecuteWorker) Timeout(_ *river.Job[ExecuteArgs]) time.Duration {
	return toolUseJobTimeout
}

func (w *ExecuteWorker) Work(ctx context.Context, job *river.Job[ExecuteArgs]) error {
	args := job.Args

	ctx, span := startJobSpan(ctx, "execute", args.WorkflowID, args.TraceContext)
	defer span.End()

	stepName := fmt.Sprintf("execute-%d", args.TaskIndex)

	data, err := workflow.GetOutput(ctx, w.Pool, args.WorkflowID, "receive")
	if err != nil {
		return fmt.Errorf("getting receive output: %w", err)
	}

	var payload receivePayload
	if unmarshalErr := json.Unmarshal(data, &payload); unmarshalErr != nil {
		return fmt.Errorf("unmarshaling receive output: %w", unmarshalErr)
	}

	// Only create workflow step on first attempt to avoid duplicate rows
	if job.Attempt == 1 {
		if _, createErr := workflow.CreateStep(ctx, w.Pool, args.WorkflowID, stepName); createErr != nil {
			return fmt.Errorf("creating %s step: %w", stepName, createErr)
		}
	}

	instruction := args.Instruction
	if job.Attempt > 1 && len(job.Errors) > 0 {
		lastError := job.Errors[len(job.Errors)-1]
		instruction = augmentInstructionWithError(args.Instruction, lastError.Error)
	}

	isFinalAttempt := job.Attempt >= job.MaxAttempts

	result, execErr := w.executeTask(ctx, instruction, payload)
	if execErr != nil {
		if !isFinalAttempt {
			return fmt.Errorf("executing task %d: %w", args.TaskIndex, execErr)
		}

		completed, total, failErr := w.handleExecuteFailure(ctx, args.WorkflowID, args.TaskIndex, stepName, args.Instruction, execErr)
		if failErr != nil {
			return failErr
		}

		return w.maybeTriggerSynthesize(ctx, args.WorkflowID, completed, total)
	}

	if updateErr := workflow.UpdateStepStatusByName(ctx, w.Pool, args.WorkflowID, stepName, "complete"); updateErr != nil {
		return fmt.Errorf("updating %s step status: %w", stepName, updateErr)
	}

	outputData, err := json.Marshal(map[string]string{
		"instruction": args.Instruction,
		"result":      result,
	})
	if err != nil {
		return fmt.Errorf("marshaling %s output: %w", stepName, err)
	}

	if saveErr := workflow.SaveOutput(ctx, w.Pool, args.WorkflowID, stepName, outputData); saveErr != nil {
		return fmt.Errorf("saving %s output: %w", stepName, saveErr)
	}

	completed, total, err := workflow.IncrementCompletedTasks(ctx, w.Pool, args.WorkflowID)
	if err != nil {
		return fmt.Errorf("incrementing completed tasks: %w", err)
	}

	slog.Info("execute task completed",
		"workflow_id", args.WorkflowID,
		"task_index", args.TaskIndex,
		"completed", completed,
		"total", total,
	)

	return w.maybeTriggerSynthesize(ctx, args.WorkflowID, completed, total)
}

// handleExecuteFailure saves a failure output and increments completed_tasks
// on the final attempt so synthesize can still trigger.
func (w *ExecuteWorker) handleExecuteFailure(ctx context.Context, workflowID string, taskIndex int, stepName string, instruction string, execErr error) (completed int, total int, err error) {
	slog.Warn("execute task permanently failed",
		"workflow_id", workflowID,
		"task_index", taskIndex,
		"error", execErr,
	)
	appOtel.RecordWorkflowFailed(ctx)

	if updateErr := workflow.UpdateStepStatusByName(ctx, w.Pool, workflowID, stepName, "failed"); updateErr != nil {
		return 0, 0, fmt.Errorf("updating %s step status: %w", stepName, updateErr)
	}

	outputData, marshalErr := json.Marshal(map[string]string{
		"instruction": instruction,
		"error":       execErr.Error(),
	})
	if marshalErr != nil {
		return 0, 0, fmt.Errorf("marshaling %s failure output: %w", stepName, marshalErr)
	}

	if saveErr := workflow.SaveOutput(ctx, w.Pool, workflowID, stepName, outputData); saveErr != nil {
		return 0, 0, fmt.Errorf("saving %s failure output: %w", stepName, saveErr)
	}

	completed, total, incrErr := workflow.IncrementCompletedTasks(ctx, w.Pool, workflowID)
	if incrErr != nil {
		return 0, 0, fmt.Errorf("incrementing completed tasks: %w", incrErr)
	}

	return completed, total, nil
}

func (w *ExecuteWorker) maybeTriggerSynthesize(ctx context.Context, workflowID string, completed, total int) error {
	if completed == total {
		client := river.ClientFromContext[pgx.Tx](ctx)
		_, insertErr := client.Insert(ctx, SynthesizeArgs{
			WorkflowID:   workflowID,
			TraceContext: appOtel.InjectTraceContext(ctx),
		}, &river.InsertOpts{
			UniqueOpts: river.UniqueOpts{ByArgs: true},
		})
		if insertErr != nil {
			return fmt.Errorf("enqueuing synthesize job: %w", insertErr)
		}
	}

	return nil
}

func augmentInstructionWithError(instruction string, previousError string) string {
	if previousError == "" {
		return instruction
	}
	return fmt.Sprintf("%s\n\nNote: A previous attempt at this task failed with error: %s\nPlease try a different approach if possible.",
		instruction, previousError)
}

func (w *ExecuteWorker) executeTask(ctx context.Context, instruction string, payload receivePayload) (string, error) {
	cfg, err := channel.GetConfig(ctx, w.Pool, payload.Channel)
	if err != nil {
		slog.Warn("failed to load channel config, using defaults", "channel", payload.Channel, "error", err)
	}

	displayName, tz := resolveUserFromStore(ctx, w.UserStore, w.Timezone, payload.UserID)

	now := time.Now()
	if tz != nil {
		now = now.In(tz)
	}

	prompt, tools, mcpClient := buildSystemPrompt(promptConfig{
		ChannelCfg:    cfg,
		MCPClient:     w.MCPClient,
		Tools:         w.Tools,
		DefaultPrompt: w.SystemPrompt,
		BotName:       w.BotName,
		Now:           now,
		DisplayName:   displayName,
	})

	prompt += "\n\nYou are executing a specific sub-task. Focus only on this instruction and return a concise result."

	messages := []llm.Message{
		{Role: llm.RoleUser, Content: instruction},
	}

	var userScope *llm.UserScope
	if payload.UserID != "" {
		userScope = &llm.UserScope{SlackUserID: payload.UserID, DisplayName: displayName, ChannelID: payload.Channel}
	}

	if mcpClient != nil && len(tools) > 0 {
		return w.Claude.SendConversationWithTools(ctx, prompt, messages, tools, mcpClient, userScope, llm.ModelSonnet)
	}
	return w.Claude.SendConversation(ctx, prompt, messages, llm.ModelHaiku)
}
