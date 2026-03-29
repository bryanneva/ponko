# ADR-002: Go as Implementation Language

## Status
Accepted

## Context
We need to select a primary implementation language for the workflow system. The language should support:
- High-performance concurrent execution
- Simple deployment (minimal runtime dependencies)
- Strong ecosystem for HTTP servers and database access
- Team familiarity

Alternatives considered:
- **Node.js**: Good ecosystem but runtime overhead, less efficient for CPU-bound tasks
- **Python**: Excellent LLM libraries but deployment complexity, GIL limitations
- **Rust**: Maximum performance but steeper learning curve, slower development velocity

## Decision
Use Go as the primary implementation language.

## Consequences

### Positive
- Single static binary deployment
- Excellent concurrency primitives (goroutines, channels)
- Strong standard library for HTTP and database
- Fast compilation and testing cycles
- River (Go library) provides native Postgres job queue

### Negative
- Less mature LLM client libraries compared to Python
- Verbose error handling
- Limited generics (though improved in recent versions)
