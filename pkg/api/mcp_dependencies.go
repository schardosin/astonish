package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/flowstore"
	"github.com/SAP/astonish/pkg/mcpstore"
	"github.com/SAP/astonish/pkg/store"
	"github.com/SAP/astonish/pkg/tools"
)

// ResolveMCPDependencies analyzes tools used in a flow and resolves them to MCP server dependencies.
// It looks up each tool's source server and determines if it comes from:
// - store: official MCP store (or custom tap in the store)
// - tap: a tapped repository
// - inline: user's locally configured server (fallback)
// ResolveMCPDependencies analyzes tools used in a flow and resolves them to MCP server dependencies.
// It looks up each tool's source server and determines if it comes from:
// - store: official MCP store (or custom tap in the store)
// - tap: a tapped repository
// - inline: user's locally configured server (fallback)
func ResolveMCPDependencies(toolsSelection []string, cachedTools []ToolInfo, storeServers []mcpstore.Server, existingDeps []config.MCPDependency, mcpConfig *config.MCPConfig) []config.MCPDependency {
	if len(toolsSelection) == 0 {
		return nil
	}

	// Build a map of tool name -> server source
	toolToServer := make(map[string]string)
	builtInTools := flowBuiltInToolSet()

	// 1. First populate from system cache (installed tools)
	for _, tool := range cachedTools {
		toolToServer[tool.Name] = tool.Source
	}

	// 2. Fallback: populate from existing YAML configuration (for uninstalled tools)
	for _, dep := range existingDeps {
		for _, toolName := range dep.Tools {
			if _, exists := toolToServer[toolName]; !exists {
				toolToServer[toolName] = dep.Server
			}
		}
	}

	// Group tools by their server source
	serverToTools := make(map[string][]string)
	for _, toolName := range toolsSelection {
		if builtInTools[toolName] {
			continue
		}
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

		// Match by exact server name (case-insensitive) against store servers
		// Normalize the same way as InstallMCPStoreServerHandler does: lowercase + spaces to hyphens
		serverLower := strings.ToLower(serverName)
		for i := range storeServers {
			srv := &storeServers[i]
			// Normalize store server name the same way as install handler:
			// strings.ToLower(strings.ReplaceAll(server.Name, " ", "-"))
			storeNameNormalized := strings.ToLower(strings.ReplaceAll(srv.Name, " ", "-"))
			if serverLower == storeNameNormalized {
				matchedServer = srv
				matchSource = srv.Source
				break
			}
		}

		// Fallback: match by config signature (command + args) if name didn't match
		// This handles custom-named servers installed from the store
		if matchedServer == nil && mcpConfig != nil {
			if serverCfg, found := mcpConfig.MCPServers[serverName]; found {
				for i := range storeServers {
					srv := &storeServers[i]
					if srv.Config != nil && srv.Config.Command != "" && configsMatch(serverCfg, *srv.Config) {
						matchedServer = srv
						matchSource = srv.Source
						break
					}
				}
			}
		}

		if matchedServer != nil {
			if matchSource == flowstore.OfficialStoreName {
				dep.Source = "store"
				dep.StoreID = matchedServer.McpId
			} else {
				dep.Source = "tap"
				dep.StoreID = matchedServer.McpId // Also set for taps to enable installation
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

func sanitizeMCPDependencies(deps []config.MCPDependency) []config.MCPDependency {
	if len(deps) == 0 {
		return nil
	}
	builtInTools := flowBuiltInToolSet()
	cleaned := make([]config.MCPDependency, 0, len(deps))
	for _, dep := range deps {
		if len(dep.Tools) == 0 {
			cleaned = append(cleaned, dep)
			continue
		}
		filteredTools := make([]string, 0, len(dep.Tools))
		for _, toolName := range dep.Tools {
			if !builtInTools[toolName] {
				filteredTools = append(filteredTools, toolName)
			}
		}
		if len(filteredTools) == 0 {
			continue
		}
		dep.Tools = filteredTools
		cleaned = append(cleaned, dep)
	}
	return cleaned
}

func flowBuiltInToolSet() map[string]bool {
	decls := tools.GetAllFlowToolDeclarations()
	out := make(map[string]bool, len(decls))
	for _, decl := range decls {
		out[decl.Name] = true
	}
	return out
}

// configsMatch compares two MCP server configs to determine if they're from the same source
// It compares command and args, ignoring env (which varies by user)
func configsMatch(userCfg config.MCPServerConfig, storeCfg mcpstore.ServerConfig) bool {
	// Command must match
	if userCfg.Command != storeCfg.Command {
		return false
	}

	// Args must match exactly
	if len(userCfg.Args) != len(storeCfg.Args) {
		return false
	}
	for i, arg := range userCfg.Args {
		if arg != storeCfg.Args[i] {
			return false
		}
	}

	return true
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

// MCPDependencyStatus represents the status of a single MCP dependency
type MCPDependencyStatus struct {
	Server    string                  `json:"server"`
	Tools     []string                `json:"tools"`
	Source    string                  `json:"source"` // store, tap, inline
	StoreID   string                  `json:"store_id,omitempty"`
	Config    *config.MCPServerConfig `json:"config,omitempty"`
	Installed bool                    `json:"installed"`
}

// CheckMCPDependenciesRequest is the request body for checking dependencies
type CheckMCPDependenciesRequest struct {
	Dependencies []config.MCPDependency `json:"dependencies"`
}

// CheckMCPDependenciesResponse is the response for the check endpoint
type CheckMCPDependenciesResponse struct {
	Dependencies []MCPDependencyStatus `json:"dependencies"`
	AllInstalled bool                  `json:"all_installed"`
	Missing      int                   `json:"missing"`
}

// CheckMCPDependenciesHandler handles POST /api/mcp-dependencies/check
// Checks which MCP servers from the dependency list are installed.
// In platform mode, checks both org and team DB stores.
// In personal mode, checks the filesystem config.
func CheckMCPDependenciesHandler(w http.ResponseWriter, r *http.Request) {
	var req CheckMCPDependenciesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	req.Dependencies = sanitizeMCPDependencies(req.Dependencies)

	// Determine installed servers based on mode
	installedServers := make(map[string]bool)

	if svc := store.FromRequest(r); svc != nil && svc.Mode == store.ModePlatform {
		// Platform mode: check platform, org, and team DB stores
		if svc.PlatformMCPServers != nil {
			platformServers, err := svc.PlatformMCPServers.List(r.Context())
			if err == nil {
				for _, s := range platformServers {
					installedServers[s.Name] = true
				}
			}
		}
		if svc.MCPServers != nil {
			orgServers, err := svc.MCPServers.List(r.Context())
			if err == nil {
				for _, s := range orgServers {
					installedServers[s.Name] = true
				}
			}
		}
		if svc.TeamMCPServers != nil {
			teamServers, err := svc.TeamMCPServers.List(r.Context())
			if err == nil {
				for _, s := range teamServers {
					installedServers[s.Name] = true
				}
			}
		}
		// Also include standard servers (Tavily, Brave, etc.) that are configured
		// via config.yaml / credential store. These are never stored in the DB
		// but are always available at runtime via mergeStandardServers().
		// Use the request-scoped effective config so team-level WebSearchTool is honored.
		stdCfg := &config.MCPConfig{MCPServers: make(map[string]config.MCPServerConfig)}
		config.MergeStandardServersWithConfig(stdCfg, effectiveAppConfig(r))
		for name := range stdCfg.MCPServers {
			installedServers[name] = true
		}
	} else {
		// Personal mode: check filesystem config
		mcpConfig, err := config.LoadMCPConfig()
		if err == nil && mcpConfig != nil {
			for name := range mcpConfig.MCPServers {
				installedServers[name] = true
			}
		}
	}

	// Load store servers for resolving store_id if missing
	storeServers, storeErr := loadAllServersFromTaps()
	if storeErr != nil {
		slog.Warn("failed to load MCP store servers", "error", storeErr)
	}

	// Check each dependency
	var statuses []MCPDependencyStatus
	missing := 0
	for _, dep := range req.Dependencies {
		status := MCPDependencyStatus{
			Server:  dep.Server,
			Tools:   dep.Tools,
			Source:  dep.Source,
			StoreID: dep.StoreID,
			Config:  dep.Config,
		}

		// If store_id is missing but source is tap or store, try to resolve it
		if status.StoreID == "" && (status.Source == "tap" || status.Source == "store") {
			serverLower := strings.ToLower(dep.Server)
			for _, srv := range storeServers {
				nameLower := strings.ToLower(srv.Name)
				if serverLower == nameLower || strings.Contains(serverLower, nameLower) || strings.Contains(nameLower, serverLower) {
					status.StoreID = srv.McpId
					break
				}
			}
		}

		// Check if server is installed
		status.Installed = installedServers[dep.Server]
		if !status.Installed {
			missing++
		}

		statuses = append(statuses, status)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(CheckMCPDependenciesResponse{
		Dependencies: statuses,
		AllInstalled: missing == 0,
		Missing:      missing,
	})
}
