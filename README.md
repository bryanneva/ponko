# Ponko

A self-hosted AI agent that lives in Slack. Uses tools, runs durable workflows, and costs under $15/month to operate.

## Why Ponko

You want an AI assistant in Slack that can actually do things — search the web, query your knowledge base, file GitHub issues, answer questions with real context. And you want it running on your own infrastructure for a few dollars a month, not $29/user/month on someone else's cloud.

Ponko is a single Go binary backed by Postgres. Deploy it, connect it to Slack, point it at your MCP tool servers, and you have an autonomous agent that:

- **Maintains conversation context** per Slack thread
- **Uses external tools** via the Model Context Protocol (MCP)
- **Runs durable workflows** that survive crashes and retry on failure
- **Supports per-channel configuration** — different prompts, tools, and behavior per channel
- **Includes a management dashboard** for channel config and job monitoring

## Prerequisites

You'll need these before you start:

- **Slack workspace** where you can create apps — [create a Slack app](https://api.slack.com/apps) and follow the [Slack setup guide](docs/slack-setup.md) to get your bot token, signing secret, and bot user ID
- **Anthropic API key** — [console.anthropic.com](https://console.anthropic.com)
- **Fly.io account + CLI** — [fly.io](https://fly.io), install with `curl -L https://fly.io/install.sh | sh`

## Quick Start

### Option A: Setup wizard (~10 min)

```bash
go run ./cmd/setup
```

The interactive wizard walks you through collecting credentials, provisioning Fly.io infrastructure, and deploying. It generates secrets automatically. You'll need to have your Slack app created and your Anthropic API key ready before running it.

### Option B: Manual deploy

```bash
# 1. Create config from template
cp ponko.example.yaml ponko.yaml
# Fill in your Slack and Anthropic credentials

# 2. Provision infrastructure
fly apps create my-ponko
fly postgres create --name my-ponko-db --region iad
fly postgres attach my-ponko-db --app my-ponko

# 3. Set secrets
fly secrets set -a my-ponko \
  ANTHROPIC_API_KEY="sk-ant-..." \
  SLACK_BOT_TOKEN="xoxb-..." \
  SLACK_SIGNING_SECRET="..." \
  SLACK_BOT_USER_ID="U..." \
  PONKO_API_KEY="$(openssl rand -hex 32)" \
  COOKIE_SIGNING_KEY="$(openssl rand -hex 32)" \
  DASHBOARD_URL="https://my-ponko.fly.dev"

# 4. Update fly.toml and deploy
sed -i '' "s/<your-app>/my-ponko/" fly.toml
fly deploy
```

After deploying, set your Slack app's Event Subscriptions URL to `https://<your-app>.fly.dev/slack/events`, invite the bot to a channel, and @mention it.

See the full [setup guide](docs/setup.md) for Docker deployment, dashboard OAuth setup, and troubleshooting.

### Local Development

```bash
make db-up
export DATABASE_URL="postgres://agent:agent@localhost:5433/agent_dev?sslmode=disable"
export ANTHROPIC_API_KEY="sk-ant-..."
export SLACK_BOT_TOKEN="xoxb-..."
export SLACK_SIGNING_SECRET="..."
make run
```

Database migrations run automatically on startup.

## How It Works

```
@YourBot in Slack
      |
      v
  HTTP API (Go)
      |
      v
  Job Queue (River/Postgres)
      |
      v
  Claude + MCP Tools
      |
      v
  Reply in Slack thread
```

1. User @mentions the bot in Slack
2. Bot acknowledges with an emoji reaction
3. A durable workflow is created: **receive > process > respond**
4. The process step loads thread history, calls Claude with the channel's system prompt and tools
5. Claude executes tool calls as needed (web search, knowledge queries, API calls)
6. Response is posted back to the thread

All workflow state lives in Postgres. Jobs retry automatically on failure. Nothing is lost if the process restarts.

## Connecting Tools (MCP)

Ponko discovers and calls tools via the [Model Context Protocol](https://modelcontextprotocol.io/). Point it at any MCP server:

```bash
export MCP_SERVER_URLS="https://your-knowledge-base.example.com/mcp,https://another-tool.example.com/mcp"
```

Tools are discovered automatically via `tools/list` and made available to Claude during conversations. You can control which tools are available per channel via the management dashboard.

**Example MCP integrations:**
- Knowledge bases and RAG systems
- GitHub (issues, PRs, code search)
- Linear (project management)
- Web search
- Custom internal APIs

## Per-Channel Configuration

Each Slack channel can have its own:

| Setting | Description |
|---------|-------------|
| **System prompt** | Custom personality and instructions |
| **Respond mode** | `mention` (only when @mentioned) or `all` (every message) |
| **Tool allowlist** | Which MCP tools are available in this channel |

Configure via the management dashboard or programmatically via the API.

This lets one bot serve multiple purposes — a project planning assistant in #product, a research helper in #research, a casual assistant in #general — all with different prompts and tool access.

## Management Dashboard

A React-based admin UI is embedded in the binary. No separate hosting needed.

- **Channels** — view and edit per-channel settings
- **Jobs** — monitor queue health, see recent errors
- **Tools** — see available MCP tools and channel assignments

Authentication uses Slack OAuth — only members of your workspace can access it.

### Dashboard Setup

Add these environment variables:

| Variable | Description |
|----------|-------------|
| `SLACK_CLIENT_ID` | OAuth Client ID from your Slack app |
| `SLACK_CLIENT_SECRET` | OAuth Client Secret |
| `COOKIE_SIGNING_KEY` | 32+ byte secret (`openssl rand -hex 32`) |
| `DASHBOARD_URL` | Your app's public URL |
| `SLACK_TEAM_ID` | Your workspace's Team ID |

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `DATABASE_URL` | Yes | Postgres connection string |
| `ANTHROPIC_API_KEY` | Yes | Anthropic API key for Claude |
| `SLACK_BOT_TOKEN` | Yes | Slack bot OAuth token |
| `SLACK_SIGNING_SECRET` | Yes | Slack app signing secret |
| `SLACK_BOT_USER_ID` | Yes | Bot's Slack member ID (prevents self-replies) |
| `BOT_NAME` | No | Bot's display name for prompts and messages (default: "Ponko") |
| `SYSTEM_PROMPT` | No | Custom system prompt to define the bot's personality and instructions |
| `TIMEZONE` | No | IANA timezone for scheduled messages (default: "America/Los_Angeles") |
| `PONKO_API_KEY` | No | Bearer token for API authentication |
| `WORKER_CONCURRENCY` | No | Job worker concurrency (default: 5) |
| `MCP_SERVER_URLS` | No | Comma-separated MCP server URLs |
| `MCP_ACCESS_KEY` | No | Shared access key for MCP servers |
| `GITHUB_MCP_URL` | No | GitHub MCP server URL |
| `GITHUB_PAT` | No | GitHub personal access token (required if `GITHUB_MCP_URL` is set) |
| `LINEAR_MCP_URL` | No | Linear MCP server URL |
| `LINEAR_MCP_ACCESS_TOKEN` | No | Linear MCP access token |
| `LINEAR_MCP_TOKEN_URL` | No | Linear OAuth token refresh URL |
| `LINEAR_MCP_CLIENT_ID` | No | Linear OAuth client ID |
| `LINEAR_MCP_CLIENT_SECRET` | No | Linear OAuth client secret |
| `LINEAR_MCP_REFRESH_TOKEN` | No | Linear OAuth refresh token |
| `SLACK_CLIENT_ID` | No | For dashboard OAuth |
| `SLACK_CLIENT_SECRET` | No | For dashboard OAuth |
| `COOKIE_SIGNING_KEY` | No | For dashboard sessions |
| `DASHBOARD_URL` | No | Public URL for OAuth redirect |
| `SLACK_TEAM_ID` | No | Restrict dashboard to one workspace |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | No | OpenTelemetry collector endpoint |
| `OTEL_EXPORTER` | No | Exporter type: "otlp" or "stdout" |

## Cost

| Component | Monthly Cost |
|-----------|-------------|
| Compute (Fly.io shared-cpu) | $3-5 |
| Postgres (Fly.io or Supabase free tier) | $0-7 |
| Slack | Free |
| **Total infrastructure** | **$3-12** |
| Anthropic API | Pay-as-you-go |

## Development

```bash
make lint        # Run linter
make build       # Build binary
make test-unit   # Run unit tests
make test-e2e    # Run E2E tests (requires local Postgres)
make test-cover  # Test with coverage report
```

CI runs through Dagger and can be emulated locally. See [CI](docs/ci.md).

## Contributing

Contributions welcome. Please open an issue first to discuss what you'd like to change.

## License

MIT

## Links

- [Setup Guide](docs/setup.md)
- [Slack Setup Guide](docs/slack-setup.md)
- [Architecture](docs/architecture.md)
- [Vision](vision.md)
