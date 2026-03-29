//go:build e2e

package e2e

import (
	"testing"
	"time"
)

func TestMentionOnlyThreadContinuity(t *testing.T) {
	fakeAnthropic := newFakeAnthropicServer(t, []string{
		"Got it! I'll remember the word pineapple.",
		"The word you asked me to remember was pineapple.",
	})
	defer fakeAnthropic.Close()

	slackAPI, slackMu, slackMessages := newFakeSlackServer(t, nil)

	s := setupE2EServer(t, e2eServerOpts{
		SigningSecret: "test-signing-secret",
		SlackURL:      slackAPI.URL,
		AnthropicURL:  fakeAnthropic.URL,
	})

	mentionedThreadTS := "7777777701.000001"
	unmentionedThreadTS := "7777777702.000001"

	sendSlackEvent(t, s.Ctx, s.BaseURL, "test-signing-secret", map[string]any{
		"type":     "event_callback",
		"event_id": "Ev_mo_thread_e2e_msg1",
		"event": map[string]string{
			"type":         "app_mention",
			"user":         "U_TEST_USER",
			"text":         "<@U00TESTBOT> Hi bot, remember the word: pineapple",
			"channel":      "C_MO_THREAD_E2E",
			"ts":           mentionedThreadTS,
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
	t.Logf("AC1 - First reply (app_mention): %s", firstReply.Text)

	if firstReply.ThreadTS != mentionedThreadTS {
		t.Errorf("expected thread_ts %s, got %s", mentionedThreadTS, firstReply.ThreadTS)
	}

	sendSlackEvent(t, s.Ctx, s.BaseURL, "test-signing-secret", map[string]any{
		"type":     "event_callback",
		"event_id": "Ev_mo_thread_e2e_msg2",
		"event": map[string]string{
			"type":         "message",
			"user":         "U_TEST_USER",
			"text":         "What word did I ask you to remember?",
			"channel":      "C_MO_THREAD_E2E",
			"thread_ts":    mentionedThreadTS,
			"ts":           "7777777701.000002",
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
	t.Logf("AC2 - Second reply (thread continuity): %s", secondReply.Text)

	sendSlackEvent(t, s.Ctx, s.BaseURL, "test-signing-secret", map[string]any{
		"type":     "event_callback",
		"event_id": "Ev_mo_thread_e2e_msg3",
		"event": map[string]string{
			"type":         "message",
			"user":         "U_TEST_USER",
			"text":         "Hello, is anyone there?",
			"channel":      "C_MO_THREAD_E2E",
			"thread_ts":    unmentionedThreadTS,
			"ts":           "7777777702.000002",
			"channel_type": "channel",
		},
	})

	time.Sleep(5 * time.Second)

	slackMu.Lock()
	finalCount := len(*slackMessages)
	slackMu.Unlock()

	if finalCount != 2 {
		t.Errorf("expected exactly 2 Slack messages (bot should NOT respond in unmentioned thread), got %d", finalCount)
	}
}
