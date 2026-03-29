package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
)

func deployFly(cfg *Config) error {
	if err := checkCommand("fly"); err != nil {
		return err
	}

	if cfg.Deploy.AppName == "" {
		suffix := generateSecret()[:4]
		suggested := "ponko-" + suffix

		err := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Fly.io app name").
					Placeholder(suggested).
					Value(&cfg.Deploy.AppName),
			),
		).WithTheme(huh.ThemeDracula()).Run()
		if err != nil {
			return err
		}
		if cfg.Deploy.AppName == "" {
			cfg.Deploy.AppName = suggested
		}
	}

	if cfg.Deploy.URL == "" {
		cfg.Deploy.URL = "https://" + cfg.Deploy.AppName + ".fly.dev"
	}

	appName := cfg.Deploy.AppName
	region := cfg.Deploy.Region
	if region == "" {
		region = "iad"
	}

	// Check fly auth
	if err := runCmd("fly", "auth", "whoami"); err != nil {
		fmt.Println("Not logged in to Fly.io — opening login...")
		if err := runCmd("fly", "auth", "login"); err != nil {
			return fmt.Errorf("fly auth login: %w", err)
		}
	}

	// Cache app list for existence checks
	appList, _ := cmdOutput("fly", "apps", "list")
	appExists := func(name string) bool {
		for _, line := range strings.Split(appList, "\n") {
			fields := strings.Fields(line)
			if len(fields) > 0 && fields[0] == name {
				return true
			}
		}
		return false
	}

	// Create app
	if appExists(appName) {
		fmt.Printf("App %s already exists, skipping creation.\n", appName)
	} else {
		fmt.Printf("Creating app: %s\n", appName)
		if err := runCmd("fly", "apps", "create", appName, "--machines"); err != nil {
			return fmt.Errorf("creating app: %w", err)
		}
	}

	// Create Postgres
	dbName := appName + "-db"
	if appExists(dbName) {
		fmt.Printf("Database %s already exists, skipping creation.\n", dbName)
	} else {
		fmt.Printf("Creating Postgres: %s\n", dbName)
		if err := runCmd("fly", "postgres", "create",
			"--name", dbName,
			"--region", region,
			"--initial-cluster-size", "1",
			"--vm-size", "shared-cpu-1x",
			"--volume-size", "1",
		); err != nil {
			return fmt.Errorf("creating postgres: %w", err)
		}
	}

	// Attach database
	fmt.Println("Attaching database...")
	_ = runCmd("fly", "postgres", "attach", dbName, "--app", appName)

	// Set secrets
	fmt.Println("Setting secrets...")
	secrets := buildSecrets(cfg)
	args := append([]string{"secrets", "set", "-a", appName}, secrets...)
	if err := runCmd("fly", args...); err != nil {
		return fmt.Errorf("setting secrets: %w", err)
	}

	// Save config with generated values
	if err := saveConfig(cfg, configPath); err != nil {
		fmt.Printf("Warning: could not update config file: %v\n", err)
	}

	// Update fly.toml
	flyToml, err := os.ReadFile("fly.toml")
	if err == nil && strings.Contains(string(flyToml), "<your-app>") {
		updated := strings.ReplaceAll(string(flyToml), "<your-app>", appName)
		if writeErr := os.WriteFile("fly.toml", []byte(updated), 0644); writeErr != nil {
			fmt.Printf("Warning: could not update fly.toml: %v\n", writeErr)
		} else {
			fmt.Println("Updated fly.toml with app name.")
		}
	}

	// Deploy
	fmt.Println("Deploying...")
	if err := runCmd("fly", "deploy"); err != nil {
		return fmt.Errorf("deploy failed: %w", err)
	}

	// Health check
	fmt.Println("\nWaiting for health check...")
	healthURL := cfg.Deploy.URL + "/health"
	for i := range 30 {
		resp, err := http.Get(healthURL) //nolint:gosec // URL from user config, not tainted
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			fmt.Println("Health check passed!")
			break
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		if i == 29 {
			fmt.Printf("Health check timed out. Check manually: curl %s\n", healthURL)
		}
		time.Sleep(2 * time.Second)
	}

	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  Setup Complete!")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  1. Set your Slack Event Subscriptions URL to:\n     %s/slack/events\n\n", cfg.Deploy.URL)
	fmt.Printf("  2. Invite the bot to a channel:\n     /invite @%s\n\n", cfg.Bot.Name)
	fmt.Printf("  3. Say hello:\n     @%s hello\n\n", cfg.Bot.Name)
	if cfg.Dashboard.SlackClientID != "" {
		fmt.Printf("  4. Dashboard: %s\n", cfg.Deploy.URL)
		fmt.Printf("     Set OAuth redirect URL to: %s/api/auth/slack/callback\n\n", cfg.Deploy.URL)
	}

	return nil
}

func deployDocker(cfg *Config) error {
	fmt.Println("Generating .env for Docker...")

	var b strings.Builder
	fmt.Fprintf(&b, "DATABASE_URL=postgres://agent:agent@db:5432/agent?sslmode=disable\n")
	fmt.Fprintf(&b, "ANTHROPIC_API_KEY=%s\n", cfg.AI.AnthropicAPIKey)
	fmt.Fprintf(&b, "SLACK_BOT_TOKEN=%s\n", cfg.Slack.BotToken)
	fmt.Fprintf(&b, "SLACK_SIGNING_SECRET=%s\n", cfg.Slack.SigningSecret)
	fmt.Fprintf(&b, "SLACK_BOT_USER_ID=%s\n", cfg.Slack.BotUserID)
	fmt.Fprintf(&b, "PONKO_API_KEY=%s\n", cfg.Deploy.APIKey)
	fmt.Fprintf(&b, "COOKIE_SIGNING_KEY=%s\n", cfg.Deploy.CookieSigningKey)

	fmt.Fprintf(&b, "BOT_NAME=%s\n", cfg.Bot.Name)
	fmt.Fprintf(&b, "TIMEZONE=%s\n", cfg.Bot.Timezone)
	fmt.Fprintf(&b, "WORKER_CONCURRENCY=%d\n", cfg.Deploy.WorkerConcurrency)

	writeEnvIf(&b, "SYSTEM_PROMPT", cfg.Bot.SystemPrompt)
	writeEnvIf(&b, "DASHBOARD_URL", cfg.Deploy.URL)
	writeEnvIf(&b, "SLACK_CLIENT_ID", cfg.Dashboard.SlackClientID)
	writeEnvIf(&b, "SLACK_CLIENT_SECRET", cfg.Dashboard.SlackClientSecret)
	writeEnvIf(&b, "SLACK_TEAM_ID", cfg.Dashboard.SlackTeamID)
	writeEnvIf(&b, "MCP_SERVER_URLS", cfg.MCP.ServerURLs)
	writeEnvIf(&b, "MCP_ACCESS_KEY", cfg.MCP.AccessKey)
	writeEnvIf(&b, "GITHUB_MCP_URL", cfg.GitHub.MCPURL)
	writeEnvIf(&b, "GITHUB_PAT", cfg.GitHub.PAT)
	writeEnvIf(&b, "LINEAR_MCP_URL", cfg.Linear.MCPURL)
	writeEnvIf(&b, "LINEAR_MCP_ACCESS_TOKEN", cfg.Linear.AccessToken)
	writeEnvIf(&b, "LINEAR_MCP_TOKEN_URL", cfg.Linear.TokenURL)
	writeEnvIf(&b, "LINEAR_MCP_CLIENT_ID", cfg.Linear.ClientID)
	writeEnvIf(&b, "LINEAR_MCP_CLIENT_SECRET", cfg.Linear.ClientSecret)
	writeEnvIf(&b, "LINEAR_MCP_REFRESH_TOKEN", cfg.Linear.RefreshToken)
	writeEnvIf(&b, "OTEL_EXPORTER_OTLP_ENDPOINT", cfg.Observability.OTELEndpoint)
	writeEnvIf(&b, "OTEL_EXPORTER", cfg.Observability.OTELExporter)

	if err := saveConfig(cfg, configPath); err != nil {
		fmt.Printf("Warning: could not save config: %v\n", err)
	}

	if err := os.WriteFile(".env", []byte(b.String()), 0600); err != nil {
		return fmt.Errorf("writing .env: %w", err)
	}

	fmt.Println("Generated .env")
	fmt.Println("\nTo start:")
	fmt.Println("  docker compose up -d")
	fmt.Println("\nBot will run at http://localhost:8080")
	fmt.Println("Use ngrok or similar to expose for Slack events.")
	return nil
}

func buildSecrets(cfg *Config) []string {
	var s []string
	add := func(k, v string) {
		if v != "" {
			s = append(s, k+"="+v)
		}
	}

	add("ANTHROPIC_API_KEY", cfg.AI.AnthropicAPIKey)
	add("SLACK_BOT_TOKEN", cfg.Slack.BotToken)
	add("SLACK_SIGNING_SECRET", cfg.Slack.SigningSecret)
	add("SLACK_BOT_USER_ID", cfg.Slack.BotUserID)
	add("PONKO_API_KEY", cfg.Deploy.APIKey)
	add("COOKIE_SIGNING_KEY", cfg.Deploy.CookieSigningKey)
	add("DASHBOARD_URL", cfg.Deploy.URL)

	if cfg.Bot.Name != "" && cfg.Bot.Name != "Ponko" {
		add("BOT_NAME", cfg.Bot.Name)
	}
	if cfg.Bot.Timezone != "" && cfg.Bot.Timezone != "America/Los_Angeles" {
		add("TIMEZONE", cfg.Bot.Timezone)
	}
	if cfg.Deploy.WorkerConcurrency > 0 {
		add("WORKER_CONCURRENCY", fmt.Sprintf("%d", cfg.Deploy.WorkerConcurrency))
	}
	add("SYSTEM_PROMPT", cfg.Bot.SystemPrompt)

	add("SLACK_CLIENT_ID", cfg.Dashboard.SlackClientID)
	add("SLACK_CLIENT_SECRET", cfg.Dashboard.SlackClientSecret)
	add("SLACK_TEAM_ID", cfg.Dashboard.SlackTeamID)

	add("MCP_SERVER_URLS", cfg.MCP.ServerURLs)
	add("MCP_ACCESS_KEY", cfg.MCP.AccessKey)

	add("GITHUB_MCP_URL", cfg.GitHub.MCPURL)
	add("GITHUB_PAT", cfg.GitHub.PAT)

	add("LINEAR_MCP_URL", cfg.Linear.MCPURL)
	add("LINEAR_MCP_ACCESS_TOKEN", cfg.Linear.AccessToken)
	add("LINEAR_MCP_TOKEN_URL", cfg.Linear.TokenURL)
	add("LINEAR_MCP_CLIENT_ID", cfg.Linear.ClientID)
	add("LINEAR_MCP_CLIENT_SECRET", cfg.Linear.ClientSecret)
	add("LINEAR_MCP_REFRESH_TOKEN", cfg.Linear.RefreshToken)

	add("OTEL_EXPORTER_OTLP_ENDPOINT", cfg.Observability.OTELEndpoint)
	add("OTEL_EXPORTER", cfg.Observability.OTELExporter)

	return s
}

func validate(cfg *Config) error {
	failCount := 0
	check := func(name, value string) {
		if value == "" {
			fmt.Printf("  ✗ %s is missing\n", name)
			failCount++
		} else {
			fmt.Printf("  ✓ %s is set\n", name)
		}
	}

	fmt.Println("Validating config...")
	check("slack.bot_token", cfg.Slack.BotToken)
	check("slack.signing_secret", cfg.Slack.SigningSecret)
	check("slack.bot_user_id", cfg.Slack.BotUserID)
	check("ai.anthropic_api_key", cfg.AI.AnthropicAPIKey)

	if cfg.Deploy.URL != "" {
		fmt.Printf("\nChecking health at %s...\n", cfg.Deploy.URL)
		resp, err := http.Get(cfg.Deploy.URL + "/health") //nolint:gosec // URL from user config
		if err != nil {
			fmt.Printf("  ✗ Health check failed: %s/health (%v)\n", cfg.Deploy.URL, err)
			failCount++
		} else {
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				fmt.Printf("  ✗ Health check failed: %s/health (status %d)\n", cfg.Deploy.URL, resp.StatusCode)
				failCount++
			} else {
				fmt.Println("  ✓ Health check passed")
			}
		}
	} else {
		fmt.Println("\n  ⚠ No deploy URL set — skipping health check")
	}

	if failCount > 0 {
		return fmt.Errorf("%d check(s) failed", failCount)
	}
	fmt.Println("\nAll checks passed!")
	return nil
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func cmdOutput(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}

func checkCommand(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("%s not found — install it first", name)
	}
	return nil
}

func writeEnvIf(b *strings.Builder, key, value string) {
	if value != "" {
		fmt.Fprintf(b, "%s=\"%s\"\n", key, value)
	}
}
