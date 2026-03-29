package conversation_test

import (
	"context"
	"testing"
	"time"

	"github.com/bryanneva/ponko/internal/conversation"
	"github.com/bryanneva/ponko/internal/testutil"
)

func TestThreadKey(t *testing.T) {
	t.Run("concatenates channel and threadTS", func(t *testing.T) {
		got := conversation.ThreadKey("C12345", "1234567890.123456")
		want := "C12345:1234567890.123456"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("empty values", func(t *testing.T) {
		got := conversation.ThreadKey("", "")
		want := ":"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestSaveAndGetThreadMessages(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	threadKey := "C123:1234567890.123456"

	// Clean up any leftover data from previous test runs.
	_, _ = pool.Exec(ctx, "DELETE FROM thread_messages WHERE thread_key = $1", threadKey)

	// Save messages with a small delay to ensure ordering.
	err := conversation.SaveThreadMessage(ctx, pool, threadKey, "user", "Hello", "")
	if err != nil {
		t.Fatalf("saving first message: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	err = conversation.SaveThreadMessage(ctx, pool, threadKey, "assistant", "Hi there!", "")
	if err != nil {
		t.Fatalf("saving second message: %v", err)
	}

	// Retrieve messages.
	messages, err := conversation.GetThreadMessages(ctx, pool, threadKey, 50)
	if err != nil {
		t.Fatalf("getting messages: %v", err)
	}

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	// Verify ordering is ascending by created_at.
	if !messages[0].CreatedAt.Before(messages[1].CreatedAt) {
		t.Error("expected messages ordered ascending by created_at")
	}

	if messages[0].Role != "user" || messages[0].Content != "Hello" {
		t.Errorf("first message: got role=%q content=%q, want role=user content=Hello", messages[0].Role, messages[0].Content)
	}

	if messages[1].Role != "assistant" || messages[1].Content != "Hi there!" {
		t.Errorf("second message: got role=%q content=%q, want role=assistant content='Hi there!'", messages[1].Role, messages[1].Content)
	}
}

func TestGetThreadMessages_Limit(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	threadKey := "C456:9999999999.999999"

	// Clean up any leftover data from previous test runs.
	_, _ = pool.Exec(ctx, "DELETE FROM thread_messages WHERE thread_key = $1", threadKey)

	// Save 3 messages.
	for i, msg := range []string{"one", "two", "three"} {
		_ = i
		if err := conversation.SaveThreadMessage(ctx, pool, threadKey, "user", msg, ""); err != nil {
			t.Fatalf("saving message %q: %v", msg, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Retrieve with limit 2.
	messages, err := conversation.GetThreadMessages(ctx, pool, threadKey, 2)
	if err != nil {
		t.Fatalf("getting messages: %v", err)
	}

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages with limit, got %d", len(messages))
	}

	// Should get the newest 2 messages, returned in chronological order.
	if messages[0].Content != "two" {
		t.Errorf("expected first message 'two', got %q", messages[0].Content)
	}
	if messages[1].Content != "three" {
		t.Errorf("expected second message 'three', got %q", messages[1].Content)
	}
}

func TestCheckParticipation(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	threadKey := "C789:1111111111.111111"

	_, _ = pool.Exec(ctx, "DELETE FROM thread_messages WHERE thread_key = $1", threadKey)

	// No messages — both should be false.
	p, err := conversation.CheckParticipation(ctx, pool, threadKey, 3*24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Any || p.Recent {
		t.Errorf("expected both false for empty thread, got any=%v recent=%v", p.Any, p.Recent)
	}

	// Add a user message only — still both false.
	saveErr := conversation.SaveThreadMessage(ctx, pool, threadKey, "user", "hello", "")
	if saveErr != nil {
		t.Fatalf("saving user message: %v", saveErr)
	}

	p, err = conversation.CheckParticipation(ctx, pool, threadKey, 3*24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Any || p.Recent {
		t.Errorf("expected both false with only user messages, got any=%v recent=%v", p.Any, p.Recent)
	}

	// Add an assistant message — both should be true.
	saveErr = conversation.SaveThreadMessage(ctx, pool, threadKey, "assistant", "hi there", "")
	if saveErr != nil {
		t.Fatalf("saving assistant message: %v", saveErr)
	}

	p, err = conversation.CheckParticipation(ctx, pool, threadKey, 3*24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !p.Any || !p.Recent {
		t.Errorf("expected both true with recent assistant message, got any=%v recent=%v", p.Any, p.Recent)
	}

	// With a very short maxAge — Any=true but Recent=false.
	p, err = conversation.CheckParticipation(ctx, pool, threadKey, time.Nanosecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !p.Any {
		t.Error("expected Any=true with nanosecond maxAge")
	}
	if p.Recent {
		t.Error("expected Recent=false with nanosecond maxAge")
	}
}

func TestGetThreadMessages_Empty(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	messages, err := conversation.GetThreadMessages(ctx, pool, "nonexistent:thread", 50)
	if err != nil {
		t.Fatalf("getting messages for nonexistent thread: %v", err)
	}

	if len(messages) != 0 {
		t.Errorf("expected 0 messages for nonexistent thread, got %d", len(messages))
	}
}

func TestSaveThreadMessages_StoresUserID(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	threadKey := "CUSERID:1234567890.111111"
	_, _ = pool.Exec(ctx, "DELETE FROM thread_messages WHERE thread_key = $1", threadKey)

	err := conversation.SaveThreadMessages(ctx, pool, threadKey, "user", "hello from user", "assistant", "hi back", "UTEST789")
	if err != nil {
		t.Fatalf("saving thread messages: %v", err)
	}

	messages, err := conversation.GetThreadMessages(ctx, pool, threadKey, 50)
	if err != nil {
		t.Fatalf("getting messages: %v", err)
	}

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	if messages[0].UserID != "UTEST789" {
		t.Errorf("expected user message user_id=UTEST789, got %q", messages[0].UserID)
	}
	if messages[1].UserID != "" {
		t.Errorf("expected assistant message user_id empty, got %q", messages[1].UserID)
	}
}

func TestSaveThreadMessage_StoresUserID(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	threadKey := "CUSERID:1234567890.222222"
	_, _ = pool.Exec(ctx, "DELETE FROM thread_messages WHERE thread_key = $1", threadKey)

	err := conversation.SaveThreadMessage(ctx, pool, threadKey, "user", "hello", "UTEST456")
	if err != nil {
		t.Fatalf("saving message: %v", err)
	}

	messages, err := conversation.GetThreadMessages(ctx, pool, threadKey, 50)
	if err != nil {
		t.Fatalf("getting messages: %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}

	if messages[0].UserID != "UTEST456" {
		t.Errorf("expected user_id=UTEST456, got %q", messages[0].UserID)
	}
}
