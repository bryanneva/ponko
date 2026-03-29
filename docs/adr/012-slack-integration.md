# ADR-012: Slack as Primary External Interface

## Status
Proposed

## Context
The system needs an interface for:
- Triggering workflows
- Receiving status updates
- Human-in-the-loop interactions
- Notifications and alerts

Options considered:
- **Slack**: Ubiquitous, good bot API, real-time messaging
- **Discord**: Similar capabilities but less common in professional settings
- **Email**: Universal but poor for real-time interaction
- **Custom web UI**: Maximum control but development overhead
- **CLI only**: Simple but limited accessibility

## Decision
Evaluate Slack as the primary external interface for human interaction.

## Consequences

### Positive
- Familiar interface for most users
- Rich bot API with slash commands, buttons, modals
- Real-time notifications
- Mobile accessibility
- Existing team presence (no new tool adoption)

### Negative
- Slack API complexity
- Requires Slack workspace
- Rate limits on bot messages
- Dependency on external service

### Implementation Notes
- Start with slash commands for workflow triggers
- Add interactive buttons for approvals
- Consider webhook-based status updates
