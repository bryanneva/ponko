package user_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bryanneva/ponko/internal/slack"
	"github.com/bryanneva/ponko/internal/testutil"
	"github.com/bryanneva/ponko/internal/user"
)

func setupMockSlack(t *testing.T, callCount *atomic.Int32) *slack.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users.info" {
			http.NotFound(w, r)
			return
		}
		callCount.Add(1)
		userID := r.URL.Query().Get("user")
		resp := map[string]any{
			"ok": true,
			"user": map[string]any{
				"tz":       "America/New_York",
				"is_admin": false,
				"profile": map[string]string{
					"display_name": "User " + userID,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return slack.NewClient("test-token", srv.URL)
}

func TestGetOrFetch_InsertsNewUser(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	testutil.EnsureUsersTable(t, pool)
	_, _ = pool.Exec(ctx, "DELETE FROM users WHERE slack_user_id = 'UNEW001'")

	var callCount atomic.Int32
	slackClient := setupMockSlack(t, &callCount)

	store := &user.Store{Pool: pool, Slack: slackClient}
	u, err := store.GetOrFetch(ctx, "UNEW001")
	if err != nil {
		t.Fatalf("GetOrFetch: %v", err)
	}

	if u.SlackUserID != "UNEW001" {
		t.Errorf("SlackUserID = %q, want UNEW001", u.SlackUserID)
	}
	if u.DisplayName != "User UNEW001" {
		t.Errorf("DisplayName = %q, want 'User UNEW001'", u.DisplayName)
	}
	if u.Timezone != "America/New_York" {
		t.Errorf("Timezone = %q, want America/New_York", u.Timezone)
	}
	if callCount.Load() != 1 {
		t.Errorf("Slack API calls = %d, want 1", callCount.Load())
	}
}

func TestGetOrFetch_ReturnsCachedUser(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	testutil.EnsureUsersTable(t, pool)
	_, _ = pool.Exec(ctx, "DELETE FROM users WHERE slack_user_id = 'UCACHE01'")

	var callCount atomic.Int32
	slackClient := setupMockSlack(t, &callCount)

	store := &user.Store{Pool: pool, Slack: slackClient}

	// First call — hits Slack
	_, err := store.GetOrFetch(ctx, "UCACHE01")
	if err != nil {
		t.Fatalf("first GetOrFetch: %v", err)
	}

	// Second call — should use cache
	u, err := store.GetOrFetch(ctx, "UCACHE01")
	if err != nil {
		t.Fatalf("second GetOrFetch: %v", err)
	}

	if u.DisplayName != "User UCACHE01" {
		t.Errorf("DisplayName = %q, want 'User UCACHE01'", u.DisplayName)
	}
	if callCount.Load() != 1 {
		t.Errorf("Slack API calls = %d, want 1 (should have used cache)", callCount.Load())
	}
}

func TestGetOrFetch_RefreshesExpiredCache(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	testutil.EnsureUsersTable(t, pool)
	_, _ = pool.Exec(ctx, "DELETE FROM users WHERE slack_user_id = 'UEXPIRE1'")

	var callCount atomic.Int32
	slackClient := setupMockSlack(t, &callCount)

	store := &user.Store{Pool: pool, Slack: slackClient}

	// Insert with old cached_at
	_, _ = pool.Exec(ctx,
		"INSERT INTO users (slack_user_id, display_name, timezone, is_admin, cached_at) VALUES ($1, $2, $3, $4, $5)",
		"UEXPIRE1", "Old Name", "America/Chicago", false, time.Now().Add(-25*time.Hour),
	)

	u, err := store.GetOrFetch(ctx, "UEXPIRE1")
	if err != nil {
		t.Fatalf("GetOrFetch: %v", err)
	}

	if u.DisplayName != "User UEXPIRE1" {
		t.Errorf("DisplayName = %q, want 'User UEXPIRE1' (should have refreshed)", u.DisplayName)
	}
	if callCount.Load() != 1 {
		t.Errorf("Slack API calls = %d, want 1", callCount.Load())
	}
}
