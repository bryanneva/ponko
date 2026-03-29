# Ponko - Vision

## What It Is

A self-hosted AI agent that lives in Slack, uses external tools, and runs durable multi-step workflows autonomously. Single binary, under $15/month to operate.

## The Problem

You want an AI assistant that lives where you already work (Slack), can take real actions (not just chat), survives failures, and doesn't cost $29/user/month or require a PhD in infrastructure to deploy.

Your options today:

- **Dust.tt** — close, but complex to self-host and expensive on their cloud ($29/user/month)
- **n8n + AI nodes** — visual builder paradigm, AI feels bolted on, heavier to run
- **LangGraph + Slack Bolt** — DIY Python stack with 3+ services, higher ops burden
- **OpenWebUI / LibreChat** — web chat only, no Slack, no autonomous workflows
- **Temporal** — enterprise-grade, $200+/month, massive overkill
- **Roll your own** — no durability, no retry, no tool framework

None of them nail all five things at once:

1. **Trivial to deploy** — one binary or one `docker run`
2. **Conversational** — lives in Slack, not another dashboard to check
3. **Durable** — survives crashes, retries failures, persists workflow state
4. **Tool-using** — takes real actions via MCP (APIs, knowledge bases, GitHub, search)
5. **Cheap** — $5-15/month infrastructure, excluding LLM usage

## What Ponko Does

Ponko is a Slack bot backed by a durable workflow engine. When you @mention it:

1. It acknowledges with a reaction
2. Creates a workflow: receive > process > respond
3. Loads the thread's conversation history
4. Sends it to Claude with a system prompt and available tools
5. Executes any tool calls (search, API calls, knowledge queries via MCP)
6. Posts the response back to the thread

Conversations in threads maintain full context. Each channel can have its own system prompt, personality, tool access, and behavior mode.

### Key Capabilities

- **Per-channel configuration** — different system prompts, tools, and behavior per channel. One bot, many personas.
- **Tool use via MCP** — connect any MCP-compatible server (knowledge bases, GitHub, Linear, web search, custom APIs). Tools are discovered automatically.
- **Durable execution** — multi-step workflows backed by Postgres. Jobs survive crashes, retry on failure, and persist all outputs.
- **Management dashboard** — web UI for channel config, job monitoring, and tool access. Authenticated via Slack OAuth.
- **Proactive messaging** — scheduled outreach, reminders, and information collection.
- **Conversational modes** — mention-only or always-respond per channel. Topic channels can have ongoing conversations without @mentions.

## What Ponko Is Not

- **Not a framework** — it's a deployable product, not a library to build on
- **Not multi-tenant** — one instance per workspace
- **Not enterprise orchestration** — no DAGs, no distributed clusters, no visual workflow builder
- **Not LLM-agnostic** — built for Claude (Anthropic). This is an intentional architectural choice, not a limitation.
- **Not a chat UI** — Slack is the interface. There is no web chat.

## Architecture

```
Slack @mention
      |
      v
  HTTP API (Go)
      |
      v
  River Queue (Postgres)
      |
  +---+---+
  |       |
  v       v
Worker  Worker
  |       |
  v       v
Claude API + MCP Tools
      |
      v
  Slack Reply
```

Single Go binary. Single Postgres database. That's the entire infrastructure.

- **River** — Postgres-backed job queue. No Redis, no external queue service.
- **MCP** — Model Context Protocol for tool integration. Plug in any MCP server.
- **Embedded SPA** — React dashboard compiled into the Go binary via `go:embed`.

## Target User

Solopreneurs and small teams (1-3 people) who want a capable AI assistant running in their Slack workspace without managing complex infrastructure or paying enterprise SaaS prices.

You should be comfortable with:
- Deploying a Go binary (or Docker container) to a host like Fly.io, Railway, or your own server
- Setting up a Slack app
- Managing environment variables for API keys

## Cost Model

| Component | Cost |
|-----------|------|
| Fly.io (or similar) | $3-5/month |
| Postgres | $0-7/month |
| Slack | Free |
| **Total infrastructure** | **$3-12/month** |
| Anthropic API usage | Pay-as-you-go (varies) |

## What Success Looks Like

- Deploy in under 10 minutes
- Running for under $15/month
- One bot serving multiple channels with different personalities and tools
- Workflows that survive crashes and retry automatically
- External tools connected via MCP with zero custom integration code
