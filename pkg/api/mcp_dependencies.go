package api

import (
	"strings"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/flowstore"
	"github.com/schardosin/astonish/pkg/mcpstore"
)

// ResolveMCPDependencies analyzes tools used in a flow and resolves them to MCP server dependencies.
// It looks up each tool's source server and determines if it comes from:
// - store: official MCP store (or custom tap in the store)
// - tap: a tapped repository
// - inline: user's locally configured server (fallback)
func ResolveMCPDependencies(toolsSelection []string, cachedTools []ToolInfo, storeServers []mcpstore.Server) []config.MCPDependency {
	if len(toolsSelection) == 0 {
		return nil
	}

	// Build a map of tool name -> server source
	toolToServer := make(map[string]string)
	for _, tool := range cachedTools {
		toolToServer[tool.Name] = tool.Source
	}

	// Load user's MCP config
	mcpConfig, _ := config.LoadMCPConfig()

	// Group tools by their server source
	serverToTools := make(map[string][]string)
	for _, toolName := range toolsSelection {
		serverName := toolToServer[toolName]
		if serverName == "" || serverName == "internal" {
			continue // Skip internal/unknown tools
		}
		serverToTools[serverName] = append(serverToTools[serverName], toolName)
	}

	// Build dependency list
	var deps []config.MCPDependency
	for serverName, tools := range serverToTools {
		dep := config.MCPDependency{
			Server: serverName,
			Tools:  tools,
		}

		// Try to find matching store server
		var matchedServer *mcpstore.Server
		var matchSource string

		// First, try to match by comparing user's server config to store server configs
		if mcpConfig != nil {
			if userServerCfg, found := mcpConfig.MCPServers[serverName]; found {
				// Build a signature from command + args for matching
				userSig := buildServerSignature(userServerCfg.Command, userServerCfg.Args)
				
				for i := range storeServers {
					srv := &storeServers[i]
					if srv.Config != nil {
						storeSig := buildServerSignature(srv.Config.Command, srv.Config.Args)
						if userSig == storeSig {
							matchedServer = srv
							matchSource = srv.Source
							break
						}
					}
				}
			}
		}

		// Second fallback: try matching by name (case-insensitive, partial)
		if matchedServer == nil {
			serverLower := strings.ToLower(serverName)
			for i := range storeServers {
				srv := &storeServers[i]
				nameLower := strings.ToLower(srv.Name)
				// Try exact match first, then partial
				if serverLower == nameLower || strings.Contains(serverLower, nameLower) || strings.Contains(nameLower, serverLower) {
					matchedServer = srv
					matchSource = srv.Source
					break
				}
			}
		}

		if matchedServer != nil {
			if matchSource == flowstore.OfficialStoreName {
				dep.Source = "store"
				dep.StoreID = matchedServer.McpId
			} else {
				dep.Source = "tap"
			}
		} else if mcpConfig != nil {
			// Fallback to inline from user's config
			if serverCfg, found := mcpConfig.MCPServers[serverName]; found {
				dep.Source = "inline"
				// Clear env values for security
				clearedEnv := make(map[string]string)
				for k := range serverCfg.Env {
					clearedEnv[k] = ""
				}
				dep.Config = &config.MCPServerConfig{
					Command:   serverCfg.Command,
					Args:      serverCfg.Args,
					Env:       clearedEnv,
					Transport: serverCfg.Transport,
				}
			} else {
				dep.Source = "inline"
			}
		} else {
			dep.Source = "inline"
		}

		deps = append(deps, dep)
	}

	return deps
}

// buildServerSignature creates a normalized signature from command and args for matching
func buildServerSignature(command string, args []string) string {
	// Normalize the signature: command + sorted/cleaned args
	sig := strings.ToLower(command)
	for _, arg := range args {
		// Skip version-specific parts and paths
		argLower := strings.ToLower(arg)
		if !strings.HasPrefix(argLower, "/") && !strings.Contains(argLower, "@") {
			sig += "|" + argLower
		} else if strings.Contains(argLower, "@") {
			// Extract package name without version
			parts := strings.Split(argLower, "@")
			sig += "|" + parts[0]
		}
	}
	return sig
}

// CollectToolsFromNodes extracts all tools_selection from all nodes in an agent config.
func CollectToolsFromNodes(nodes []config.Node) []string {
	toolSet := make(map[string]bool)
	for _, node := range nodes {
		for _, tool := range node.ToolsSelection {
			toolSet[tool] = true
		}
	}

	var tools []string
	for tool := range toolSet {
		tools = append(tools, tool)
	}
	return tools
}
