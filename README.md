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

## Quick Start

### Prerequisites

- Go 1.25+ (or use the Docker image)
- PostgreSQL
- A Slack workspace where you can create apps
- An Anthropic API key

### 1. Start the database

```bash
make db-up
```

### 2. Set environment variables

```bash
export DATABASE_URL="postgres://agent:agent@localhost:5433/agent_dev?sslmode=disable"
export ANTHROPIC_API_KEY="sk-ant-..."
export SLACK_BOT_TOKEN="xoxb-..."
export SLACK_SIGNING_SECRET="..."
```

### 3. Run

```bash
make run
```

### 4. Deploy

Ponko ships as a single binary. Deploy it anywhere that runs Go or Docker:

```bash
# Fly.io
fly deploy

# Docker
docker build -t ponko .
docker run -e DATABASE_URL=... -e ANTHROPIC_API_KEY=... -e SLACK_BOT_TOKEN=... ponko
```

Database migrations run automatically on startup.

## Slack Bot Setup

1. Create a Slack app at [api.slack.com/apps](https://api.slack.com/apps)
2. Add bot token scopes: `app_mentions:read`, `chat:write`, `reactions:write`
3. Enable Event Subscriptions, subscribe to `app_mention`, set the request URL to `https://<your-host>/slack/events`
4. Install the app to your workspace
5. Copy the Bot Token (`SLACK_BOT_TOKEN`) and Signing Secret (`SLACK_SIGNING_SECRET`)
6. Invite the bot to a channel: `/invite @YourBot`
7. Mention it: `@YourBot what's the weather?`

See the full [Slack setup guide](docs/slack-setup.md) for details on OAuth, dashboard access, and advanced configuration.

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
| `PONKO_API_KEY` | No | Bearer token for API authentication |
| `MCP_SERVER_URLS` | No | Comma-separated MCP server URLs |
| `MCP_ACCESS_KEY` | No | Shared access key for MCP servers |
| `SLACK_CLIENT_ID` | No | For dashboard OAuth |
| `SLACK_CLIENT_SECRET` | No | For dashboard OAuth |
| `COOKIE_SIGNING_KEY` | No | For dashboard sessions |
| `DASHBOARD_URL` | No | Public URL for OAuth redirect |
| `SLACK_TEAM_ID` | No | Restrict dashboard to one workspace |
| `WORKER_CONCURRENCY` | No | Job worker concurrency (default: 5) |

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

## Contributing

Contributions welcome. Please open an issue first to discuss what you'd like to change.

## License

MIT

## Links

- [Vision](vision.md)
- [Architecture](docs/architecture.md)
- [Slack Setup Guide](docs/slack-setup.md)
