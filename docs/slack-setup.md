# Slack Setup Guide

Complete guide to creating and configuring a Slack bot for Ponko. Part of the [setup guide](setup.md).

## Step 1: Create a Slack App

1. Go to [api.slack.com/apps](https://api.slack.com/apps)
2. Click **Create New App** > **From scratch**
3. Name your app (e.g., "Otto", "Jarvis", or whatever you'd like)
4. Select your workspace
5. Click **Create App**

## Step 2: Configure OAuth Scopes

Navigate to **OAuth & Permissions** in the sidebar and add these **Bot Token Scopes**:

| Scope | Purpose |
|-------|---------|
| `app_mentions:read` | Receive events when users @mention the bot |
| `chat:write` | Send messages and replies in channels |
| `reactions:write` | Add/remove emoji reactions to show processing status |

## Step 3: Enable Event Subscriptions

1. Navigate to **Event Subscriptions** in the sidebar
2. Toggle **Enable Events** to ON
3. Set the **Request URL** to your deployed app's Slack events endpoint:
   ```
   https://<your-host>/slack/events
   ```
   Slack will send a verification challenge — your app must be running to respond.

4. Under **Subscribe to bot events**, add:
   - `app_mention`

5. Click **Save Changes**

## Step 4: Install the App to Your Workspace

1. Navigate to **OAuth & Permissions**
2. Click **Install to Workspace**
3. Review the permissions and click **Allow**
4. Copy the **Bot User OAuth Token** (starts with `xoxb-`) — you'll need this for `SLACK_BOT_TOKEN`

## Step 5: Get the Signing Secret

1. Navigate to **Basic Information**
2. Under **App Credentials**, copy the **Signing Secret** — you'll need this for `SLACK_SIGNING_SECRET`

## Step 6: Get the Bot User ID

1. After installing the app, go to your Slack workspace
2. Click on the bot's name in any channel to view its profile
3. Click the **...** (more) menu and select **Copy member ID**
4. Set this as `SLACK_BOT_USER_ID` in your environment — Ponko uses this to ignore its own messages

## Step 7: Set Environment Variables

Your deployment needs these environment variables:

| Variable | Required | Description |
|----------|----------|-------------|
| `SLACK_BOT_TOKEN` | Yes | Bot User OAuth Token from Step 4 |
| `SLACK_SIGNING_SECRET` | Yes | Signing Secret from Step 5 |
| `SLACK_BOT_USER_ID` | Yes | Bot's member ID from Step 6 |
| `ANTHROPIC_API_KEY` | Yes | Your Anthropic API key |
| `DATABASE_URL` | Yes | Postgres connection string |

For Fly.io deployments:
```bash
fly secrets set SLACK_BOT_TOKEN="xoxb-..." SLACK_SIGNING_SECRET="..." SLACK_BOT_USER_ID="U..." ANTHROPIC_API_KEY="sk-ant-..."
```

## Step 8: Invite the Bot to a Channel

In Slack, go to the channel where you want the bot and type:
```
/invite @YourBotName
```

Then mention the bot to start a conversation:
```
@YourBotName hello
```

The bot will reply in a thread. All messages within the same thread share conversational context.

## Dashboard Setup (Optional)

The management dashboard uses Slack OAuth for authentication — only members of your workspace can log in.

### Additional Slack App Configuration

1. In your Slack app settings, add **User Token Scopes**: `identity.basic`
2. Under **OAuth & Permissions** > **Redirect URLs**, add:
   ```
   https://<your-host>/api/auth/slack/callback
   ```

### Additional Environment Variables

| Variable | Description |
|----------|-------------|
| `SLACK_CLIENT_ID` | OAuth Client ID (from Basic Information) |
| `SLACK_CLIENT_SECRET` | OAuth Client Secret (from Basic Information) |
| `COOKIE_SIGNING_KEY` | 32+ byte secret for session cookies (`openssl rand -hex 32`) |
| `DASHBOARD_URL` | Your app's public URL (e.g., `https://<your-host>`) |
| `SLACK_TEAM_ID` | Your workspace's Team ID (from the workspace URL: `app.slack.com/client/<TEAM_ID>`) |

### OAuth Gotcha

The dashboard uses `user_scope` (not `scope`) for `identity.basic` in the OAuth authorize URL. Using `scope` causes a `missing_scope` error when calling `users.identity`.

## Running Multiple Bots

To run a second bot with a different personality, deploy a separate Ponko instance:

1. Create a new Slack app (Steps 1-5 above) with a different name
2. Deploy a new instance with its own database
3. Set the environment variables for the new instance
4. Configure system prompts per-channel via the management dashboard
