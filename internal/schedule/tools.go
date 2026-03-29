package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bryanneva/ponko/internal/llm"
	"github.com/bryanneva/ponko/internal/user"
)

const maxSchedulesPerChannel = 10

const (
	CreateScheduleToolName = "create_schedule"
	ListSchedulesToolName  = "list_schedules"
	CancelScheduleToolName = "cancel_schedule"
)

type CreateScheduleInput struct {
	Prompt         string `json:"prompt"`
	CronExpression string `json:"cron_expression"`
	ChannelID      string `json:"channel_id"`
	Timezone       string `json:"timezone"`
	Slug           string `json:"slug"`
}

var CreateScheduleTool = llm.Tool{
	Name:        CreateScheduleToolName,
	Description: "Create a recurring scheduled message in a Slack channel. The message will be posted automatically on the specified cron schedule.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"prompt": {
				"type": "string",
				"description": "The message prompt that will be posted on each occurrence"
			},
			"cron_expression": {
				"type": "string",
				"description": "A 5-field cron expression (minute hour day-of-month month day-of-week)"
			},
			"channel_id": {
				"type": "string",
				"description": "The Slack channel ID where the message will be posted"
			},
			"timezone": {
				"type": "string",
				"description": "IANA timezone for the schedule (e.g. America/Los_Angeles)"
			},
			"slug": {
				"type": "string",
				"description": "A short kebab-case identifier for this schedule (e.g. daily-standup)"
			}
		},
		"required": ["prompt", "cron_expression", "channel_id", "timezone", "slug"]
	}`),
}

func ExecuteCreateSchedule(ctx context.Context, pool *pgxpool.Pool, input CreateScheduleInput, createdBy string) (string, error) {
	loc, err := time.LoadLocation(input.Timezone)
	if err != nil {
		return "", fmt.Errorf("invalid timezone %q: %w", input.Timezone, err)
	}

	now := time.Now().In(loc)
	nextRun, err := NextRunAt(input.CronExpression, now)
	if err != nil {
		return "", err
	}

	cron := input.CronExpression
	var createdByPtr *string
	if createdBy != "" {
		createdByPtr = &createdBy
	}

	if err = CreateWithQuota(ctx, pool, Message{
		ChannelID:    input.ChannelID,
		Prompt:       input.Prompt,
		Slug:         input.Slug,
		ScheduleCron: &cron,
		CreatedBy:    createdByPtr,
		NextRunAt:    nextRun.UTC(),
		OneShot:      false,
		Enabled:      true,
	}, maxSchedulesPerChannel); err != nil {
		if errors.Is(err, ErrQuotaExceeded) {
			return "", fmt.Errorf("this channel has reached the limit of %d active schedules — cancel an existing schedule first", maxSchedulesPerChannel)
		}
		return "", err
	}

	humanCron := cronToHuman(input.CronExpression)
	promptSummary := truncatePrompt(input.Prompt, 80)

	return fmt.Sprintf("Schedule created!\n• Slug: %s\n• Prompt: %s\n• Recurrence: %s\n• Next run: %s\n• Timezone: %s",
		input.Slug,
		promptSummary,
		humanCron,
		nextRun.Format("Monday, January 2 at 3:04 PM"),
		input.Timezone,
	), nil
}

func cronToHuman(expr string) string {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return expr
	}
	minute, hour, dom, month, dow := fields[0], fields[1], fields[2], fields[3], fields[4]

	if hour != "*" && dom == "*" && month == "*" {
		timeStr := formatCronTime(hour, minute)
		switch dow {
		case "*":
			return "daily at " + timeStr
		case "1-5":
			return "every weekday at " + timeStr
		case "0", "7":
			return "every Sunday at " + timeStr
		case "1":
			return "every Monday at " + timeStr
		case "2":
			return "every Tuesday at " + timeStr
		case "3":
			return "every Wednesday at " + timeStr
		case "4":
			return "every Thursday at " + timeStr
		case "5":
			return "every Friday at " + timeStr
		case "6":
			return "every Saturday at " + timeStr
		}
	}

	if minute == "0" && hour == "*" && dom == "*" && month == "*" && dow == "*" {
		return "every hour"
	}

	return expr
}

type ListSchedulesInput struct {
	ChannelID string `json:"channel_id"`
}

var ListSchedulesTool = llm.Tool{
	Name:        ListSchedulesToolName,
	Description: "List all active scheduled messages in a Slack channel.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"channel_id": {
				"type": "string",
				"description": "The Slack channel ID to list schedules for"
			}
		},
		"required": ["channel_id"]
	}`),
}

func ExecuteListSchedules(ctx context.Context, pool *pgxpool.Pool, userStore *user.Store, input ListSchedulesInput) (string, error) {
	messages, err := ListByChannel(ctx, pool, input.ChannelID)
	if err != nil {
		return "", err
	}

	if len(messages) == 0 {
		return "No scheduled messages in this channel.", nil
	}

	nameCache := make(map[string]string)
	for _, m := range messages {
		if m.CreatedBy != nil {
			nameCache[*m.CreatedBy] = ""
		}
	}
	for uid := range nameCache {
		u, fetchErr := userStore.GetOrFetch(ctx, uid)
		if fetchErr == nil {
			nameCache[uid] = u.DisplayName
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Active schedules (%d):\n", len(messages))
	for _, m := range messages {
		prompt := truncatePrompt(m.Prompt, 60)

		cron := ""
		if m.ScheduleCron != nil {
			cron = cronToHuman(*m.ScheduleCron)
		}

		creatorName := "unknown"
		if m.CreatedBy != nil {
			if name := nameCache[*m.CreatedBy]; name != "" {
				creatorName = name
			}
		}

		fmt.Fprintf(&b, "\n• %s\n  Prompt: %s\n  Recurrence: %s\n  Next run: %s\n  Created by: %s\n",
			m.Slug,
			prompt,
			cron,
			m.NextRunAt.Format("Monday, January 2 at 3:04 PM"),
			creatorName,
		)
	}
	return b.String(), nil
}

type CancelScheduleInput struct {
	ChannelID        string `json:"channel_id"`
	Slug             string `json:"slug"`
	RequestingUserID string `json:"requesting_user_id"`
}

var CancelScheduleTool = llm.Tool{
	Name:        CancelScheduleToolName,
	Description: "Cancel an active scheduled message in a Slack channel by its slug. Only the creator or a workspace admin can cancel a schedule.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"channel_id": {
				"type": "string",
				"description": "The Slack channel ID where the schedule exists"
			},
			"slug": {
				"type": "string",
				"description": "The slug identifier of the schedule to cancel"
			}
		},
		"required": ["channel_id", "slug"]
	}`),
}

func ExecuteCancelSchedule(ctx context.Context, pool *pgxpool.Pool, userStore *user.Store, input CancelScheduleInput) (string, error) {
	msg, err := GetBySlug(ctx, pool, input.ChannelID, input.Slug)
	if err != nil {
		return fmt.Sprintf("No active schedule with slug %q found in this channel. Use list_schedules to see active schedules.", input.Slug), nil
	}

	isCreator := msg.CreatedBy != nil && *msg.CreatedBy == input.RequestingUserID
	if !isCreator {
		requestor, fetchErr := userStore.GetOrFetch(ctx, input.RequestingUserID)
		if fetchErr != nil {
			return "", fmt.Errorf("checking user permissions: %w", fetchErr)
		}
		if !requestor.IsAdmin {
			return "You don't have permission to cancel this schedule. Only the creator or a workspace admin can cancel it.", nil
		}
	}

	if err = Cancel(ctx, pool, msg.ID); err != nil {
		return "", err
	}

	promptSummary := truncatePrompt(msg.Prompt, 80)

	cron := ""
	if msg.ScheduleCron != nil {
		cron = cronToHuman(*msg.ScheduleCron)
	}

	return fmt.Sprintf("Schedule cancelled!\n• Slug: %s\n• Prompt: %s\n• Recurrence: %s",
		msg.Slug,
		promptSummary,
		cron,
	), nil
}

func truncatePrompt(s string, max int) string {
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}

func formatCronTime(hour, minute string) string {
	var h, m int
	if _, err := fmt.Sscanf(hour, "%d", &h); err != nil {
		return hour + ":" + minute
	}
	if _, err := fmt.Sscanf(minute, "%d", &m); err != nil {
		return hour + ":" + minute
	}
	t := time.Date(2000, 1, 1, h, m, 0, 0, time.UTC)
	return t.Format("3:04 PM")
}
