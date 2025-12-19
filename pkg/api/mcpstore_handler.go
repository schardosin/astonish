package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/flowstore"
	"github.com/schardosin/astonish/pkg/mcp"
	"github.com/schardosin/astonish/pkg/mcpstore"
)

// MCPStoreListResponse is the response for GET /api/mcp-store
type MCPStoreListResponse struct {
	Servers []mcpstore.Server `json:"servers"`
	Sources []string          `json:"sources"` // Available sources for filtering
	Total   int               `json:"total"`
}

// MCPStoreInstallRequest is the request for POST /api/mcp-store/:id/install
type MCPStoreInstallRequest struct {
	ServerName string            `json:"serverName,omitempty"` // Optional: custom name for the server
	Env        map[string]string `json:"env,omitempty"`        // Optional: environment variable overrides
}

// loadAllServersFromTaps loads all MCP servers from taps (including official)
func loadAllServersFromTaps() ([]mcpstore.Server, error) {
	store, err := flowstore.NewStore()
	if err != nil {
		return nil, err
	}

	_ = store.UpdateAllManifests()
	tappedMCPs := store.ListAllMCPs()

	var inputs []mcpstore.TappedMCPInput
	for _, mcp := range tappedMCPs {
		// Skip MCPs that have neither command nor URL (not installable)
		if mcp.Command == "" && mcp.URL == "" {
			continue
		}
		inputs = append(inputs, mcpstore.TappedMCPInput{
			ID:             mcp.ID,
			Name:           mcp.Name,
			Description:    mcp.Description,
			Author:         mcp.Author,
			GithubUrl:      mcp.GithubUrl,
			GithubStars:    mcp.GithubStars,
			RequiresApiKey: mcp.RequiresApiKey,
			Command:        mcp.Command,
			Args:           mcp.Args,
			Env:            mcp.Env,
			Tags:           mcp.Tags,
			Transport:      mcp.Transport,
			URL:            mcp.URL,
			TapName:        mcp.TapName,
		})
	}

	return mcpstore.ListServers(inputs), nil
}

// ListMCPStoreHandler handles GET /api/mcp-store
// Supports query parameters:
// - ?q=<search query> for text search
// - ?source=<source name> to filter by source (e.g., "official", tap name, or "all")
// Only returns servers with valid configs (installable)
// All MCPs come from tapped repos (including official tap)
func ListMCPStoreHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	sourceFilter := r.URL.Query().Get("source")

	// Load all servers from taps
	servers, err := loadAllServersFromTaps()
	if err != nil {
		http.Error(w, "Failed to load servers: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Apply search filter if specified
	if query != "" {
		servers = mcpstore.SearchServers(servers, query)
	}

	// Collect unique sources for the dropdown
	sourceSet := make(map[string]bool)
	for _, srv := range servers {
		if srv.Source != "" {
			sourceSet[srv.Source] = true
		}
	}
	sources := make([]string, 0, len(sourceSet))
	for source := range sourceSet {
		sources = append(sources, source)
	}

	// Apply source filter if specified (and not "all")
	if sourceFilter != "" && sourceFilter != "all" {
		var filtered []mcpstore.Server
		for _, srv := range servers {
			if srv.Source == sourceFilter {
				filtered = append(filtered, srv)
			}
		}
		servers = filtered
	}

	response := MCPStoreListResponse{
		Servers: servers,
		Sources: sources,
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

	// Load all servers from taps
	servers, err := loadAllServersFromTaps()
	if err != nil {
		http.Error(w, "Failed to load servers: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Search by ID first
	server := mcpstore.GetServer(servers, id)
	if server == nil {
		// Try by name
		server = mcpstore.GetServerByName(servers, id)
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

	// Load all servers from taps
	servers, err := loadAllServersFromTaps()
	if err != nil {
		http.Error(w, "Failed to load servers: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Search by ID first
	server := mcpstore.GetServer(servers, id)
	if server == nil {
		// Try by name
		server = mcpstore.GetServerByName(servers, id)
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

	// Incrementally load just this server's tools (synchronous, should be fast for one server)
	toolsLoaded := 0
	toolError := ""
	mcpManager, err := mcp.NewManager()
	if err != nil {
		toolError = fmt.Sprintf("Failed to create MCP manager: %v", err)
		log.Printf("Warning: %s", toolError)
	} else {
		namedToolset, err := mcpManager.InitializeSingleToolset(r.Context(), serverName)
		if err != nil {
			toolError = fmt.Sprintf("Failed to initialize server: %v", err)
			log.Printf("Warning: %s", toolError)
		} else {
			// Get tools from this server and add to cache
			minimalCtx := &minimalReadonlyContext{Context: r.Context()}
			mcpTools, err := namedToolset.Toolset.Tools(minimalCtx)
			if err != nil {
				toolError = fmt.Sprintf("Server started but failed to get tools: %v", err)
				log.Printf("Warning: %s", toolError)
			} else {
				var newTools []ToolInfo
				for _, t := range mcpTools {
					newTools = append(newTools, ToolInfo{
						Name:        t.Name(),
						Description: t.Description(),
						Source:      serverName,
					})
				}
				AddServerToolsToCache(serverName, newTools)
				toolsLoaded = len(newTools)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	response := map[string]interface{}{
		"status":      "ok",
		"serverName":  serverName,
		"message":     "Server installed successfully",
		"toolsLoaded": toolsLoaded,
	}
	if toolError != "" {
		response["toolError"] = toolError
	}
	json.NewEncoder(w).Encode(response)
}

// GetMCPStoreTagsHandler handles GET /api/mcp-store/tags
func GetMCPStoreTagsHandler(w http.ResponseWriter, r *http.Request) {
	// Load all servers from taps
	servers, err := loadAllServersFromTaps()
	if err != nil {
		http.Error(w, "Failed to load servers: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Extract unique tags
	tagSet := make(map[string]bool)
	for _, srv := range servers {
		for _, tag := range srv.Tags {
			tagSet[tag] = true
		}
	}
	tags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		tags = append(tags, tag)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tags": tags,
	})
}
