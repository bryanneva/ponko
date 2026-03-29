# ADR-007: Single Binary Deployment

## Status
Accepted

## Context
We need to decide how to package and deploy the system components:
- HTTP API server
- Worker pool
- Workflow controller

Options:
1. **Separate services**: API and workers as independent deployments
2. **Single binary**: All components in one executable
3. **Monorepo with shared code**: Multiple binaries from same codebase

## Decision
Deploy as a single Go binary containing both the HTTP API server and worker pool.

## Consequences

### Positive
- Simplest deployment model
- No inter-service communication overhead
- Single codebase, single deployment artifact
- Easier local development and testing
- Lower infrastructure cost (one service vs. multiple)

### Negative
- Scaling API and workers together (may over-provision one)
- Single failure domain
- Cannot scale components independently

### Mitigation
- Worker concurrency configurable via `WORKER_CONCURRENCY` environment variable
- Can split into separate services later if scaling needs change
