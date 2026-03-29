package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bryanneva/ponko/internal/llm"
)

func newMCPToolsListServer(t *testing.T, tools []mcpTool) *httptest.Server {
	t.Helper()
	return newMCPTestServer(t, "test-session", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(toolsListResponse{
			ID:     1,
			Result: &toolsListResult{Tools: tools},
		})
	}))
}

func newMCPToolsCallServer(t *testing.T, toolsJSON string) *httptest.Server {
	t.Helper()
	return newMCPTestServer(t, "test-session", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		if decodeErr := json.NewDecoder(r.Body).Decode(&req); decodeErr != nil {
			t.Fatalf("decoding request: %v", decodeErr)
		}

		w.Header().Set("Content-Type", "application/json")
		if req.Method == "tools/list" {
			_, _ = w.Write([]byte(toolsJSON))
			return
		}
		_ = json.NewEncoder(w).Encode(toolCallResponse{
			ID: 1,
			Result: &toolCallResult{
				Content: []contentItem{{Type: "text", Text: "ok"}},
			},
		})
	}))
}

func TestMultiClientDiscoversBothServers(t *testing.T) {
	server1 := newMCPToolsCallServer(t, `{
		"jsonrpc":"2.0","id":1,
		"result":{"tools":[{"name":"tool_a","description":"Tool A","inputSchema":{"type":"object"}}]}
	}`)
	defer server1.Close()

	server2 := newMCPToolsCallServer(t, `{
		"jsonrpc":"2.0","id":1,
		"result":{"tools":[{"name":"tool_b","description":"Tool B","inputSchema":{"type":"object"}}]}
	}`)
	defer server2.Close()

	clients := []Server{
		&Client{HTTP: &http.Client{}, BaseURL: server1.URL, BearerToken: "key"},
		&Client{HTTP: &http.Client{}, BaseURL: server2.URL, BearerToken: "key"},
	}

	mc, err := NewMultiClient(context.Background(), clients)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tools := mc.Tools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if tools[0].Name != "tool_a" {
		t.Errorf("expected 'tool_a', got %q", tools[0].Name)
	}
	if tools[1].Name != "tool_b" {
		t.Errorf("expected 'tool_b', got %q", tools[1].Name)
	}

	result, callErr := mc.CallTool(context.Background(), "tool_a", map[string]any{}, nil)
	if callErr != nil {
		t.Fatalf("unexpected error calling tool_a: %v", callErr)
	}
	if result != "ok" {
		t.Errorf("expected 'ok', got %q", result)
	}

	result, callErr = mc.CallTool(context.Background(), "tool_b", map[string]any{}, nil)
	if callErr != nil {
		t.Fatalf("unexpected error calling tool_b: %v", callErr)
	}
	if result != "ok" {
		t.Errorf("expected 'ok', got %q", result)
	}
}

func TestMultiClientOneServerDown(t *testing.T) {
	downServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer downServer.Close()

	upServer := newMCPToolsListServer(t, []mcpTool{
		{Name: "working_tool", Description: "Works", InputSchema: json.RawMessage(`{"type":"object"}`)},
	})
	defer upServer.Close()

	clients := []Server{
		&Client{HTTP: &http.Client{}, BaseURL: downServer.URL, BearerToken: "key"},
		&Client{HTTP: &http.Client{}, BaseURL: upServer.URL, BearerToken: "key"},
	}

	mc, err := NewMultiClient(context.Background(), clients)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tools := mc.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "working_tool" {
		t.Errorf("expected 'working_tool', got %q", tools[0].Name)
	}
}

func TestMultiClientAllServersDown(t *testing.T) {
	down1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer down1.Close()

	down2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer down2.Close()

	clients := []Server{
		&Client{HTTP: &http.Client{}, BaseURL: down1.URL, BearerToken: "key"},
		&Client{HTTP: &http.Client{}, BaseURL: down2.URL, BearerToken: "key"},
	}

	_, err := NewMultiClient(context.Background(), clients)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestMultiClientDeduplicatesToolNames(t *testing.T) {
	server1 := newMCPToolsCallServer(t, `{
		"jsonrpc":"2.0","id":1,
		"result":{"tools":[
			{"name":"search_users","description":"Search users (server 1)","inputSchema":{"type":"object"}},
			{"name":"unique_tool","description":"Only on server 1","inputSchema":{"type":"object"}}
		]}
	}`)
	defer server1.Close()

	server2 := newMCPToolsCallServer(t, `{
		"jsonrpc":"2.0","id":1,
		"result":{"tools":[
			{"name":"search_users","description":"Search users (server 2)","inputSchema":{"type":"object"}},
			{"name":"other_tool","description":"Only on server 2","inputSchema":{"type":"object"}}
		]}
	}`)
	defer server2.Close()

	clients := []Server{
		&Client{HTTP: &http.Client{}, BaseURL: server1.URL, BearerToken: "key"},
		&Client{HTTP: &http.Client{}, BaseURL: server2.URL, BearerToken: "key"},
	}

	mc, err := NewMultiClient(context.Background(), clients)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tools := mc.Tools()
	if len(tools) != 3 {
		t.Fatalf("expected 3 unique tools, got %d", len(tools))
	}

	// Verify exactly the expected tools are present
	seen := make(map[string]string)
	for _, tool := range tools {
		if _, dup := seen[tool.Name]; dup {
			t.Errorf("duplicate tool name: %s", tool.Name)
		}
		seen[tool.Name] = tool.Description
	}
	for _, name := range []string{"search_users", "unique_tool", "other_tool"} {
		if _, ok := seen[name]; !ok {
			t.Errorf("expected tool %q not found", name)
		}
	}

	// First-registered wins — server 1's search_users should be kept
	if desc := seen["search_users"]; desc != "Search users (server 1)" {
		t.Errorf("expected first-registered search_users (server 1), got description %q", desc)
	}
}

func TestMultiClientCallUnknownTool(t *testing.T) {
	server := newMCPToolsListServer(t, []mcpTool{
		{Name: "known_tool", Description: "Known", InputSchema: json.RawMessage(`{"type":"object"}`)},
	})
	defer server.Close()

	clients := []Server{
		&Client{HTTP: &http.Client{}, BaseURL: server.URL, BearerToken: "key"},
	}

	mc, err := NewMultiClient(context.Background(), clients)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, callErr := mc.CallTool(context.Background(), "unknown_tool", map[string]any{}, nil)
	if callErr == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestMultiClientCallsInitializeBeforeListTools(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		method, _ := req["method"].(string)
		methods = append(methods, method)

		w.Header().Set("Content-Type", "application/json")
		switch method {
		case "initialize":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"test"}}}`))
		case "notifications/initialized":
			w.WriteHeader(http.StatusOK)
		case "tools/list":
			_ = json.NewEncoder(w).Encode(toolsListResponse{
				ID:     1,
				Result: &toolsListResult{Tools: []mcpTool{{Name: "tool_a", Description: "A", InputSchema: json.RawMessage(`{"type":"object"}`)}}},
			})
		}
	}))
	defer server.Close()

	clients := []Server{
		&Client{HTTP: &http.Client{}, BaseURL: server.URL, BearerToken: "key"},
	}

	mc, err := NewMultiClient(context.Background(), clients)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mc.Tools()) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(mc.Tools()))
	}

	// Verify initialize was called before tools/list
	if len(methods) < 3 {
		t.Fatalf("expected at least 3 calls, got %d: %v", len(methods), methods)
	}
	if methods[0] != "initialize" {
		t.Errorf("expected first call to be 'initialize', got %q", methods[0])
	}
	if methods[1] != "notifications/initialized" {
		t.Errorf("expected second call to be 'notifications/initialized', got %q", methods[1])
	}
	if methods[2] != "tools/list" {
		t.Errorf("expected third call to be 'tools/list', got %q", methods[2])
	}
}

func TestMultiClientInitFailureStillTriesListTools(t *testing.T) {
	// Server that rejects initialize but accepts tools/list
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		method, _ := req["method"].(string)

		w.Header().Set("Content-Type", "application/json")
		if method == "initialize" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("unknown method"))
			return
		}
		if method == "tools/list" {
			_ = json.NewEncoder(w).Encode(toolsListResponse{
				ID:     1,
				Result: &toolsListResult{Tools: []mcpTool{{Name: "tool_a", Description: "A", InputSchema: json.RawMessage(`{"type":"object"}`)}}},
			})
		}
	}))
	defer server.Close()

	clients := []Server{
		&Client{HTTP: &http.Client{}, BaseURL: server.URL, BearerToken: "key"},
	}

	mc, err := NewMultiClient(context.Background(), clients)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mc.Tools()) != 1 {
		t.Fatalf("expected 1 tool despite init failure, got %d", len(mc.Tools()))
	}
}

func TestMultiClientCallToolWithUserScope(t *testing.T) {
	var capturedArgs map[string]any
	server := newMCPTestServer(t, "test-session", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		if decodeErr := json.NewDecoder(r.Body).Decode(&req); decodeErr != nil {
			t.Fatalf("decoding request: %v", decodeErr)
		}

		w.Header().Set("Content-Type", "application/json")
		if req.Method == "tools/list" {
			_ = json.NewEncoder(w).Encode(toolsListResponse{
				ID:     1,
				Result: &toolsListResult{Tools: []mcpTool{{Name: "capture_thought", Description: "Save", InputSchema: json.RawMessage(`{"type":"object"}`)}}},
			})
			return
		}

		var params struct {
			Arguments map[string]any `json:"arguments"`
			Name      string         `json:"name"`
		}
		paramsBytes, marshalErr := json.Marshal(req.Params)
		if marshalErr == nil {
			if unmarshalErr := json.Unmarshal(paramsBytes, &params); unmarshalErr == nil {
				capturedArgs = params.Arguments
			}
		}

		_ = json.NewEncoder(w).Encode(toolCallResponse{
			ID:     1,
			Result: &toolCallResult{Content: []contentItem{{Type: "text", Text: "ok"}}},
		})
	}))
	defer server.Close()

	clients := []Server{&Client{HTTP: &http.Client{}, BaseURL: server.URL, BearerToken: "key"}}
	mc, err := NewMultiClient(context.Background(), clients)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	scope := &llm.UserScope{SlackUserID: "U123", DisplayName: "TestUser"}
	_, callErr := mc.CallTool(context.Background(), "capture_thought", map[string]any{"content": "hello"}, scope)
	if callErr != nil {
		t.Fatalf("unexpected error: %v", callErr)
	}
	if _, hasUserID := capturedArgs["user_id"]; hasUserID {
		t.Error("expected no user_id injection — MCP tools manage their own user scoping")
	}
	if capturedArgs["content"] != "hello" {
		t.Errorf("expected content 'hello', got %v", capturedArgs["content"])
	}
}

func TestMultiClientCallToolWithNilUserScope(t *testing.T) {
	var capturedArgs map[string]any
	server := newMCPTestServer(t, "test-session", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		if decodeErr := json.NewDecoder(r.Body).Decode(&req); decodeErr != nil {
			t.Fatalf("decoding request: %v", decodeErr)
		}

		w.Header().Set("Content-Type", "application/json")
		if req.Method == "tools/list" {
			_ = json.NewEncoder(w).Encode(toolsListResponse{
				ID:     1,
				Result: &toolsListResult{Tools: []mcpTool{{Name: "search_thoughts", Description: "Search", InputSchema: json.RawMessage(`{"type":"object"}`)}}},
			})
			return
		}

		var params struct {
			Arguments map[string]any `json:"arguments"`
			Name      string         `json:"name"`
		}
		paramsBytes, marshalErr := json.Marshal(req.Params)
		if marshalErr == nil {
			if unmarshalErr := json.Unmarshal(paramsBytes, &params); unmarshalErr == nil {
				capturedArgs = params.Arguments
			}
		}

		_ = json.NewEncoder(w).Encode(toolCallResponse{
			ID:     1,
			Result: &toolCallResult{Content: []contentItem{{Type: "text", Text: "ok"}}},
		})
	}))
	defer server.Close()

	clients := []Server{&Client{HTTP: &http.Client{}, BaseURL: server.URL, BearerToken: "key"}}
	mc, err := NewMultiClient(context.Background(), clients)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, callErr := mc.CallTool(context.Background(), "search_thoughts", map[string]any{"query": "test"}, nil)
	if callErr != nil {
		t.Fatalf("unexpected error: %v", callErr)
	}
	if _, hasUserID := capturedArgs["user_id"]; hasUserID {
		t.Error("expected no user_id in arguments when UserScope is nil")
	}
	if capturedArgs["query"] != "test" {
		t.Errorf("expected query 'test', got %v", capturedArgs["query"])
	}
}
