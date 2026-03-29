package channel_test

import (
	"context"
	"testing"

	"github.com/bryanneva/ponko/internal/channel"
	"github.com/bryanneva/ponko/internal/testutil"
)

func TestEffectiveRespondMode(t *testing.T) {
	t.Run("nil receiver returns mention_only", func(t *testing.T) {
		var cfg *channel.Config
		got := cfg.EffectiveRespondMode()
		if got != "mention_only" {
			t.Errorf("expected mention_only, got %q", got)
		}
	})

	t.Run("empty RespondMode returns mention_only", func(t *testing.T) {
		cfg := &channel.Config{RespondMode: ""}
		got := cfg.EffectiveRespondMode()
		if got != "mention_only" {
			t.Errorf("expected mention_only, got %q", got)
		}
	})

	t.Run("mention_only returns mention_only", func(t *testing.T) {
		cfg := &channel.Config{RespondMode: "mention_only"}
		got := cfg.EffectiveRespondMode()
		if got != "mention_only" {
			t.Errorf("expected mention_only, got %q", got)
		}
	})

	t.Run("all_messages returns all_messages", func(t *testing.T) {
		cfg := &channel.Config{RespondMode: "all_messages"}
		got := cfg.EffectiveRespondMode()
		if got != "all_messages" {
			t.Errorf("expected all_messages, got %q", got)
		}
	})
}

func requireConfig(t *testing.T, cfg *channel.Config) channel.Config {
	t.Helper()
	if cfg == nil {
		t.Fatal("expected config, got nil")
		return channel.Config{}
	}
	return *cfg
}

func TestGetConfig_UnknownChannel(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	cfg, err := channel.GetConfig(ctx, pool, "C_NONEXISTENT")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config for unknown channel, got %+v", cfg)
	}
}

func TestGetConfig_SeededChannel(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	_, _ = pool.Exec(ctx, "DELETE FROM channel_configs WHERE channel_id = 'C_TEST_SEEDED'")
	_, err := pool.Exec(ctx,
		"INSERT INTO channel_configs (channel_id, system_prompt, tool_allowlist) VALUES ($1, $2, $3)",
		"C_TEST_SEEDED", "You are a test bot.", []string{"search_thoughts", "capture_thought"},
	)
	if err != nil {
		t.Fatalf("seeding channel config: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM channel_configs WHERE channel_id = 'C_TEST_SEEDED'")
	})

	cfgPtr, err := channel.GetConfig(ctx, pool, "C_TEST_SEEDED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg := requireConfig(t, cfgPtr)
	if cfg.SystemPrompt != "You are a test bot." {
		t.Errorf("expected system prompt 'You are a test bot.', got %q", cfg.SystemPrompt)
	}
	if len(cfg.ToolAllowlist) != 2 {
		t.Fatalf("expected 2 tools in allowlist, got %d", len(cfg.ToolAllowlist))
	}
	if cfg.ToolAllowlist[0] != "search_thoughts" || cfg.ToolAllowlist[1] != "capture_thought" {
		t.Errorf("unexpected allowlist: %v", cfg.ToolAllowlist)
	}
}

func TestGetConfig_NullAllowlist(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	_, _ = pool.Exec(ctx, "DELETE FROM channel_configs WHERE channel_id = 'C_TEST_NULL_AL'")
	_, err := pool.Exec(ctx,
		"INSERT INTO channel_configs (channel_id, system_prompt) VALUES ($1, $2)",
		"C_TEST_NULL_AL", "All tools allowed.",
	)
	if err != nil {
		t.Fatalf("seeding channel config: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM channel_configs WHERE channel_id = 'C_TEST_NULL_AL'")
	})

	cfgPtr, err := channel.GetConfig(ctx, pool, "C_TEST_NULL_AL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg := requireConfig(t, cfgPtr)
	if cfg.ToolAllowlist != nil {
		t.Errorf("expected nil allowlist for NULL column, got %v", cfg.ToolAllowlist)
	}
}

func TestGetConfig_EmptyAllowlist(t *testing.T) {
	pool := testutil.TestDB(t)
	ctx := context.Background()

	_, _ = pool.Exec(ctx, "DELETE FROM channel_configs WHERE channel_id = 'C_TEST_EMPTY_AL'")
	_, err := pool.Exec(ctx,
		"INSERT INTO channel_configs (channel_id, system_prompt, tool_allowlist) VALUES ($1, $2, $3)",
		"C_TEST_EMPTY_AL", "No tools.", []string{},
	)
	if err != nil {
		t.Fatalf("seeding channel config: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM channel_configs WHERE channel_id = 'C_TEST_EMPTY_AL'")
	})

	cfgPtr, err := channel.GetConfig(ctx, pool, "C_TEST_EMPTY_AL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg := requireConfig(t, cfgPtr)
	if cfg.ToolAllowlist == nil {
		t.Error("expected non-nil empty allowlist, got nil")
	}
	if len(cfg.ToolAllowlist) != 0 {
		t.Errorf("expected empty allowlist, got %v", cfg.ToolAllowlist)
	}
}
