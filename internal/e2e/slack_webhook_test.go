//go:build e2e

package e2e

import (
	"testing"
	"time"
)

func TestSlackWebhookFlow(t *testing.T) {
	slackAPI, slackMu, slackMessages := newFakeSlackServer(t, nil)

	signingSecret := "test-signing-secret"
	s := setupE2EServer(t, e2eServerOpts{
		SigningSecret: signingSecret,
		SlackURL:      slackAPI.URL,
		Timeout:       90 * time.Second,
	})

	sendSlackEvent(t, s.Ctx, s.BaseURL, signingSecret, map[string]any{
		"type":     "event_callback",
		"event_id": "Ev_test_slack_webhook_e2e",
		"event": map[string]string{
			"type":         "app_mention",
			"user":         "U_TEST_USER",
			"text":         "<@U1234BOT> hello from e2e test",
			"channel":      "C_TEST_CHANNEL",
			"ts":           "1234567890.123456",
			"channel_type": "channel",
		},
	})

	waitForSlackMessages(t, slackMu, slackMessages, 1, 60*time.Second)

	slackMu.Lock()
	defer slackMu.Unlock()

	msg := (*slackMessages)[0]

	if msg.Channel != "C_TEST_CHANNEL" {
		t.Errorf("expected channel C_TEST_CHANNEL, got %s", msg.Channel)
	}

	if msg.ThreadTS != "1234567890.123456" {
		t.Errorf("expected thread_ts 1234567890.123456, got %s", msg.ThreadTS)
	}

	if msg.Text == "" {
		t.Error("expected non-empty response text from Claude")
	} else {
		t.Logf("Slack reply text: %s", msg.Text)
	}
}
