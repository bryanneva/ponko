package jobs

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bryanneva/ponko/internal/llm"
	"github.com/bryanneva/ponko/internal/slack"
)

type stubMCPClient struct {
	err       error
	arguments map[string]any
	name      string
	result    string
}

func (s *stubMCPClient) CallTool(_ context.Context, name string, arguments map[string]any, _ *llm.UserScope) (string, error) {
	s.name = name
	s.arguments = arguments
	return s.result, s.err
}

func TestCallTool_UnknownToolWithMCPClient(t *testing.T) {
	mcp := &stubMCPClient{result: "mcp result"}
	d := &ToolDispatcher{MCPClient: mcp}

	result, err := d.CallTool(context.Background(), "some_unknown_tool", map[string]any{"key": "val"}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "mcp result" {
		t.Errorf("expected %q, got %q", "mcp result", result)
	}
	if mcp.name != "some_unknown_tool" {
		t.Errorf("expected MCP called with %q, got %q", "some_unknown_tool", mcp.name)
	}
}

func TestCallTool_UnknownToolWithoutMCPClient(t *testing.T) {
	d := &ToolDispatcher{}

	_, err := d.CallTool(context.Background(), "some_unknown_tool", nil, nil)

	if err == nil {
		t.Fatal("expected error for unknown tool without MCP client")
	}
	if err.Error() != "unknown tool: some_unknown_tool" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCallTool_MCPClientError(t *testing.T) {
	mcp := &stubMCPClient{err: fmt.Errorf("connection refused")}
	d := &ToolDispatcher{MCPClient: mcp}

	_, err := d.CallTool(context.Background(), "some_tool", nil, nil)

	if err == nil {
		t.Fatal("expected error from MCP client")
	}
	if err.Error() != "connection refused" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCallTool_UserScopePassedToMCP(t *testing.T) {
	mcp := &stubMCPClient{result: "ok"}
	d := &ToolDispatcher{MCPClient: mcp}
	scope := &llm.UserScope{SlackUserID: "U123", ChannelID: "C456"}

	_, err := d.CallTool(context.Background(), "custom_tool", map[string]any{"q": "test"}, scope)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mcp.name != "custom_tool" {
		t.Errorf("expected MCP called with %q, got %q", "custom_tool", mcp.name)
	}
}

func TestCallTool_MCPReceivesArguments(t *testing.T) {
	mcp := &stubMCPClient{result: "ok"}
	d := &ToolDispatcher{MCPClient: mcp}
	args := map[string]any{"query": "test", "limit": float64(10)}

	_, err := d.CallTool(context.Background(), "search_tool", args, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mcp.arguments["query"] != "test" {
		t.Errorf("expected argument query=%q, got %v", "test", mcp.arguments["query"])
	}
	if mcp.arguments["limit"] != float64(10) {
		t.Errorf("expected argument limit=%v, got %v", float64(10), mcp.arguments["limit"])
	}
}

func TestCallTool_ReadSlackThread(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/conversations.replies", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ok": true,
			"messages": [
				{"user": "U111", "text": "parent message", "ts": "1774549751.570729"},
				{"user": "U222", "text": "a reply", "ts": "1774549760.000000"}
			]
		}`))
	})
	mux.HandleFunc("/users.info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("user") {
		case "U111":
			_, _ = w.Write([]byte(`{"ok": true, "user": {"profile": {"display_name": "Alice"}, "tz": "", "is_admin": false}}`))
		case "U222":
			_, _ = w.Write([]byte(`{"ok": true, "user": {"profile": {"display_name": "Bob"}, "tz": "", "is_admin": false}}`))
		default:
			_, _ = w.Write([]byte(`{"ok": false, "error": "user_not_found"}`))
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	slackClient := slack.NewClient("test-token", server.URL)
	d := &ToolDispatcher{Slack: slackClient}

	result, err := d.CallTool(context.Background(), "read_slack_thread", map[string]any{
		"url": "https://example.slack.com/archives/C00TESTCHAN/p1774549751570729",
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "parent message") {
		t.Errorf("expected result to contain 'parent message', got: %s", result)
	}
	if !strings.Contains(result, "a reply") {
		t.Errorf("expected result to contain 'a reply', got: %s", result)
	}
}

func TestCallTool_ReadSlackThreadNoClient(t *testing.T) {
	d := &ToolDispatcher{}

	_, err := d.CallTool(context.Background(), "read_slack_thread", map[string]any{
		"url": "https://example.slack.com/archives/C00TESTCHAN/p1774549751570729",
	}, nil)

	if err == nil {
		t.Fatal("expected error when Slack client is nil")
	}
}

func TestRemarshal(t *testing.T) {
	tests := []struct {
		src     map[string]any
		name    string
		wantErr bool
	}{
		{
			name: "valid input",
			src:  map[string]any{"prompt": "hello", "cron_expression": "0 9 * * *"},
		},
		{
			name: "empty input",
			src:  map[string]any{},
		},
		{
			name: "nil map",
			src:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dst struct {
				Prompt         string `json:"prompt"`
				CronExpression string `json:"cron_expression"`
			}
			err := remarshal(tt.src, &dst)
			if (err != nil) != tt.wantErr {
				t.Errorf("remarshal() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
