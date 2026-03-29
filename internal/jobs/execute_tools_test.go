package jobs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/bryanneva/ponko/internal/llm"
	"github.com/bryanneva/ponko/internal/testutil"
)

func newClaudeServerWithToolDetection(t *testing.T, gotTools *atomic.Bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if _, ok := body["tools"]; ok {
			gotTools.Store(true)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_test",
			"type":        "message",
			"role":        "assistant",
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "result"}},
		})
	}))
}

func TestExecuteTask_UsesToolsWhenMCPClientSet(t *testing.T) {
	pool := testutil.TestDB(t)

	var gotTools atomic.Bool
	server := newClaudeServerWithToolDetection(t, &gotTools)
	defer server.Close()

	worker := &ExecuteWorker{
		Pool:      pool,
		Claude:    llm.NewClient("fake-key", server.URL),
		MCPClient: &stubMCPClient{result: "ok"},
		Tools:     []llm.Tool{{Name: "web_search", Description: "Search the web"}},
	}

	payload := receivePayload{Channel: "C_TEST", ThreadKey: "C_TEST:123"}

	_, err := worker.executeTask(context.Background(), "research competitors", payload)
	if err != nil {
		t.Fatalf("executeTask: %v", err)
	}

	if !gotTools.Load() {
		t.Error("expected request to include tools (SendConversationWithTools path), but tools were not present in the API request")
	}
}

func TestExecuteTask_FallsBackToHaikuWithoutTools(t *testing.T) {
	pool := testutil.TestDB(t)

	var gotTools atomic.Bool
	server := newClaudeServerWithToolDetection(t, &gotTools)
	defer server.Close()

	worker := &ExecuteWorker{
		Pool:   pool,
		Claude: llm.NewClient("fake-key", server.URL),
	}

	payload := receivePayload{Channel: "C_TEST", ThreadKey: "C_TEST:123"}

	_, err := worker.executeTask(context.Background(), "research competitors", payload)
	if err != nil {
		t.Fatalf("executeTask: %v", err)
	}

	if gotTools.Load() {
		t.Error("expected no tools in request when MCPClient is nil, but tools were present")
	}
}
