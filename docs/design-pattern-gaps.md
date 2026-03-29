# Design Pattern Gaps

Known deviations from [design-patterns.md](design-patterns.md). Each gap is a candidate ticket. Deeper analysis via RPI research may surface additional gaps not listed here.

## Hexagonal Architecture

- **No pool interface** — `workflow.CreateWorkflow`, `saga`, `conversation` packages take `*pgxpool.Pool` directly. Need a pool interface with pgx adapter.
- **No repository layer** — Domain functions embed SQL queries directly. No separation between data access and domain logic.
- **Orchestrator depends on adapters** — Workers receive concrete types (`*slack.Client`, `*llm.Client`) instead of ports/interfaces.

## Testing Hierarchy

- **Inverted pyramid** — Too many E2E tests, too few integration tests, limited unit tests with mocks.
- **Workers not unit-testable** — Workers take concrete types, not interfaces. Can't mock collaborators.
- **No integration tests for job pipeline** — receive → process → respond handoffs are untested at the integration layer.
- **"Unit" tests hitting real DB** — Tests using `testutil.TestDB` are effectively integration tests miscategorized as unit tests.
- **No contract tests** — No tests verifying our assumptions about Slack API, Claude API, or MCP protocol responses.
- **E2E covering too many edge cases** — Some E2E tests should be pushed down to unit/integration layers.

## Configuration

- **Raw `os.Getenv` everywhere** — No viper/cobra, no startup validation, no structured config object.
- **No config validation at load time** — Invalid channel configs can persist in DB undetected.
- **Mixed config layers** — Some domain config (prompt additions) lives in code but is coupled to runtime config (tool allowlists) via string matching.

## Prompt Assembly

- **Tool-prompt coupling via string sniffing** — `toolsInclude()` checks tool names to decide which prompt additions to include. Adding a new tool domain requires editing three places.
- **No co-registration** — Tools and their prompt additions are defined separately with no structural link.

## Immutability & State

- **Mutable state passing** — Workers mutate shared state and pass references rather than creating updated copies.
- **No State Pattern usage** — Workflow/saga state transitions are procedural, not embedded in the domain objects.
- **Schema-driven domain** — Some domain types mirror the DB schema rather than the other way around.

## Idempotency

- **Side effect ordering not consistent** — Some workers produce side effects before state transitions, risking duplicates on retry.
