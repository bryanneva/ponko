package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandleHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %s", contentType)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %s", body["status"])
	}
}

func TestHandleStartWorkflow_InvalidBody(t *testing.T) {
	handler := handleStartWorkflow(nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/workflows/start", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body["error"] != "invalid request body" {
		t.Fatalf("expected error 'invalid request body', got %s", body["error"])
	}
}

func TestHandleGetWorkflow_InvalidUUID(t *testing.T) {
	handler := handleGetWorkflow(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/workflows/not-a-uuid", nil)
	req.SetPathValue("id", "not-a-uuid")
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body["error"] != "invalid workflow_id: must be a valid UUID" {
		t.Fatalf("expected UUID validation error, got %s", body["error"])
	}
}

func TestAPIKeyAuth(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	tests := []struct {
		name       string
		apiKey     string
		authHeader string
		wantStatus int
	}{
		{"valid key", "test-key", "Bearer test-key", http.StatusOK},
		{"wrong key", "test-key", "Bearer wrong-key", http.StatusUnauthorized},
		{"missing header", "test-key", "", http.StatusUnauthorized},
		{"no key configured", "", "", http.StatusOK},
		{"no key configured ignores header", "", "Bearer something", http.StatusOK},
		{"malformed header", "test-key", "Basic test-key", http.StatusUnauthorized},
		{"bearer without space", "test-key", "Bearertest-key", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := apiKeyAuth(tt.apiKey, okHandler)
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			w := httptest.NewRecorder()
			handler(w, req)
			if w.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, w.Code)
			}
		})
	}
}

func TestApiKeyOrSessionAuth(t *testing.T) {
	signingKey := []byte("test-signing-key-32-bytes-long!!")

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	validCookie := func() string {
		payload := sessionPayload{
			UserID:      "U123",
			DisplayName: "Test User",
			Exp:         time.Now().Add(time.Hour).Unix(),
		}
		val, err := signCookie(payload, signingKey)
		if err != nil {
			t.Fatalf("failed to sign cookie: %v", err)
		}
		return val
	}

	expiredCookie := func() string {
		payload := sessionPayload{
			UserID:      "U123",
			DisplayName: "Test User",
			Exp:         time.Now().Add(-time.Hour).Unix(),
		}
		val, err := signCookie(payload, signingKey)
		if err != nil {
			t.Fatalf("failed to sign cookie: %v", err)
		}
		return val
	}

	tests := []struct {
		name       string
		apiKey     string
		authHeader string
		cookie     string
		wantStatus int
	}{
		{"valid bearer token", "test-key", "Bearer test-key", "", http.StatusOK},
		{"valid session cookie", "test-key", "", validCookie(), http.StatusOK},
		{"expired session cookie", "test-key", "", expiredCookie(), http.StatusUnauthorized},
		{"invalid bearer token", "test-key", "Bearer wrong-key", "", http.StatusUnauthorized},
		{"no auth at all", "test-key", "", "", http.StatusUnauthorized},
		{"bearer wins when both present", "test-key", "Bearer test-key", validCookie(), http.StatusOK},
		{"no auth configured allows all", "", "", "", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := signingKey
			if tt.apiKey == "" {
				key = nil
			}
			handler := apiKeyOrSession(tt.apiKey, key, okHandler)
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tt.cookie})
			}
			w := httptest.NewRecorder()
			handler(w, req)
			if w.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, w.Code)
			}
		})
	}
}

func TestParseJobErrors(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want []string
	}{
		{"nil input", nil, []string{}},
		{"empty bytes", []byte{}, []string{}},
		{"null JSON", []byte("null"), []string{}},
		{"empty array", []byte("[]"), []string{}},
		{"single error", []byte(`[{"error":"something broke"}]`), []string{"something broke"}},
		{"multiple errors", []byte(`[{"error":"first"},{"error":"second"}]`), []string{"first", "second"}},
		{"empty error string skipped", []byte(`[{"error":"real"},{"error":""}]`), []string{"real"}},
		{"malformed JSON", []byte(`not json`), []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseJobErrors(tt.data)
			if len(got) != len(tt.want) {
				t.Fatalf("expected %d errors, got %d: %v", len(tt.want), len(got), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("error[%d]: expected %q, got %q", i, tt.want[i], got[i])
				}
			}
		})
	}
}

func TestHandleListTools(t *testing.T) {
	tests := []struct {
		name      string
		toolNames []string
		wantTools []string
	}{
		{"with tools", []string{"tool_a", "tool_b"}, []string{"tool_a", "tool_b"}},
		{"empty list", []string{}, []string{}},
		{"nil list", nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := handleListTools(tt.toolNames)
			req := httptest.NewRequest(http.MethodGet, "/tools", nil)
			w := httptest.NewRecorder()
			handler(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", w.Code)
			}

			var body map[string][]string
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			got := body["tools"]
			if len(got) != len(tt.wantTools) {
				t.Fatalf("expected %d tools, got %d: %v", len(tt.wantTools), len(got), got)
			}
			for i := range got {
				if got[i] != tt.wantTools[i] {
					t.Fatalf("tool[%d]: expected %q, got %q", i, tt.wantTools[i], got[i])
				}
			}
		})
	}
}

func TestHandleStartWorkflow_EmptyWorkflowType(t *testing.T) {
	handler := handleStartWorkflow(nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/workflows/start", strings.NewReader(`{"workflow_type": ""}`))
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body["error"] != "workflow_type is required" {
		t.Fatalf("expected error 'workflow_type is required', got %s", body["error"])
	}
}
