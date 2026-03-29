package schedule_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bryanneva/ponko/internal/schedule"
	"github.com/bryanneva/ponko/internal/slack"
	"github.com/bryanneva/ponko/internal/testutil"
	"github.com/bryanneva/ponko/internal/user"
)

func setupSlackUserServer(isAdmin bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.URL.Query().Get("user")
		admin := "false"
		if isAdmin {
			admin = "true"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ok": true,
			"user": {
				"tz": "America/Los_Angeles",
				"is_admin": ` + admin + `,
				"profile": {
					"display_name": "User ` + userID + `"
				}
			}
		}`))
	}))
}

func TestCreateWithQuota(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM scheduled_messages WHERE channel_id = 'C_QUOTA_TEST'")
	})

	cron := "0 9 * * 1-5"
	for i := range 10 {
		err := schedule.CreateWithQuota(ctx, pool, schedule.Message{
			ChannelID:    "C_QUOTA_TEST",
			Prompt:       "quota test",
			Slug:         fmt.Sprintf("qt-%d", i),
			ScheduleCron: &cron,
			NextRunAt:    time.Now().Add(time.Hour),
		}, 10)
		if err != nil {
			t.Fatalf("CreateWithQuota %d: %v", i, err)
		}
	}

	err := schedule.CreateWithQuota(ctx, pool, schedule.Message{
		ChannelID:    "C_QUOTA_TEST",
		Prompt:       "over quota",
		Slug:         "over",
		ScheduleCron: &cron,
		NextRunAt:    time.Now().Add(time.Hour),
	}, 10)
	if err == nil {
		t.Fatal("expected quota error, got nil")
	}
	if !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("expected quota exceeded error, got: %v", err)
	}
}

func TestCreateWithQuota_SlugCollision(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM scheduled_messages WHERE channel_id = 'C_SLUG_COLL'")
	})

	cron := "0 9 * * *"
	err := schedule.CreateWithQuota(ctx, pool, schedule.Message{
		ChannelID:    "C_SLUG_COLL",
		Prompt:       "first",
		Slug:         "daily-check",
		ScheduleCron: &cron,
		NextRunAt:    time.Now().Add(time.Hour),
	}, 10)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	err = schedule.CreateWithQuota(ctx, pool, schedule.Message{
		ChannelID:    "C_SLUG_COLL",
		Prompt:       "second",
		Slug:         "daily-check",
		ScheduleCron: &cron,
		NextRunAt:    time.Now().Add(time.Hour),
	}, 10)
	if err != nil {
		t.Fatalf("second create (should auto-suffix): %v", err)
	}

	msgs, err := schedule.ListByChannel(ctx, pool, "C_SLUG_COLL")
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
}

func TestGetBySlug(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM scheduled_messages WHERE channel_id = 'C_GETSLUG'")
	})

	cron := "0 9 * * *"
	err := schedule.CreateWithQuota(ctx, pool, schedule.Message{
		ChannelID:    "C_GETSLUG",
		Prompt:       "find me",
		Slug:         "find-me",
		ScheduleCron: &cron,
		NextRunAt:    time.Now().Add(time.Hour),
	}, 10)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	msg, err := schedule.GetBySlug(ctx, pool, "C_GETSLUG", "find-me")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if msg.Prompt != "find me" {
		t.Errorf("expected prompt 'find me', got %q", msg.Prompt)
	}
	if msg.Slug != "find-me" {
		t.Errorf("expected slug 'find-me', got %q", msg.Slug)
	}
}

func TestGetBySlug_NotFound(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	_, err := schedule.GetBySlug(ctx, pool, "C_NONEXIST", "nope")
	if err == nil {
		t.Fatal("expected error for missing slug")
	}
	if !strings.Contains(err.Error(), "no active schedule") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExecuteCreateSchedule(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM scheduled_messages WHERE channel_id = 'C_EXEC_CREATE'")
	})

	result, err := schedule.ExecuteCreateSchedule(ctx, pool, schedule.CreateScheduleInput{
		Prompt:         "standup check",
		CronExpression: "0 9 * * 1-5",
		ChannelID:      "C_EXEC_CREATE",
		Timezone:       "America/Los_Angeles",
		Slug:           "standup-check",
	}, "U_CREATOR")
	if err != nil {
		t.Fatalf("ExecuteCreateSchedule: %v", err)
	}
	if !strings.Contains(result, "Schedule created!") {
		t.Errorf("expected confirmation, got: %s", result)
	}
	if !strings.Contains(result, "standup-check") {
		t.Errorf("expected slug in confirmation, got: %s", result)
	}
	if !strings.Contains(result, "every weekday") {
		t.Errorf("expected human-readable cron, got: %s", result)
	}

	msg, err := schedule.GetBySlug(ctx, pool, "C_EXEC_CREATE", "standup-check")
	if err != nil {
		t.Fatalf("verifying DB row: %v", err)
	}
	if msg.CreatedBy == nil || *msg.CreatedBy != "U_CREATOR" {
		t.Errorf("expected created_by U_CREATOR, got %v", msg.CreatedBy)
	}
}

func TestExecuteListSchedules_Empty(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()
	testutil.EnsureUsersTable(t, pool)
	slackServer := setupSlackUserServer(false)
	defer slackServer.Close()

	store := &user.Store{Pool: pool, Slack: slack.NewClient("test-token", slackServer.URL)}

	result, err := schedule.ExecuteListSchedules(ctx, pool, store, schedule.ListSchedulesInput{
		ChannelID: "C_EMPTY_LIST",
	})
	if err != nil {
		t.Fatalf("ExecuteListSchedules: %v", err)
	}
	if result != "No scheduled messages in this channel." {
		t.Errorf("expected empty state, got: %s", result)
	}
}

func TestExecuteListSchedules_WithSchedules(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()
	testutil.EnsureUsersTable(t, pool)
	slackServer := setupSlackUserServer(false)
	defer slackServer.Close()

	store := &user.Store{Pool: pool, Slack: slack.NewClient("test-token", slackServer.URL)}

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM scheduled_messages WHERE channel_id = 'C_LIST_EXEC'")
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE slack_user_id = 'U_LIST_CREATOR'")
	})

	cron := "0 9 * * 1-5"
	creator := "U_LIST_CREATOR"
	err := schedule.CreateWithQuota(ctx, pool, schedule.Message{
		ChannelID:    "C_LIST_EXEC",
		Prompt:       "standup check",
		Slug:         "standup",
		ScheduleCron: &cron,
		CreatedBy:    &creator,
		NextRunAt:    time.Now().Add(time.Hour),
	}, 10)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	result, err := schedule.ExecuteListSchedules(ctx, pool, store, schedule.ListSchedulesInput{
		ChannelID: "C_LIST_EXEC",
	})
	if err != nil {
		t.Fatalf("ExecuteListSchedules: %v", err)
	}
	if !strings.Contains(result, "standup") {
		t.Errorf("expected slug in output, got: %s", result)
	}
	if !strings.Contains(result, "Active schedules (1)") {
		t.Errorf("expected count header, got: %s", result)
	}
}

func TestExecuteCancelSchedule_Creator(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()
	testutil.EnsureUsersTable(t, pool)
	slackServer := setupSlackUserServer(false)
	defer slackServer.Close()

	store := &user.Store{Pool: pool, Slack: slack.NewClient("test-token", slackServer.URL)}

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM scheduled_messages WHERE channel_id = 'C_CANCEL_EXEC'")
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE slack_user_id LIKE 'U_CANCEL_%'")
	})

	cron := "0 9 * * *"
	creator := "U_CANCEL_CREATOR"
	err := schedule.CreateWithQuota(ctx, pool, schedule.Message{
		ChannelID:    "C_CANCEL_EXEC",
		Prompt:       "cancel me",
		Slug:         "cancel-me",
		ScheduleCron: &cron,
		CreatedBy:    &creator,
		NextRunAt:    time.Now().Add(time.Hour),
	}, 10)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	result, err := schedule.ExecuteCancelSchedule(ctx, pool, store, schedule.CancelScheduleInput{
		ChannelID:        "C_CANCEL_EXEC",
		Slug:             "cancel-me",
		RequestingUserID: "U_CANCEL_CREATOR",
	})
	if err != nil {
		t.Fatalf("ExecuteCancelSchedule: %v", err)
	}
	if !strings.Contains(result, "Schedule cancelled!") {
		t.Errorf("expected cancellation confirmation, got: %s", result)
	}

	_, err = schedule.GetBySlug(ctx, pool, "C_CANCEL_EXEC", "cancel-me")
	if err == nil {
		t.Error("expected schedule to be disabled after cancel")
	}
}

func TestExecuteCancelSchedule_Unauthorized(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()
	testutil.EnsureUsersTable(t, pool)
	slackServer := setupSlackUserServer(false)
	defer slackServer.Close()

	store := &user.Store{Pool: pool, Slack: slack.NewClient("test-token", slackServer.URL)}

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM scheduled_messages WHERE channel_id = 'C_CANCEL_UNAUTH'")
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE slack_user_id LIKE 'U_UNAUTH_%'")
	})

	cron := "0 9 * * *"
	creator := "U_UNAUTH_CREATOR"
	err := schedule.CreateWithQuota(ctx, pool, schedule.Message{
		ChannelID:    "C_CANCEL_UNAUTH",
		Prompt:       "not yours",
		Slug:         "not-yours",
		ScheduleCron: &cron,
		CreatedBy:    &creator,
		NextRunAt:    time.Now().Add(time.Hour),
	}, 10)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	result, err := schedule.ExecuteCancelSchedule(ctx, pool, store, schedule.CancelScheduleInput{
		ChannelID:        "C_CANCEL_UNAUTH",
		Slug:             "not-yours",
		RequestingUserID: "U_UNAUTH_OTHER",
	})
	if err != nil {
		t.Fatalf("ExecuteCancelSchedule: %v", err)
	}
	if !strings.Contains(result, "don't have permission") {
		t.Errorf("expected permission denied, got: %s", result)
	}

	// Verify schedule is still active
	msg, err := schedule.GetBySlug(ctx, pool, "C_CANCEL_UNAUTH", "not-yours")
	if err != nil {
		t.Fatalf("schedule should still be active: %v", err)
	}
	if !msg.Enabled {
		t.Error("schedule should still be enabled")
	}
}

func TestExecuteCancelSchedule_Admin(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()
	testutil.EnsureUsersTable(t, pool)
	slackServer := setupSlackUserServer(true)
	defer slackServer.Close()

	store := &user.Store{Pool: pool, Slack: slack.NewClient("test-token", slackServer.URL)}

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM scheduled_messages WHERE channel_id = 'C_CANCEL_ADMIN'")
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE slack_user_id LIKE 'U_ADMIN_%'")
	})

	cron := "0 9 * * *"
	creator := "U_ADMIN_CREATOR"
	err := schedule.CreateWithQuota(ctx, pool, schedule.Message{
		ChannelID:    "C_CANCEL_ADMIN",
		Prompt:       "admin can cancel",
		Slug:         "admin-cancel",
		ScheduleCron: &cron,
		CreatedBy:    &creator,
		NextRunAt:    time.Now().Add(time.Hour),
	}, 10)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	result, err := schedule.ExecuteCancelSchedule(ctx, pool, store, schedule.CancelScheduleInput{
		ChannelID:        "C_CANCEL_ADMIN",
		Slug:             "admin-cancel",
		RequestingUserID: "U_ADMIN_ADMIN",
	})
	if err != nil {
		t.Fatalf("ExecuteCancelSchedule: %v", err)
	}
	if !strings.Contains(result, "Schedule cancelled!") {
		t.Errorf("expected admin to cancel successfully, got: %s", result)
	}
}

func TestExecuteCancelSchedule_NotFound(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()
	testutil.EnsureUsersTable(t, pool)
	slackServer := setupSlackUserServer(false)
	defer slackServer.Close()

	store := &user.Store{Pool: pool, Slack: slack.NewClient("test-token", slackServer.URL)}

	result, err := schedule.ExecuteCancelSchedule(ctx, pool, store, schedule.CancelScheduleInput{
		ChannelID:        "C_NONEXIST",
		Slug:             "nope",
		RequestingUserID: "U_ANYONE",
	})
	if err != nil {
		t.Fatalf("ExecuteCancelSchedule: %v", err)
	}
	if !strings.Contains(result, "No active schedule") {
		t.Errorf("expected not-found message, got: %s", result)
	}
}
