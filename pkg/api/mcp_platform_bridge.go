package api

import (
	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/store"
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
