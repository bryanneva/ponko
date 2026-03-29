# System Architecture

*Living document - updated as the system evolves*

## 1. Executive Summary

The Lightweight Agent Workflow System is a Go-based workflow orchestration platform designed for small teams. It prioritizes operational simplicity and cost efficiency over enterprise features.

**Key constraints:**
- Single Go binary deployment
- Postgres as the only external dependency
- Target cost: $3-10/month
- Optimized for 1-3 person teams

## 2. System Context

### Purpose
Enable durable, resumable workflow execution for autonomous agent tasks without heavy infrastructure.

### Users
- Solopreneurs
- Small development teams (1-3 people)
- Cost-conscious startups

### Typical Workloads
- Agent-driven development workflows
- Document processing pipelines
- Multi-step API integrations

### Deployment
Fly.io with managed Postgres

### Cost Model
| Component | Monthly Cost |
|-----------|--------------|
| Fly.io service | $3-6 |
| Postgres | $0-10 |
| River | Free (library) |
| **Total infrastructure** | **$3-16** |

*LLM API costs are separate and typically dominate total cost.*

## 3. Architecture Principles

1. **Simplicity over features** - Fewer moving parts means fewer failure modes
2. **Postgres-backed everything** - Single source of truth, SQL-queryable
3. **Job-chains over DAGs** - Linear workflows are easier to reason about
4. **Operational minimalism** - No Redis, no workflow clusters, no complex monitoring

## 4. Design Overview

### System Context Diagram

```
            External Systems
           (Slack, GitHub, Email)
                    |
                    v
             HTTP API (Go Service)
                    |
            Workflow Controller
                    |
                    v
                River Queue
                (Postgres)
                    |
         +----------+-----------+
         |                      |
         v                      v
     Worker 1              Worker 2
         |                      |
         +----------+-----------+
                    |
                    v
                 LLM APIs
          (Anthropic Claude)
                    |
                    v
               External Actions
          (GitHub PRs, Slack, etc.)
```

### Key Components

| Component | Responsibility |
|-----------|---------------|
| **HTTP API** | Workflow initiation, status queries, dashboard API |
| **Management Dashboard** | Web UI for channel config, job monitoring, tool access |
| **Workflow Controller** | Coordinates job sequencing |
| **River Queue** | Durable job persistence and delivery |
| **Workers** | Execute individual workflow steps |
| **Postgres** | All persistent state |

## 5. Workflow Model

### Definition
Workflows are sequences of jobs. Each job:
1. Performs work
2. Persists output
3. Enqueues the next step

```
AgentWorkflow
  +- ResearchJob
  +- PlanJob
  +- ImplementJob
```

### Parallel Execution
Parallelism via multiple concurrent jobs:

```
AnalyzeFileJob(file_1)
AnalyzeFileJob(file_2)
AnalyzeFileJob(file_3)
...
```

Completion tracked via counter-based fan-in (see [ADR-009](adr/009-parallel-coordination.md)).

## 6. Data Model

### Core Tables

**workflows**
| Column | Type | Description |
|--------|------|-------------|
| workflow_id | string | Primary identifier |
| workflow_type | string | Workflow template name |
| status | string | running, completed, failed |
| created_at | timestamp | Creation time |
| updated_at | timestamp | Last update |

**workflow_steps**
| Column | Type | Description |
|--------|------|-------------|
| step_id | string | Primary identifier |
| workflow_id | string | Parent workflow |
| step_name | string | Step identifier |
| status | string | pending, running, complete |
| started_at | timestamp | Execution start |
| completed_at | timestamp | Execution end |

**workflow_outputs**
| Column | Type | Description |
|--------|------|-------------|
| workflow_id | string | Parent workflow |
| step_name | string | Producing step |
| data | jsonb | Output payload |
| created_at | timestamp | Creation time |

**river_jobs** (managed by River)
- Job payload
- Status
- Retry count
- Execution metadata

## 7. API Contract

### Authentication

Workflow endpoints require Bearer token auth when `PONKO_API_KEY` is set (production). When unset (local dev), endpoints are open.

```
Authorization: Bearer <token>
```

Missing or invalid token returns `401 {"error": "unauthorized"}`. Health and Slack endpoints are unauthenticated.

### Start Workflow
```
POST /workflows/start
Authorization: Bearer <token>
```

**Request:**
```json
{
  "workflow_type": "ticket_agent",
  "payload": {
    "issue_id": "1234"
  }
}
```

**Response:**
```json
{
  "workflow_id": "wf_abc123"
}
```

### Get Workflow Status
```
GET /workflows/{workflow_id}
Authorization: Bearer <token>
```

**Response:**
```json
{
  "workflow_id": "wf_abc123",
  "status": "running",
  "steps": [
    { "step": "research", "status": "complete" },
    { "step": "plan", "status": "running" }
  ]
}
```

## 8. Deployment & Operations

### Fly.io Configuration
- Single Go binary (API + Workers)
- Worker concurrency via environment variable: `WORKER_CONCURRENCY=10`

### Scaling
- Horizontal scaling via additional Fly.io instances
- All instances share Postgres for coordination

### Observability
- Structured logging
- Management dashboard for job monitoring and channel config (see below)
- SQL queries for direct workflow inspection

```sql
-- Active workflows
SELECT * FROM workflows WHERE status='running';

-- Failed jobs
SELECT * FROM river_jobs WHERE state='failed';
```

### Management Dashboard

A React SPA (`web/`) embedded in the Go binary via `go:embed`. Serves from the same origin as the API — no separate hosting or CORS configuration needed.

**Architecture:**
- **Frontend**: React + TypeScript + Vite, CSS Modules for styling, React Router for client-side navigation
- **Embedding**: `web/embed.go` uses `//go:embed all:dist` to bundle built assets into the binary
- **Serving**: SPA catch-all handler in `internal/api/spa.go` serves static files or falls back to `index.html`
- **Auth**: Slack OAuth 2.0 flow with HMAC-SHA256 signed cookies (stateless — no session table)
- **Build**: Dockerfile uses multi-stage builds: Node stage (frontend) → Go stage (backend) → slim runtime

**Panels:**

| Panel | Route | API Endpoints | Purpose |
|-------|-------|---------------|---------|
| Channels | `/channels` | `GET /api/channels`, `GET/PUT /api/channels/{id}/config` | List all configured channels, edit system prompts, respond modes, and tool allowlists |
| Jobs | `/jobs` | `GET /api/jobs/summary`, `GET /api/jobs/recent` | River job queue health: counts by state, recent jobs with errors, 30s auto-refresh |
| Tools | `/tools` | `GET /api/tools`, `GET /api/channels` | MCP tool inventory with per-channel access cross-reference |

**Auth endpoints:** `GET /api/auth/slack`, `GET /api/auth/slack/callback`, `GET /api/auth/me`, `POST /api/auth/logout`

All dashboard API endpoints use session cookie auth (`requireSession` middleware). Workflow endpoints (`/workflows/*`) use separate API key auth for programmatic access.

## 9. Failure Handling

### Job Retries
- Maximum attempts: 5
- Backoff: Exponential
- Failed jobs logged and marked

### Workflow Recovery
- All state persisted to Postgres
- Workflows resume on worker restart
- No state lost on crashes

## 10. ADR Index

| ADR | Title | Status |
|-----|-------|--------|
| [001](adr/001-reference-architecture.md) | Reference Architecture | Accepted |
| [002](adr/002-go-language.md) | Go Language | Accepted |
| [003](adr/003-fly-io-platform.md) | Fly.io Platform | Accepted |
| [004](adr/004-river-job-queue.md) | River Job Queue | Accepted |
| [005](adr/005-postgres-state.md) | Postgres State | Accepted |
| [006](adr/006-supabase-postgres.md) | Supabase Postgres | Proposed |
| [007](adr/007-single-binary.md) | Single Binary | Accepted |
| [008](adr/008-job-chain-workflows.md) | Job-Chain Workflows | Accepted |
| [009](adr/009-parallel-coordination.md) | Parallel Coordination | Accepted |
| [010](adr/010-retry-strategy.md) | Retry Strategy | Accepted |
| [011](adr/011-llm-provider.md) | LLM Provider | Accepted |
| [012](adr/012-slack-integration.md) | Slack Integration | Proposed |
| [013](adr/013-observability.md) | Observability | Proposed |

## 11. Constraints & Trade-Offs

### Known Limitations
- Linear job-chains only (no complex DAGs)
- Single-region deployment
- No visual workflow builder
- Limited to Postgres-scale workloads

### Risks
| Risk | Mitigation |
|------|------------|
| Workflow complexity growth | Can migrate to Temporal if needed |
| Parallel coordination complexity | Counter-based tracking in Postgres |
| Postgres scaling limits | Sufficient for target workloads |

## 12. Future Enhancements

**Shipped:**
- Slack bot interface with conversational threads
- Management dashboard (channels, jobs, tools)
- Per-channel system prompts and tool allowlists
- Slack OAuth authentication

**Planned:**
- Cost tracking / token usage panel (depends on structured logging)
- Proactive messaging and schedules
- GitHub webhook integration
- Cross-Slack message ingestion

## 13. Technology Stack Summary

| Layer | Technology | Rationale |
|-------|------------|-----------|
| Language | Go | Performance, deployment simplicity |
| Frontend | React + Vite | Lightweight SPA, embedded in Go binary |
| Job Queue | River | Postgres-backed, no Redis needed |
| Database | PostgreSQL | Durability, SQL queryability |
| Hosting | Fly.io | Low cost, simple deployment |
| LLM | Anthropic Claude | Primary AI provider |
