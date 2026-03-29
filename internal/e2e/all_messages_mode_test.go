//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/bryanneva/ponko/internal/channel"
)

func TestAllMessagesMode(t *testing.T) {
	fakeAnthropic := newFakeAnthropicServer(t, []string{
		"Got it! I'll remember that your favorite color is blue.",
		"Your favorite color is blue.",
	})
	defer fakeAnthropic.Close()

	slackAPI, slackMu, slackMessages := newFakeSlackServer(t, nil)

	s := setupE2EServer(t, e2eServerOpts{
		SigningSecret: "test-signing-secret",
		SlackURL:      slackAPI.URL,
		AnthropicURL:  fakeAnthropic.URL,
	})

	upsertErr := channel.UpsertConfig(s.Ctx, s.Pool, &channel.Config{
		ChannelID:   "C_ALL_MSG_E2E",
		RespondMode: "all_messages",
	})
	if upsertErr != nil {
		t.Fatalf("failed to set channel config: %v", upsertErr)
	}

	topLevelTS := "8888888801.000001"

	sendSlackEvent(t, s.Ctx, s.BaseURL, "test-signing-secret", map[string]any{
		"type":     "event_callback",
		"event_id": "Ev_all_msg_e2e_msg1",
		"event": map[string]string{
			"type":         "message",
			"user":         "U_TEST_USER",
			"text":         "My favorite color is blue. Please remember that.",
			"channel":      "C_ALL_MSG_E2E",
			"ts":           topLevelTS,
			"channel_type": "channel",
		},
	})

	waitForSlackMessages(t, slackMu, slackMessages, 1, 60*time.Second)

	slackMu.Lock()
	firstReply := (*slackMessages)[0]
	slackMu.Unlock()

	if firstReply.Text == "" {
		t.Fatal("first reply text is empty")
	}
	t.Logf("First reply: %s", firstReply.Text)

	if firstReply.ThreadTS != topLevelTS {
		t.Errorf("expected thread_ts %s, got %s", topLevelTS, firstReply.ThreadTS)
	}

	sendSlackEvent(t, s.Ctx, s.BaseURL, "test-signing-secret", map[string]any{
		"type":     "event_callback",
		"event_id": "Ev_all_msg_e2e_msg2",
		"event": map[string]string{
			"type":         "message",
			"user":         "U_TEST_USER",
			"text":         "What is my favorite color?",
			"channel":      "C_ALL_MSG_E2E",
			"thread_ts":    topLevelTS,
			"ts":           "8888888802.000001",
			"channel_type": "channel",
		},
	})

	waitForSlackMessages(t, slackMu, slackMessages, 2, 60*time.Second)

	slackMu.Lock()
	secondReply := (*slackMessages)[1]
	slackMu.Unlock()

	if secondReply.Text == "" {
		t.Fatal("second reply text is empty")
	}
	t.Logf("Second reply: %s", secondReply.Text)

	if !strings.Contains(strings.ToLower(secondReply.Text), "blue") {
		t.Errorf("expected second reply to mention 'blue' (context from first message), got: %s", secondReply.Text)
	}
}
