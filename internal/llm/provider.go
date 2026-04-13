package llm

import "context"

// Provider defines the LLM operations that any provider must implement.
// The concrete ClaudeProvider (Client) satisfies this interface.
// Future adapters (Gemini, Ollama) will implement this to enable provider swapping.
type Provider interface {
	SendMessage(ctx context.Context, prompt string, model string) (string, error)
	SendConversation(ctx context.Context, systemPrompt string, messages []Message, model string) (string, error)
	SendConversationWithTools(ctx context.Context, systemPrompt string, messages []Message, tools []Tool, toolCaller ToolCaller, userScope *UserScope, model string) (string, error)
}
