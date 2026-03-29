package jobs

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/riverqueue/river"

	"github.com/bryanneva/ponko/internal/slack"
)

type SlackReplyArgs struct {
	WorkflowID   string `json:"workflow_id"`
	Response     string `json:"response"`
	Channel      string `json:"channel"`
	ThreadTS     string `json:"thread_ts"`
	EventTS      string `json:"event_ts"`
	ChannelType  string `json:"channel_type"`
	TraceContext string `json:"trace_context,omitempty"`
}

func (SlackReplyArgs) Kind() string { return "slack_reply" }

type SlackReplyWorker struct {
	river.WorkerDefaults[SlackReplyArgs]
	Slack *slack.Client
}

func (w *SlackReplyWorker) Work(ctx context.Context, job *river.Job[SlackReplyArgs]) error {
	args := job.Args

	ctx, span := startJobSpan(ctx, "slack_reply", args.WorkflowID, args.TraceContext)
	defer span.End()

	threadTS := threadTSForReply(args.ThreadTS, args.ChannelType)

	if args.EventTS != "" {
		if err := w.Slack.RemoveReaction(ctx, args.Channel, args.EventTS, "eyes"); err != nil {
			slog.Warn("failed to remove eyes reaction", "error", err)
		}
	}

	var messageText string
	var reaction string
	if args.Response == "" {
		slog.Error("empty response in slack reply args",
			"workflow_id", args.WorkflowID,
			"channel", args.Channel,
		)
		messageText = fmt.Sprintf(":warning: Workflow `%s` failed: empty response from LLM", args.WorkflowID)
		reaction = "warning"
	} else {
		messageText = slack.MarkdownToMrkdwn(args.Response)
		reaction = "white_check_mark"
	}

	if postErr := w.Slack.PostMessage(ctx, args.Channel, messageText, threadTS); postErr != nil {
		if slack.IsPermanentError(postErr) {
			logLevel := slog.LevelWarn
			if slack.IsCredentialError(postErr) {
				logLevel = slog.LevelError
			}
			slog.Log(ctx, logLevel, "discarding job due to permanent Slack error",
				"error", postErr,
				"workflow_id", args.WorkflowID,
				"channel", args.Channel,
			)
			return nil
		}
		return fmt.Errorf("posting slack message: %w", postErr)
	}

	if args.EventTS != "" {
		if err := w.Slack.AddReaction(ctx, args.Channel, args.EventTS, reaction); err != nil {
			slog.Warn("failed to add reaction", "error", err, "reaction", reaction)
		}
	}

	slog.Info("slack reply sent",
		"workflow_id", args.WorkflowID,
		"channel", args.Channel,
		"channel_type", args.ChannelType,
	)

	return nil
}
