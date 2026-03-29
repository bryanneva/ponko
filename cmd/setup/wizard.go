package main

import (
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/huh"
)

func collectRequired(cfg *Config) error {
	// Step 1: Platform and bot name (needed before Slack manifest)
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Deployment platform").
				Options(
					huh.NewOption("Fly.io (recommended)", platformFly),
					huh.NewOption("Docker (local/self-hosted)", platformDocker),
				).
				Value(&cfg.Deploy.Platform),
			huh.NewInput().
				Title("Bot Name").
				Description("Your bot's display name in Slack and prompts").
				Placeholder("Ponko").
				Value(&cfg.Bot.Name),
		).Title("Getting Started"),
	).WithTheme(huh.ThemeDracula()).Run(); err != nil {
		return err
	}

	// Default bot name if left blank
	botName := cfg.Bot.Name
	if botName == "" {
		botName = "Ponko"
		cfg.Bot.Name = botName
	}

	// Write the manifest with the chosen bot name
	manifestPath := filepath.Join(filepath.Dir(configPath), "slack-app-manifest.json")
	if err := writeManifest(botName, manifestPath); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}
	fmt.Printf("Wrote Slack app manifest for %q to %s\n\n", botName, manifestPath)

	// Steps 2-5: Slack setup using the manifest
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Step 1: Create a Slack App from Manifest").
				Description(
					"1. Go to api.slack.com/apps and click Create New App\n"+
						"2. Choose \"From a manifest\"\n"+
						"3. Select your workspace\n"+
						"4. Switch to JSON tab and paste the contents of:\n"+
						"   "+manifestPath+"\n"+
						"5. Click Create\n\n"+
						"The manifest pre-configures all scopes and event subscriptions\n"+
						"with your bot named \""+botName+"\". You'll update the event URL after deploy."),
			huh.NewConfirm().
				Title("App created?").
				Affirmative("Next").
				Negative("").
				Value(new(bool)),
		).Title("Slack Setup"),

		huh.NewGroup(
			huh.NewNote().
				Title("Step 2: Install App & Get Bot Token").
				Description(
					"1. In the sidebar, click Install App\n"+
						"2. Click \"Install to [your workspace]\"\n"+
						"3. Review permissions and click Allow\n"+
						"4. You'll see the Bot User OAuth Token — copy it (starts with xoxb-)"),
			huh.NewInput().
				Title("Bot User OAuth Token").
				Placeholder("xoxb-...").
				Value(&cfg.Slack.BotToken).
				Validate(required("bot token")),
		).Title("Slack Setup"),

		huh.NewGroup(
			huh.NewNote().
				Title("Step 3: Get the Signing Secret").
				Description(
					"1. In the sidebar, click Basic Information\n"+
						"2. Scroll to App Credentials\n"+
						"3. Next to Signing Secret, click Show, then copy it"),
			huh.NewInput().
				Title("Signing Secret").
				EchoMode(huh.EchoModePassword).
				Value(&cfg.Slack.SigningSecret).
				Validate(required("signing secret")),
		).Title("Slack Setup"),

		huh.NewGroup(
			huh.NewNote().
				Title("Step 4: Get the Bot User ID").
				Description(
					"1. Open your Slack workspace\n"+
						"2. Find "+botName+" under Apps in the sidebar and click it\n"+
						"3. Click "+botName+"'s name at the top of the conversation\n"+
						"4. The Member ID is shown in the details panel — copy it"),
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

		// Remaining bot settings
		huh.NewGroup(
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
