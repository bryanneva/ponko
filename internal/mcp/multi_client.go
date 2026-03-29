package mcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/bryanneva/ponko/internal/llm"
)

type Server interface {
	Initialize(ctx context.Context) error
	ListTools(ctx context.Context) ([]llm.Tool, error)
	CallTool(ctx context.Context, name string, arguments map[string]any) (string, error)
}

type MultiClient struct {
	routing map[string]Server
	tools   []llm.Tool
}

func NewMultiClient(ctx context.Context, servers []Server) (*MultiClient, error) {
	mc := &MultiClient{
		routing: make(map[string]Server),
	}

	for _, s := range servers {
		if initErr := s.Initialize(ctx); initErr != nil {
			slog.Warn("MCP initialize failed, continuing", "error", initErr)
		}
		tools, err := s.ListTools(ctx)
		if err != nil {
			slog.Warn("failed to discover tools from MCP server", "error", err)
			continue
		}
		var added int
		for _, t := range tools {
			if _, exists := mc.routing[t.Name]; exists {
				slog.Warn("skipping duplicate tool name", "tool", t.Name)
				continue
			}
			mc.tools = append(mc.tools, t)
			mc.routing[t.Name] = s
			added++
		}
		slog.Info("discovered MCP tools", "count", added, "skipped_duplicates", len(tools)-added)
	}

	if len(mc.tools) == 0 {
		return nil, fmt.Errorf("no tools discovered from any MCP server")
	}

	return mc, nil
}

func (mc *MultiClient) Tools() []llm.Tool {
	return mc.tools
}

func (mc *MultiClient) CallTool(ctx context.Context, name string, arguments map[string]any, _ *llm.UserScope) (string, error) {
	server, ok := mc.routing[name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	return server.CallTool(ctx, name, arguments)
}
