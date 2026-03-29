# ADR-008: Job-Chain Workflow Model

## Status
Accepted

## Context
We need a workflow execution model that supports:
- Sequential step execution
- Durability across failures
- Simple mental model for developers
- Extensibility to parallel execution

Options:
1. **DAG engine**: Full directed acyclic graph execution (like Temporal, Airflow)
2. **Job chains**: Linear sequences where each job enqueues the next
3. **State machine**: Explicit state transitions

## Decision
Use a job-chain model where each job:
1. Performs its work
2. Persists its output
3. Enqueues the next job in the sequence

```
ResearchJob → PlanJob → ImplementJob
```

## Consequences

### Positive
- Simple mental model
- Easy to implement and debug
- Each step is independently retriable
- Clear execution flow
- Works well with River's job model

### Negative
- No built-in DAG support (complex dependencies require manual coordination)
- Fan-out/fan-in requires additional patterns (see ADR-009)
- Less flexible than full workflow engines

### Trade-off
Simplicity over flexibility. If complex DAG workflows become necessary, can migrate to Temporal.
