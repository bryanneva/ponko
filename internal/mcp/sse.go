package mcp

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
)

// parseSSEResponse extracts the last JSON-RPC payload from an SSE event stream.
// SSE format: lines prefixed with "data: " contain the payload, events separated by blank lines.
func parseSSEResponse(body []byte) ([]byte, error) {
	var lastData []byte
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max — GitHub tool responses (file contents, PR diffs) can exceed the default 64KB
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			lastData = []byte(strings.TrimPrefix(line, "data: "))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning SSE response: %w", err)
	}
	if lastData == nil {
		return nil, fmt.Errorf("no data events in SSE response")
	}
	return lastData, nil
}
