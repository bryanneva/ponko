# ADR-005: PostgreSQL as Single Source of Truth

## Status
Accepted

## Context
We need a storage solution for:
- Workflow definitions and state
- Step execution history
- Intermediate outputs
- Job queue persistence (via River)

The storage must support:
- ACID transactions
- Structured queries for observability
- JSON/JSONB for flexible payloads
- High availability for production use

## Decision
Use PostgreSQL as the single source of truth for all persistent state.

## Consequences

### Positive
- Single database to operate and backup
- SQL queries for observability and debugging
- JSONB support for flexible workflow outputs
- Transactional consistency across workflow state and job queue
- Mature, well-understood technology
- Many managed hosting options

### Negative
- Single point of failure (mitigated by managed hosting)
- Scaling limits for very high throughput
- No built-in caching layer (acceptable for target workloads)
