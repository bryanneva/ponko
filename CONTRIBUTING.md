# Contributing to Ponko

Thanks for your interest in contributing! Ponko is a self-hosted AI agent that lives in Slack — a single Go binary backed by Postgres.

## Getting Started

### Prerequisites

- Go 1.22+
- PostgreSQL (local or via Docker)
- A Slack workspace where you can install a bot (for integration testing)

### Local Setup

1. Clone the repo and install dependencies:
   ```bash
   git clone https://github.com/bryanneva/ponko.git
   cd ponko
   go mod download
   ```

2. Copy the example env file and fill in your credentials:
   ```bash
   cp .env.example .env
   ```

3. Run the setup CLI to configure your deployment:
   ```bash
   make build
   ./bin/setup
   ```

### Running Tests

```bash
make test-unit          # Fast unit tests, no external dependencies
make test-e2e           # Full integration tests (requires local Postgres + ANTHROPIC_API_KEY)
make test-cover         # Unit tests with coverage report
make lint               # Run golangci-lint
make build              # Compile the binary
```

Unit tests run fast and have no external dependencies. Run them frequently during development.

E2E tests require a running Postgres instance and a valid `ANTHROPIC_API_KEY`. They exercise the full request path including the database and AI calls.

## Making Changes

### Code Style

- Follow standard Go conventions (`gofmt`, `golangci-lint` enforced in CI)
- No excessive comments — only comment non-obvious "why" decisions, not what the code does
- No unnecessary abstractions — prefer three similar lines over a premature helper
- Proper error handling — never silently swallow errors

### Test-Driven Development

New code follows TDD:

1. Write a failing test based on expected behavior
2. Run `make test-unit` to confirm it fails
3. Implement minimum code to make it pass
4. Run `make test-unit` again to confirm green

### Architecture

Ponko follows a ports-and-adapters (hex) architecture. See [docs/architecture.md](docs/architecture.md) for the full picture. The key constraint: keep infrastructure imports out of the core domain packages.

## Submitting a Pull Request

1. Fork the repo and create a branch from `main`
2. Make your changes with tests
3. Ensure `make lint`, `make test-unit`, and `make test-e2e` all pass
4. Open a PR against `main` with a clear description of what and why

### PR Expectations

- Link the related issue (e.g., "Closes #42")
- Describe what changed and why, not just what the code does
- Keep PRs focused — one concern per PR
- Tests are required for non-trivial changes

## Reporting Issues

Use GitHub Issues. Bug reports and feature requests each have a template to fill in. The more context you provide, the faster we can triage.

**Security issues**: see [SECURITY.md](SECURITY.md) — please do not file security vulnerabilities as public issues.

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md). By participating, you agree to uphold it.
