package jobs

import (
	"strings"
	"testing"
)

func TestDecompose_NoThreadKey(t *testing.T) {
	w := &PlanWorker{}
	payload := receivePayload{Message: "hello", ThreadKey: ""}

	tasks, err := w.decompose(t.Context(), payload)
	if err != nil {
		t.Fatalf("decompose: %v", err)
	}
	if tasks != nil {
		t.Errorf("expected nil tasks for no thread key, got %v", tasks)
	}
}

func TestStripMarkdownFences(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no fences",
			input: `{"action":"direct"}`,
			want:  `{"action":"direct"}`,
		},
		{
			name:  "json fences",
			input: "```json\n{\"action\":\"fan_out\",\"tasks\":[{\"instruction\":\"do thing\"}]}\n```",
			want:  `{"action":"fan_out","tasks":[{"instruction":"do thing"}]}`,
		},
		{
			name:  "plain fences",
			input: "```\n{\"action\":\"direct\"}\n```",
			want:  `{"action":"direct"}`,
		},
		{
			name:  "fences with extra whitespace",
			input: "  ```json\n{\"action\":\"direct\"}\n```  ",
			want:  `{"action":"direct"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripMarkdownFences(tt.input)
			if got != tt.want {
				t.Errorf("stripMarkdownFences() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDecompose_InvalidJSON(t *testing.T) {
	w := &PlanWorker{}
	payload := receivePayload{Message: "hello", ThreadKey: "C123:1234567.890"}

	// Without a Claude client, decompose will fail the LLM call and fall back to bypass
	tasks, err := w.decompose(t.Context(), payload)
	if err != nil {
		t.Fatalf("decompose: %v", err)
	}
	if tasks != nil {
		t.Errorf("expected nil tasks when LLM fails, got %v", tasks)
	}
}

func TestBuildAckMessage_TwoTasks(t *testing.T) {
	tasks := []taskPlan{
		{Instruction: "Research company A"},
		{Instruction: "Research company B"},
	}
	msg := buildAckMessage(tasks, "wf-123", "https://example.com")

	if !strings.Contains(msg, "1. Research company A") {
		t.Errorf("expected task 1 in message, got: %s", msg)
	}
	if !strings.Contains(msg, "2. Research company B") {
		t.Errorf("expected task 2 in message, got: %s", msg)
	}
	if !strings.Contains(msg, "https://example.com/workflows/wf-123") {
		t.Errorf("expected workflow link in message, got: %s", msg)
	}
}

func TestBuildAckMessage_ThreeTasks(t *testing.T) {
	tasks := []taskPlan{
		{Instruction: "Task one"},
		{Instruction: "Task two"},
		{Instruction: "Task three"},
	}
	msg := buildAckMessage(tasks, "wf-456", "https://example.com")

	if !strings.Contains(msg, "3. Task three") {
		t.Errorf("expected task 3 in message, got: %s", msg)
	}
}

func TestBuildAckMessage_TrailingSlash(t *testing.T) {
	tasks := []taskPlan{
		{Instruction: "Do something"},
		{Instruction: "Do another thing"},
	}
	msg := buildAckMessage(tasks, "wf-123", "https://example.com/")

	if !strings.Contains(msg, "https://example.com/workflows/wf-123") {
		t.Errorf("expected trailing slash stripped from URL, got: %s", msg)
	}
	if strings.Contains(msg, "//workflows") {
		t.Errorf("expected no double-slash in URL, got: %s", msg)
	}
}

func TestBuildAckMessage_NoBaseURL(t *testing.T) {
	tasks := []taskPlan{
		{Instruction: "Do something"},
		{Instruction: "Do another thing"},
	}
	msg := buildAckMessage(tasks, "wf-789", "")

	if strings.Contains(msg, "workflows/") {
		t.Errorf("expected no workflow link when base URL empty, got: %s", msg)
	}
	if !strings.Contains(msg, "1. Do something") {
		t.Errorf("expected tasks in message, got: %s", msg)
	}
}
