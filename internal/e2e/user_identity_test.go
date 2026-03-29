//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestUserIdentityFlow(t *testing.T) {
	slackAPI, slackMu, slackMessages := newFakeSlackServer(t, &fakeSlackOpts{
		UsersInfoHandler: func(w http.ResponseWriter, r *http.Request) {
			userID := r.URL.Query().Get("user")
			w.Header().Set("Content-Type", "application/json")
			switch userID {
			case "UTEST123":
				_, _ = fmt.Fprint(w, `{"ok":true,"user":{"tz":"America/New_York","profile":{"display_name":"TestUser"}}}`)
			case "UTEST456":
				_, _ = fmt.Fprint(w, `{"ok":true,"user":{"tz":"America/Chicago","profile":{"display_name":"OtherUser"}}}`)
			default:
				_, _ = fmt.Fprint(w, `{"ok":true,"user":{"tz":"UTC","profile":{"display_name":"Unknown"}}}`)
			}
		},
	})

	s := setupE2EServer(t, e2eServerOpts{
		SigningSecret: "test-signing-secret",
		SlackURL:      slackAPI.URL,
		Timeout:       180 * time.Second,
		WithUserStore: true,
	})

	threadTS := "6666666601.000001"

	sendSlackEvent(t, s.Ctx, s.BaseURL, "test-signing-secret", map[string]any{
		"type":     "event_callback",
		"event_id": "Ev_user_identity_e2e_msg1",
		"event": map[string]string{
			"type":         "app_mention",
			"user":         "UTEST123",
			"text":         "<@U00TESTBOT> Hello! What is my name?",
			"channel":      "C_USER_IDENTITY_E2E",
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

	if !strings.Contains(firstReply.Text, "TestUser") {
		t.Errorf("expected first reply to contain 'TestUser', got: %s", firstReply.Text)
	}

	sendSlackEvent(t, s.Ctx, s.BaseURL, "test-signing-secret", map[string]any{
		"type":     "event_callback",
		"event_id": "Ev_user_identity_e2e_msg2",
		"event": map[string]string{
			"type":         "app_mention",
			"user":         "UTEST456",
			"text":         "<@U00TESTBOT> And what is my name?",
			"channel":      "C_USER_IDENTITY_E2E",
			"thread_ts":    threadTS,
			"ts":           "6666666602.000001",
			"channel_type": "channel",
		},
	})

	waitForSlackMessages(t, slackMu, slackMessages, 2, 90*time.Second)

	slackMu.Lock()
	secondReply := (*slackMessages)[1]
	slackMu.Unlock()

	if secondReply.Text == "" {
		t.Fatal("second reply text is empty")
	}
	t.Logf("Second reply: %s", secondReply.Text)

	if !strings.Contains(secondReply.Text, "OtherUser") {
		t.Errorf("expected second reply to contain 'OtherUser', got: %s", secondReply.Text)
	}
}
