package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/flowstore"
	"github.com/schardosin/astonish/pkg/mcpstore"
)

// MCPStoreListResponse is the response for GET /api/mcp-store
type MCPStoreListResponse struct {
	Servers []mcpstore.Server `json:"servers"`
	Total   int               `json:"total"`
}

// MCPStoreInstallRequest is the request for POST /api/mcp-store/:id/install
type MCPStoreInstallRequest struct {
	ServerName string            `json:"serverName,omitempty"` // Optional: custom name for the server
	Env        map[string]string `json:"env,omitempty"`        // Optional: environment variable overrides
}

// ListMCPStoreHandler handles GET /api/mcp-store
// Supports query parameter ?q=<search query>
// Only returns servers with valid configs (installable)
// Includes MCPs from tapped repos with source field set
func ListMCPStoreHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")

	var servers []mcpstore.Server
	var err error

	if query != "" {
		// Search and filter to installable only
		allMatches, err := mcpstore.SearchServers(query)
		if err != nil {
			http.Error(w, "Failed to load MCP store: "+err.Error(), http.StatusInternalServerError)
			return
		}
		// Filter to servers with configs and set source
		for _, srv := range allMatches {
			if srv.Config != nil && srv.Config.Command != "" {
				if srv.Source == "" {
					srv.Source = "official"
				}
				servers = append(servers, srv)
			}
		}
	} else {
		servers, err = mcpstore.ListInstallableServers()
	}

	if err != nil {
		http.Error(w, "Failed to load MCP store: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Mark official servers with source
	for i := range servers {
		if servers[i].Source == "" {
			servers[i].Source = "official"
		}
	}

	// Add tapped MCPs
	store, storeErr := flowstore.NewStore()
	if storeErr == nil {
		// Update manifests
		_ = store.UpdateAllManifests()
		tappedMCPs := store.ListAllMCPs()

		for _, mcp := range tappedMCPs {
			// Skip if there's no command
			if mcp.Command == "" {
				continue
			}
			servers = append(servers, mcpstore.Server{
				McpId:       mcp.TapName + "/" + mcp.Name,
				Name:        mcp.Name,
				Description: mcp.Description,
				Tags:        mcp.Tags,
				Source:      mcp.TapName,
				Config: &mcpstore.ServerConfig{
					Command: mcp.Command,
					Args:    mcp.Args,
					Env:     mcp.Env,
				},
			})
		}
	}

	response := MCPStoreListResponse{
		Servers: servers,
		Total:   len(servers),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetMCPStoreServerHandler handles GET /api/mcp-store/{id}
func GetMCPStoreServerHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// URL decode the ID (it may contain slashes encoded as %2F)
	id = strings.ReplaceAll(id, "%2F", "/")

	server, err := mcpstore.GetServer(id)
	if err != nil {
		http.Error(w, "Failed to get server: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if server == nil {
		// Try by name
		server, err = mcpstore.GetServerByName(id)
		if err != nil {
			http.Error(w, "Failed to get server: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if server == nil {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(server)
}

// InstallMCPStoreServerHandler handles POST /api/mcp-store/{id}/install
// Adds the MCP server configuration to the user's MCP config
func InstallMCPStoreServerHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// URL decode the ID
	id = strings.ReplaceAll(id, "%2F", "/")

	// Get the server from the store
	server, err := mcpstore.GetServer(id)
	if err != nil {
		http.Error(w, "Failed to get server: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if server == nil {
		// Try by name
		server, err = mcpstore.GetServerByName(id)
		if err != nil {
			http.Error(w, "Failed to get server: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if server == nil {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	if server.Config == nil {
		http.Error(w, "Server has no configuration available", http.StatusBadRequest)
		return
	}

	// Parse request body for optional overrides
	var installReq MCPStoreInstallRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&installReq); err != nil {
			http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Load current MCP config
	mcpCfg, err := config.LoadMCPConfig()
	if err != nil {
		http.Error(w, "Failed to load MCP config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Determine server name
	serverName := strings.ToLower(strings.ReplaceAll(server.Name, " ", "-"))
	if installReq.ServerName != "" {
		serverName = installReq.ServerName
	}

	// Create the server config
	newConfig := config.MCPServerConfig{
		Command:   server.Config.Command,
		Args:      server.Config.Args,
		Transport: "stdio",
	}

	// Merge environment variables
	if server.Config.Env != nil || installReq.Env != nil {
		newConfig.Env = make(map[string]string)
		// Copy from store config
		for k, v := range server.Config.Env {
			newConfig.Env[k] = v
		}
		// Override with request values
		for k, v := range installReq.Env {
			newConfig.Env[k] = v
		}
	}

	// Handle transport
	if server.Config.Transport != "" {
		newConfig.Transport = server.Config.Transport
	}
	if server.Config.URL != "" {
		newConfig.URL = server.Config.URL
	}

	// Initialize map if nil
	if mcpCfg.MCPServers == nil {
		mcpCfg.MCPServers = make(map[string]config.MCPServerConfig)
	}

	// Add/update the server
	mcpCfg.MCPServers[serverName] = newConfig

	// Save config
	if err := config.SaveMCPConfig(mcpCfg); err != nil {
		http.Error(w, "Failed to save MCP config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Re-setup environment variables
	config.SetupMCPEnv(mcpCfg)

	// Refresh tools cache
	RefreshToolsCache(r.Context())

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"serverName": serverName,
		"message":    "Server installed successfully",
	})
}

// GetMCPStoreTagsHandler handles GET /api/mcp-store/tags
func GetMCPStoreTagsHandler(w http.ResponseWriter, r *http.Request) {
	tags, err := mcpstore.GetAllTags()
	if err != nil {
		http.Error(w, "Failed to get tags: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tags": tags,
	})
}
