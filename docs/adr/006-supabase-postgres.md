# ADR-006: Supabase as Managed Postgres Provider

## Status
Proposed

## Context
We need a managed Postgres provider that offers:
- Low cost for small workloads
- Easy setup and management
- Reliable backups
- Good connectivity from Fly.io

Alternatives considered:
- **Fly.io Postgres**: Simpler but less mature managed offering
- **AWS RDS**: Enterprise-grade but expensive and complex
- **Neon**: Serverless Postgres, good for variable workloads
- **Railway Postgres**: Simple but limited free tier

## Decision
Evaluate Supabase as the managed Postgres provider.

## Consequences

### Positive
- Generous free tier
- Built-in dashboard for database inspection
- Automatic backups
- Additional features (auth, storage) available if needed

### Negative
- Another vendor dependency
- Network latency between Fly.io and Supabase
- Free tier limitations may require upgrade for production
