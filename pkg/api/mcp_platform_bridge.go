package api

import (
	"context"
	"log/slog"

	"github.com/schardosin/astonish/pkg/common"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/mcp"
	"github.com/schardosin/astonish/pkg/store"
)

// mcpServerToConfig converts a store.MCPServer to config.MCPServerConfig
// for use with the MCP Manager's transport creation logic.
func mcpServerToConfig(server *store.MCPServer) config.MCPServerConfig {
	return config.MCPServerConfig{
		Command:   server.Command,
		Args:      server.Args,
		Env:       server.Env,
		Transport: server.Transport,
		URL:       server.URL,
		Enabled:   server.Enabled,
	}
}

// platformMCPManager is a simplified manager for discovering tools from a single MCP server.
type platformMCPManager struct {
	name string
	cfg  config.MCPServerConfig
	mgr  *mcp.Manager
}

// newSingleServerManager creates a manager that can discover tools for a single server.
// Returns nil if it cannot be created (no valid config).
func newSingleServerManager(name string, cfg config.MCPServerConfig) *platformMCPManager {
	return &platformMCPManager{
		name: name,
		cfg:  cfg,
	}
}

// DiscoverTools starts the MCP server, lists its tools, and returns them.
func (p *platformMCPManager) DiscoverTools() ([]MCPDiscoveredTool, error) {
	// Create a temporary MCP config with just this server
	mcpCfg := &config.MCPConfig{
		MCPServers: map[string]config.MCPServerConfig{
			p.name: p.cfg,
		},
	}

	// Create a manager with this config
	mgr := mcp.NewManagerFromConfig(mcpCfg)
	p.mgr = mgr

	// Initialize the toolset (starts the server and discovers tools)
	ctx := context.Background()
	if err := mgr.InitializeToolsets(ctx); err != nil {
		slog.Warn("MCP platform tool discovery failed", "server", p.name, "error", err)
		return nil, err
	}

	// Extract tool declarations from the initialized toolsets
	var tools []MCPDiscoveredTool
	for _, nt := range mgr.GetNamedToolsets() {
		if nt.Name != p.name {
			continue
		}
		// Get tool definitions from the toolset using the same minimalReadonlyContext
		// pattern used by RefreshSingleServer and other MCP handlers.
		roCtx := &minimalReadonlyContext{Context: ctx}
		mcpTools, err := nt.Toolset.Tools(roCtx)
		if err != nil {
			slog.Warn("failed to list tools from MCP server", "server", p.name, "error", err)
			continue
		}

		for _, t := range mcpTools {
			tools = append(tools, MCPDiscoveredTool{
				Name:        t.Name(),
				Description: t.Description(),
				InputSchema: common.ExtractToolInputSchema(t),
			})
		}
	}

	return tools, nil
}

// Cleanup stops the MCP server process.
func (p *platformMCPManager) Cleanup() {
	if p.mgr != nil {
		p.mgr.Cleanup()
	}
}
