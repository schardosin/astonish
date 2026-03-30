package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/cache"
	"github.com/schardosin/astonish/pkg/common"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/mcp"
)

// StandardServerResponse represents a standard server in the API response.
type StandardServerResponse struct {
	ID             string                     `json:"id"`
	DisplayName    string                     `json:"displayName"`
	Description    string                     `json:"description"`
	Installed      bool                       `json:"installed"`
	IsDefault      bool                       `json:"isDefault"`
	EnvVars        []config.StandardEnvVar    `json:"envVars"`
	Capabilities   StandardServerCapabilities `json:"capabilities"`
	WebSearchTool  string                     `json:"webSearchTool,omitempty"`
	WebExtractTool string                     `json:"webExtractTool,omitempty"`
}

// StandardServerCapabilities describes what a standard server can do.
type StandardServerCapabilities struct {
	WebSearch  bool `json:"webSearch"`
	WebExtract bool `json:"webExtract"`
}

// ListStandardServersHandler handles GET /api/standard-servers
// Returns all standard servers with their install status.
func ListStandardServersHandler(w http.ResponseWriter, r *http.Request) {
	servers := config.GetStandardServers()

	response := make([]StandardServerResponse, 0, len(servers))
	for _, srv := range servers {
		response = append(response, StandardServerResponse{
			ID:          srv.ID,
			DisplayName: srv.DisplayName,
			Description: srv.Description,
			Installed:   config.IsStandardServerInstalled(srv.ID),
			IsDefault:   srv.IsDefault,
			EnvVars:     srv.EnvVars,
			Capabilities: StandardServerCapabilities{
				WebSearch:  srv.WebSearchTool != "",
				WebExtract: srv.WebExtractTool != "",
			},
			WebSearchTool:  srv.WebSearchTool,
			WebExtractTool: srv.WebExtractTool,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"servers": response,
	})
}

// InstallStandardServerRequest is the request for POST /api/standard-servers/{id}/install
type InstallStandardServerRequest struct {
	Env map[string]string `json:"env"`
}

// InstallStandardServerHandler handles POST /api/standard-servers/{id}/install
// Installs a standard MCP server, configures web tools, and loads its tools into cache.
func InstallStandardServerHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	serverID := vars["id"]

	srv := config.GetStandardServer(serverID)
	if srv == nil {
		http.Error(w, "Unknown standard server: "+serverID, http.StatusNotFound)
		return
	}

	var req InstallStandardServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Save API key to the shared credential store so that the in-memory
	// cache is updated immediately (mergeStandardServers uses the same
	// instance via getInstalledSecretGetter to resolve keys at load time).
	storeKeyInConfig := true
	if store := getAPICredentialStore(); store != nil {
		for _, ev := range srv.EnvVars {
			if val, ok := req.Env[ev.Name]; ok && val != "" {
				storeKey := "web_servers." + serverID + ".api_key"
				if setErr := store.SetSecret(storeKey, val); setErr == nil {
					storeKeyInConfig = false
				}
				break
			}
		}
	}

	// Install to config.yaml (web tool settings; key excluded if stored in credential store)
	if err := config.InstallStandardServer(serverID, req.Env, storeKeyInConfig); err != nil {
		http.Error(w, "Failed to install server: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Setup environment variables for the new MCP server
	mcpCfg, err := config.LoadMCPConfig()
	if err != nil {
		slog.Warn("failed to load MCP config", "error", err)
	}
	if mcpCfg != nil {
		config.SetupMCPEnv(mcpCfg)
	}

	// Load tools from the server (synchronous, same pattern as InstallInlineMCPServerHandler)
	toolsLoaded := 0
	toolError := ""

	mcpManager, err := mcp.NewManager()
	if err != nil {
		toolError = fmt.Sprintf("Failed to create MCP manager: %v", err)
		slog.Warn(toolError)
	} else {
		namedToolset, err := mcpManager.InitializeSingleToolset(r.Context(), srv.ID)
		if err != nil {
			toolError = fmt.Sprintf("Failed to initialize server: %v", err)
			slog.Warn(toolError)
		} else {
			minimalCtx := &minimalReadonlyContext{Context: r.Context()}
			mcpTools, err := namedToolset.Toolset.Tools(minimalCtx)
			if err != nil {
				stderrOutput := mcp.GetStderr(namedToolset.Stderr)
				if stderrOutput != "" && stderrOutput != "no stderr output" {
					toolError = stderrOutput
				} else {
					toolError = fmt.Sprintf("Server started but failed to get tools: %v", err)
				}
				slog.Warn(toolError)
			} else {
				// Update in-memory cache
				var newTools []ToolInfo
				for _, t := range mcpTools {
					newTools = append(newTools, ToolInfo{
						Name:        t.Name(),
						Description: t.Description(),
						Source:      srv.ID,
					})
				}
				AddServerToolsToCache(srv.ID, newTools)
				toolsLoaded = len(newTools)

				// Update persistent cache
				serverCfg := config.MCPServerConfig{
					Command: srv.Command,
					Args:    srv.Args,
					Env:     req.Env,
				}
				persistentTools := make([]cache.ToolEntry, 0, len(mcpTools))
				for _, t := range mcpTools {
					persistentTools = append(persistentTools, cache.ToolEntry{
						Name:        t.Name(),
						Description: t.Description(),
						Source:      srv.ID,
						InputSchema: common.ExtractToolInputSchema(t),
					})
				}
				checksum := cache.ComputeServerChecksum(serverCfg.Command, serverCfg.Args, serverCfg.Env)
				cache.AddServerTools(srv.ID, persistentTools, checksum)
				if err := cache.SaveCache(); err != nil {
					slog.Warn("failed to save persistent cache", "component", "cache", "error", err)
				} else {
					slog.Info("saved tools for standard server", "component", "cache", "count", len(persistentTools), "server", srv.ID)
				}

				// Update server status to healthy
				SetServerStatus(srv.ID, cache.ServerStatus{
					Name:      srv.ID,
					Status:    "healthy",
					ToolCount: len(persistentTools),
					LastCheck: time.Now().UTC().Format(time.RFC3339),
				})
			}
		}
	}

	// Refresh in-memory tools cache
	RefreshToolsCache(context.Background())

	slog.Info("installed standard server", "component", "standard-server", "server", srv.ID, "displayName", srv.DisplayName, "toolsLoaded", toolsLoaded)

	w.Header().Set("Content-Type", "application/json")
	response := map[string]interface{}{
		"status":         "installed",
		"serverName":     srv.ID,
		"toolsLoaded":    toolsLoaded,
		"webSearchTool":  srv.WebSearchTool,
		"webExtractTool": srv.WebExtractTool,
	}
	if toolError != "" {
		response["toolError"] = toolError
	}
	json.NewEncoder(w).Encode(response)
}

// UninstallStandardServerHandler handles DELETE /api/standard-servers/{id}
// Removes a standard MCP server's configuration, credentials, and cached tools.
func UninstallStandardServerHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	serverID := vars["id"]

	srv := config.GetStandardServer(serverID)
	if srv == nil {
		http.Error(w, "Unknown standard server: "+serverID, http.StatusNotFound)
		return
	}

	// Keyless servers cannot be uninstalled
	if len(srv.EnvVars) == 0 {
		http.Error(w, "Server does not require configuration", http.StatusBadRequest)
		return
	}

	// Remove API key from the shared credential store
	if store := getAPICredentialStore(); store != nil {
		storeKey := "web_servers." + serverID + ".api_key"
		if err := store.RemoveSecret(storeKey); err != nil {
			slog.Warn("failed to remove secret during uninstall", "key", storeKey, "error", err)
		}
	}

	// Remove from config.yaml (web_servers entry + web tool references)
	if err := config.UninstallStandardServer(serverID); err != nil {
		http.Error(w, "Failed to uninstall server: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Remove from in-memory and persistent caches
	RemoveServerToolsFromCache(serverID)
	cache.RemoveServer(serverID)
	if err := cache.SaveCache(); err != nil {
		slog.Warn("failed to save cache after uninstall", "component", "cache", "error", err)
	}

	// Clear server status
	ClearServerStatus(serverID)

	slog.Info("uninstalled standard server", "component", "standard-server", "server", srv.ID, "displayName", srv.DisplayName)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "uninstalled",
		"serverName": srv.ID,
	})
}
