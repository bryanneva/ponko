package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bryanneva/ponko/internal/channel"
	"github.com/bryanneva/ponko/internal/llm"
	"github.com/bryanneva/ponko/internal/slack"
	"github.com/bryanneva/ponko/internal/testutil"
	"github.com/bryanneva/ponko/internal/user"
)

func TestTimestampFormat(t *testing.T) {
	loc, _ := time.LoadLocation("America/New_York")
	now := time.Now().In(loc)

	stamp := "It is currently " + now.Format(timestampFormat) + "."

	dayOfWeek := now.Format("Monday")
	if !strings.Contains(stamp, dayOfWeek) {
		t.Errorf("timestamp missing day of week %q: %s", dayOfWeek, stamp)
	}

	tz := now.Format("MST")
	if !strings.Contains(stamp, tz) {
		t.Errorf("timestamp missing timezone abbreviation %q: %s", tz, stamp)
	}

	if !strings.HasPrefix(stamp, "It is currently ") {
		t.Errorf("timestamp missing prefix: %s", stamp)
	}

	if !strings.HasSuffix(stamp, ".") {
		t.Errorf("timestamp missing trailing period: %s", stamp)
	}
}

func TestSystemPromptAdditions(t *testing.T) {
	if !strings.Contains(knowledgeBasePromptAddition, "knowledge base") {
		t.Error("knowledgeBasePromptAddition missing expected content")
	}
	if !strings.Contains(knowledgeBasePromptAddition, "no results") {
		t.Error("knowledgeBasePromptAddition should instruct honest handling of missing data")
	}
	if !strings.Contains(webSearchPromptAddition, "web search") {
		t.Error("webSearchPromptAddition missing expected content")
	}
	if !strings.Contains(webSearchPromptAddition, "cite sources") {
		t.Error("webSearchPromptAddition should instruct citing sources")
	}
}

func newMockSlackUserServer(tz string, displayName string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"ok": true,
			"user": map[string]any{
				"tz":       tz,
				"is_admin": false,
				"profile": map[string]any{
					"display_name": displayName,
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestResolveUserFromUserStore(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	testutil.EnsureUsersTable(t, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE slack_user_id IN ('U12345')")
	})

	server := newMockSlackUserServer("America/Chicago", "TestUser")
	defer server.Close()

	fallback, _ := time.LoadLocation("America/Los_Angeles")
	slackClient := slack.NewClient("test-token", server.URL)
	worker := &ProcessWorker{
		Slack:     slackClient,
		UserStore: &user.Store{Pool: pool, Slack: slackClient},
		Timezone:  fallback,
	}

	displayName, loc := worker.resolveUser(context.Background(), "U12345")
	if loc == nil || loc.String() != "America/Chicago" {
		t.Errorf("expected America/Chicago, got %v", loc)
	}
	if displayName != "TestUser" {
		t.Errorf("expected TestUser, got %s", displayName)
	}
}

func TestResolveUserStoreFailureFallback(t *testing.T) {
	pool := testutil.TestDB(t)
	testutil.EnsureUsersTable(t, pool)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"error":"user_not_found"}`))
	}))
	defer server.Close()

	fallback, _ := time.LoadLocation("America/Los_Angeles")
	slackClient := slack.NewClient("test-token", server.URL)
	worker := &ProcessWorker{
		Slack:     slackClient,
		UserStore: &user.Store{Pool: pool, Slack: slackClient},
		Timezone:  fallback,
	}

	displayName, loc := worker.resolveUser(context.Background(), "U12345")
	if loc.String() != "America/Los_Angeles" {
		t.Errorf("expected fallback America/Los_Angeles, got %s", loc)
	}
	if displayName != "" {
		t.Errorf("expected empty display name on failure, got %s", displayName)
	}
}

func TestResolveUserNoUserStore(t *testing.T) {
	fallback, _ := time.LoadLocation("America/New_York")
	worker := &ProcessWorker{Timezone: fallback}

	displayName, loc := worker.resolveUser(context.Background(), "U12345")
	if loc.String() != "America/New_York" {
		t.Errorf("expected fallback America/New_York, got %s", loc)
	}
	if displayName != "" {
		t.Errorf("expected empty display name, got %s", displayName)
	}
}

func TestResolveUserEmptyUserID(t *testing.T) {
	fallback, _ := time.LoadLocation("America/New_York")
	worker := &ProcessWorker{
		UserStore: &user.Store{},
		Timezone:  fallback,
	}

	displayName, loc := worker.resolveUser(context.Background(), "")
	if loc.String() != "America/New_York" {
		t.Errorf("expected fallback America/New_York, got %s", loc)
	}
	if displayName != "" {
		t.Errorf("expected empty display name, got %s", displayName)
	}
}

func TestTimestampNilTimezone(t *testing.T) {
	now := time.Now()
	stamp := "It is currently " + now.Format(timestampFormat) + "."

	if !strings.Contains(stamp, "It is currently ") {
		t.Errorf("nil timezone timestamp unexpected format: %s", stamp)
	}
}

func makeTool(name string) llm.Tool {
	return llm.Tool{Name: name, Description: "test", InputSchema: json.RawMessage(`{}`)}
}

func TestFilterTools_SpecificNames(t *testing.T) {
	tools := []llm.Tool{makeTool("search_thoughts"), makeTool("capture_thought"), makeTool("list_thoughts")}
	filtered := filterTools(tools, []string{"search_thoughts", "capture_thought"})
	if len(filtered) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(filtered))
	}
	if filtered[0].Name != "search_thoughts" || filtered[1].Name != "capture_thought" {
		t.Errorf("unexpected tools: %v, %v", filtered[0].Name, filtered[1].Name)
	}
}

func TestFilterTools_EmptyAllowlist(t *testing.T) {
	tools := []llm.Tool{makeTool("search_thoughts"), makeTool("capture_thought")}
	filtered := filterTools(tools, []string{})
	if len(filtered) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(filtered))
	}
}

func TestFilterTools_NoMatch(t *testing.T) {
	tools := []llm.Tool{makeTool("search_thoughts")}
	filtered := filterTools(tools, []string{"nonexistent_tool"})
	if len(filtered) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(filtered))
	}
}

func TestToolsInclude_Match(t *testing.T) {
	if !toolsInclude([]string{"search_thoughts", "capture_thought"}, "search_thoughts") {
		t.Error("expected toolsInclude to return true")
	}
}

func TestToolsInclude_NoMatch(t *testing.T) {
	if toolsInclude([]string{"search_thoughts"}, "nonexistent") {
		t.Error("expected toolsInclude to return false")
	}
}

func TestToolsInclude_EmptyAllowlist(t *testing.T) {
	if toolsInclude([]string{}, "search_thoughts") {
		t.Error("expected toolsInclude to return false for empty allowlist")
	}
}

type fakeMCPClient struct{}

func (fakeMCPClient) CallTool(_ context.Context, _ string, _ map[string]any, _ *llm.UserScope) (string, error) {
	return "", nil
}

func TestBuildSystemPrompt_NilConfig(t *testing.T) {
	now := time.Date(2026, 3, 23, 9, 0, 0, 0, time.UTC)
	tools := []llm.Tool{makeTool("search_thoughts")}
	mcp := fakeMCPClient{}

	prompt, retTools, retMCP := buildSystemPrompt(promptConfig{
		MCPClient: mcp,
		Tools:     tools,
		BotName:   "TestBot",
		Now:       now,
	})

	if !strings.Contains(prompt, "You are TestBot") {
		t.Error("expected default system prompt with bot name when config is nil")
	}
	if !strings.Contains(prompt, "knowledge base") {
		t.Error("expected knowledge base addition when config is nil and MCP is set")
	}
	if !strings.Contains(prompt, "web search") {
		t.Error("expected web search addition when config is nil and MCP is set")
	}
	if !strings.Contains(prompt, "Monday, March 23, 2026") {
		t.Errorf("expected timestamp in prompt, got: %s", prompt)
	}
	if len(retTools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(retTools))
	}
	if retMCP == nil {
		t.Error("expected MCP client to be returned")
	}
}

func TestBuildSystemPrompt_ChannelConfig(t *testing.T) {
	now := time.Date(2026, 3, 23, 9, 0, 0, 0, time.UTC)
	cfg := &channel.Config{
		SystemPrompt: "You are a project assistant.",
	}

	prompt, _, _ := buildSystemPrompt(promptConfig{
		ChannelCfg: cfg,
		Now:        now,
	})

	if !strings.Contains(prompt, "project assistant") {
		t.Error("expected channel system prompt")
	}
	if strings.Contains(prompt, "You are a helpful assistant") {
		t.Error("should not contain default prompt when channel config is set")
	}
}

func TestBuildSystemPrompt_AllowlistFilters(t *testing.T) {
	now := time.Date(2026, 3, 23, 9, 0, 0, 0, time.UTC)
	tools := []llm.Tool{makeTool("search_thoughts"), makeTool("web_search"), makeTool("capture_thought")}
	mcp := fakeMCPClient{}
	cfg := &channel.Config{
		SystemPrompt:  "test",
		ToolAllowlist: []string{"search_thoughts"},
	}

	prompt, retTools, _ := buildSystemPrompt(promptConfig{
		MCPClient:  mcp,
		ChannelCfg: cfg,
		Tools:      tools,
		Now:        now,
	})

	if !strings.Contains(prompt, "knowledge base") {
		t.Error("expected knowledge base addition when search_thoughts is in allowlist")
	}
	if strings.Contains(prompt, "web search") {
		t.Error("should not include web search when web_search not in allowlist")
	}
	if len(retTools) != 1 || retTools[0].Name != "search_thoughts" {
		t.Errorf("expected only search_thoughts tool, got %v", retTools)
	}
}

func TestBuildSystemPrompt_EmptyAllowlistDisablesMCP(t *testing.T) {
	now := time.Date(2026, 3, 23, 9, 0, 0, 0, time.UTC)
	tools := []llm.Tool{makeTool("search_thoughts")}
	mcp := fakeMCPClient{}
	cfg := &channel.Config{
		SystemPrompt:  "test",
		ToolAllowlist: []string{},
	}

	_, retTools, retMCP := buildSystemPrompt(promptConfig{
		MCPClient:  mcp,
		ChannelCfg: cfg,
		Tools:      tools,
		Now:        now,
	})

	if len(retTools) != 0 {
		t.Errorf("expected 0 tools with empty allowlist, got %d", len(retTools))
	}
	if retMCP != nil {
		t.Error("expected MCP client to be nil when no tools pass allowlist")
	}
}

func TestBuildSystemPrompt_DisplayName(t *testing.T) {
	now := time.Date(2026, 3, 23, 9, 0, 0, 0, time.UTC)

	prompt, _, _ := buildSystemPrompt(promptConfig{
		Now:         now,
		DisplayName: "TestUser",
	})

	if !strings.Contains(prompt, "You are talking to TestUser.") {
		t.Errorf("expected identity line in prompt, got: %s", prompt)
	}
}

func TestBuildSystemPrompt_NoDisplayName(t *testing.T) {
	now := time.Date(2026, 3, 23, 9, 0, 0, 0, time.UTC)

	prompt, _, _ := buildSystemPrompt(promptConfig{
		Now: now,
	})

	if strings.Contains(prompt, "You are talking to") {
		t.Errorf("should not contain identity line when no display name: %s", prompt)
	}
}

func TestIdentityInjectedIntoPrompt(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	testutil.EnsureUsersTable(t, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE slack_user_id = 'UIDENTITY'")
	})

	server := newMockSlackUserServer("America/New_York", "TestUser")
	defer server.Close()

	slackClient := slack.NewClient("test-token", server.URL)
	worker := &ProcessWorker{
		UserStore: &user.Store{Pool: pool, Slack: slackClient},
		Slack:     slackClient,
	}

	displayName, _ := worker.resolveUser(ctx, "UIDENTITY")
	if displayName != "TestUser" {
		t.Fatalf("expected TestUser, got %s", displayName)
	}

	prompt := defaultSystemPromptFor("TestBot")
	if displayName != "" {
		prompt += fmt.Sprintf("\nYou are talking to %s.", displayName)
	}
	if !strings.Contains(prompt, "You are talking to TestUser.") {
		t.Errorf("prompt missing identity line: %s", prompt)
	}
}

func TestIdentityNotInjectedWhenEmpty(t *testing.T) {
	fallback, _ := time.LoadLocation("America/New_York")
	worker := &ProcessWorker{Timezone: fallback}

	displayName, _ := worker.resolveUser(context.Background(), "U12345")

	prompt := defaultSystemPromptFor("TestBot")
	if displayName != "" {
		prompt += fmt.Sprintf("\nYou are talking to %s.", displayName)
	}
	if strings.Contains(prompt, "You are talking to") {
		t.Errorf("prompt should not contain identity line when displayName is empty: %s", prompt)
	}
}

func TestSystemPromptContainsNoPlanInstruction(t *testing.T) {
	p := defaultSystemPromptFor("TestBot")
	if !strings.Contains(p, "Never generate plan summaries") {
		t.Error("defaultSystemPromptFor missing anti-hallucination instruction about plan summaries")
	}
	if !strings.Contains(p, "progress tracking URLs") {
		t.Error("defaultSystemPromptFor missing anti-hallucination instruction about progress URLs")
	}
}

func newMockSlackPostServer(t *testing.T, posted *bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat.postMessage" {
			*posted = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
}

func TestProgressMessagePostedForToolUse(t *testing.T) {
	var posted bool
	server := newMockSlackPostServer(t, &posted)
	defer server.Close()

	slackClient := slack.NewClient("test-token", server.URL)
	worker := &ProcessWorker{
		Slack:   slackClient,
		BaseURL: "https://example.com",
	}

	worker.postProgressIfNeeded(context.Background(), "wf-123", "C123", "1234567890.123456", "channel", true)

	if !posted {
		t.Error("expected progress message to be posted for tool-use request")
	}
}

func TestNoProgressMessageWithoutTools(t *testing.T) {
	var posted bool
	server := newMockSlackPostServer(t, &posted)
	defer server.Close()

	slackClient := slack.NewClient("test-token", server.URL)
	worker := &ProcessWorker{
		Slack:   slackClient,
		BaseURL: "https://example.com",
	}

	worker.postProgressIfNeeded(context.Background(), "wf-123", "C123", "1234567890.123456", "channel", false)

	if posted {
		t.Error("expected no progress message for non-tool request")
	}
}

func TestNoProgressMessageWithoutBaseURL(t *testing.T) {
	var posted bool
	server := newMockSlackPostServer(t, &posted)
	defer server.Close()

	slackClient := slack.NewClient("test-token", server.URL)
	worker := &ProcessWorker{
		Slack:   slackClient,
		BaseURL: "",
	}

	worker.postProgressIfNeeded(context.Background(), "wf-123", "C123", "1234567890.123456", "channel", true)

	if posted {
		t.Error("expected no progress message when BaseURL is empty")
	}
}

func TestProgressMessageFormat(t *testing.T) {
	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat.postMessage" {
			body, _ := io.ReadAll(r.Body)
			capturedBody = string(body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	slackClient := slack.NewClient("test-token", server.URL)
	worker := &ProcessWorker{
		Slack:   slackClient,
		BaseURL: "https://app.example.com",
	}

	worker.postProgressIfNeeded(context.Background(), "wf-abc-123", "C456", "1234567890.123456", "channel", true)

	if !strings.Contains(capturedBody, "https://app.example.com/workflows/wf-abc-123") {
		t.Errorf("progress message missing workflow URL, got: %s", capturedBody)
	}
}

func TestProgressMessageDMNoThread(t *testing.T) {
	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat.postMessage" {
			body, _ := io.ReadAll(r.Body)
			capturedBody = string(body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	slackClient := slack.NewClient("test-token", server.URL)
	worker := &ProcessWorker{
		Slack:   slackClient,
		BaseURL: "https://app.example.com",
	}

	worker.postProgressIfNeeded(context.Background(), "wf-dm-123", "D123", "1234567890.123456", "im", true)

	if strings.Contains(capturedBody, `"thread_ts":"1234567890.123456"`) {
		t.Error("DM progress message should not use thread_ts")
	}
}
