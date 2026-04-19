package event

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// JSONLBus implements Bus by appending JSON-encoded events to a JSONL file.
type JSONLBus struct {
	path string
	mu   sync.Mutex
}

// NewJSONLBus creates a JSONLBus that writes to path.
// Parent directories are created if they don't exist.
func NewJSONLBus(path string) (*JSONLBus, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create event log directory: %w", err)
	}
	return &JSONLBus{path: path}, nil
}

// Emit appends the event as a JSON line to the JSONL file.
// It sets Timestamp if not already set.
func (b *JSONLBus) Emit(e Event) error {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}

	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	f, err := os.OpenFile(b.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open event log: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write event: %w", err)
	}
	return nil
}
