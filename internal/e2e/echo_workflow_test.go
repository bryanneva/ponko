//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bryanneva/ponko/internal/workflow"
)

func TestEchoWorkflow(t *testing.T) {
	slackReplyReceived := make(chan struct{}, 1)
	slackStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true}`)
		if r.URL.Path == "/chat.postMessage" {
			select {
			case slackReplyReceived <- struct{}{}:
			default:
			}
		}
	}))
	defer slackStub.Close()

	s := setupE2EServer(t, e2eServerOpts{
		SlackURL: slackStub.URL,
		Timeout:  60 * time.Second,
	})

	reqBody, _ := json.Marshal(map[string]any{
		"workflow_type": "echo",
		"payload":       map[string]string{"message": "hello"},
	})

	resp, err := http.Post(s.BaseURL+"/api/workflows/start", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("failed to POST /api/workflows/start: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var startResp struct {
		WorkflowID string `json:"workflow_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&startResp); err != nil {
		t.Fatalf("failed to decode start response: %v", err)
	}

	workflowID := startResp.WorkflowID
	t.Logf("started workflow: %s", workflowID)

	var wf *workflow.WorkflowWithDetails
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		pollResp, pollErr := http.Get(s.BaseURL + "/api/workflows/" + workflowID)
		if pollErr != nil {
			t.Fatalf("failed to GET workflow: %v", pollErr)
		}

		var result workflow.WorkflowWithDetails
		if decErr := json.NewDecoder(pollResp.Body).Decode(&result); decErr != nil {
			_ = pollResp.Body.Close()
			t.Fatalf("failed to decode workflow: %v", decErr)
		}
		_ = pollResp.Body.Close()

		if result.Status == "completed" {
			wf = &result
			break
		}

		if result.Status == "failed" {
			t.Fatalf("workflow failed")
		}

		time.Sleep(500 * time.Millisecond)
	}

	if wf == nil {
		t.Fatal("workflow did not complete within 30s")
	}

	if len(wf.Steps) != 4 {
		t.Fatalf("expected 4 steps, got %d", len(wf.Steps))
	}

	expectedSteps := map[string]bool{"receive": false, "plan": false, "process": false, "respond": false}
	for _, step := range wf.Steps {
		if step.Status != "complete" {
			t.Errorf("step %q has status %q, expected 'complete'", step.StepName, step.Status)
		}
		expectedSteps[step.StepName] = true
	}
	for name, found := range expectedSteps {
		if !found {
			t.Errorf("missing step: %s", name)
		}
	}

	var foundRespondOutput bool
	for _, output := range wf.Outputs {
		if output.StepName == "respond" {
			foundRespondOutput = true
			var data map[string]string
			if err := json.Unmarshal(output.Data, &data); err != nil {
				t.Fatalf("failed to unmarshal respond output: %v", err)
			}
			if data["response"] == "" {
				t.Error("respond output 'response' field is empty")
			} else {
				t.Logf("Claude response: %s", data["response"])
			}
		}
	}
	if !foundRespondOutput {
		t.Error("no respond output found")
	}

	select {
	case <-slackReplyReceived:
	case <-s.Ctx.Done():
		t.Fatal("timed out waiting for SlackReplyWorker to post message")
	}
}
