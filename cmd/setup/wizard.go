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

		// Slack Step 1: Create app from manifest
		huh.NewGroup(
			huh.NewNote().
				Title("Step 1: Create a Slack App from Manifest").
				Description(
					"1. Go to api.slack.com/apps and click Create New App\n"+
						"2. Choose \"From a manifest\"\n"+
						"3. Select your workspace\n"+
						"4. Switch to JSON tab and paste the contents of:\n"+
						"   slack-app-manifest.json (in the repo root)\n"+
						"5. Click Create\n\n"+
						"This sets up all scopes and event subscriptions automatically.\n"+
						"You'll update the event URL after deploy."),
			huh.NewConfirm().
				Title("App created?").
				Affirmative("Next").
				Negative("").
				Value(new(bool)),
		).Title("Slack Setup"),

		// Slack Step 2: Install and get bot token
		huh.NewGroup(
			huh.NewNote().
				Title("Step 2: Install App & Get Bot Token").
				Description(
					"1. Go to OAuth & Permissions\n"+
						"2. Click Install to Workspace\n"+
						"3. Review permissions and click Allow\n"+
						"4. Copy the Bot User OAuth Token (starts with xoxb-)"),
			huh.NewInput().
				Title("Bot Token").
				Placeholder("xoxb-...").
				Value(&cfg.Slack.BotToken).
				Validate(required("bot token")),
		).Title("Slack Setup"),

		// Slack Step 3: Signing secret
		huh.NewGroup(
			huh.NewNote().
				Title("Step 3: Get the Signing Secret").
				Description(
					"1. Go to Basic Information\n"+
						"2. Under App Credentials, copy the Signing Secret"),
			huh.NewInput().
				Title("Signing Secret").
				EchoMode(huh.EchoModePassword).
				Value(&cfg.Slack.SigningSecret).
				Validate(required("signing secret")),
		).Title("Slack Setup"),

		// Slack Step 4: Bot user ID
		huh.NewGroup(
			huh.NewNote().
				Title("Step 4: Get the Bot User ID").
				Description(
					"1. Go to your Slack workspace\n"+
						"2. Find the bot in any channel or in the Apps section\n"+
						"3. Click on the bot's name to view its profile\n"+
						"4. Click the ··· menu and select Copy member ID"),
			huh.NewInput().
				Title("Bot User ID").
				Placeholder("U...").
				Value(&cfg.Slack.BotUserID).
				Validate(required("bot user ID")),
		).Title("Slack Setup"),

		// Anthropic API key
		huh.NewGroup(
			huh.NewNote().
				Title("Anthropic API Key").
				Description(
					"1. Go to console.anthropic.com\n"+
						"2. Create an API key\n"+
						"3. Paste it below"),
			huh.NewInput().
				Title("API Key").
				Placeholder("sk-ant-...").
				EchoMode(huh.EchoModePassword).
				Value(&cfg.AI.AnthropicAPIKey).
				Validate(required("Anthropic API key")),
		).Title("AI"),

		// Bot settings
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
			huh.NewNote().
				Title("Dashboard OAuth Setup").
				Description(
					"In your Slack app settings:\n"+
						"1. Add User Token Scope: identity.basic\n"+
						"2. Under OAuth & Permissions > Redirect URLs, add:\n"+
						"   https://<your-host>/api/auth/slack/callback\n\n"+
						"Then grab these values from Basic Information:"),
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
				Description("From your workspace URL: app.slack.com/client/<TEAM_ID>").
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
