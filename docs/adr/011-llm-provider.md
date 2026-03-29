# ADR-011: Anthropic Claude as Primary LLM Provider

## Status
Accepted

## Context
The workflow system requires LLM capabilities for:
- Research and analysis tasks
- Planning and decision-making
- Code generation and implementation
- Natural language understanding

We need to select a primary LLM provider considering:
- Model quality and capabilities
- API reliability
- Cost efficiency
- Developer experience

Alternatives considered:
- **OpenAI GPT-4**: Mature ecosystem but higher costs
- **Google Gemini**: Strong capabilities but less established API
- **Open source (Llama, Mistral)**: Lower costs but requires hosting infrastructure

## Decision
Use Anthropic Claude as the primary LLM provider.

## Consequences

### Positive
- Strong reasoning and coding capabilities
- Competitive pricing
- Good API design and documentation
- Context window suitable for agent workflows
- Claude's instruction-following well-suited for structured tasks

### Negative
- Single vendor dependency
- API rate limits may affect parallel execution
- Less ecosystem tooling compared to OpenAI

### Mitigation
- Design LLM client interface to allow provider switching
- Implement rate limiting and backoff at application level
- Monitor Claude API status and have fallback plan
