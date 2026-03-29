package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestOAuthRefresherRefreshesOn401(t *testing.T) {
	var toolCallCount atomic.Int32

	mcpServer := newMCPTestServer(t, "test-session", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		if decodeErr := json.NewDecoder(r.Body).Decode(&req); decodeErr != nil {
			t.Fatalf("decoding request: %v", decodeErr)
		}

		n := toolCallCount.Add(1)

		if req.Method == "tools/list" {
			if n == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("unauthorized"))
				return
			}
			if r.Header.Get("Authorization") != "Bearer new-token" {
				t.Errorf("expected refreshed token, got %q", r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(toolsListResponse{
				ID:     1,
				Result: &toolsListResult{Tools: []mcpTool{{Name: "test_tool", Description: "Test", InputSchema: json.RawMessage(`{"type":"object"}`)}}},
			})
			return
		}

		if req.Method == "tools/call" {
			if n == 3 {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("unauthorized"))
				return
			}
			if r.Header.Get("Authorization") != "Bearer new-token" {
				t.Errorf("expected refreshed token, got %q", r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(toolCallResponse{
				ID:     1,
				Result: &toolCallResult{Content: []contentItem{{Type: "text", Text: "ok"}}},
			})
		}
	}))
	defer mcpServer.Close()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-token","token_type":"Bearer","refresh_token":"new-refresh"}`))
	}))
	defer tokenServer.Close()

	refresher := &OAuthRefresher{
		Client:       &Client{HTTP: &http.Client{}, BaseURL: mcpServer.URL, BearerToken: "expired-token"},
		TokenURL:     tokenServer.URL,
		ClientID:     "test-id",
		ClientSecret: "test-secret",
		RefreshToken: "test-refresh",
	}

	tools, err := refresher.ListTools(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "test_tool" {
		t.Errorf("expected 1 tool named 'test_tool', got %v", tools)
	}

	toolCallCount.Store(2)
	result, callErr := refresher.CallTool(context.Background(), "test_tool", map[string]any{})
	if callErr != nil {
		t.Fatalf("unexpected error: %v", callErr)
	}
	if result != "ok" {
		t.Errorf("expected 'ok', got %q", result)
	}
}

func TestOAuthRefresherInitializeRefreshesOn401(t *testing.T) {
	var callCount atomic.Int32

	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		method, _ := req["method"].(string)

		n := callCount.Add(1)

		w.Header().Set("Content-Type", "application/json")
		if method == "initialize" {
			if n == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("unauthorized"))
				return
			}
			if r.Header.Get("Authorization") != "Bearer new-token" {
				t.Errorf("expected refreshed token, got %q", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"test"}}}`))
			return
		}
		if method == "notifications/initialized" {
			w.WriteHeader(http.StatusOK)
			return
		}
	}))
	defer mcpServer.Close()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-token","token_type":"Bearer","refresh_token":"new-refresh"}`))
	}))
	defer tokenServer.Close()

	refresher := &OAuthRefresher{
		Client:       &Client{HTTP: &http.Client{}, BaseURL: mcpServer.URL, BearerToken: "expired-token"},
		TokenURL:     tokenServer.URL,
		ClientID:     "test-id",
		ClientSecret: "test-secret",
		RefreshToken: "test-refresh",
	}

	err := refresher.Initialize(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOAuthRefresherPassesThroughOnSuccess(t *testing.T) {
	mcpServer := newMCPTestServer(t, "test-session", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "application/json")
		if req.Method == "tools/list" {
			_ = json.NewEncoder(w).Encode(toolsListResponse{
				ID:     1,
				Result: &toolsListResult{Tools: []mcpTool{{Name: "tool_a", Description: "A", InputSchema: json.RawMessage(`{"type":"object"}`)}}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(toolCallResponse{
			ID:     1,
			Result: &toolCallResult{Content: []contentItem{{Type: "text", Text: "success"}}},
		})
	}))
	defer mcpServer.Close()

	tokenCallCount := 0
	tokenServer := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		tokenCallCount++
	}))
	defer tokenServer.Close()

	refresher := &OAuthRefresher{
		Client:       &Client{HTTP: &http.Client{}, BaseURL: mcpServer.URL, BearerToken: "valid-token"},
		TokenURL:     tokenServer.URL,
		ClientID:     "test-id",
		ClientSecret: "test-secret",
		RefreshToken: "test-refresh",
	}

	tools, err := refresher.ListTools(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	result, callErr := refresher.CallTool(context.Background(), "tool_a", map[string]any{})
	if callErr != nil {
		t.Fatalf("unexpected error: %v", callErr)
	}
	if result != "success" {
		t.Errorf("expected 'success', got %q", result)
	}

	if tokenCallCount != 0 {
		t.Errorf("expected 0 token refresh calls, got %d", tokenCallCount)
	}
}

func TestOAuthRefresherFailsWhenRefreshFails(t *testing.T) {
	mcpServer := newMCPTestServer(t, "test-session", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("unauthorized"))
	}))
	defer mcpServer.Close()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer tokenServer.Close()

	refresher := &OAuthRefresher{
		Client:       &Client{HTTP: &http.Client{}, BaseURL: mcpServer.URL, BearerToken: "expired-token"},
		TokenURL:     tokenServer.URL,
		ClientID:     "test-id",
		ClientSecret: "test-secret",
		RefreshToken: "bad-refresh",
	}

	_, err := refresher.ListTools(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	_, callErr := refresher.CallTool(context.Background(), "any_tool", map[string]any{})
	if callErr == nil {
		t.Fatal("expected error, got nil")
	}
}
