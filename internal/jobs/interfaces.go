package jobs

import (
	"context"

	"github.com/bryanneva/ponko/internal/llm"
)

// LLMClient defines LLM operations used by workers.
type LLMClient interface {
	SendMessage(ctx context.Context, prompt string, model string) (string, error)
	SendConversation(ctx context.Context, systemPrompt string, messages []llm.Message, model string) (string, error)
	SendConversationWithTools(ctx context.Context, systemPrompt string, messages []llm.Message, tools []llm.Tool, toolCaller llm.ToolCaller, userScope *llm.UserScope, model string) (string, error)
}
