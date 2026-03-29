# Engineering Design Document

**Project:** Lightweight Agent Workflow System (Go + Fly.io)
**Author:** Bryan Neva
**Date:** March 14, 2026
**Status:** Draft

## 1. Context and Scope

Modern autonomous-agent workflows often rely on a stack like:
- Redis
- Trigger.dev
- LangChain
- Node-based worker infrastructure

While powerful, this architecture introduces significant operational complexity and cost for small teams. For a single developer or small team (1-3 users), these systems are often overbuilt relative to actual workload needs.

This design proposes a simplified architecture built in Go that provides the most valuable operational capabilities:
- durable workflow execution
- parallel task coordination
- workflow visibility

without introducing heavy infrastructure dependencies.

The system will run primarily on Fly.io with a Go service and a Postgres-backed job queue using River.

The goal is to maintain:
- low infrastructure cost
- simple deployment
- easy workflow authoring
- maintainable operational footprint

## 2. Goals

### Primary Goals

- **Durable workflow execution**
  Workflows must resume correctly if workers crash or restart.

- **Simple workflow authoring**
  Developers should be able to define workflows like:
  ```
  research -> plan -> implement
  ```
  without large frameworks.

- **Parallel task coordination**
  Workflows should support fan-out workloads such as:
  - analyze 20 files
  - scrape 50 pages
  - summarize documents

- **Minimal infrastructure**
  The system should operate with:
  - Go service
  - Postgres
  - Fly.io

  No Redis or workflow cluster required.

- **Low operational cost**
  Target infrastructure cost: **$3-10/month** excluding LLM usage.

## 3. Non-Goals

The system intentionally does not attempt to solve the following:
- enterprise-scale workflow orchestration
- distributed workflow clusters
- visual workflow builders
- generic agent frameworks
- multi-tenant agent infrastructure

The system is optimized for small internal automation and development agents.

## 4. Design Overview

### High-Level Architecture

The system uses a durable job queue backed by Postgres.

Each workflow step is represented as a job. Steps enqueue subsequent jobs when completed.

Example workflow:
```
ResearchJob
   |
   v
PlanJob
   |
   v
ImplementJob
```

Durability is provided through Postgres persistence.

Parallel work is achieved by enqueueing multiple jobs.

## 5. System Context Diagram

```
            External Systems
           (Slack, GitHub, Email)
                    |
                    |
                    v
             HTTP API (Go Service)
                    |
                    |
            Workflow Controller
                    |
                    |
                    v
                River Queue
                (Postgres)
                    |
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
          (OpenAI / Anthropic etc.)
                    |
                    v
               External Actions
          (GitHub PRs, Slack, etc.)
```

## 6. Workflow Model

Workflows are represented as a sequence of jobs.

Example workflow:
```
AgentWorkflow
  +- ResearchJob
  +- PlanJob
  +- ImplementJob
```

Each job:
- performs work
- persists output
- enqueues the next step

## 7. APIs

### Workflow Start API

Starts a new workflow execution.

**Endpoint:**
```
POST /workflows/start
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

### Workflow Status API

Retrieve workflow progress.

**Endpoint:**
```
GET /workflows/{workflow_id}
```

**Response:**
```json
{
  "workflow_id": "wf_abc123",
  "status": "running",
  "steps": [
    {
      "step": "research",
      "status": "complete"
    },
    {
      "step": "plan",
      "status": "running"
    }
  ]
}
```

## 8. Job Execution Model

Each step corresponds to a River job.

Example job:
```go
type ResearchJob struct {
    IssueID string
}
```

Worker execution flow:
```
worker receives job
      |
      v
perform LLM research
      |
      v
persist research output
      |
      v
enqueue PlanJob
```

## 9. Parallel Task Coordination

Parallelism is achieved by enqueueing multiple jobs.

Example:
```
AnalyzeFileJob(file_1)
AnalyzeFileJob(file_2)
AnalyzeFileJob(file_3)
...
```

Workers process these concurrently.

Completion can be tracked using a workflow-step counter:
```
expected_tasks = 20
completed_tasks = 0
```

When all tasks finish, the next workflow step begins.

## 10. Data Storage

**Primary datastore:** Postgres

### Tables

**workflows**
| Column | Type |
|--------|------|
| workflow_id | string |
| workflow_type | string |
| status | string |
| created_at | timestamp |
| updated_at | timestamp |

**workflow_steps**
| Column | Type |
|--------|------|
| step_id | string |
| workflow_id | string |
| step_name | string |
| status | string |
| started_at | timestamp |
| completed_at | timestamp |

**workflow_outputs**

Stores intermediate outputs.

| Column | Type |
|--------|------|
| workflow_id | string |
| step_name | string |
| data | jsonb |
| created_at | timestamp |

**river_jobs**

Managed by River. Stores:
- job payload
- status
- retries
- execution metadata

## 11. Deployment Architecture

**Deployment target:** Fly.io

**Services:**
- Go API + Worker (same binary)
- Postgres (external service)

The Go binary runs both:
- HTTP server
- worker pool

Worker concurrency configurable via environment variable:
```
WORKER_CONCURRENCY=10
```

## 12. Failure Handling

Failures are handled at the job level.

**Retry strategy:**
```
max_attempts = 5
exponential_backoff
```

Failed jobs are logged and marked as failed.

Workflow state remains durable because all state is stored in Postgres.

## 13. Observability

Basic observability provided through:
- structured logs
- Postgres queries
- lightweight admin dashboard

Example admin queries:
```sql
SELECT * FROM workflows WHERE status='running'
SELECT * FROM river_jobs WHERE state='failed'
```

**Future enhancement:** simple web UI for workflow inspection.

## 14. Cost Estimate

Expected monthly cost:

| Component | Cost |
|-----------|------|
| Fly.io service | $3-6 |
| Postgres | $0-10 |
| River | free |

**Estimated total:** $3-16/month

LLM API costs are expected to dominate infrastructure costs.

## 15. Future Improvements

Potential enhancements include:

### Workflow visualization UI
Graph-based view of workflows.

### Workflow DSL
Define workflows declaratively:
```go
workflow(
  research,
  plan,
  implement
)
```

### Task fan-in helpers
Utilities for managing parallel job completion.

### Agent abstraction layer
Reusable agents:
- research agent
- planning agent
- implementation agent

## 16. Risks

### Workflow complexity growth
If workflows become very complex, this design may require a migration to a full workflow engine.

**Mitigation:** The system can evolve to use Temporal if necessary.

### Parallel job coordination
Tracking completion across many parallel tasks requires careful state management.

**Mitigation:** Implement workflow-step counters in Postgres.

## 17. Summary

This design provides a lightweight, durable workflow system for agent execution with minimal infrastructure.

**Key characteristics:**
- Go-based execution environment
- Postgres-backed durable jobs
- simple job-chain workflows
- parallel task support
- extremely low infrastructure cost

The system captures most operational benefits of heavier stacks (Trigger.dev, Redis queues) while remaining simple enough for a small team to operate and extend.
