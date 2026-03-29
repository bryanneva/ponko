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
	"github.com/bryanneva/ponko/internal/channel"
	"github.com/bryanneva/ponko/internal/conversation"
	"github.com/bryanneva/ponko/internal/llm"
	"github.com/bryanneva/ponko/internal/schedule"
	"github.com/bryanneva/ponko/internal/slack"
	"github.com/bryanneva/ponko/internal/user"
	"github.com/bryanneva/ponko/internal/workflow"
)

const timestampFormat = "Monday, January 2, 2006 at 3:04 PM MST"

func defaultSystemPromptFor(botName string) string {
	return fmt.Sprintf("You are %s, a helpful assistant in a Slack workspace. Be direct and concise. Do not use emoji in your responses unless the user asks for them. When you don't know something, say so briefly rather than over-explaining what you can't do. Never generate plan summaries, numbered step lists, or progress tracking URLs. Just do the work and reply with the result.", botName)
}

var knowledgeBasePromptAddition = " You have access to a knowledge base via MCP tools. Proactively search it when questions relate to topics it covers — don't wait for the user to mention it by name. If a search returns no results, say you don't have that information rather than guessing."

var webSearchPromptAddition = " You have access to web search. Use it when the user asks about current events, weather, prices, competitor research, recent news, or anything that requires up-to-date information beyond your training data. Never claim you cannot access the internet or browse websites — you have web search tools available. If a search fails, report the actual error instead of claiming you lack the capability. Summarize results concisely and cite sources with URLs. Do not search for things you already know."

var slackThreadPromptAddition = ` When a user shares a Slack thread URL (like https://workspace.slack.com/archives/CHANNEL/pTIMESTAMP), use the read_slack_thread tool to fetch the thread contents. This lets you see what was discussed and respond helpfully.`

var schedulePromptAddition = ` You can manage scheduled recurring messages in Slack channels using these tools:
- create_schedule: Create a new recurring message. Translate the user's natural language schedule into a 5-field cron expression (minute hour day-of-month month day-of-week). Examples: "every weekday at 9am" → 0 9 * * 1-5, "every Monday at 10am" → 0 10 * * 1, "daily at 8:30am" → 30 8 * * *, "every hour" → 0 * * * *. Generate a short kebab-case slug from the prompt text (e.g., "daily-standup", "weekly-report"). Resolve the user's timezone from context. If the schedule request is ambiguous, ask for clarification before creating.
- list_schedules: List all active scheduled messages in the current channel.
- cancel_schedule: Cancel a schedule by its slug. Use list_schedules first if the user doesn't know the slug.`

type promptConfig struct {
	MCPClient     llm.ToolCaller
	ChannelCfg    *channel.Config
	Now           time.Time
	DefaultPrompt string
	BotName       string
	DisplayName   string
	Tools         []llm.Tool
}

// Used by both ProcessWorker and ProactiveMessageWorker.
func buildSystemPrompt(pc promptConfig) (string, []llm.Tool, llm.ToolCaller) {
	var prompt string
	if pc.ChannelCfg != nil {
		prompt = pc.ChannelCfg.SystemPrompt
	} else {
		prompt = pc.DefaultPrompt
		if prompt == "" {
			prompt = defaultSystemPromptFor(pc.BotName)
		}
	}

	if pc.ChannelCfg != nil {
		if pc.ChannelCfg.ToolAllowlist == nil && pc.MCPClient != nil {
			prompt += knowledgeBasePromptAddition
			prompt += webSearchPromptAddition
		} else if pc.MCPClient != nil {
			if toolsInclude(pc.ChannelCfg.ToolAllowlist, "search_thoughts", "capture_thought") {
				prompt += knowledgeBasePromptAddition
			}
			if toolsInclude(pc.ChannelCfg.ToolAllowlist, "web_search") {
				prompt += webSearchPromptAddition
			}
		}
	} else if pc.MCPClient != nil {
		prompt += knowledgeBasePromptAddition
		prompt += webSearchPromptAddition
	}

	prompt += slackThreadPromptAddition
	prompt += schedulePromptAddition

	prompt += "\n\nIt is currently " + pc.Now.Format(timestampFormat) + "."

	if pc.DisplayName != "" {
		prompt += fmt.Sprintf("\nYou are talking to %s.", pc.DisplayName)
	}

	tools := pc.Tools
	mcpClient := pc.MCPClient

	if pc.ChannelCfg != nil && pc.ChannelCfg.ToolAllowlist != nil {
		tools = filterTools(pc.Tools, pc.ChannelCfg.ToolAllowlist)
		if len(tools) == 0 {
			mcpClient = nil
		}
	}

	return prompt, tools, mcpClient
}

type ProcessArgs struct {
	WorkflowID   string `json:"workflow_id"`
	TraceContext string `json:"trace_context,omitempty"`
}

func (ProcessArgs) Kind() string { return "process" }

type RespondArgs struct {
	WorkflowID   string `json:"workflow_id"`
	ResponseStep string `json:"response_step,omitempty"`
	TraceContext string `json:"trace_context,omitempty"`
}

func (RespondArgs) Kind() string { return "respond" }

type ProcessWorker struct {
	river.WorkerDefaults[ProcessArgs]
	MCPClient    llm.ToolCaller
	Pool         *pgxpool.Pool
	Claude       *llm.Client
	Slack        *slack.Client
	UserStore    *user.Store
	Timezone     *time.Location
	SystemPrompt string
	BotName      string
	BaseURL      string
	Tools        []llm.Tool
}

func (w *ProcessWorker) postProgressIfNeeded(ctx context.Context, workflowID, channelID, threadTS, channelType string, hasTools bool) {
	if !hasTools || w.Slack == nil || w.BaseURL == "" {
		return
	}
	progressURL := w.BaseURL + "/workflows/" + workflowID
	progressMsg := fmt.Sprintf("_Processing your request..._\n<%s|View progress>", progressURL)
	replyTS := threadTS
	if channelType == "im" {
		replyTS = ""
	}
	if postErr := w.Slack.PostMessage(ctx, channelID, progressMsg, replyTS); postErr != nil {
		slog.Warn("failed to post progress message", "error", postErr, "workflow_id", workflowID)
	}
}

func (w *ProcessWorker) Timeout(_ *river.Job[ProcessArgs]) time.Duration {
	return toolUseJobTimeout
}

func (w *ProcessWorker) Work(ctx context.Context, job *river.Job[ProcessArgs]) error {
	args := job.Args

	ctx, span := startJobSpan(ctx, "process", args.WorkflowID, args.TraceContext)
	defer span.End()

	data, err := workflow.GetOutput(ctx, w.Pool, args.WorkflowID, "receive")
	if err != nil {
		return fmt.Errorf("getting receive output: %w", err)
	}

	var payload receivePayload
	err = json.Unmarshal(data, &payload)
	if err != nil {
		return fmt.Errorf("unmarshaling receive output: %w", err)
	}

	response, err := w.processMessage(ctx, payload.Channel, payload.ThreadKey, payload.Message, payload.UserID, args.WorkflowID, payload.ThreadTS, payload.ChannelType)
	if err != nil {
		return fmt.Errorf("calling Claude: %w", err)
	}

	stepID, err := workflow.CreateStep(ctx, w.Pool, args.WorkflowID, "process")
	if err != nil {
		return fmt.Errorf("creating process step: %w", err)
	}

	err = workflow.UpdateStepStatus(ctx, w.Pool, stepID, "complete")
	if err != nil {
		return fmt.Errorf("updating process step status: %w", err)
	}

	outputData, err := json.Marshal(map[string]string{
		"response":     response,
		"channel":      payload.Channel,
		"thread_ts":    payload.ThreadTS,
		"event_ts":     payload.EventTS,
		"channel_type": payload.ChannelType,
	})
	if err != nil {
		return fmt.Errorf("marshaling process output: %w", err)
	}

	err = workflow.SaveOutput(ctx, w.Pool, args.WorkflowID, "process", outputData)
	if err != nil {
		return fmt.Errorf("saving process output: %w", err)
	}

	client := river.ClientFromContext[pgx.Tx](ctx)
	_, err = client.Insert(ctx, RespondArgs{
		WorkflowID:   args.WorkflowID,
		TraceContext: appOtel.InjectTraceContext(ctx),
	}, nil)
	if err != nil {
		return fmt.Errorf("enqueuing respond job: %w", err)
	}

	return nil
}

func (w *ProcessWorker) processMessage(ctx context.Context, channelID, threadKey, message, userID, workflowID, threadTS, channelType string) (string, error) {
	if threadKey == "" {
		return strings.ToUpper(message), nil
	}

	const maxThreadHistory = 20
	history, err := conversation.GetThreadMessages(ctx, w.Pool, threadKey, maxThreadHistory)
	if err != nil {
		return "", fmt.Errorf("loading thread history: %w", err)
	}

	// Cache display names to avoid repeated DB lookups for the same user
	nameCache := make(map[string]string)
	resolveDisplayName := func(uid string) string {
		if uid == "" || w.UserStore == nil {
			return ""
		}
		if name, ok := nameCache[uid]; ok {
			return name
		}
		u, uErr := w.UserStore.GetOrFetch(ctx, uid)
		if uErr != nil || u.DisplayName == "" {
			nameCache[uid] = ""
			return ""
		}
		nameCache[uid] = u.DisplayName
		return u.DisplayName
	}

	messages := make([]llm.Message, 0, len(history)+1)
	for _, msg := range history {
		content := msg.Content
		if msg.Role == llm.RoleUser {
			if name := resolveDisplayName(msg.UserID); name != "" {
				content = name + ": " + content
			}
		}
		messages = append(messages, llm.Message{Role: msg.Role, Content: content})
	}
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: message})

	cfg, err := channel.GetConfig(ctx, w.Pool, channelID)
	if err != nil {
		slog.Warn("failed to load channel config, using defaults", "channel", channelID, "error", err)
	}

	displayName, tz := w.resolveUser(ctx, userID)

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

	var userScope *llm.UserScope
	if userID != "" {
		userScope = &llm.UserScope{SlackUserID: userID, DisplayName: displayName, ChannelID: channelID}
	}

	hasTools := mcpClient != nil && len(tools) > 0
	w.postProgressIfNeeded(ctx, workflowID, channelID, threadTS, channelType, hasTools)

	var response string
	if hasTools {
		response, err = w.Claude.SendConversationWithTools(ctx, prompt, messages, tools, mcpClient, userScope, llm.ModelSonnet)
	} else {
		response, err = w.Claude.SendConversation(ctx, prompt, messages, llm.ModelHaiku)
	}
	if err != nil {
		return "", err
	}

	if saveErr := conversation.SaveThreadMessages(ctx, w.Pool, threadKey, llm.RoleUser, message, llm.RoleAssistant, response, userID); saveErr != nil {
		return "", fmt.Errorf("saving thread messages: %w", saveErr)
	}

	return response, nil
}

func toolsInclude(allowlist []string, names ...string) bool {
	for _, name := range names {
		for _, allowed := range allowlist {
			if allowed == name {
				return true
			}
		}
	}
	return false
}

var internalTools = map[string]bool{
	schedule.CreateScheduleToolName: true,
	schedule.ListSchedulesToolName:  true,
	schedule.CancelScheduleToolName: true,
	slack.ReadSlackThreadToolName:   true,
}

func filterTools(tools []llm.Tool, allowlist []string) []llm.Tool {
	allowed := make(map[string]bool, len(allowlist))
	for _, name := range allowlist {
		allowed[name] = true
	}
	var filtered []llm.Tool
	for _, t := range tools {
		if allowed[t.Name] || internalTools[t.Name] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func (w *ProcessWorker) resolveUser(ctx context.Context, userID string) (string, *time.Location) {
	return resolveUserFromStore(ctx, w.UserStore, w.Timezone, userID)
}

func resolveUserFromStore(ctx context.Context, userStore *user.Store, fallbackTZ *time.Location, userID string) (string, *time.Location) {
	if userStore != nil && userID != "" {
		u, err := userStore.GetOrFetch(ctx, userID)
		if err != nil {
			slog.Warn("failed to get user from store, using fallback", "user_id", userID, "error", err)
			return "", fallbackTZ
		}
		loc, locErr := time.LoadLocation(u.Timezone)
		if locErr != nil {
			slog.Warn("invalid timezone from user profile, using fallback", "user_id", userID, "timezone", u.Timezone, "error", locErr)
			return u.DisplayName, fallbackTZ
		}
		return u.DisplayName, loc
	}
	return "", fallbackTZ
}
