package main

import (
	"testing"
)

func TestParseCIStatusJSON_Pending(t *testing.T) {
	cases := []struct {
		want   string
		reason string
		raw    []byte
	}{
		{raw: []byte(`[]`), want: "pending", reason: "empty run list"},
		{raw: []byte(`not json`), want: "pending", reason: "invalid JSON"},
		{raw: []byte(`[{"status":"in_progress","conclusion":""}]`), want: "pending", reason: "in_progress"},
		{raw: []byte(`[{"status":"queued","conclusion":""}]`), want: "pending", reason: "queued"},
	}
	for _, tc := range cases {
		got := parseCIStatusJSON(tc.raw)
		if got != tc.want {
			t.Errorf("%s: parseCIStatusJSON = %q, want %q", tc.reason, got, tc.want)
		}
	}
}

func TestParseCIStatusJSON_Green(t *testing.T) {
	raw := []byte(`[{"status":"completed","conclusion":"success"}]`)
	if got := parseCIStatusJSON(raw); got != "green" {
		t.Errorf("expected green, got %q", got)
	}
}

func TestParseCIStatusJSON_Failed(t *testing.T) {
	cases := [][]byte{
		[]byte(`[{"status":"completed","conclusion":"failure"}]`),
		[]byte(`[{"status":"completed","conclusion":"cancelled"}]`),
	}
	for _, raw := range cases {
		if got := parseCIStatusJSON(raw); got != "failed" {
			t.Errorf("expected failed for %s, got %q", raw, got)
		}
	}
}
