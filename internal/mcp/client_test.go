package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

type mcpTestHandler struct {
	t           *testing.T
	handler     http.HandlerFunc
	sessionID   string
	initCount   atomic.Int32
	initialized atomic.Int32
}

func newMCPTestServer(t *testing.T, sessionID string, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	h := &mcpTestHandler{t: t, sessionID: sessionID, handler: handler}
	return httptest.NewServer(h)
}

func (h *mcpTestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))

	var rpc struct {
		Method string `json:"method"`
	}
	_ = json.Unmarshal(body, &rpc)

	switch rpc.Method {
	case "initialize":
		h.initCount.Add(1)
		if h.sessionID != "" {
			w.Header().Set("Mcp-Session-Id", h.sessionID)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities":   map[string]any{"tools": map[string]any{}},
				"serverInfo":     map[string]any{"name": "test-server", "version": "1.0.0"},
			},
		})
	case "notifications/initialized":
		h.initialized.Add(1)
		w.WriteHeader(http.StatusAccepted)
	default:
		if h.initialized.Load() == 0 {
			h.t.Errorf("received %s before initialization", rpc.Method)
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		h.handler.ServeHTTP(w, r)
	}
}

func toolsListHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"result": {
				"tools": [
					{
						"name": "capture_thought",
						"description": "Save a new thought",
						"inputSchema": {"type": "object", "properties": {"content": {"type": "string"}}, "required": ["content"]}
					},
					{
						"name": "search_thoughts",
						"description": "Search thoughts by meaning",
						"inputSchema": {"type": "object", "properties": {"query": {"type": "string"}}, "required": ["query"]}
					}
				]
			}
		}`))
	}
}

// --- New initialization tests ---

func TestClientInitializesBeforeListTools(t *testing.T) {
	server := newMCPTestServer(t, "test-session", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"test","description":"Test","inputSchema":{"type":"object"}}]}}`))
	}))
	defer server.Close()
	handler := server.Config.Handler.(*mcpTestHandler)

	client := &Client{HTTP: &http.Client{}, BaseURL: server.URL, BearerToken: "key"}
	_, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := handler.initCount.Load(); got != 1 {
		t.Errorf("expected 1 initialization, got %d", got)
	}
	if got := handler.initialized.Load(); got != 1 {
		t.Errorf("expected 1 initialized notification, got %d", got)
	}
}

func TestClientInitializesBeforeCallTool(t *testing.T) {
	server := newMCPTestServer(t, "test-session", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(toolCallResponse{
			ID:     1,
			Result: &toolCallResult{Content: []contentItem{{Type: "text", Text: "ok"}}},
		})
	}))
	defer server.Close()
	handler := server.Config.Handler.(*mcpTestHandler)

	client := &Client{HTTP: &http.Client{}, BaseURL: server.URL, BearerToken: "key"}
	_, err := client.CallTool(context.Background(), "test", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := handler.initCount.Load(); got != 1 {
		t.Errorf("expected 1 initialization, got %d", got)
	}
	if got := handler.initialized.Load(); got != 1 {
		t.Errorf("expected 1 initialized notification, got %d", got)
	}
}

func TestClientInitializesOnlyOnce(t *testing.T) {
	server := newMCPTestServer(t, "test-session", toolsListHandler(t))
	defer server.Close()
	handler := server.Config.Handler.(*mcpTestHandler)

	client := &Client{HTTP: &http.Client{}, BaseURL: server.URL, BearerToken: "key"}
	_, _ = client.ListTools(context.Background())
	_, _ = client.ListTools(context.Background())

	if got := handler.initCount.Load(); got != 1 {
		t.Errorf("expected 1 initialization, got %d", got)
	}
}

func TestClientIncludesSessionID(t *testing.T) {
	var sessionHeaders []string
	server := newMCPTestServer(t, "my-session-123", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionHeaders = append(sessionHeaders, r.Header.Get("Mcp-Session-Id"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"test","description":"Test","inputSchema":{"type":"object"}}]}}`))
	}))
	defer server.Close()

	client := &Client{HTTP: &http.Client{}, BaseURL: server.URL, BearerToken: "key"}
	_, _ = client.ListTools(context.Background())

	if len(sessionHeaders) != 1 || sessionHeaders[0] != "my-session-123" {
		t.Errorf("expected Mcp-Session-Id 'my-session-123' on tool request, got %v", sessionHeaders)
	}
}

func TestClientHandlesNoSessionID(t *testing.T) {
	server := newMCPTestServer(t, "", toolsListHandler(t))
	defer server.Close()

	client := &Client{HTTP: &http.Client{}, BaseURL: server.URL, BearerToken: "key"}
	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
}

func TestClientReInitializesOn404(t *testing.T) {
	var toolCallCount atomic.Int32
	server := newMCPTestServer(t, "session-v2", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := toolCallCount.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"test","description":"Test","inputSchema":{"type":"object"}}]}}`))
	}))
	defer server.Close()
	handler := server.Config.Handler.(*mcpTestHandler)

	client := &Client{HTTP: &http.Client{}, BaseURL: server.URL, BearerToken: "key"}
	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(tools))
	}
	if got := handler.initCount.Load(); got != 2 {
		t.Errorf("expected 2 initializations (initial + re-init), got %d", got)
	}
}

func TestClientInitializeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer server.Close()

	client := &Client{HTTP: &http.Client{}, BaseURL: server.URL, BearerToken: "key"}
	_, err := client.ListTools(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- Updated existing tests ---

func TestCallToolSuccess(t *testing.T) {
	server := newMCPTestServer(t, "test-session", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type 'application/json', got %q", r.Header.Get("Content-Type"))
		}

		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("expected Authorization 'Bearer test-token', got %q", got)
		}

		var req jsonRPCRequest
		if decodeErr := json.NewDecoder(r.Body).Decode(&req); decodeErr != nil {
			t.Fatalf("decoding request: %v", decodeErr)
		}
		if req.JSONRPC != "2.0" {
			t.Errorf("expected jsonrpc '2.0', got %q", req.JSONRPC)
		}
		if req.Method != "tools/call" {
			t.Errorf("expected method 'tools/call', got %q", req.Method)
		}

		params, paramsOK := req.Params.(map[string]any)
		if !paramsOK {
			t.Fatalf("expected params to be map, got %T", req.Params)
		}
		if params["name"] != "capture_thought" {
			t.Errorf("expected tool name 'capture_thought', got %v", params["name"])
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(toolCallResponse{
			ID: 1,
			Result: &toolCallResult{
				Content: []contentItem{{Type: "text", Text: "Captured as observation — testing"}},
			},
		})
	}))
	defer server.Close()

	client := &Client{
		HTTP:        &http.Client{},
		BaseURL:     server.URL,
		BearerToken: "test-token",
	}

	result, callErr := client.CallTool(context.Background(), "capture_thought", map[string]any{"content": "test thought"})
	if callErr != nil {
		t.Fatalf("unexpected error: %v", callErr)
	}
	if result != "Captured as observation — testing" {
		t.Errorf("expected 'Captured as observation — testing', got %q", result)
	}
}

func TestCallToolHTTPError(t *testing.T) {
	server := newMCPTestServer(t, "test-session", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	client := &Client{
		HTTP:        &http.Client{},
		BaseURL:     server.URL,
		BearerToken: "test-token",
	}

	_, callErr := client.CallTool(context.Background(), "capture_thought", map[string]any{"content": "test"})
	if callErr == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCallToolJSONRPCError(t *testing.T) {
	server := newMCPTestServer(t, "test-session", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(toolCallResponse{
			ID:    1,
			Error: &jsonRPCError{Code: -32600, Message: "invalid request"},
		})
	}))
	defer server.Close()

	client := &Client{
		HTTP:        &http.Client{},
		BaseURL:     server.URL,
		BearerToken: "test-token",
	}

	_, callErr := client.CallTool(context.Background(), "bad_tool", nil)
	if callErr == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCallToolMalformedResponse(t *testing.T) {
	server := newMCPTestServer(t, "test-session", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	client := &Client{
		HTTP:        &http.Client{},
		BaseURL:     server.URL,
		BearerToken: "test-token",
	}

	_, callErr := client.CallTool(context.Background(), "capture_thought", map[string]any{"content": "test"})
	if callErr == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListToolsSuccess(t *testing.T) {
	server := newMCPTestServer(t, "test-session", toolsListHandler(t))
	defer server.Close()

	client := &Client{HTTP: &http.Client{}, BaseURL: server.URL, BearerToken: "test-token"}

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if tools[0].Name != "capture_thought" {
		t.Errorf("expected first tool 'capture_thought', got %q", tools[0].Name)
	}
	if tools[1].Name != "search_thoughts" {
		t.Errorf("expected second tool 'search_thoughts', got %q", tools[1].Name)
	}
	if tools[0].Description != "Save a new thought" {
		t.Errorf("expected description 'Save a new thought', got %q", tools[0].Description)
	}
	if len(tools[0].InputSchema) == 0 {
		t.Error("expected non-empty InputSchema for first tool")
	}
}

func TestListToolsHTTPError(t *testing.T) {
	server := newMCPTestServer(t, "test-session", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	client := &Client{HTTP: &http.Client{}, BaseURL: server.URL, BearerToken: "test-token"}

	_, err := client.ListTools(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListToolsJSONRPCError(t *testing.T) {
	server := newMCPTestServer(t, "test-session", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(toolsListResponse{
			ID:    1,
			Error: &jsonRPCError{Code: -32600, Message: "invalid request"},
		})
	}))
	defer server.Close()

	client := &Client{HTTP: &http.Client{}, BaseURL: server.URL, BearerToken: "test-token"}

	_, err := client.ListTools(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestInitializeSuccess(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if decodeErr := json.NewDecoder(r.Body).Decode(&req); decodeErr != nil {
			t.Fatalf("decoding request: %v", decodeErr)
		}

		method, _ := req["method"].(string)
		methods = append(methods, method)

		w.Header().Set("Content-Type", "application/json")
		if method == "initialize" {
			// Verify protocol version and client info are sent
			params, _ := req["params"].(map[string]any)
			if params["protocolVersion"] == nil {
				t.Error("expected protocolVersion in params")
			}
			clientInfo, _ := params["clientInfo"].(map[string]any)
			if clientInfo["name"] == nil {
				t.Error("expected clientInfo.name in params")
			}
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"test"}}}`))
			return
		}
		if method == "notifications/initialized" {
			w.WriteHeader(http.StatusOK)
			return
		}
		t.Errorf("unexpected method: %s", method)
	}))
	defer server.Close()

	client := &Client{HTTP: &http.Client{}, BaseURL: server.URL, BearerToken: "test-token"}
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(methods) != 2 {
		t.Fatalf("expected 2 calls, got %d: %v", len(methods), methods)
	}
	if methods[0] != "initialize" {
		t.Errorf("expected first call to be 'initialize', got %q", methods[0])
	}
	if methods[1] != "notifications/initialized" {
		t.Errorf("expected second call to be 'notifications/initialized', got %q", methods[1])
	}
}

func TestInitializeHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	client := &Client{HTTP: &http.Client{}, BaseURL: server.URL, BearerToken: "test-token"}
	err := client.Initialize(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListToolsSSEResponse(t *testing.T) {
	server := newMCPTestServer(t, "test-session", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"tools\":[{\"name\":\"get_issue\",\"description\":\"Get a GitHub issue\",\"inputSchema\":{\"type\":\"object\"}}]}}\n\n"))
	}))
	defer server.Close()

	client := &Client{HTTP: &http.Client{}, BaseURL: server.URL, BearerToken: "test-token"}

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "get_issue" {
		t.Errorf("expected tool 'get_issue', got %q", tools[0].Name)
	}
}

func TestCallToolSSEResponse(t *testing.T) {
	server := newMCPTestServer(t, "test-session", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"Issue #42: Fix bug\"}]}}\n\n"))
	}))
	defer server.Close()

	client := &Client{HTTP: &http.Client{}, BaseURL: server.URL, BearerToken: "test-token"}

	result, err := client.CallTool(context.Background(), "get_issue", map[string]any{"owner": "test", "repo": "test", "number": 42})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Issue #42: Fix bug" {
		t.Errorf("expected 'Issue #42: Fix bug', got %q", result)
	}
}

func TestListToolsEmptyResult(t *testing.T) {
	server := newMCPTestServer(t, "test-session", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`))
	}))
	defer server.Close()

	client := &Client{HTTP: &http.Client{}, BaseURL: server.URL, BearerToken: "test-token"}

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}
