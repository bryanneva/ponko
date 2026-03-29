# ADR-009: Counter-Based Parallel Coordination

## Status
Accepted

## Context
Many workflows require parallel execution:
- Analyze 20 files concurrently
- Scrape 50 pages simultaneously
- Process documents in parallel

We need a pattern for:
- Enqueueing multiple parallel jobs (fan-out)
- Detecting when all parallel jobs complete (fan-in)
- Triggering the next workflow step

## Decision
Use counter-based fan-in tracking in Postgres:

```
expected_tasks = N
completed_tasks = 0
```

Each parallel job:
1. Completes its work
2. Atomically increments `completed_tasks`
3. Checks if `completed_tasks == expected_tasks`
4. If complete, enqueues the next workflow step

## Consequences

### Positive
- Simple to implement
- Uses existing Postgres infrastructure
- Atomic increment prevents race conditions
- No additional coordination service needed

### Negative
- Requires careful transaction management
- Counter state adds complexity to workflow model
- All parallel jobs must know the expected count

### Implementation Notes
- Use `UPDATE ... RETURNING` for atomic increment and check
- Consider workflow_step table with `expected_count` and `completed_count` columns
