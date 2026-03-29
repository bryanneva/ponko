package main

import (
	"fmt"

	"github.com/charmbracelet/huh"
)

func collectRequired(cfg *Config) error {
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Deployment platform").
				Options(
					huh.NewOption("Fly.io (recommended)", "fly"),
					huh.NewOption("Docker (local/self-hosted)", "docker"),
				).
				Value(&cfg.Deploy.Platform),
		).Title("Platform"),

		huh.NewGroup(
			huh.NewNote().
				Title("Slack App").
				Description("You need a Slack app with bot token scopes.\nSee docs/slack-setup.md for setup instructions."),
			huh.NewInput().
				Title("Bot Token").
				Description("Bot User OAuth Token (xoxb-...)").
				Placeholder("xoxb-...").
				Value(&cfg.Slack.BotToken).
				Validate(required("bot token")),
			huh.NewInput().
				Title("Signing Secret").
				Description("App Signing Secret from Basic Information").
				Value(&cfg.Slack.SigningSecret).
				Validate(required("signing secret")),
			huh.NewInput().
				Title("Bot User ID").
				Description("Bot's member ID — click bot profile → ... → Copy member ID").
				Placeholder("U...").
				Value(&cfg.Slack.BotUserID).
				Validate(required("bot user ID")),
		).Title("Slack"),

		huh.NewGroup(
			huh.NewInput().
				Title("Anthropic API Key").
				Description("From console.anthropic.com").
				Placeholder("sk-ant-...").
				EchoMode(huh.EchoModePassword).
				Value(&cfg.AI.AnthropicAPIKey).
				Validate(required("Anthropic API key")),
		).Title("AI"),

		huh.NewGroup(
			huh.NewInput().
				Title("Bot Name").
				Description("Display name in prompts and Slack messages").
				Placeholder("Ponko").
				Value(&cfg.Bot.Name),
			huh.NewInput().
				Title("Timezone").
				Description("IANA timezone for scheduled messages").
				Placeholder("America/Los_Angeles").
				Value(&cfg.Bot.Timezone),
			huh.NewInput().
				Title("System Prompt").
				Description("Optional: override default personality (leave blank for default)").
				Value(&cfg.Bot.SystemPrompt),
		).Title("Bot Settings"),
	).WithTheme(huh.ThemeDracula()).Run()

	return err
}

func collectOptional(cfg *Config) error {
	var wantDashboard, wantMCP, wantGitHub, wantLinear bool

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Set up Dashboard OAuth?").
				Description("Allows Slack login to the admin UI").
				Value(&wantDashboard),
			huh.NewConfirm().
				Title("Configure MCP tool servers?").
				Description("Connect external tool servers via Model Context Protocol").
				Value(&wantMCP),
			huh.NewConfirm().
				Title("Configure GitHub MCP?").
				Description("GitHub integration via MCP server").
				Value(&wantGitHub),
			huh.NewConfirm().
				Title("Configure Linear MCP?").
				Description("Linear project management integration via MCP").
				Value(&wantLinear),
		).Title("Optional Integrations"),
	).WithTheme(huh.ThemeDracula()).Run()
	if err != nil {
		return err
	}

	var groups []*huh.Group

	if wantDashboard {
		groups = append(groups, huh.NewGroup(
			huh.NewInput().
				Title("Slack Client ID").
				Description("OAuth Client ID from Basic Information").
				Value(&cfg.Dashboard.SlackClientID),
			huh.NewInput().
				Title("Slack Client Secret").
				Description("OAuth Client Secret from Basic Information").
				EchoMode(huh.EchoModePassword).
				Value(&cfg.Dashboard.SlackClientSecret),
			huh.NewInput().
				Title("Slack Team ID").
				Description("Your workspace's Team ID (T...)").
				Placeholder("T...").
				Value(&cfg.Dashboard.SlackTeamID),
		).Title("Dashboard OAuth"))
	}

	if wantMCP {
		groups = append(groups, huh.NewGroup(
			huh.NewInput().
				Title("MCP Server URLs").
				Description("Comma-separated MCP server URLs").
				Value(&cfg.MCP.ServerURLs),
			huh.NewInput().
				Title("MCP Access Key").
				Description("Shared access key (leave blank if none)").
				EchoMode(huh.EchoModePassword).
				Value(&cfg.MCP.AccessKey),
		).Title("MCP Tools"))
	}

	if wantGitHub {
		groups = append(groups, huh.NewGroup(
			huh.NewInput().
				Title("GitHub MCP Server URL").
				Value(&cfg.GitHub.MCPURL),
			huh.NewInput().
				Title("GitHub Personal Access Token").
				EchoMode(huh.EchoModePassword).
				Value(&cfg.GitHub.PAT),
		).Title("GitHub MCP"))
	}

	if wantLinear {
		groups = append(groups, huh.NewGroup(
			huh.NewInput().
				Title("Linear MCP Server URL").
				Value(&cfg.Linear.MCPURL),
			huh.NewInput().
				Title("Linear Access Token").
				EchoMode(huh.EchoModePassword).
				Value(&cfg.Linear.AccessToken),
			huh.NewInput().
				Title("Linear OAuth Token URL").
				Value(&cfg.Linear.TokenURL),
			huh.NewInput().
				Title("Linear OAuth Client ID").
				Value(&cfg.Linear.ClientID),
			huh.NewInput().
				Title("Linear OAuth Client Secret").
				EchoMode(huh.EchoModePassword).
				Value(&cfg.Linear.ClientSecret),
			huh.NewInput().
				Title("Linear OAuth Refresh Token").
				EchoMode(huh.EchoModePassword).
				Value(&cfg.Linear.RefreshToken),
		).Title("Linear MCP"))
	}

	if len(groups) > 0 {
		if err := huh.NewForm(groups...).WithTheme(huh.ThemeDracula()).Run(); err != nil {
			return err
		}
	}

	return nil
}

func required(field string) func(string) error {
	return func(s string) error {
		if s == "" {
			return fmt.Errorf("%s is required", field)
		}
		return nil
	}
}
