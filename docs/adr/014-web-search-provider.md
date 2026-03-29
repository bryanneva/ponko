# ADR-014: Brave Search API for Web Search

## Status
Accepted

## Context
Ponko needs web search capability to answer questions requiring current information (GH-12). The search provider must fit within the $3-10/month infrastructure budget and work well as an LLM tool (structured results that Claude can summarize).

### Options Evaluated

| Provider | Cost per 1K queries | Free tier | Index | Status |
|----------|-------------------|-----------|-------|--------|
| **Brave Search API** | $5.00 | ~1,000/mo ($5 credit) | Independent (30B+ pages) | Active, LLM-optimized |
| **Serper** | $1.00–$0.30 | 2,500/mo | Google proxy | Active |
| **Tavily** | $8.00 | 1,000/mo | Purpose-built for agents | Active |
| **Google Custom Search** | $5.00 | ~3,000/mo | Google | Deprecated, closed to new customers |
| **Bing Search API** | N/A | N/A | Bing | Shut down Aug 2025 |
| **SerpAPI** | $15.00 | 100/mo | Multi-engine (80+) | Active, expensive |

### Key Findings

**Brave** scored highest in agentic search benchmarks (Agent Score: 14.89) and had the lowest latency (669ms). Its LLM Context API returns pre-ranked, compact "smart chunks" optimized for LLM consumption. In blinded testing, LLMs using Brave's context data outperformed ChatGPT and Perplexity.

**Serper** is 5x cheaper and proxies Google results (better long-tail coverage), but adds a dependency on Google's infrastructure without a direct relationship. If Google changes SERP structure, Serper breaks.

**Tavily** is purpose-built for AI agents with pre-scored results, but costs 60% more than Brave with slightly lower benchmark scores.

**Google Custom Search** and **Bing** are both deprecated/shutting down — not viable.

## Decision
Use **Brave Search API** as the web search provider.

### Rationale
1. **Best agent quality**: Highest benchmarked score for agentic use, lowest latency
2. **LLM-optimized output**: Smart chunks reduce token waste, better than raw SERP scraping
3. **Independent index**: No dependency on Google/Bing infrastructure
4. **Budget fit**: ~1,000 free queries/month covers personal assistant usage; paid rate ($5/1K) is affordable if usage grows
5. **Privacy**: No query logging or tracking
6. **Stability**: Own index means no risk of upstream SERP changes breaking the integration

### Trade-offs Accepted
- Weaker long-tail/niche query coverage compared to Google-proxied alternatives (Serper)
- Slightly more expensive than Serper ($5 vs $1 per 1K queries)
- For Ponko's use cases (weather, news, prices, current events), mainstream coverage is sufficient

## Consequences

### Positive
- Single API key (`BRAVE_API_KEY`) added to Fly.io secrets
- REST API with JSON responses — straightforward to integrate in a Supabase Edge Function or standalone MCP server
- Free tier covers expected personal usage volume
- Results optimized for LLM consumption reduce prompt token costs

### Negative
- Long-tail queries may return less relevant results than Google
- Brave's free tier is credit-based ($5/mo) rather than a true free plan — could change
- Another API key to manage
