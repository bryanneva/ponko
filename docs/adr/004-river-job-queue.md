# ADR-004: River as Job Queue

## Status
Accepted

## Context
We need a durable job queue that supports:
- Job persistence across restarts
- Retry with backoff
- Concurrent worker processing
- Visibility into job state

Alternatives considered:
- **Redis + custom queue**: Requires additional infrastructure, more operational complexity
- **AWS SQS**: Vendor lock-in, additional cost
- **RabbitMQ**: Additional service to operate
- **Postgres-based custom solution**: Development overhead, reinventing the wheel

## Decision
Use River, a Postgres-backed job queue library for Go.

## Consequences

### Positive
- No additional infrastructure (uses existing Postgres)
- Native Go library with good ergonomics
- Built-in retry with exponential backoff
- Job state queryable via SQL
- Transactional job enqueue (jobs enqueued in same transaction as business logic)

### Negative
- Postgres becomes a bottleneck for very high job throughput
- Less ecosystem/tooling compared to Redis-based solutions
- Relatively newer library (less battle-tested than alternatives)
