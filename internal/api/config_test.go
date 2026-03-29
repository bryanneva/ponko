package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bryanneva/ponko/internal/channel"
	"github.com/bryanneva/ponko/internal/slack"
	"github.com/bryanneva/ponko/internal/testutil"
)

func TestHandleListChannels(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, "DELETE FROM channel_configs WHERE channel_id LIKE 'C_LIST_%'")
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	slackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"channel":{"name":"test-channel"}}`))
	}))
	defer slackServer.Close()
	slackClient := slack.NewClient("test-token", slackServer.URL)

	t.Run("empty list", func(t *testing.T) {
		handler := handleListChannels(pool, slackClient)
		req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var body []channelListItem
		if decErr := json.NewDecoder(w.Body).Decode(&body); decErr != nil {
			t.Fatalf("decode: %v", decErr)
		}
		if len(body) != 0 {
			t.Fatalf("expected 0 channels, got %d", len(body))
		}
	})

	t.Run("returns channels with slack names", func(t *testing.T) {
		cfg := &channel.Config{
			ChannelID:    "C_LIST_1",
			SystemPrompt: "test prompt",
			RespondMode:  "mention_only",
		}
		if upsertErr := channel.UpsertConfig(ctx, pool, cfg); upsertErr != nil {
			t.Fatalf("upsert: %v", upsertErr)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM channel_configs WHERE channel_id = $1", "C_LIST_1")
		})

		handler := handleListChannels(pool, slackClient)
		req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var body []channelListItem
		if decErr := json.NewDecoder(w.Body).Decode(&body); decErr != nil {
			t.Fatalf("decode: %v", decErr)
		}
		if len(body) != 1 {
			t.Fatalf("expected 1 channel, got %d", len(body))
		}
		if body[0].ChannelID != "C_LIST_1" {
			t.Fatalf("expected channel ID C_LIST_1, got %s", body[0].ChannelID)
		}
		if body[0].ChannelName != "test-channel" {
			t.Fatalf("expected channel name test-channel, got %s", body[0].ChannelName)
		}
		if body[0].SystemPrompt != "test prompt" {
			t.Fatalf("expected system prompt 'test prompt', got %s", body[0].SystemPrompt)
		}
	})

	t.Run("slack name fetch failure falls back gracefully", func(t *testing.T) {
		cfg := &channel.Config{
			ChannelID:    "C_LIST_2",
			SystemPrompt: "test",
			RespondMode:  "mention_only",
		}
		if upsertErr := channel.UpsertConfig(ctx, pool, cfg); upsertErr != nil {
			t.Fatalf("upsert: %v", upsertErr)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM channel_configs WHERE channel_id = $1", "C_LIST_2")
		})

		failSlack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"ok":false,"error":"channel_not_found"}`))
		}))
		defer failSlack.Close()
		failClient := slack.NewClient("test-token", failSlack.URL)

		handler := handleListChannels(pool, failClient)
		req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var body []channelListItem
		if decErr := json.NewDecoder(w.Body).Decode(&body); decErr != nil {
			t.Fatalf("decode: %v", decErr)
		}
		if len(body) != 1 {
			t.Fatalf("expected 1 channel, got %d", len(body))
		}
		if body[0].ChannelName != "" {
			t.Fatalf("expected empty channel name on slack failure, got %s", body[0].ChannelName)
		}
	})
}

func TestHandleGetChannelConfig(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	slackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"channel":{"name":"my-channel"}}`))
	}))
	defer slackServer.Close()
	slackClient := slack.NewClient("test-token", slackServer.URL)

	t.Run("channel found returns config", func(t *testing.T) {
		cfg := &channel.Config{
			ChannelID:     "C_GET_1",
			SystemPrompt:  "you are helpful",
			RespondMode:   "all_messages",
			ToolAllowlist: []string{"tool_a"},
		}
		if upsertErr := channel.UpsertConfig(ctx, pool, cfg); upsertErr != nil {
			t.Fatalf("upsert: %v", upsertErr)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM channel_configs WHERE channel_id = $1", "C_GET_1")
		})

		handler := handleGetChannelConfig(pool, slackClient)
		req := httptest.NewRequest(http.MethodGet, "/api/channels/C_GET_1/config", nil)
		req.SetPathValue("id", "C_GET_1")
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var resp channelConfigResponse
		if decErr := json.NewDecoder(w.Body).Decode(&resp); decErr != nil {
			t.Fatalf("decode: %v", decErr)
		}
		if resp.ChannelID != "C_GET_1" {
			t.Fatalf("expected channel ID C_GET_1, got %s", resp.ChannelID)
		}
		if resp.ChannelName != "my-channel" {
			t.Fatalf("expected channel name my-channel, got %s", resp.ChannelName)
		}
		if resp.SystemPrompt != "you are helpful" {
			t.Fatalf("expected system prompt 'you are helpful', got %s", resp.SystemPrompt)
		}
		if resp.RespondMode != "all_messages" {
			t.Fatalf("expected respond mode all_messages, got %s", resp.RespondMode)
		}
		if len(resp.ToolAllowlist) != 1 || resp.ToolAllowlist[0] != "tool_a" {
			t.Fatalf("expected tool_allowlist [tool_a], got %v", resp.ToolAllowlist)
		}
	})

	t.Run("channel not found returns empty config", func(t *testing.T) {
		handler := handleGetChannelConfig(pool, slackClient)
		req := httptest.NewRequest(http.MethodGet, "/api/channels/C_NONEXISTENT/config", nil)
		req.SetPathValue("id", "C_NONEXISTENT")
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var resp channelConfigResponse
		if decErr := json.NewDecoder(w.Body).Decode(&resp); decErr != nil {
			t.Fatalf("decode: %v", decErr)
		}
		if resp.ChannelID != "C_NONEXISTENT" {
			t.Fatalf("expected channel ID C_NONEXISTENT, got %s", resp.ChannelID)
		}
		if resp.SystemPrompt != "" {
			t.Fatalf("expected empty system prompt, got %s", resp.SystemPrompt)
		}
	})

	t.Run("slack name fetch failure handled", func(t *testing.T) {
		failSlack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"ok":false,"error":"channel_not_found"}`))
		}))
		defer failSlack.Close()
		failClient := slack.NewClient("test-token", failSlack.URL)

		handler := handleGetChannelConfig(pool, failClient)
		req := httptest.NewRequest(http.MethodGet, "/api/channels/C_NOSLACK/config", nil)
		req.SetPathValue("id", "C_NOSLACK")
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var resp channelConfigResponse
		if decErr := json.NewDecoder(w.Body).Decode(&resp); decErr != nil {
			t.Fatalf("decode: %v", decErr)
		}
		if resp.ChannelName != "" {
			t.Fatalf("expected empty channel name on slack failure, got %s", resp.ChannelName)
		}
	})

	t.Run("missing channel ID returns 400", func(t *testing.T) {
		handler := handleGetChannelConfig(pool, slackClient)
		req := httptest.NewRequest(http.MethodGet, "/api/channels//config", nil)
		req.SetPathValue("id", "")
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})
}

func TestHandlePutChannelConfig(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	t.Run("valid config upserted", func(t *testing.T) {
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM channel_configs WHERE channel_id = $1", "C_PUT_1")
		})

		handler := handlePutChannelConfig(pool)
		body := `{"system_prompt":"be nice","respond_mode":"mention_only","tool_allowlist":["tool_x"]}`
		req := httptest.NewRequest(http.MethodPut, "/api/channels/C_PUT_1/config", strings.NewReader(body))
		req.SetPathValue("id", "C_PUT_1")
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp channelConfigResponse
		if decErr := json.NewDecoder(w.Body).Decode(&resp); decErr != nil {
			t.Fatalf("decode: %v", decErr)
		}
		if resp.ChannelID != "C_PUT_1" {
			t.Fatalf("expected channel ID C_PUT_1, got %s", resp.ChannelID)
		}
		if resp.SystemPrompt != "be nice" {
			t.Fatalf("expected system prompt 'be nice', got %s", resp.SystemPrompt)
		}
		if resp.RespondMode != "mention_only" {
			t.Fatalf("expected respond mode mention_only, got %s", resp.RespondMode)
		}

		// Verify persisted in DB
		cfg, getErr := channel.GetConfig(ctx, pool, "C_PUT_1")
		if getErr != nil {
			t.Fatalf("get config: %v", getErr)
		}
		if cfg == nil {
			t.Fatal("expected config to be persisted in DB")
		}
		if cfg.SystemPrompt != "be nice" {
			t.Fatalf("expected persisted prompt 'be nice', got %s", cfg.SystemPrompt)
		}
	})

	t.Run("empty system_prompt triggers delete", func(t *testing.T) {
		// First insert a config
		cfg := &channel.Config{
			ChannelID:    "C_PUT_DEL",
			SystemPrompt: "will be deleted",
			RespondMode:  "mention_only",
		}
		if upsertErr := channel.UpsertConfig(ctx, pool, cfg); upsertErr != nil {
			t.Fatalf("upsert: %v", upsertErr)
		}

		handler := handlePutChannelConfig(pool)
		body := `{"system_prompt":"","respond_mode":""}`
		req := httptest.NewRequest(http.MethodPut, "/api/channels/C_PUT_DEL/config", strings.NewReader(body))
		req.SetPathValue("id", "C_PUT_DEL")
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp channelConfigResponse
		if decErr := json.NewDecoder(w.Body).Decode(&resp); decErr != nil {
			t.Fatalf("decode: %v", decErr)
		}
		if resp.ChannelID != "C_PUT_DEL" {
			t.Fatalf("expected channel ID C_PUT_DEL, got %s", resp.ChannelID)
		}
		if resp.SystemPrompt != "" {
			t.Fatalf("expected empty system prompt in response, got %s", resp.SystemPrompt)
		}

		// Verify deleted from DB
		got, getErr := channel.GetConfig(ctx, pool, "C_PUT_DEL")
		if getErr != nil {
			t.Fatalf("get config: %v", getErr)
		}
		if got != nil {
			t.Fatal("expected config to be deleted from DB")
		}
	})

	t.Run("invalid respond_mode returns 400", func(t *testing.T) {
		handler := handlePutChannelConfig(pool)
		body := `{"system_prompt":"test","respond_mode":"invalid_mode"}`
		req := httptest.NewRequest(http.MethodPut, "/api/channels/C_PUT_BAD/config", strings.NewReader(body))
		req.SetPathValue("id", "C_PUT_BAD")
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}

		var errBody map[string]string
		if decErr := json.NewDecoder(w.Body).Decode(&errBody); decErr != nil {
			t.Fatalf("decode: %v", decErr)
		}
		if !strings.Contains(errBody["error"], "invalid respond_mode") {
			t.Fatalf("expected respond_mode error, got %s", errBody["error"])
		}
	})

	t.Run("missing channelID returns 400", func(t *testing.T) {
		handler := handlePutChannelConfig(pool)
		body := `{"system_prompt":"test"}`
		req := httptest.NewRequest(http.MethodPut, "/api/channels//config", strings.NewReader(body))
		req.SetPathValue("id", "")
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("invalid request body returns 400", func(t *testing.T) {
		handler := handlePutChannelConfig(pool)
		req := httptest.NewRequest(http.MethodPut, "/api/channels/C_PUT_ERR/config", strings.NewReader("not json"))
		req.SetPathValue("id", "C_PUT_ERR")
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("empty respond_mode defaults correctly", func(t *testing.T) {
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM channel_configs WHERE channel_id = $1", "C_PUT_DEF")
		})

		handler := handlePutChannelConfig(pool)
		body := `{"system_prompt":"test prompt","respond_mode":""}`
		req := httptest.NewRequest(http.MethodPut, "/api/channels/C_PUT_DEF/config", strings.NewReader(body))
		req.SetPathValue("id", "C_PUT_DEF")
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		cfg, getErr := channel.GetConfig(ctx, pool, "C_PUT_DEF")
		if getErr != nil {
			t.Fatalf("get config: %v", getErr)
		}
		if cfg == nil {
			t.Fatal("expected config to be persisted")
		}
		if cfg.RespondMode != "" {
			t.Fatalf("expected empty respond_mode stored in DB, got %q", cfg.RespondMode)
		}
	})
}
