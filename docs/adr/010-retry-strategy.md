# ADR-010: Exponential Backoff Retry Strategy

## Status
Accepted

## Context
Jobs may fail due to:
- Transient network errors
- Rate limiting from external APIs
- Temporary resource unavailability
- LLM API errors

We need a retry strategy that:
- Handles transient failures automatically
- Avoids overwhelming failing services
- Eventually gives up on persistent failures
- Provides visibility into failure patterns

## Decision
Use exponential backoff with the following parameters:

```
max_attempts = 5
backoff = exponential
```

River's built-in retry mechanism handles this automatically.

## Consequences

### Positive
- Transient failures recovered automatically
- External services not overwhelmed during outages
- Clear failure visibility after max attempts
- Consistent retry behavior across all job types

### Negative
- Long-running retries may delay workflow completion
- Must distinguish retriable vs. non-retriable errors
- Failed jobs require manual intervention after max attempts

### Configuration
- Default to 5 attempts for most jobs
- Consider shorter retry counts for time-sensitive operations
- Log each retry attempt for debugging
