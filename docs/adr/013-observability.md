# ADR-013: Observability

## Status
Superseded — evolved from minimal approach to full OpenTelemetry instrumentation (March 2026).

## Original Context
We need visibility into:
- Workflow execution status
- Job failures and retries
- System health
- Performance metrics

Options originally considered:
- **Full observability stack**: Prometheus, Grafana, distributed tracing
- **Managed APM**: Datadog, New Relic (expensive)
- **Minimal approach**: Structured logs + SQL queries
- **Cloud-native**: Fly.io metrics + logs

## Original Decision (2025)
Start with a minimal observability approach:
1. Structured logging (JSON format)
2. SQL queries against workflow and job tables
3. Fly.io built-in metrics and logs

This served the project well during initial development — zero cost, no infrastructure, and sufficient for debugging single-request issues.

## Evolution to OpenTelemetry (March 2026)

### Why the minimal approach became insufficient
As the system grew to include multi-step workflow chains (receive → plan → execute → process → synthesize → respond), structured logs alone could not answer:
- Which step in a workflow chain is slow?
- How do LLM call latencies vary across providers and models?
- What does the full lifecycle of a Slack mention look like end-to-end?

Without distributed tracing, debugging required manually correlating log entries by workflow ID — error-prone and time-consuming.

### New Decision
Adopt OpenTelemetry with Grafana Cloud as the backend:

1. **OTel SDK over vendor agents** — vendor-neutral, avoids lock-in, single SDK covers traces + metrics + logs
2. **Grafana Cloud free tier over self-hosted** — generous limits (50 GB logs, 10K active metrics, 50 GB traces/month), no infrastructure to maintain, fits $3-10/month target
3. **Traces as primary signal** — workflow chains are the core abstraction; traces map naturally to them
4. **slog bridge for log correlation** — existing slog calls automatically gain trace/span IDs without changing call sites

### Implementation Approach
- `internal/otel/` package handles SDK initialization, context propagation, and metrics helpers
- HTTP middleware via `otelhttp` for inbound request tracing
- Outbound HTTP transport wrapping for LLM and MCP call tracing
- W3C `traceparent` propagation through River job args for cross-job trace linking
- Runtime metrics (Go memory, GC, goroutines) via `runtime.Start()`
- Custom counters: `ponko.workflow.completed`, `ponko.workflow.failed`

See [docs/otel-conventions.md](../otel-conventions.md) for naming conventions and attribute patterns.

### No-op by Default
When `OTEL_EXPORTER_OTLP_ENDPOINT` is not set, OTel initializes with no-op providers — zero overhead, no behavioral change. This keeps local development and tests unaffected.

## Consequences (Updated)

### Positive
- End-to-end trace visibility across workflow chains
- Log-to-trace correlation via automatic trace/span ID injection
- Runtime and custom metrics without additional tooling
- Grafana Cloud free tier stays within budget
- No-op mode means zero impact on local development
- Vendor-neutral — can switch backends by changing exporter config

### Negative
- Additional Go dependencies (~10 OTel packages)
- TraceContext field added to job args structs (minor schema change)
- Grafana Cloud free-tier limits could be hit at scale (50 GB traces/month)

### SQL Queries Still Useful
The original SQL queries remain valid for workflow and job analysis. OTel complements rather than replaces them — SQL for state queries, traces for latency and flow analysis.

```sql
-- Active workflows
SELECT * FROM workflows WHERE status = 'running';

-- Failed jobs in last hour
SELECT * FROM river_jobs
WHERE state = 'failed'
AND finalized_at > NOW() - INTERVAL '1 hour';

-- Workflow duration statistics
SELECT workflow_type,
       AVG(EXTRACT(EPOCH FROM (updated_at - created_at))) as avg_duration
FROM workflows
WHERE status = 'completed'
GROUP BY workflow_type;
```
