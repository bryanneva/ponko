# Design Patterns

Living reference for the architectural principles this system follows.

## 1. Hexagonal Architecture (Ports & Adapters)

The codebase is organized in three concentric layers:

**Core (innermost)** — Domain models and business logic. No infrastructure dependencies. Pure Go types and functions.

**Ports** — Go interfaces that define how the core communicates with the outside world. Defined near the domain consumer, not near the implementation (Go convention: interfaces belong to the caller).

**Adapters (outermost)** — Implementations of ports for specific infrastructure: Postgres, Slack API, Claude API, MCP servers.

### Database Layer

The database is its own layer, not embedded in domain packages. This avoids Go import cycle issues and allows cross-domain query optimization without packages doing a dance to reshape data.

At minimum, a pool interface wrapping the methods we actually use, with a pgx adapter behind it. This enables swapping implementations and mocking in tests.

### Orchestrators

Some components compose across domains without being part of any single domain. Orchestrators live outside the core and depend on ports, never on adapters directly.

## 2. Testing Hierarchy

Many unit tests, fewer integration tests, fewest E2E tests. The pyramid shape matters.

### Unit Tests

Test public methods of "units" (types/packages). Arrive at the unit's API via TDD — write the test first, from the outside perspective, before the implementation exists.

- Mock collaborators via interfaces. Don't test sub-components through layers; that's a smell about something overly complicated.
- Avoid testing private methods. Instead, extract "private" logic into its own testable unit when it's complex enough to warrant direct testing.
- Workers and services should depend on interfaces so collaborators can be mocked.

### Integration Tests

Test the interaction between ports-and-adapters layers. One integration point per test, not end-to-end.

- Job pipeline handoffs belong here — mock side effects and upstream dependencies, test that the handoff works.
- Database adapter tests: verify that the adapter correctly implements the port against a real database.
- Keep these focused on a single layer boundary.

### Contract Tests

A subset of integration tests that verify our system's interaction with external systems.

These protect against external API changes breaking our adapters.

### E2E Tests

Journey pattern oriented around user personas and jobs-to-be-done. Build up state from nothing — no fixtures, no pre-seeded data.

- Oriented around "a user wants to accomplish X" — not around internal system states.
- Minimal edge cases. If an edge case is checked, do it in a way that's recoverable (the journey continues).
- Leave edge case coverage to unit and integration layers.
- E2E tests are expensive; each one should represent a real user journey that can't be adequately covered by lower layers.

## 3. Composition Over Inheritance

Compose behavior by embedding interfaces and delegating. Go doesn't have classical inheritance, but the temptation exists via struct embedding. Prefer explicit delegation.

- Build complex behavior by composing simple, focused types
- When a type needs behavior from another type, take it as an interface parameter — don't embed the concrete type

## 4. SOLID via Go Idioms

Standard SOLID principles, adapted to Go's conventions:

| Principle | Go Expression |
|-----------|--------------|
| **Single Responsibility** | One package, one concern. Package name = what it does. |
| **Open/Closed** | Extend via new interface implementations, not modifying existing code. |
| **Liskov Substitution** | Implicit in Go — if it satisfies the interface, it substitutes. |
| **Interface Segregation** | Small interfaces. Go convention: 1-2 methods. `io.Reader` over `io.ReadWriteCloser`. |
| **Dependency Inversion** | Depend on interfaces, not concrete types. Interfaces defined by the consumer, not the provider. |

## 5. Repeat Yourself (Until ~3 Times)

Don't abstract prematurely. Duplication is cheaper than the wrong abstraction.

- The first time you write something: just write it.
- The second time: note the duplication, tolerate it.
- The third time: now you have enough examples to extract the right abstraction.

This applies to helpers, utilities, and middleware. Domain types should still be modeled correctly from the start.

This is NOT "DRY" — it's a feedback loop for identifying when abstraction is actually warranted versus when you're guessing at a pattern that doesn't exist yet.

## 6. Idempotency

First-class principle in an agent system where workers retry on failure.

Every worker that produces side effects must answer: "What happens if I run twice with the same input?"

### Side Effect Ordering

Transition state first, then produce side effects. If side effects must come first, make them idempotent.

**Bad:**
```
createOutboxEntry()   // side effect
transitionStatus()    // if this fails, retry creates duplicate entries
```

**Good:**
```
transitionStatus()    // state change first
createOutboxEntry()   // side effect after — if this fails, retry is safe because status is already transitioned
```

Alternatively, use idempotency keys so repeated side effects are no-ops.

## 7. Immutability & State Transfer

Prefer immutable objects over carried mutable state. Transfer state changes by creating updated copies, not by mutating and passing references.

### State Pattern

When behavior varies by state, use the [State Pattern](https://refactoring.guru/design-patterns/state) — embed state behavior within the object itself. Each state knows its own transitions and valid operations.

No exhaustive finite state machine libraries. The state pattern keeps transitions local and comprehensible.

### Schema Direction

The database schema follows from the core domain model, not the other way around. Design the domain objects first, then figure out how to persist them. If the persistence layer is driving your domain model shape, something is backwards.

## 8. Observability

Pragmatic observability — present everywhere but not a rigid architectural layer. Shortcuts are fine when a strict layer would add more complexity than value.

- **Structured logging** via `slog` — contextual fields, not string interpolation
- **Distributed tracing** via OpenTelemetry — trace correlation across job chains is critical for debugging agent workflows
- **Metrics** via OpenTelemetry — request counts, latencies, error rates

## 9. Configuration Layering

Three distinct configuration layers, each with a clear home:

| Layer | Home | Examples |
|-------|------|----------|
| **Infrastructure** | Env vars / flags (viper/cobra style) | DB connection, API keys, port, worker concurrency |
| **Runtime** | Database | Channel behavior, respond modes, tool allowlists |
| **Domain** | Code | System prompt templates, tool definitions, model selection |

Mixing layers creates brittleness. If you find yourself checking env vars in domain logic or hardcoding runtime behavior in code, something is in the wrong layer.

Use standard Go configuration patterns (viper/cobra) for infrastructure config rather than raw `os.Getenv` scattered through the entrypoint.

## 10. Prompt Assembly

Agent-specific pattern for how system prompts are composed.

### Principle: Tool-Prompt Co-registration

Each tool domain should register both its tools AND its prompt additions as a unit. When a tool is available, its prompt comes along automatically. This eliminates implicit coupling between tool names and prompt strings.

Each `llm.Tool` carries an optional system prompt addition. The prompt builder iterates surviving tools (after channel config filtering) and collects their prompt additions. No tool-name sniffing needed.
