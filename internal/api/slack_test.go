package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bryanneva/ponko/internal/channel"
	"github.com/bryanneva/ponko/internal/testutil"
)

const testSigningSecret = "test-signing-secret-12345"

func signRequest(t *testing.T, body string, ts int64) (string, string) {
	t.Helper()
	timestamp := fmt.Sprintf("%d", ts)
	baseString := fmt.Sprintf("v0:%s:%s", timestamp, body)
	mac := hmac.New(sha256.New, []byte(testSigningSecret))
	mac.Write([]byte(baseString))
	sig := "v0=" + hex.EncodeToString(mac.Sum(nil))
	return timestamp, sig
}

func TestHandleSlackEvents_URLVerification(t *testing.T) {
	handler := handleSlackEvents(testSigningSecret, nil, nil, nil, "", "TestBot")

	body := `{"type":"url_verification","token":"abc","challenge":"test-challenge-value"}`
	ts, sig := signRequest(t, body, time.Now().Unix())

	req := httptest.NewRequest(http.MethodPost, "/slack/events", strings.NewReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", sig)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	if w.Body.String() != "test-challenge-value" {
		t.Fatalf("expected challenge 'test-challenge-value', got %q", w.Body.String())
	}
}

func TestHandleSlackEvents_ValidSignature(t *testing.T) {
	handler := handleSlackEvents(testSigningSecret, nil, nil, nil, "", "TestBot")

	body := `{"type":"event_callback","event":{"type":"app_mention"}}`
	ts, sig := signRequest(t, body, time.Now().Unix())

	req := httptest.NewRequest(http.MethodPost, "/slack/events", strings.NewReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", sig)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
}

func TestHandleSlackEvents_InvalidSignature(t *testing.T) {
	handler := handleSlackEvents(testSigningSecret, nil, nil, nil, "", "TestBot")

	body := `{"type":"event_callback"}`
	ts := fmt.Sprintf("%d", time.Now().Unix())

	req := httptest.NewRequest(http.MethodPost, "/slack/events", strings.NewReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", "v0=invalidsignature")
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", w.Code)
	}
}

func TestHandleSlackEvents_MissingSignatureHeaders(t *testing.T) {
	handler := handleSlackEvents(testSigningSecret, nil, nil, nil, "", "TestBot")

	body := `{"type":"event_callback"}`

	req := httptest.NewRequest(http.MethodPost, "/slack/events", strings.NewReader(body))
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", w.Code)
	}
}

func TestHandleSlackEvents_ExpiredTimestamp(t *testing.T) {
	handler := handleSlackEvents(testSigningSecret, nil, nil, nil, "", "TestBot")

	body := `{"type":"event_callback"}`
	oldTS := time.Now().Unix() - 600 // 10 minutes ago
	ts, sig := signRequest(t, body, oldTS)

	req := httptest.NewRequest(http.MethodPost, "/slack/events", strings.NewReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", sig)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", w.Code)
	}
}

func TestStripMentionPrefix(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"simple mention", "<@U12345> hello", "hello"},
		{"mention with extra spaces", "<@U12345>   hello world", "hello world"},
		{"no mention", "hello world", "hello world"},
		{"mention only", "<@U12345>", ""},
		{"mention with multiword", "<@UBOT123> tell me a joke", "tell me a joke"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripMentionPrefix(tt.text)
			if got != tt.want {
				t.Errorf("stripMentionPrefix(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestHandleSlackEvents_AppMentionReturns200(t *testing.T) {
	handler := handleSlackEvents(testSigningSecret, nil, nil, nil, "", "TestBot")

	body := `{"type":"event_callback","event_id":"Ev01","event":{"type":"app_mention","text":"<@U123> hello","channel":"C123","ts":"1234567890.123456","channel_type":"channel"}}`
	ts, sig := signRequest(t, body, time.Now().Unix())

	req := httptest.NewRequest(http.MethodPost, "/slack/events", strings.NewReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", sig)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
}

func TestHandleSlackEvents_IgnoresNonAppMention(t *testing.T) {
	handler := handleSlackEvents(testSigningSecret, nil, nil, nil, "", "TestBot")

	body := `{"type":"event_callback","event_id":"Ev02","event":{"type":"message","text":"hello","channel":"C123"}}`
	ts, sig := signRequest(t, body, time.Now().Unix())

	req := httptest.NewRequest(http.MethodPost, "/slack/events", strings.NewReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", sig)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
}

func TestHandleSlackEvents_DeduplicatesEvents(t *testing.T) {
	handler := handleSlackEvents(testSigningSecret, nil, nil, nil, "", "TestBot")

	body := `{"type":"event_callback","event_id":"Ev03","event":{"type":"app_mention","text":"<@U123> hello","channel":"C123","ts":"1234567890.123456","channel_type":"channel"}}`
	ts, sig := signRequest(t, body, time.Now().Unix())

	// First request
	req1 := httptest.NewRequest(http.MethodPost, "/slack/events", strings.NewReader(body))
	req1.Header.Set("X-Slack-Request-Timestamp", ts)
	req1.Header.Set("X-Slack-Signature", sig)
	w1 := httptest.NewRecorder()
	handler(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("first request: expected status 200, got %d", w1.Code)
	}

	// Duplicate request with same event_id — should be deduped (still 200)
	ts2, sig2 := signRequest(t, body, time.Now().Unix())
	req2 := httptest.NewRequest(http.MethodPost, "/slack/events", strings.NewReader(body))
	req2.Header.Set("X-Slack-Request-Timestamp", ts2)
	req2.Header.Set("X-Slack-Signature", sig2)
	w2 := httptest.NewRecorder()
	handler(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("duplicate request: expected status 200, got %d", w2.Code)
	}
}

func TestRetryWithBackoff_SucceedsImmediately(t *testing.T) {
	calls := 0
	err := retryWithBackoff(func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestRetryWithBackoff_SucceedsAfterRetries(t *testing.T) {
	calls := 0
	err := retryWithBackoff(func() error {
		calls++
		if calls < 3 {
			return errors.New("transient error")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestRetryWithBackoff_ExhaustsRetries(t *testing.T) {
	calls := 0
	persistentErr := errors.New("persistent error")
	err := retryWithBackoff(func() error {
		calls++
		return persistentErr
	})
	if !errors.Is(err, persistentErr) {
		t.Fatalf("expected persistent error, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls (all retries exhausted), got %d", calls)
	}
}

func TestInferChannelType(t *testing.T) {
	tests := []struct {
		name      string
		channelID string
		want      string
	}{
		{"public channel", "C12345", "channel"},
		{"DM", "D12345", "im"},
		{"group", "G12345", "group"},
		{"unknown prefix", "X12345", ""},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferChannelType(tt.channelID)
			if got != tt.want {
				t.Errorf("inferChannelType(%q) = %q, want %q", tt.channelID, got, tt.want)
			}
		})
	}
}

func TestHandleSlackEvents_EventParsing(t *testing.T) {
	handler := handleSlackEvents(testSigningSecret, nil, nil, nil, "", "TestBot")

	body := `{"type":"event_callback","event_id":"Ev04","event":{"type":"app_mention","text":"<@UBOT> tell me a joke","channel":"C456","thread_ts":"111.222","ts":"333.444","channel_type":"group"}}`
	ts, sig := signRequest(t, body, time.Now().Unix())

	req := httptest.NewRequest(http.MethodPost, "/slack/events", strings.NewReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", sig)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
}

func postSlackCommand(handler http.HandlerFunc, formValues url.Values) *httptest.ResponseRecorder {
	body := formValues.Encode()
	req := httptest.NewRequest(http.MethodPost, "/slack/commands", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

type slackCommandResponse struct {
	ResponseType string `json:"response_type"`
	Text         string `json:"text"`
}

func TestHandleSlackCommand(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	_, cleanupErr := pool.Exec(ctx, "DELETE FROM channel_configs WHERE channel_id LIKE 'C_CMD_%'")
	if cleanupErr != nil {
		t.Fatalf("cleanup: %v", cleanupErr)
	}

	t.Run("missing channel_id returns warning", func(t *testing.T) {
		handler := handleSlackCommand(pool, "", "TestBot")
		w := postSlackCommand(handler, url.Values{"text": {"config"}})

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp slackCommandResponse
		if decErr := json.NewDecoder(w.Body).Decode(&resp); decErr != nil {
			t.Fatalf("decode: %v", decErr)
		}
		if !strings.Contains(resp.Text, "Could not determine channel") {
			t.Fatalf("expected channel warning, got %q", resp.Text)
		}
	})

	t.Run("no args shows config", func(t *testing.T) {
		handler := handleSlackCommand(pool, "test-api-key", "TestBot")
		w := postSlackCommand(handler, url.Values{
			"channel_id": {"C_CMD_1"},
			"text":       {""},
		})

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp slackCommandResponse
		if decErr := json.NewDecoder(w.Body).Decode(&resp); decErr != nil {
			t.Fatalf("decode: %v", decErr)
		}
		if !strings.Contains(resp.Text, "respond_mode") {
			t.Fatalf("expected config info, got %q", resp.Text)
		}
		if !strings.Contains(resp.Text, "mention_only") {
			t.Fatalf("expected default respond_mode mention_only, got %q", resp.Text)
		}
		if !strings.Contains(resp.Text, "key=test-api-key") {
			t.Fatalf("expected API key in config URL, got %q", resp.Text)
		}
	})

	t.Run("config subcommand shows config", func(t *testing.T) {
		handler := handleSlackCommand(pool, "", "TestBot")
		w := postSlackCommand(handler, url.Values{
			"channel_id": {"C_CMD_2"},
			"text":       {"config"},
		})

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp slackCommandResponse
		if decErr := json.NewDecoder(w.Body).Decode(&resp); decErr != nil {
			t.Fatalf("decode: %v", decErr)
		}
		if !strings.Contains(resp.Text, "Channel config") {
			t.Fatalf("expected config header, got %q", resp.Text)
		}
		if !strings.Contains(resp.Text, "C_CMD_2") {
			t.Fatalf("expected channel ID in URL, got %q", resp.Text)
		}
	})

	t.Run("config with no apiKey omits key param", func(t *testing.T) {
		handler := handleSlackCommand(pool, "", "TestBot")
		w := postSlackCommand(handler, url.Values{
			"channel_id": {"C_CMD_NOKEY"},
			"text":       {"config"},
		})

		var resp slackCommandResponse
		if decErr := json.NewDecoder(w.Body).Decode(&resp); decErr != nil {
			t.Fatalf("decode: %v", decErr)
		}
		if strings.Contains(resp.Text, "key=") {
			t.Fatalf("expected no key param when apiKey is empty, got %q", resp.Text)
		}
	})

	t.Run("respond_mode mention_only updates config", func(t *testing.T) {
		handler := handleSlackCommand(pool, "", "TestBot")
		w := postSlackCommand(handler, url.Values{
			"channel_id": {"C_CMD_3"},
			"text":       {"respond_mode mention_only"},
		})

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp slackCommandResponse
		if decErr := json.NewDecoder(w.Body).Decode(&resp); decErr != nil {
			t.Fatalf("decode: %v", decErr)
		}
		if !strings.Contains(resp.Text, "mention_only") {
			t.Fatalf("expected confirmation, got %q", resp.Text)
		}

		cfg, getErr := channel.GetConfig(ctx, pool, "C_CMD_3")
		if getErr != nil {
			t.Fatalf("get config: %v", getErr)
		}
		if cfg == nil || cfg.RespondMode != "mention_only" {
			t.Fatalf("expected respond_mode=mention_only in DB, got %+v", cfg)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM channel_configs WHERE channel_id = $1", "C_CMD_3")
		})
	})

	t.Run("respond_mode all_messages updates config", func(t *testing.T) {
		handler := handleSlackCommand(pool, "", "TestBot")
		w := postSlackCommand(handler, url.Values{
			"channel_id": {"C_CMD_4"},
			"text":       {"respond_mode all_messages"},
		})

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp slackCommandResponse
		if decErr := json.NewDecoder(w.Body).Decode(&resp); decErr != nil {
			t.Fatalf("decode: %v", decErr)
		}
		if !strings.Contains(resp.Text, "all_messages") {
			t.Fatalf("expected confirmation, got %q", resp.Text)
		}

		cfg, getErr := channel.GetConfig(ctx, pool, "C_CMD_4")
		if getErr != nil {
			t.Fatalf("get config: %v", getErr)
		}
		if cfg == nil || cfg.RespondMode != "all_messages" {
			t.Fatalf("expected respond_mode=all_messages in DB, got %+v", cfg)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM channel_configs WHERE channel_id = $1", "C_CMD_4")
		})
	})

	t.Run("respond_mode invalid returns error", func(t *testing.T) {
		handler := handleSlackCommand(pool, "", "TestBot")
		w := postSlackCommand(handler, url.Values{
			"channel_id": {"C_CMD_5"},
			"text":       {"respond_mode invalid_mode"},
		})

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp slackCommandResponse
		if decErr := json.NewDecoder(w.Body).Decode(&resp); decErr != nil {
			t.Fatalf("decode: %v", decErr)
		}
		if !strings.Contains(resp.Text, "Invalid respond_mode") {
			t.Fatalf("expected invalid mode error, got %q", resp.Text)
		}
		if !strings.Contains(resp.Text, "invalid_mode") {
			t.Fatalf("expected the invalid value echoed back, got %q", resp.Text)
		}
	})

	t.Run("unknown subcommand returns usage", func(t *testing.T) {
		handler := handleSlackCommand(pool, "", "TestBot")
		w := postSlackCommand(handler, url.Values{
			"channel_id": {"C_CMD_6"},
			"text":       {"unknown_command"},
		})

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp slackCommandResponse
		if decErr := json.NewDecoder(w.Body).Decode(&resp); decErr != nil {
			t.Fatalf("decode: %v", decErr)
		}
		if !strings.Contains(resp.Text, "Usage") {
			t.Fatalf("expected usage text, got %q", resp.Text)
		}
	})

	t.Run("respond_mode updates existing config", func(t *testing.T) {
		// Pre-create a config
		preExisting := &channel.Config{
			ChannelID:    "C_CMD_7",
			SystemPrompt: "existing prompt",
			RespondMode:  "mention_only",
		}
		if upsertErr := channel.UpsertConfig(ctx, pool, preExisting); upsertErr != nil {
			t.Fatalf("upsert: %v", upsertErr)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM channel_configs WHERE channel_id = $1", "C_CMD_7")
		})

		handler := handleSlackCommand(pool, "", "TestBot")
		w := postSlackCommand(handler, url.Values{
			"channel_id": {"C_CMD_7"},
			"text":       {"respond_mode all_messages"},
		})

		var resp slackCommandResponse
		if decErr := json.NewDecoder(w.Body).Decode(&resp); decErr != nil {
			t.Fatalf("decode: %v", decErr)
		}
		if !strings.Contains(resp.Text, "all_messages") {
			t.Fatalf("expected confirmation, got %q", resp.Text)
		}

		cfg, getErr := channel.GetConfig(ctx, pool, "C_CMD_7")
		if getErr != nil {
			t.Fatalf("get config: %v", getErr)
		}
		if cfg.RespondMode != "all_messages" {
			t.Fatalf("expected respond_mode=all_messages, got %q", cfg.RespondMode)
		}
		if cfg.SystemPrompt != "existing prompt" {
			t.Fatalf("expected system_prompt preserved, got %q", cfg.SystemPrompt)
		}
	})
}
