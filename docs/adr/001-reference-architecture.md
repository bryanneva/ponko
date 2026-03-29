# ADR-001: Reference Architecture

## Status
Accepted

## Context
We need to define the overall system topology for the Lightweight Agent Workflow System. The architecture must support:
- Durable workflow execution
- Parallel task coordination
- Low operational complexity
- Minimal infrastructure cost

## Decision
Adopt a layered architecture with the following topology:

```
External Systems (Slack, GitHub, Email)
        ↓
    HTTP API (Go Service)
        ↓
    Workflow Controller
        ↓
    River Queue (Postgres-backed)
        ↓
    Workers (concurrent)
        ↓
    LLM APIs (Anthropic Claude)
        ↓
    External Actions
```

Key characteristics:
- Single Go binary contains both API and workers
- All durable state stored in Postgres
- River provides job queue semantics on top of Postgres
- Workers process jobs concurrently

## Consequences

### Positive
- Simple deployment (single binary + database)
- All state queryable via SQL
- No additional infrastructure (Redis, etc.)
- Clear separation of concerns

### Negative
- Limited to Postgres throughput for job processing
- Single-region deployment initially
- Workers must share database connections efficiently
