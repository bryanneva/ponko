# Security Policy

## Supported Versions

Ponko is currently in early development. Security fixes are applied to the latest version on `main`.

## Reporting a Vulnerability

**Do not file security vulnerabilities as public GitHub Issues.**

If you discover a security vulnerability, please report it by emailing the maintainer directly. You can find contact info on the GitHub profile.

Please include:
- A description of the vulnerability
- Steps to reproduce
- Potential impact
- Any suggested fixes (optional)

You should receive a response within 72 hours. Once the issue is confirmed, we'll work on a fix and coordinate a disclosure timeline with you.

## Scope

Ponko runs as a self-hosted service. Users are responsible for securing their own deployments (Fly.io secrets, Postgres access, Slack bot token handling). Reports about misconfigurations in user-controlled infrastructure are out of scope.

In-scope reports include vulnerabilities in:
- The Ponko binary itself
- The API surface (`/api/*`, `/slack/*`)
- Authentication and authorization logic
- Data handling (Postgres queries, job processing)
