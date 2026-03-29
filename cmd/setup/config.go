package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Bot           BotConfig           `yaml:"bot"`
	Slack         SlackConfig         `yaml:"slack"`
	AI            AIConfig            `yaml:"ai"`
	Dashboard     DashboardConfig     `yaml:"dashboard"`
	MCP           MCPConfig           `yaml:"mcp"`
	GitHub        GitHubConfig        `yaml:"github"`
	Linear        LinearConfig        `yaml:"linear"`
	Observability ObservabilityConfig `yaml:"observability"`
	Deploy        DeployConfig        `yaml:"deploy"`
}

type BotConfig struct {
	Name         string `yaml:"name"`
	Timezone     string `yaml:"timezone"`
	SystemPrompt string `yaml:"system_prompt"`
}

type SlackConfig struct {
	BotToken      string `yaml:"bot_token"`
	SigningSecret string `yaml:"signing_secret"`
	BotUserID     string `yaml:"bot_user_id"`
}

type AIConfig struct {
	AnthropicAPIKey string `yaml:"anthropic_api_key"`
}

type DeployConfig struct {
	Platform         string `yaml:"platform"`
	AppName          string `yaml:"app_name"`
	Region           string `yaml:"region"`
	URL              string `yaml:"url"`
	APIKey           string `yaml:"api_key"`
	CookieSigningKey string `yaml:"cookie_signing_key"`
	WorkerConcurrency int   `yaml:"worker_concurrency"`
}

type DashboardConfig struct {
	SlackClientID     string `yaml:"slack_client_id"`
	SlackClientSecret string `yaml:"slack_client_secret"`
	SlackTeamID       string `yaml:"slack_team_id"`
}

type MCPConfig struct {
	ServerURLs string `yaml:"server_urls"`
	AccessKey  string `yaml:"access_key"`
}

type GitHubConfig struct {
	MCPURL string `yaml:"mcp_url"`
	PAT    string `yaml:"pat"`
}

type LinearConfig struct {
	MCPURL       string `yaml:"mcp_url"`
	AccessToken  string `yaml:"access_token"`
	TokenURL     string `yaml:"token_url"`
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	RefreshToken string `yaml:"refresh_token"`
}

type ObservabilityConfig struct {
	OTELEndpoint string `yaml:"otel_endpoint"`
	OTELExporter string `yaml:"otel_exporter"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

func saveConfig(cfg *Config, path string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

func generateSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}

func writeManifest(botName, path string) error {
	manifest := fmt.Sprintf(`{
  "display_information": {
    "name": %q,
    "description": "A self-hosted AI agent that lives in Slack",
    "background_color": "#1a1a2e"
  },
  "features": {
    "bot_user": {
      "display_name": %q,
      "always_online": false
    }
  },
  "oauth_config": {
    "scopes": {
      "bot": [
        "app_mentions:read",
        "chat:write",
        "reactions:write",
        "channels:history",
        "channels:read",
        "users:read"
      ],
      "user": [
        "identity.basic"
      ]
    }
  },
  "settings": {
    "event_subscriptions": {
      "request_url": "https://YOUR-APP.fly.dev/slack/events",
      "bot_events": [
        "app_mention"
      ]
    },
    "interactivity": {
      "is_enabled": false
    },
    "org_deploy_enabled": false,
    "socket_mode_enabled": false,
    "token_rotation_enabled": false
  }
}
`, botName, botName)
	return os.WriteFile(path, []byte(manifest), 0644)
}

func defaultConfig() *Config {
	return &Config{
		Bot: BotConfig{
			Name:     "Ponko",
			Timezone: "America/Los_Angeles",
		},
		Deploy: DeployConfig{
			Platform:          "fly",
			Region:            "iad",
			WorkerConcurrency: 10,
		},
	}
}
