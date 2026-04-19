package event_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bryanneva/ponko/internal/event"
)

func TestJSONLBus_EmitWritesValidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	bus, err := event.NewJSONLBus(path)
	if err != nil {
		t.Fatalf("NewJSONLBus: %v", err)
	}

	e := event.Event{
		Type:          event.TaskCreated,
		TaskID:        "task-1",
		CorrelationID: "corr-1",
		Payload:       map[string]any{"key": "value"},
	}
	err = bus.Emit(e)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var got event.Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Type != event.TaskCreated {
		t.Errorf("Type: got %q, want %q", got.Type, event.TaskCreated)
	}
	if got.TaskID != "task-1" {
		t.Errorf("TaskID: got %q, want task-1", got.TaskID)
	}
	if got.Timestamp.IsZero() {
		t.Error("Timestamp should be set automatically")
	}
}

func TestJSONLBus_MultipleEmitsAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	bus, err := event.NewJSONLBus(path)
	if err != nil {
		t.Fatalf("NewJSONLBus: %v", err)
	}

	for i := 0; i < 3; i++ {
		err = bus.Emit(event.Event{Type: event.TaskStarted, TaskID: "t"})
		if err != nil {
			t.Fatalf("Emit %d: %v", i, err)
		}
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()

	var count int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if scanner.Text() == "" {
			continue
		}
		count++
	}
	if count != 3 {
		t.Errorf("expected 3 lines, got %d", count)
	}
}

func TestJSONLBus_CreatesFileIfNotExist(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	path := filepath.Join(dir, "events.jsonl")

	bus, err := event.NewJSONLBus(path)
	if err != nil {
		t.Fatalf("NewJSONLBus: %v", err)
	}

	if err := bus.Emit(event.Event{Type: event.PhaseStarted}); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("file should have been created")
	}
}

func TestEventTypeConstants(t *testing.T) {
	expected := []string{
		string(event.TaskCreated),
		string(event.TaskStarted),
		string(event.PhaseStarted),
		string(event.PhaseCompleted),
		string(event.TaskBlocked),
		string(event.TaskCompleted),
		string(event.TaskFailed),
		string(event.BudgetExceeded),
	}
	for _, e := range expected {
		if e == "" {
			t.Error("event type constant must not be empty")
		}
	}
}
