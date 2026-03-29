package user

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bryanneva/ponko/internal/slack"
)

const cacheTTL = 24 * time.Hour

type User struct {
	CachedAt    time.Time
	SlackUserID string
	DisplayName string
	Timezone    string
	IsAdmin     bool
}

type Store struct {
	Pool  *pgxpool.Pool
	Slack *slack.Client
}

func (s *Store) GetOrFetch(ctx context.Context, slackUserID string) (*User, error) {
	var u User
	err := s.Pool.QueryRow(ctx,
		"SELECT slack_user_id, display_name, timezone, is_admin, cached_at FROM users WHERE slack_user_id = $1",
		slackUserID,
	).Scan(&u.SlackUserID, &u.DisplayName, &u.Timezone, &u.IsAdmin, &u.CachedAt)

	if err == nil && time.Since(u.CachedAt) < cacheTTL {
		return &u, nil
	}
	if err != nil && err != pgx.ErrNoRows {
		return nil, fmt.Errorf("querying user cache: %w", err)
	}

	profile, fetchErr := s.Slack.GetUserProfile(ctx, slackUserID)
	if fetchErr != nil {
		return nil, fmt.Errorf("fetching user profile from Slack: %w", fetchErr)
	}

	u = User{
		SlackUserID: slackUserID,
		DisplayName: profile.DisplayName,
		Timezone:    profile.Timezone,
		IsAdmin:     profile.IsAdmin,
	}

	err = s.Pool.QueryRow(ctx,
		`INSERT INTO users (slack_user_id, display_name, timezone, is_admin, cached_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (slack_user_id) DO UPDATE SET display_name = $2, timezone = $3, is_admin = $4, cached_at = now()
		RETURNING cached_at`,
		slackUserID, u.DisplayName, u.Timezone, u.IsAdmin,
	).Scan(&u.CachedAt)
	if err != nil {
		return nil, fmt.Errorf("upserting user cache: %w", err)
	}

	return &u, nil
}
