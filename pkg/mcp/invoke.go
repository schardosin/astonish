package mcp

import (
	"context"
	"fmt"
	"log/slog"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/toolconfirmation"
	"google.golang.org/genai"
)

// InvokeTool creates a temporary MCP manager, initializes the named server,
// finds the tool, calls Run(), cleans up, and returns the result.
// This is a simple per-request approach — each call spins up a fresh MCP
// server process. Suitable for low-frequency calls (dashboard polling at
// 30s+ intervals). For high-frequency use, consider pooling managers.
func InvokeTool(ctx context.Context, serverName, toolName string, args map[string]any) (map[string]any, error) {
	mgr, err := NewManager()
	if err != nil {
		return nil, fmt.Errorf("mcp invoke: failed to create manager: %w", err)
	}
	defer mgr.Cleanup()

	nt, err := mgr.InitializeSingleToolset(ctx, serverName)
	if err != nil {
		return nil, fmt.Errorf("mcp invoke: failed to init server %q: %w", serverName, err)
	}

	// Get tools from the toolset
	toolCtx := &invokeToolContext{Context: ctx}
	tools, err := nt.Toolset.Tools(toolCtx)
	if err != nil {
		return nil, fmt.Errorf("mcp invoke: failed to list tools from %q: %w", serverName, err)
	}

	// Find the requested tool
	var targetTool tool.Tool
	for _, t := range tools {
		if declTool, ok := t.(interface {
			Declaration() *genai.FunctionDeclaration
		}); ok {
			decl := declTool.Declaration()
			if decl != nil && decl.Name == toolName {
				targetTool = t
				break
			}
		}
	}
	if targetTool == nil {
		available := make([]string, 0, len(tools))
		for _, t := range tools {
			if declTool, ok := t.(interface {
				Declaration() *genai.FunctionDeclaration
			}); ok {
				decl := declTool.Declaration()
				if decl != nil {
					available = append(available, decl.Name)
				}
			}
		}
		return nil, fmt.Errorf("mcp invoke: tool %q not found on server %q (available: %v)", toolName, serverName, available)
	}

	// Call Run
	runner, ok := targetTool.(interface {
		Run(tool.Context, any) (map[string]any, error)
	})
	if !ok {
		return nil, fmt.Errorf("mcp invoke: tool %q on %q does not implement Run", toolName, serverName)
	}

	slog.Debug("mcp invoke: calling tool", "server", serverName, "tool", toolName)
	result, err := runner.Run(toolCtx, args)
	if err != nil {
		return nil, fmt.Errorf("mcp invoke: tool %q returned error: %w", toolName, err)
	}

	return result, nil
}

// invokeToolContext is a minimal tool.Context for programmatic MCP tool invocation.
// All optional methods return zero values — this is sufficient for most MCP tools
// which only need the embedded context.Context for cancellation/timeout.
type invokeToolContext struct {
	context.Context
}

func (c *invokeToolContext) Actions() *session.EventActions       { return &session.EventActions{} }
func (c *invokeToolContext) Branch() string                       { return "" }
func (c *invokeToolContext) AgentName() string                    { return "app-data-proxy" }
func (c *invokeToolContext) AppName() string                      { return "astonish" }
func (c *invokeToolContext) Artifacts() agent.Artifacts           { return nil }
func (c *invokeToolContext) FunctionCallID() string               { return "" }
func (c *invokeToolContext) InvocationID() string                 { return "" }
func (c *invokeToolContext) SessionID() string                    { return "" }
func (c *invokeToolContext) UserID() string                       { return "" }
func (c *invokeToolContext) UserContent() *genai.Content          { return nil }
func (c *invokeToolContext) ReadonlyState() session.ReadonlyState { return nil }
func (c *invokeToolContext) State() session.State                 { return nil }
func (c *invokeToolContext) SearchMemory(_ context.Context, _ string) (*memory.SearchResponse, error) {
	return nil, nil
}
func (c *invokeToolContext) RequestConfirmation(_ string, _ any) error   { return nil }
func (c *invokeToolContext) ToolConfirmation() *toolconfirmation.ToolConfirmation { return nil }
