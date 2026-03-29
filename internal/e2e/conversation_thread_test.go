//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"
)

func TestConversationThread(t *testing.T) {
	fakeAnthropic := newFakeAnthropicServer(t, []string{
		"Sure, I'll remember that your name is Alice!",
		"Your name is Alice — you told me earlier.",
	})
	defer fakeAnthropic.Close()

	slackAPI, slackMu, slackMessages := newFakeSlackServer(t, nil)

	s := setupE2EServer(t, e2eServerOpts{
		SigningSecret: "test-signing-secret",
		SlackURL:      slackAPI.URL,
		AnthropicURL:  fakeAnthropic.URL,
	})

	threadTS := "9999999901.000001"

	sendSlackEvent(t, s.Ctx, s.BaseURL, "test-signing-secret", map[string]any{
		"type":     "event_callback",
		"event_id": "Ev_conv_thread_e2e_msg1",
		"event": map[string]string{
			"type":         "app_mention",
			"text":         "<@U1234BOT> My name is Alice. Please remember that.",
			"channel":      "C_CONV_THREAD",
			"ts":           threadTS,
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

	sendSlackEvent(t, s.Ctx, s.BaseURL, "test-signing-secret", map[string]any{
		"type":     "event_callback",
		"event_id": "Ev_conv_thread_e2e_msg2",
		"event": map[string]string{
			"type":         "app_mention",
			"text":         "<@U1234BOT> What is my name?",
			"channel":      "C_CONV_THREAD",
			"thread_ts":    threadTS,
			"ts":           "9999999902.000001",
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

	if !strings.Contains(strings.ToLower(secondReply.Text), "alice") {
		t.Errorf("expected second reply to mention 'Alice' (context from first message), got: %s", secondReply.Text)
	}

	if firstReply.Text == secondReply.Text {
		t.Error("first and second replies are identical — expected different responses")
	}
}
