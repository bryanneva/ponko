# ADR-003: Fly.io as Deployment Platform

## Status
Accepted

## Context
We need to select a deployment platform that supports:
- Low cost for small workloads
- Simple deployment process
- Go binary hosting
- Postgres connectivity (managed or external)

Alternatives considered:
- **AWS/GCP/Azure**: Full-featured but complex setup, higher base costs
- **Heroku**: Simple but expensive for always-on services
- **Railway**: Good developer experience but higher costs
- **Self-hosted VPS**: Low cost but operational overhead

## Decision
Use Fly.io as the primary deployment platform.

## Consequences

### Positive
- Low cost ($3-6/month for small instances)
- Simple CLI-based deployment
- Built-in support for Go applications
- Global edge network available if needed later
- Easy horizontal scaling

### Negative
- Smaller ecosystem than major clouds
- Limited managed service integrations
- May need external Postgres provider for production
