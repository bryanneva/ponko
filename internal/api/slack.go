package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	channelPkg "github.com/bryanneva/ponko/internal/channel"
	"github.com/bryanneva/ponko/internal/conversation"
	"github.com/bryanneva/ponko/internal/jobs"
	"github.com/bryanneva/ponko/internal/llm"
	appOtel "github.com/bryanneva/ponko/internal/otel"
	"github.com/bryanneva/ponko/internal/saga"
	"github.com/bryanneva/ponko/internal/slack"
	"github.com/bryanneva/ponko/internal/workflow"
)

type slackEventCallback struct {
	EventID string     `json:"event_id"`
	Type    string     `json:"type"`
	Event   slackEvent `json:"event"`
}

type slackEvent struct {
	Type        string `json:"type"`
	SubType     string `json:"subtype"`
	User        string `json:"user"`
	Text        string `json:"text"`
	Channel     string `json:"channel"`
	ThreadTS    string `json:"thread_ts"`
	TS          string `json:"ts"`
	ChannelType string `json:"channel_type"`
}

type seenEntry struct {
	at      time.Time
	eventID string
}

const (
	seenEventsTTL      = 10 * time.Minute
	threadExpiryMaxAge = 3 * 24 * time.Hour
)

func signOffMessageFor(botName string) string {
	return fmt.Sprintf("I've been away from this thread for a while, so I'm signing off. Mention me with @%s if you'd like to bring me back into the conversation!", botName)
}

var botUserID = os.Getenv("SLACK_BOT_USER_ID")

var ignoredSubtypes = map[string]bool{
	"bot_message":     true,
	"message_changed": true,
	"message_deleted": true,
}

var mentionPrefixRe = regexp.MustCompile(`^<@[A-Z0-9]+>\s*`)

func stripMentionPrefix(text string) string {
	return strings.TrimSpace(mentionPrefixRe.ReplaceAllString(text, ""))
}

var validRespondModes = map[string]bool{
	channelPkg.RespondModeMentionOnly: true,
	channelPkg.RespondModeAllMessages: true,
}

func handleSlackCommand(pool *pgxpool.Pool, apiKey, botName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		channelID := r.FormValue("channel_id")
		text := strings.TrimSpace(r.FormValue("text"))

		if channelID == "" {
			respondSlackCommand(w, ":warning: Could not determine channel.")
			return
		}

		args := strings.Fields(text)

		// /<command> (no args) or /<command> config — show current config
		if len(args) == 0 || (len(args) == 1 && strings.EqualFold(args[0], "config")) {
			cfg, err := channelPkg.GetConfig(r.Context(), pool, channelID)
			if err != nil {
				slog.Error("failed to get channel config", "error", err)
				respondSlackCommand(w, ":warning: Failed to read config.")
				return
			}
			respondMode := cfg.EffectiveRespondMode()
			host := r.Host
			if host == "" {
				host = r.Header.Get("X-Forwarded-Host")
			}
			configURL := fmt.Sprintf("https://%s/channels/%s/config", host, channelID)
			if apiKey != "" {
				configURL += "?key=" + apiKey
			}
			msg := fmt.Sprintf("*Channel config:*\n• `respond_mode`: `%s`\n\n<%s|Full config UI> (system prompt, tool access)", respondMode, configURL)
			respondSlackCommand(w, msg)
			return
		}

		// /<command> respond_mode <value>
		if len(args) == 2 && strings.EqualFold(args[0], "respond_mode") {
			value := strings.ToLower(args[1])
			if !validRespondModes[value] {
				respondSlackCommand(w, fmt.Sprintf(":warning: Invalid respond_mode `%s`. Valid options: `mention_only`, `all_messages`", args[1]))
				return
			}
			cfg, err := channelPkg.GetConfig(r.Context(), pool, channelID)
			if err != nil {
				slog.Error("failed to get channel config for update", "error", err)
				respondSlackCommand(w, ":warning: Failed to read config.")
				return
			}
			if cfg == nil {
				cfg = &channelPkg.Config{ChannelID: channelID}
			}
			cfg.RespondMode = value
			if err := channelPkg.UpsertConfig(r.Context(), pool, cfg); err != nil {
				slog.Error("failed to update respond_mode", "error", err)
				respondSlackCommand(w, ":warning: Failed to update config.")
				return
			}
			respondSlackCommand(w, fmt.Sprintf(":white_check_mark: `respond_mode` set to `%s`", value))
			return
		}

		cmd := strings.ToLower(botName)
		respondSlackCommand(w, fmt.Sprintf("Usage: `/%s` or `/%s respond_mode <mention_only|all_messages>`", cmd, cmd))
	}
}

func respondSlackCommand(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"response_type": "ephemeral", "text": text})
}

func inferChannelType(channelID string) string {
	if channelID == "" {
		return ""
	}
	switch channelID[0] {
	case 'C':
		return "channel"
	case 'D':
		return "im"
	case 'G':
		return "group"
	default:
		return ""
	}
}

func replyThreadTS(threadTS, channelType string) string {
	if channelType == "im" {
		return ""
	}
	return threadTS
}

func notifySlackError(ctx context.Context, client *slack.Client, channel, eventTS, replyTS string, err error) {
	if client == nil {
		return
	}
	errMsg := err.Error()
	if len(errMsg) > 200 {
		errMsg = errMsg[:200]
	}
	msg := fmt.Sprintf(":warning: Something went wrong: `%s`", errMsg)
	_ = client.PostMessage(ctx, channel, msg, replyTS)
	_ = client.RemoveReaction(ctx, channel, eventTS, "eyes")
}

func postSignOff(ctx context.Context, pool *pgxpool.Pool, slackClient *slack.Client, channel, replyTS, threadKey, botName string) {
	msg := signOffMessageFor(botName)
	if err := slackClient.PostMessage(ctx, channel, msg, replyTS); err != nil {
		slog.Error("failed to post sign-off message", "error", err)
		return
	}
	if err := conversation.SaveThreadMessage(ctx, pool, threadKey, llm.RoleAssistant, msg, ""); err != nil {
		slog.Error("failed to save sign-off message", "error", err)
	}
}

func retryWithBackoff(fn func() error) error {
	delays := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}
	var lastErr error
	for _, delay := range delays {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		slog.Warn("retryable operation failed, backing off", "error", lastErr, "delay", delay)
		time.Sleep(delay)
	}
	return lastErr
}

func handleSlackEvents(signingSecret string, pool *pgxpool.Pool, riverClient *river.Client[pgx.Tx], slackClient *slack.Client, apiKey, botName string) http.HandlerFunc {
	var (
		seenEvents sync.Map
		seenOrder  []seenEntry
		seenMu     sync.Mutex
	)

	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
		if err != nil {
			slog.Error("failed to read slack request body", "error", err)
			writeError(w, http.StatusBadRequest, "failed to read request body")
			return
		}

		if !verifySlackSignature(signingSecret, r.Header, body) {
			writeError(w, http.StatusUnauthorized, "invalid signature")
			return
		}

		var payload struct {
			Type      string     `json:"type"`
			Challenge string     `json:"challenge"`
			EventID   string     `json:"event_id"`
			Event     slackEvent `json:"event"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if payload.Type == "url_verification" {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, payload.Challenge)
			return
		}

		if payload.Type == "event_callback" {
			callback := slackEventCallback{
				EventID: payload.EventID,
				Event:   payload.Event,
			}

			// Deduplicate by event_id with TTL-based eviction
			if _, loaded := seenEvents.LoadOrStore(callback.EventID, true); loaded {
				w.WriteHeader(http.StatusOK)
				return
			}
			now := time.Now()
			seenMu.Lock()
			seenOrder = append(seenOrder, seenEntry{eventID: callback.EventID, at: now})
			cutoff := now.Add(-seenEventsTTL)
			evicted := 0
			for _, entry := range seenOrder {
				if entry.at.Before(cutoff) {
					seenEvents.Delete(entry.eventID)
					evicted++
				} else {
					break
				}
			}
			seenOrder = seenOrder[evicted:]
			seenMu.Unlock()

			eventType := callback.Event.Type

			// Ignore message subtypes that are not user-authored messages
			if eventType == "message" && ignoredSubtypes[callback.Event.SubType] {
				w.WriteHeader(http.StatusOK)
				return
			}

			// Ignore messages from the bot to prevent self-loops
			if eventType == "message" && callback.Event.User == botUserID {
				w.WriteHeader(http.StatusOK)
				return
			}

			if (eventType == "app_mention" || eventType == "message") && pool != nil && riverClient != nil {
				message := stripMentionPrefix(callback.Event.Text)
				threadTS := callback.Event.ThreadTS
				if threadTS == "" {
					threadTS = callback.Event.TS
				}
				channel := callback.Event.Channel
				channelType := callback.Event.ChannelType
				if channelType == "" {
					channelType = inferChannelType(channel)
				}
				eventTS := callback.Event.TS
				replyTS := replyThreadTS(threadTS, channelType)

				// Respond with a brief greeting if the message is empty after stripping @mention
				if message == "" && slackClient != nil {
					go func() {
						_ = slackClient.PostMessage(context.Background(), channel, "Hey! How can I help?", replyTS)
					}()
					w.WriteHeader(http.StatusOK)
					return
				}

				// Route message events based on channel respond_mode
				if eventType == "message" {
					chanCfg, cfgErr := channelPkg.GetConfig(context.Background(), pool, channel)
					if cfgErr != nil {
						slog.Error("failed to get channel config for routing", "error", cfgErr)
					}
					respondMode := chanCfg.EffectiveRespondMode()
					isThreaded := callback.Event.ThreadTS != ""

					if respondMode == channelPkg.RespondModeMentionOnly && !isThreaded {
						w.WriteHeader(http.StatusOK)
						return
					}

					if isThreaded {
						threadKey := conversation.ThreadKey(channel, threadTS)
						part, partErr := conversation.CheckParticipation(context.Background(), pool, threadKey, threadExpiryMaxAge)
						if partErr != nil {
							slog.Error("failed to check thread participation", "error", partErr)
							if respondMode == channelPkg.RespondModeMentionOnly {
								w.WriteHeader(http.StatusOK)
								return
							}
						} else if part.Any && !part.Recent {
							if slackClient != nil {
								go postSignOff(context.Background(), pool, slackClient, channel, replyTS, threadKey, botName)
							}
							w.WriteHeader(http.StatusOK)
							return
						} else if respondMode == channelPkg.RespondModeMentionOnly && !part.Recent {
							w.WriteHeader(http.StatusOK)
							return
						}
					}
				}

				if slackClient != nil {
					if err := slackClient.AddReaction(context.Background(), channel, eventTS, "eyes"); err != nil {
						slog.Warn("failed to add eyes reaction", "error", err)
					}
				}

				go func() {
					ctx := context.Background()

					var workflowID string
					createErr := retryWithBackoff(func() error {
						var retryErr error
						workflowID, retryErr = workflow.CreateWorkflow(ctx, pool, "echo")
						return retryErr
					})
					if createErr != nil {
						slog.Error("failed to create workflow from slack event after retries", "error", createErr)
						notifySlackError(ctx, slackClient, channel, eventTS, replyTS, createErr)
						return
					}

					var convID string
					convErr := retryWithBackoff(func() error {
						var retryErr error
						convID, _, retryErr = saga.FindOrCreateConversation(ctx, pool, channel, threadTS)
						return retryErr
					})
					if convErr != nil {
						slog.Warn("failed to create conversation after retries", "error", convErr, "workflow_id", workflowID)
					} else {
						if setErr := workflow.SetConversationID(ctx, pool, workflowID, convID); setErr != nil {
							slog.Warn("failed to set conversation_id on workflow", "error", setErr, "workflow_id", workflowID)
						}
						if turnErr := saga.AddTurn(ctx, pool, convID, workflowID, "message"); turnErr != nil {
							slog.Warn("failed to add conversation turn", "error", turnErr, "workflow_id", workflowID)
						}
					}

					threadKey := conversation.ThreadKey(channel, threadTS)

					insertErr := retryWithBackoff(func() error {
						_, retryErr := riverClient.Insert(ctx, jobs.ReceiveArgs{
							WorkflowID:   workflowID,
							ThreadKey:    threadKey,
							Channel:      channel,
							ThreadTS:     threadTS,
							EventTS:      eventTS,
							ChannelType:  channelType,
							Message:      message,
							UserID:       callback.Event.User,
							TraceContext: appOtel.InjectTraceContext(ctx),
						}, nil)
						return retryErr
					})
					if insertErr != nil {
						slog.Error("failed to enqueue receive job from slack event after retries", "error", insertErr, "workflow_id", workflowID)
						notifySlackError(ctx, slackClient, channel, eventTS, replyTS, insertErr)
						return
					}

					slog.Info("slack event triggered workflow",
						"event_type", eventType,
						"workflow_id", workflowID,
						"channel", channel,
						"thread_ts", threadTS,
						"channel_type", channelType,
					)
				}()
			}
		}

		w.WriteHeader(http.StatusOK)
	}
}

func verifySlackSignature(signingSecret string, headers http.Header, body []byte) bool {
	timestamp := headers.Get("X-Slack-Request-Timestamp")
	signature := headers.Get("X-Slack-Signature")

	if timestamp == "" || signature == "" {
		return false
	}

	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}

	// Reject requests older than 5 minutes to prevent replay attacks
	if abs(time.Now().Unix()-ts) > 300 {
		return false
	}

	baseString := fmt.Sprintf("v0:%s:%s", timestamp, body)
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(baseString))
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expected), []byte(signature))
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
