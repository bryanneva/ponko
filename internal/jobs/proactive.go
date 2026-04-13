package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/bryanneva/ponko/internal/channel"
	"github.com/bryanneva/ponko/internal/llm"
	"github.com/bryanneva/ponko/internal/schedule"
	"github.com/bryanneva/ponko/internal/slack"
)

type ProactiveMessageArgs struct {
	ScheduleCron *string `json:"schedule_cron,omitempty"`
	ScheduleID   string  `json:"schedule_id"`
	ChannelID    string  `json:"channel_id"`
	Prompt       string  `json:"prompt"`
}

func (ProactiveMessageArgs) Kind() string { return "proactive_message" }

type ProactiveMessageWorker struct {
	river.WorkerDefaults[ProactiveMessageArgs]
	MCPClient    llm.ToolCaller
	Pool         *pgxpool.Pool
	Claude       LLMClient
	Slack        *slack.Client
	Timezone     *time.Location
	SystemPrompt string
	BotName      string
	Tools        []llm.Tool
}

func (w *ProactiveMessageWorker) Timeout(_ *river.Job[ProactiveMessageArgs]) time.Duration {
	return toolUseJobTimeout
}

func (w *ProactiveMessageWorker) Work(ctx context.Context, job *river.Job[ProactiveMessageArgs]) error {
	args := job.Args

	cfg, err := channel.GetConfig(ctx, w.Pool, args.ChannelID)
	if err != nil {
		slog.Warn("failed to load channel config, using defaults", "channel", args.ChannelID, "error", err)
	}

	now := time.Now()
	if w.Timezone != nil {
		now = now.In(w.Timezone)
	}

	prompt, tools, mcpClient := buildSystemPrompt(promptConfig{
		ChannelCfg:    cfg,
		MCPClient:     w.MCPClient,
		Tools:         w.Tools,
		DefaultPrompt: w.SystemPrompt,
		BotName:       w.BotName,
		Now:           now,
	})

	messages := []llm.Message{{Role: llm.RoleUser, Content: args.Prompt}}

	var response string
	if mcpClient != nil && len(tools) > 0 {
		response, err = w.Claude.SendConversationWithTools(ctx, prompt, messages, tools, mcpClient, nil, llm.ModelSonnet)
	} else {
		response, err = w.Claude.SendConversation(ctx, prompt, messages, llm.ModelHaiku)
	}
	if err != nil {
		return fmt.Errorf("calling Claude for proactive message: %w", err)
	}

	// Mark the run BEFORE posting to Slack. If Slack fails, River retries
	// the job — but we won't double-send because MarkRun already advanced
	// next_run_at (or disabled the one-shot).
	var nextRunAt *time.Time
	if args.ScheduleCron != nil {
		next, cronErr := schedule.NextRunAt(*args.ScheduleCron, time.Now())
		if cronErr != nil {
			return fmt.Errorf("computing next run time for schedule %s: %w", args.ScheduleID, cronErr)
		}
		nextRunAt = &next
	}

	if markErr := schedule.MarkRun(ctx, w.Pool, args.ScheduleID, nextRunAt); markErr != nil {
		return fmt.Errorf("marking scheduled message run: %w", markErr)
	}

	messageText := slack.MarkdownToMrkdwn(response)
	if postErr := w.Slack.PostMessage(ctx, args.ChannelID, messageText, ""); postErr != nil {
		return fmt.Errorf("posting proactive message to Slack: %w", postErr)
	}

	slog.Info("proactive message sent",
		"schedule_id", args.ScheduleID,
		"channel", args.ChannelID,
	)

	return nil
}
