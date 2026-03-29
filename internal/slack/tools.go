package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/bryanneva/ponko/internal/llm"
)

const ReadSlackThreadToolName = "read_slack_thread"

type ReadSlackThreadInput struct {
	URL string `json:"url"`
}

var ReadSlackThreadTool = llm.Tool{
	Name:        ReadSlackThreadToolName,
	Description: "Read the contents of a Slack thread given a Slack message URL. Returns the parent message and all replies. Use this when a user shares a Slack thread link and you need to see what was discussed.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {
				"type": "string",
				"description": "A Slack thread URL (e.g., https://workspace.slack.com/archives/CHANNEL_ID/pTIMESTAMP)"
			}
		},
		"required": ["url"]
	}`),
}

func ExecuteReadSlackThread(ctx context.Context, client *Client, rawURL string) (string, error) {
	channelID, ts, err := ParseThreadURL(rawURL)
	if err != nil {
		return "", fmt.Errorf("parsing Slack thread URL: %w", err)
	}

	messages, err := client.GetConversationReplies(ctx, channelID, ts)
	if err != nil {
		return "", fmt.Errorf("fetching thread replies: %w", err)
	}

	if len(messages) == 0 {
		return "Thread is empty or contains no messages.", nil
	}

	// Resolve unique user IDs to display names
	names := make(map[string]string)
	for _, m := range messages {
		if _, seen := names[m.UserID]; !seen {
			profile, profileErr := client.GetUserProfile(ctx, m.UserID)
			if profileErr != nil {
				slog.Info("failed to resolve display name, using user ID", "user_id", m.UserID, "error", profileErr)
				names[m.UserID] = m.UserID
			} else if profile.DisplayName == "" {
				names[m.UserID] = m.UserID
			} else {
				names[m.UserID] = profile.DisplayName
			}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Thread in channel %s (%d messages):\n\n", channelID, len(messages))
	for _, m := range messages {
		fmt.Fprintf(&b, "[%s] %s\n", names[m.UserID], m.Text)
	}
	return b.String(), nil
}
