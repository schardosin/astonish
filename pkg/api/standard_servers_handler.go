package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/cache"
	"github.com/schardosin/astonish/pkg/common"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/credentials"
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

	// Save API key to encrypted credential store
	storeKeyInConfig := true
	configDir, _ := config.GetConfigDir()
	if configDir != "" {
		if store, storeErr := credentials.Open(configDir); storeErr == nil {
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
	}

	// Install to config.yaml (web tool settings; key excluded if stored in credential store)
	if err := config.InstallStandardServer(serverID, req.Env, storeKeyInConfig); err != nil {
		http.Error(w, "Failed to install server: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Setup environment variables for the new MCP server
	mcpCfg, _ := config.LoadMCPConfig()
	if mcpCfg != nil {
		config.SetupMCPEnv(mcpCfg)
	}

	// Load tools from the server (synchronous, same pattern as InstallInlineMCPServerHandler)
	toolsLoaded := 0
	toolError := ""

	mcpManager, err := mcp.NewManager()
	if err != nil {
		toolError = fmt.Sprintf("Failed to create MCP manager: %v", err)
		log.Printf("Warning: %s", toolError)
	} else {
		namedToolset, err := mcpManager.InitializeSingleToolset(r.Context(), srv.ID)
		if err != nil {
			toolError = fmt.Sprintf("Failed to initialize server: %v", err)
			log.Printf("Warning: %s", toolError)
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
				log.Printf("Warning: %s", toolError)
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
					log.Printf("[Cache] Warning: Failed to save persistent cache: %v", err)
				} else {
					log.Printf("[Cache] Saved %d tools for standard server '%s'", len(persistentTools), srv.ID)
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

	log.Printf("[Standard Server] Installed '%s' (%s) with %d tools", srv.ID, srv.DisplayName, toolsLoaded)

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
