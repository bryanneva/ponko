package jobs

import "github.com/bryanneva/ponko/internal/llm"

// Compile-time check: *llm.Client must satisfy LLMClient.
var _ LLMClient = (*llm.Client)(nil)
