# Setup Guide

Get Ponko running in Slack in under 10 minutes.

## Prerequisites

- [Fly.io](https://fly.io) account + CLI (`fly` command)
- Slack workspace with admin access
- [Anthropic API key](https://console.anthropic.com)

## Quick Start (automated)

```bash
go run ./cmd/setup
```

The setup script walks you through everything interactively. It creates your config, provisions infrastructure, and deploys.

## Manual Setup

### 1. Create a Slack App (~3 min)

Follow the [Slack setup guide](slack-setup.md) to create your bot and collect:
- Bot User OAuth Token (`xoxb-...`)
- Signing Secret
- Bot User ID (`U...`)

### 2. Get an Anthropic API Key (~1 min)

1. Go to [console.anthropic.com](https://console.anthropic.com)
2. Create an API key
3. Save it — you'll need it in step 3

### 3. Create Config

```bash
cp ponko.example.yaml ponko.yaml
```

Fill in the required values:
```yaml
slack:
  bot_token: "xoxb-your-token"
  signing_secret: "your-signing-secret"
  bot_user_id: "U12345678"

ai:
  anthropic_api_key: "sk-ant-your-key"
```

Generate secrets for API auth and cookie signing:
```bash
openssl rand -hex 32  # Use for deploy.api_key
openssl rand -hex 32  # Use for deploy.cookie_signing_key
```

### 4. Deploy to Fly.io

```bash
# Create the app
fly apps create my-ponko

# Create and attach a Postgres database
fly postgres create --name my-ponko-db --region iad
fly postgres attach my-ponko-db --app my-ponko

# Set secrets
fly secrets set -a my-ponko \
  ANTHROPIC_API_KEY="sk-ant-..." \
  SLACK_BOT_TOKEN="xoxb-..." \
  SLACK_SIGNING_SECRET="..." \
  SLACK_BOT_USER_ID="U..." \
  PONKO_API_KEY="$(openssl rand -hex 32)" \
  COOKIE_SIGNING_KEY="$(openssl rand -hex 32)" \
  DASHBOARD_URL="https://my-ponko.fly.dev"

# Update fly.toml with your app name
sed -i '' "s/<your-app>/my-ponko/" fly.toml

# Deploy
fly deploy
```

### 5. Connect Slack

1. Go to your [Slack app settings](https://api.slack.com/apps) > Event Subscriptions
2. Set the Request URL to:
   ```
   https://my-ponko.fly.dev/slack/events
   ```
3. Slack will verify the URL — your app must be running

### 6. Test

```
/invite @YourBot
@YourBot hello
```

The bot should reply in a thread.

## Docker (Local/Self-Hosted)

For local development or self-hosted Docker deployments:

```bash
# Create config
cp ponko.example.yaml ponko.yaml
# Edit ponko.yaml with your values, set deploy.platform to "docker"

# Generate .env from config
go run ./cmd/setup deploy

# Start services
docker compose up -d
```

Your bot will be running at `http://localhost:8080`. Use [ngrok](https://ngrok.com) or a similar tunnel to expose it for Slack event subscriptions.

## Dashboard Setup (Optional)

The management dashboard uses Slack OAuth for authentication. Add these to your config:

```yaml
dashboard:
  slack_client_id: "your-client-id"
  slack_client_secret: "your-client-secret"
  slack_team_id: "T12345678"
```

See [Slack setup guide — Dashboard Setup](slack-setup.md#dashboard-setup-optional) for how to get these values.

## Re-deploy / Validate

```bash
# Re-deploy from existing ponko.yaml
go run ./cmd/setup deploy

# Validate config and check health
go run ./cmd/setup validate
```

## Troubleshooting

**Slack verification fails**
Your app must be running and accessible before Slack can verify the Event Subscriptions URL. Deploy first, then set the URL.

**Bot doesn't reply**
Check that `SLACK_BOT_USER_ID` is correct. This prevents the bot from responding to its own messages — if it's wrong, the bot ignores everything.

**Dashboard login fails**
Verify the OAuth redirect URL in your Slack app settings matches `DASHBOARD_URL` + `/api/auth/slack/callback`.

**Health check fails**
```bash
curl https://your-app.fly.dev/health
```
Should return `{"status":"ok"}`. If not, check `fly logs --app your-app --no-tail`.
