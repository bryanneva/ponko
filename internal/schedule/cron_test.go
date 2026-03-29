package schedule

import (
	"strings"
	"testing"
	"time"
)

func TestNextRunAt(t *testing.T) {
	ref := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC) // Monday

	tests := []struct {
		after   time.Time
		want    time.Time
		name    string
		expr    string
		wantErr bool
	}{
		{
			name:  "weekdays at 9am",
			expr:  "0 9 * * 1-5",
			after: ref,
			want:  time.Date(2026, 3, 24, 9, 0, 0, 0, time.UTC), // Tuesday 9am
		},
		{
			name:  "every hour",
			expr:  "0 * * * *",
			after: ref,
			want:  time.Date(2026, 3, 23, 13, 0, 0, 0, time.UTC),
		},
		{
			name:  "daily at 8:30am",
			expr:  "30 8 * * *",
			after: ref,
			want:  time.Date(2026, 3, 24, 8, 30, 0, 0, time.UTC),
		},
		{
			name:    "invalid expression",
			expr:    "not a cron",
			after:   ref,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextRunAt(tt.expr, tt.after)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCronToHuman(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want string
	}{
		{name: "daily", expr: "30 9 * * *", want: "daily at 9:30 AM"},
		{name: "weekday", expr: "0 9 * * 1-5", want: "every weekday at 9:00 AM"},
		{name: "sunday with 0", expr: "0 10 * * 0", want: "every Sunday at 10:00 AM"},
		{name: "sunday with 7", expr: "0 10 * * 7", want: "every Sunday at 10:00 AM"},
		{name: "monday", expr: "0 8 * * 1", want: "every Monday at 8:00 AM"},
		{name: "tuesday", expr: "30 14 * * 2", want: "every Tuesday at 2:30 PM"},
		{name: "wednesday", expr: "0 12 * * 3", want: "every Wednesday at 12:00 PM"},
		{name: "thursday", expr: "0 17 * * 4", want: "every Thursday at 5:00 PM"},
		{name: "friday", expr: "0 16 * * 5", want: "every Friday at 4:00 PM"},
		{name: "saturday", expr: "0 11 * * 6", want: "every Saturday at 11:00 AM"},
		{name: "every hour", expr: "0 * * * *", want: "every hour"},
		{name: "specific dom and month", expr: "0 9 15 6 *", want: "0 9 15 6 *"},
		{name: "invalid field count", expr: "0 9 *", want: "0 9 *"},
		{name: "unmatched pattern with dom set", expr: "0 * 15 * *", want: "0 * 15 * *"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cronToHuman(tt.expr)
			if got != tt.want {
				t.Errorf("cronToHuman(%q) = %q, want %q", tt.expr, got, tt.want)
			}
		})
	}
}

func TestTruncatePrompt(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want string
		max  int
	}{
		{name: "under limit", s: "short", max: 10, want: "short"},
		{name: "at limit", s: "exactly10!", max: 10, want: "exactly10!"},
		{name: "over limit", s: "this is a long prompt that exceeds the limit", max: 20, want: "this is a long pr..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncatePrompt(tt.s, tt.max)
			if got != tt.want {
				t.Errorf("truncatePrompt(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
			}
			if strings.HasSuffix(got, "...") && len(got) != tt.max {
				t.Errorf("truncated result length %d != max %d", len(got), tt.max)
			}
		})
	}
}

func TestFormatCronTime(t *testing.T) {
	tests := []struct {
		name   string
		hour   string
		minute string
		want   string
	}{
		{name: "morning", hour: "9", minute: "0", want: "9:00 AM"},
		{name: "afternoon", hour: "14", minute: "30", want: "2:30 PM"},
		{name: "midnight", hour: "0", minute: "0", want: "12:00 AM"},
		{name: "noon", hour: "12", minute: "0", want: "12:00 PM"},
		{name: "invalid hour", hour: "*", minute: "0", want: "*:0"},
		{name: "invalid minute", hour: "9", minute: "*", want: "9:*"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCronTime(tt.hour, tt.minute)
			if got != tt.want {
				t.Errorf("formatCronTime(%q, %q) = %q, want %q", tt.hour, tt.minute, got, tt.want)
			}
		})
	}
}
