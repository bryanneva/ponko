package jobs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bryanneva/ponko/internal/llm"
	"github.com/bryanneva/ponko/internal/schedule"
	"github.com/bryanneva/ponko/internal/slack"
	"github.com/bryanneva/ponko/internal/user"
)

type ToolDispatcher struct {
	Pool      *pgxpool.Pool
	UserStore *user.Store
	MCPClient llm.ToolCaller
	Slack     *slack.Client
}

func (d *ToolDispatcher) CallTool(ctx context.Context, name string, arguments map[string]any, userScope *llm.UserScope) (string, error) {
	switch name {
	case schedule.CreateScheduleToolName:
		return d.handleCreateSchedule(ctx, arguments, userScope)
	case schedule.ListSchedulesToolName:
		return d.handleListSchedules(ctx, arguments, userScope)
	case schedule.CancelScheduleToolName:
		return d.handleCancelSchedule(ctx, arguments, userScope)
	case slack.ReadSlackThreadToolName:
		return d.handleReadSlackThread(ctx, arguments)
	default:
		if d.MCPClient != nil {
			return d.MCPClient.CallTool(ctx, name, arguments, userScope)
		}
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (d *ToolDispatcher) handleCreateSchedule(ctx context.Context, arguments map[string]any, userScope *llm.UserScope) (string, error) {
	var input schedule.CreateScheduleInput
	if err := remarshal(arguments, &input); err != nil {
		return "", fmt.Errorf("parsing create_schedule input: %w", err)
	}
	var createdBy string
	if userScope != nil {
		createdBy = userScope.SlackUserID
		if input.ChannelID == "" {
			input.ChannelID = userScope.ChannelID
		}
		if input.Timezone == "" {
			input.Timezone = d.resolveTimezone(ctx, userScope.SlackUserID)
		}
	}
	return schedule.ExecuteCreateSchedule(ctx, d.Pool, input, createdBy)
}

func (d *ToolDispatcher) handleListSchedules(ctx context.Context, arguments map[string]any, userScope *llm.UserScope) (string, error) {
	var input schedule.ListSchedulesInput
	if err := remarshal(arguments, &input); err != nil {
		return "", fmt.Errorf("parsing list_schedules input: %w", err)
	}
	if input.ChannelID == "" && userScope != nil {
		input.ChannelID = userScope.ChannelID
	}
	return schedule.ExecuteListSchedules(ctx, d.Pool, d.UserStore, input)
}

func (d *ToolDispatcher) handleCancelSchedule(ctx context.Context, arguments map[string]any, userScope *llm.UserScope) (string, error) {
	var input schedule.CancelScheduleInput
	if err := remarshal(arguments, &input); err != nil {
		return "", fmt.Errorf("parsing cancel_schedule input: %w", err)
	}
	if userScope != nil {
		input.RequestingUserID = userScope.SlackUserID
		if input.ChannelID == "" {
			input.ChannelID = userScope.ChannelID
		}
	}
	return schedule.ExecuteCancelSchedule(ctx, d.Pool, d.UserStore, input)
}

func (d *ToolDispatcher) resolveTimezone(ctx context.Context, slackUserID string) string {
	if d.UserStore == nil || slackUserID == "" {
		return ""
	}
	u, err := d.UserStore.GetOrFetch(ctx, slackUserID)
	if err != nil {
		return ""
	}
	return u.Timezone
}

func (d *ToolDispatcher) handleReadSlackThread(ctx context.Context, arguments map[string]any) (string, error) {
	if d.Slack == nil {
		return "", fmt.Errorf("slack client not configured")
	}
	var input slack.ReadSlackThreadInput
	if err := remarshal(arguments, &input); err != nil {
		return "", fmt.Errorf("parsing read_slack_thread input: %w", err)
	}
	return slack.ExecuteReadSlackThread(ctx, d.Slack, input.URL)
}

func remarshal(src map[string]any, dst any) error {
	b, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}
