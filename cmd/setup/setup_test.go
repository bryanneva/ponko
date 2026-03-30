package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigRoundTrip(t *testing.T) {
	cfg := &Config{
		Bot:   BotConfig{Name: "TestBot", Timezone: "UTC", SystemPrompt: "Be helpful"},
		Slack: SlackConfig{BotToken: "xoxb-test", SigningSecret: "secret123", BotUserID: "U123"},
		AI:    AIConfig{AnthropicAPIKey: "sk-ant-test"},
		Deploy: DeployConfig{
			Platform:          platformFly,
			AppName:           "test-app",
			Region:            "iad",
			URL:               "https://test.fly.dev",
			APIKey:            "apikey123",
			CookieSigningKey:  "cookie123",
			WorkerConcurrency: 5,
		},
		Dashboard: DashboardConfig{SlackClientID: "cid", SlackClientSecret: "csecret", SlackTeamID: "T123"},
		MCP:       MCPConfig{ServerURLs: "https://mcp.example.com", AccessKey: "mcpkey"},
		GitHub:    GitHubConfig{MCPURL: "https://github-mcp.example.com", PAT: "ghp_test"},
		Linear: LinearConfig{
			MCPURL: "https://linear-mcp.example.com", AccessToken: "lin_token",
			TokenURL: "https://linear.app/token", ClientID: "lin_cid",
			ClientSecret: "lin_csecret", RefreshToken: "lin_refresh",
		},
		Observability: ObservabilityConfig{OTELEndpoint: "https://otel.example.com", OTELExporter: "otlp"},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "ponko.yaml")

	if err := saveConfig(cfg, path); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected file mode 0600, got %o", info.Mode().Perm())
	}

	loaded, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if loaded.Bot.Name != cfg.Bot.Name {
		t.Errorf("Bot.Name: got %q, want %q", loaded.Bot.Name, cfg.Bot.Name)
	}
	if loaded.Slack.BotToken != cfg.Slack.BotToken {
		t.Errorf("Slack.BotToken: got %q, want %q", loaded.Slack.BotToken, cfg.Slack.BotToken)
	}
	if loaded.AI.AnthropicAPIKey != cfg.AI.AnthropicAPIKey {
		t.Errorf("AI.AnthropicAPIKey: got %q, want %q", loaded.AI.AnthropicAPIKey, cfg.AI.AnthropicAPIKey)
	}
	if loaded.Deploy.Platform != cfg.Deploy.Platform {
		t.Errorf("Deploy.Platform: got %q, want %q", loaded.Deploy.Platform, cfg.Deploy.Platform)
	}
	if loaded.Deploy.WorkerConcurrency != cfg.Deploy.WorkerConcurrency {
		t.Errorf("Deploy.WorkerConcurrency: got %d, want %d", loaded.Deploy.WorkerConcurrency, cfg.Deploy.WorkerConcurrency)
	}
	if loaded.Linear.MCPURL != cfg.Linear.MCPURL {
		t.Errorf("Linear.MCPURL: got %q, want %q", loaded.Linear.MCPURL, cfg.Linear.MCPURL)
	}
	if loaded.Observability.OTELExporter != cfg.Observability.OTELExporter {
		t.Errorf("Observability.OTELExporter: got %q, want %q", loaded.Observability.OTELExporter, cfg.Observability.OTELExporter)
	}
}

func TestLoadConfigFileNotFound(t *testing.T) {
	_, err := loadConfig("/nonexistent/ponko.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "reading config") {
		t.Errorf("expected 'reading config' in error, got: %v", err)
	}
}

func TestLoadConfigMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("bot:\n  name: [\ninvalid"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := loadConfig(path)
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
	if !strings.Contains(err.Error(), "parsing config") {
		t.Errorf("expected 'parsing config' in error, got: %v", err)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()

	if cfg.Bot.Name != "Ponko" {
		t.Errorf("default Bot.Name: got %q, want %q", cfg.Bot.Name, "Ponko")
	}
	if cfg.Bot.Timezone != "America/Los_Angeles" {
		t.Errorf("default Bot.Timezone: got %q, want %q", cfg.Bot.Timezone, "America/Los_Angeles")
	}
	if cfg.Deploy.Platform != platformFly {
		t.Errorf("default Deploy.Platform: got %q, want %q", cfg.Deploy.Platform, platformFly)
	}
	if cfg.Deploy.Region != "iad" {
		t.Errorf("default Deploy.Region: got %q, want %q", cfg.Deploy.Region, "iad")
	}
	if cfg.Deploy.WorkerConcurrency != 10 {
		t.Errorf("default Deploy.WorkerConcurrency: got %d, want %d", cfg.Deploy.WorkerConcurrency, 10)
	}
}

func TestGenerateSecret(t *testing.T) {
	s := generateSecret()
	if len(s) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(s))
	}

	s2 := generateSecret()
	if s == s2 {
		t.Error("two calls returned the same secret")
	}
}

func TestBuildSecretsRequiredFields(t *testing.T) {
	cfg := &Config{
		Slack:  SlackConfig{BotToken: "xoxb-1", SigningSecret: "sig", BotUserID: "U1"},
		AI:     AIConfig{AnthropicAPIKey: "sk-ant-1"},
		Deploy: DeployConfig{APIKey: "api1", CookieSigningKey: "cookie1", URL: "https://test.fly.dev"},
	}

	secrets := buildSecrets(cfg)
	m := secretsToMap(secrets)

	required := map[string]string{
		"ANTHROPIC_API_KEY":  "sk-ant-1",
		"SLACK_BOT_TOKEN":   "xoxb-1",
		"SLACK_SIGNING_SECRET": "sig",
		"SLACK_BOT_USER_ID": "U1",
		"PONKO_API_KEY":     "api1",
		"COOKIE_SIGNING_KEY": "cookie1",
		"DASHBOARD_URL":     "https://test.fly.dev",
	}

	for k, want := range required {
		got, ok := m[k]
		if !ok {
			t.Errorf("missing required secret: %s", k)
		} else if got != want {
			t.Errorf("%s: got %q, want %q", k, got, want)
		}
	}
}

func TestBuildSecretsOmitsEmptyValues(t *testing.T) {
	cfg := &Config{}
	secrets := buildSecrets(cfg)

	for _, s := range secrets {
		t.Errorf("expected no secrets for empty config, got: %s", s)
	}
}

func TestBuildSecretsOmitsDefaultBotName(t *testing.T) {
	cfg := &Config{
		Bot: BotConfig{Name: "Ponko", Timezone: "America/Los_Angeles"},
	}

	secrets := buildSecrets(cfg)
	m := secretsToMap(secrets)

	if _, ok := m["BOT_NAME"]; ok {
		t.Error("BOT_NAME should be omitted when set to default 'Ponko'")
	}
	if _, ok := m["TIMEZONE"]; ok {
		t.Error("TIMEZONE should be omitted when set to default 'America/Los_Angeles'")
	}
}

func TestBuildSecretsIncludesNonDefaultBotName(t *testing.T) {
	cfg := &Config{
		Bot: BotConfig{Name: "Jarvis", Timezone: "Europe/London"},
	}

	secrets := buildSecrets(cfg)
	m := secretsToMap(secrets)

	if got, ok := m["BOT_NAME"]; !ok || got != "Jarvis" {
		t.Errorf("BOT_NAME: got %q, want %q", got, "Jarvis")
	}
	if got, ok := m["TIMEZONE"]; !ok || got != "Europe/London" {
		t.Errorf("TIMEZONE: got %q, want %q", got, "Europe/London")
	}
}

func TestBuildSecretsWorkerConcurrency(t *testing.T) {
	cfg := &Config{Deploy: DeployConfig{WorkerConcurrency: 20}}
	secrets := buildSecrets(cfg)
	m := secretsToMap(secrets)

	if got, ok := m["WORKER_CONCURRENCY"]; !ok || got != "20" {
		t.Errorf("WORKER_CONCURRENCY: got %q, want %q", got, "20")
	}

	cfg.Deploy.WorkerConcurrency = 0
	secrets = buildSecrets(cfg)
	m = secretsToMap(secrets)

	if _, ok := m["WORKER_CONCURRENCY"]; ok {
		t.Error("WORKER_CONCURRENCY should be omitted when 0")
	}
}

func TestWriteManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	if err := writeManifest("TestBot", path); err != nil {
		t.Fatalf("writeManifest: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading manifest: %v", err)
	}

	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}

	displayInfo := manifest["display_information"].(map[string]any)
	if displayInfo["name"] != "TestBot" {
		t.Errorf("display_information.name: got %q, want %q", displayInfo["name"], "TestBot")
	}

	features := manifest["features"].(map[string]any)
	botUser := features["bot_user"].(map[string]any)
	if botUser["display_name"] != "TestBot" {
		t.Errorf("bot_user.display_name: got %q, want %q", botUser["display_name"], "TestBot")
	}
}

func TestWriteManifestSpecialChars(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	if err := writeManifest(`Bot "With" Quotes`, path); err != nil {
		t.Fatalf("writeManifest: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading manifest: %v", err)
	}

	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("manifest with special chars is not valid JSON: %v", err)
	}

	displayInfo := manifest["display_information"].(map[string]any)
	if displayInfo["name"] != `Bot "With" Quotes` {
		t.Errorf("display_information.name: got %q, want %q", displayInfo["name"], `Bot "With" Quotes`)
	}
}

func TestWriteEnvIf(t *testing.T) {
	var b strings.Builder

	writeEnvIf(&b, "KEY1", "value1")
	writeEnvIf(&b, "EMPTY", "")
	writeEnvIf(&b, "KEY2", "value2")

	got := b.String()
	if !strings.Contains(got, `KEY1="value1"`) {
		t.Errorf("expected KEY1 in output, got: %s", got)
	}
	if strings.Contains(got, "EMPTY") {
		t.Errorf("empty value should be omitted, got: %s", got)
	}
	if !strings.Contains(got, `KEY2="value2"`) {
		t.Errorf("expected KEY2 in output, got: %s", got)
	}
}

func TestValidateAllFieldsMissing(t *testing.T) {
	cfg := &Config{}
	err := validate(cfg)
	if err == nil {
		t.Fatal("expected error for empty config")
	}
	if !strings.Contains(err.Error(), "4 check(s) failed") {
		t.Errorf("expected 4 failures, got: %v", err)
	}
}

func TestValidateAllFieldsPresent(t *testing.T) {
	cfg := &Config{
		Slack: SlackConfig{BotToken: "xoxb-1", SigningSecret: "sig", BotUserID: "U1"},
		AI:    AIConfig{AnthropicAPIKey: "sk-ant-1"},
	}
	err := validate(cfg)
	if err != nil {
		t.Errorf("expected no error for valid config, got: %v", err)
	}
}

func secretsToMap(secrets []string) map[string]string {
	m := make(map[string]string)
	for _, s := range secrets {
		parts := strings.SplitN(s, "=", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	return m
}
