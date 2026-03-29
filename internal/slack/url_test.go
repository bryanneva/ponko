package slack

import (
	"testing"
)

func TestParseThreadURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		channelID string
		timestamp string
		wantErr   bool
	}{
		{
			name:      "valid thread URL",
			url:       "https://example.slack.com/archives/C00TESTCHAN/p1774549751570729",
			channelID: "C00TESTCHAN",
			timestamp: "1774549751.570729",
		},
		{
			name:      "different workspace",
			url:       "https://acme.slack.com/archives/C12345ABCDE/p1234567890123456",
			channelID: "C12345ABCDE",
			timestamp: "1234567890.123456",
		},
		{
			name:      "URL with query params",
			url:       "https://example.slack.com/archives/C00TESTCHAN/p1774549751570729?thread_ts=1774549751.570729&cid=C00TESTCHAN",
			channelID: "C00TESTCHAN",
			timestamp: "1774549751.570729",
		},
		{
			name:    "empty string",
			url:     "",
			wantErr: true,
		},
		{
			name:    "not a slack URL",
			url:     "https://example.com/archives/C00TESTCHAN/p1774549751570729",
			wantErr: true,
		},
		{
			name:    "missing p prefix on timestamp",
			url:     "https://example.slack.com/archives/C00TESTCHAN/1774549751570729",
			wantErr: true,
		},
		{
			name:    "too few path segments",
			url:     "https://example.slack.com/archives/C00TESTCHAN",
			wantErr: true,
		},
		{
			name:    "non-numeric timestamp",
			url:     "https://example.slack.com/archives/C00TESTCHAN/pabc123",
			wantErr: true,
		},
		{
			name:    "timestamp too short for decimal split",
			url:     "https://example.slack.com/archives/C00TESTCHAN/p12345",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channelID, timestamp, err := ParseThreadURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got channelID=%q timestamp=%q", channelID, timestamp)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if channelID != tt.channelID {
				t.Errorf("channelID = %q, want %q", channelID, tt.channelID)
			}
			if timestamp != tt.timestamp {
				t.Errorf("timestamp = %q, want %q", timestamp, tt.timestamp)
			}
		})
	}
}
