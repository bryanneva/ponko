package mcp

import "testing"

func TestParseSSEResponseSingleEvent(t *testing.T) {
	body := []byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"tools\":[]}}\n\n")
	got, err := parseSSEResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`
	if string(got) != expected {
		t.Errorf("expected %q, got %q", expected, string(got))
	}
}

func TestParseSSEResponseMultipleEvents(t *testing.T) {
	body := []byte("event: message\ndata: {\"first\":true}\n\nevent: message\ndata: {\"second\":true}\n\n")
	got, err := parseSSEResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != `{"second":true}` {
		t.Errorf("expected last event, got %q", string(got))
	}
}

func TestParseSSEResponseNoEventPrefix(t *testing.T) {
	body := []byte("data: {\"bare\":true}\n\n")
	got, err := parseSSEResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != `{"bare":true}` {
		t.Errorf("expected %q, got %q", `{"bare":true}`, string(got))
	}
}

func TestParseSSEResponseNoData(t *testing.T) {
	body := []byte("event: message\n\n")
	_, err := parseSSEResponse(body)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestParseSSEResponseEmptyBody(t *testing.T) {
	_, err := parseSSEResponse([]byte(""))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
