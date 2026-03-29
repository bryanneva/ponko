package schedule_test

import (
	"context"
	"testing"
	"time"

	"github.com/bryanneva/ponko/internal/schedule"
	"github.com/bryanneva/ponko/internal/testutil"
)

func TestCreate(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM scheduled_messages WHERE channel_id = 'C_SCHED_TEST'")
	})

	msg := schedule.Message{
		ChannelID: "C_SCHED_TEST",
		Prompt:    "Good morning!",
		NextRunAt: time.Now().Add(time.Hour),
	}

	if err := schedule.Create(ctx, pool, msg); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var count int
	err := pool.QueryRow(ctx, "SELECT count(*) FROM scheduled_messages WHERE channel_id = 'C_SCHED_TEST'").Scan(&count)
	if err != nil {
		t.Fatalf("counting rows: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}
}

func TestGetDue(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM scheduled_messages WHERE channel_id LIKE 'C_DUE_TEST%'")
	})

	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)

	if err := schedule.Create(ctx, pool, schedule.Message{
		ChannelID: "C_DUE_TEST_1",
		Prompt:    "due message",
		NextRunAt: past,
	}); err != nil {
		t.Fatalf("creating due message: %v", err)
	}

	if err := schedule.Create(ctx, pool, schedule.Message{
		ChannelID: "C_DUE_TEST_2",
		Prompt:    "future message",
		NextRunAt: future,
	}); err != nil {
		t.Fatalf("creating future message: %v", err)
	}

	due, err := schedule.GetDue(ctx, pool)
	if err != nil {
		t.Fatalf("GetDue: %v", err)
	}

	found := false
	for _, m := range due {
		if m.ChannelID == "C_DUE_TEST_1" {
			found = true
		}
		if m.ChannelID == "C_DUE_TEST_2" {
			t.Error("future message should not be due")
		}
	}
	if !found {
		t.Error("expected due message not found")
	}
}

func TestMarkRun_Recurring(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM scheduled_messages WHERE channel_id = 'C_MARK_RECUR'")
	})

	if err := schedule.Create(ctx, pool, schedule.Message{
		ChannelID: "C_MARK_RECUR",
		Prompt:    "recurring",
		NextRunAt: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("creating message: %v", err)
	}

	var id string
	if err := pool.QueryRow(ctx, "SELECT id FROM scheduled_messages WHERE channel_id = 'C_MARK_RECUR'").Scan(&id); err != nil {
		t.Fatalf("getting id: %v", err)
	}

	nextRun := time.Now().Add(24 * time.Hour)
	if err := schedule.MarkRun(ctx, pool, id, &nextRun); err != nil {
		t.Fatalf("MarkRun: %v", err)
	}

	var enabled bool
	var lastRunAt *time.Time
	if err := pool.QueryRow(ctx, "SELECT enabled, last_run_at FROM scheduled_messages WHERE id = $1", id).Scan(&enabled, &lastRunAt); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if !enabled {
		t.Error("recurring message should still be enabled")
	}
	if lastRunAt == nil {
		t.Error("last_run_at should be set")
	}
}

func TestMarkRun_OneShot(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM scheduled_messages WHERE channel_id = 'C_MARK_ONESHOT'")
	})

	if err := schedule.Create(ctx, pool, schedule.Message{
		ChannelID: "C_MARK_ONESHOT",
		Prompt:    "one-shot",
		NextRunAt: time.Now().Add(-time.Hour),
		OneShot:   true,
	}); err != nil {
		t.Fatalf("creating message: %v", err)
	}

	var id string
	if err := pool.QueryRow(ctx, "SELECT id FROM scheduled_messages WHERE channel_id = 'C_MARK_ONESHOT'").Scan(&id); err != nil {
		t.Fatalf("getting id: %v", err)
	}

	if err := schedule.MarkRun(ctx, pool, id, nil); err != nil {
		t.Fatalf("MarkRun: %v", err)
	}

	var enabled bool
	if err := pool.QueryRow(ctx, "SELECT enabled FROM scheduled_messages WHERE id = $1", id).Scan(&enabled); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if enabled {
		t.Error("one-shot message should be disabled after run")
	}
}

func TestCancel(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM scheduled_messages WHERE channel_id = 'C_CANCEL_TEST'")
	})

	if err := schedule.Create(ctx, pool, schedule.Message{
		ChannelID: "C_CANCEL_TEST",
		Prompt:    "cancel me",
		NextRunAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("creating message: %v", err)
	}

	var id string
	if err := pool.QueryRow(ctx, "SELECT id FROM scheduled_messages WHERE channel_id = 'C_CANCEL_TEST'").Scan(&id); err != nil {
		t.Fatalf("getting id: %v", err)
	}

	if err := schedule.Cancel(ctx, pool, id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	var enabled bool
	if err := pool.QueryRow(ctx, "SELECT enabled FROM scheduled_messages WHERE id = $1", id).Scan(&enabled); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if enabled {
		t.Error("cancelled message should be disabled")
	}
}

func TestListByChannel(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM scheduled_messages WHERE channel_id LIKE 'C_LIST_TEST%'")
	})

	for _, prompt := range []string{"msg1", "msg2"} {
		if err := schedule.Create(ctx, pool, schedule.Message{
			ChannelID: "C_LIST_TEST_A",
			Prompt:    prompt,
			NextRunAt: time.Now().Add(time.Hour),
		}); err != nil {
			t.Fatalf("creating message: %v", err)
		}
	}

	if err := schedule.Create(ctx, pool, schedule.Message{
		ChannelID: "C_LIST_TEST_B",
		Prompt:    "other channel",
		NextRunAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("creating message: %v", err)
	}

	if err := schedule.Create(ctx, pool, schedule.Message{
		ChannelID: "C_LIST_TEST_A",
		Prompt:    "disabled",
		NextRunAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("creating message: %v", err)
	}
	_, _ = pool.Exec(ctx, "UPDATE scheduled_messages SET enabled = false WHERE channel_id = 'C_LIST_TEST_A' AND prompt = 'disabled'")

	msgs, err := schedule.ListByChannel(ctx, pool, "C_LIST_TEST_A")
	if err != nil {
		t.Fatalf("ListByChannel: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	for _, m := range msgs {
		if m.ChannelID != "C_LIST_TEST_A" {
			t.Errorf("unexpected channel: %s", m.ChannelID)
		}
	}
}
