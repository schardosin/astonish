package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/store"
)

// MCPServerListItem represents an MCP server in listing responses.
type MCPServerListItem struct {
	Name        string            `json:"name"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Transport   string            `json:"transport"`
	URL         string            `json:"url,omitempty"`
	Enabled     bool              `json:"enabled"`
	Scope       string            `json:"scope"` // "org" or "team"
	HasTools    bool              `json:"has_tools"`
	ToolCount   int               `json:"tool_count"`
	CachedTools json.RawMessage   `json:"cached_tools,omitempty"`
}

// MCPPlatformServersListResponse is the response for GET /api/mcp-platform/servers.
type MCPPlatformServersListResponse struct {
	Servers     []MCPServerListItem `json:"servers"`
	IsTeamAdmin bool                `json:"is_team_admin"`
	IsOrgAdmin  bool                `json:"is_org_admin"`
}

// MCPServerCreateRequest is the request body for creating/updating an MCP server.
type MCPServerCreateRequest struct {
	Name      string            `json:"name"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Transport string            `json:"transport"`
	URL       string            `json:"url,omitempty"`
	Enabled   *bool             `json:"enabled,omitempty"`
}

// ListMCPPlatformServersHandler handles GET /api/mcp-platform/servers
//
// Query params:
//   - scope=team: return only team MCP servers
//   - scope=org: return only org MCP servers
//   - (empty): return merged view (org + team, team overrides org by name)
func ListMCPPlatformServersHandler(w http.ResponseWriter, r *http.Request) {
	scope := r.URL.Query().Get("scope")

	svc := store.FromRequest(r)
	if svc == nil || svc.Mode != store.ModePlatform {
		respondError(w, http.StatusServiceUnavailable, "MCP platform servers are only available in platform mode")
		return
	}

	items, err := listMCPServersPlatform(svc, scope)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to load MCP servers: "+err.Error())
		return
	}

	resp := MCPPlatformServersListResponse{
		Servers:     items,
		IsTeamAdmin: CanManageTeam(r, GetPlatformUser(r)),
		IsOrgAdmin:  !isPlatformMode(r) || CanManageOrg(GetPlatformUser(r)),
	}
	respondJSON(w, http.StatusOK, resp)
}

// GetMCPPlatformServerHandler handles GET /api/mcp-platform/servers/{name}
func GetMCPPlatformServerHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	scope := r.URL.Query().Get("scope")

	svc := store.FromRequest(r)
	if svc == nil || svc.Mode != store.ModePlatform {
		respondError(w, http.StatusServiceUnavailable, "MCP platform servers are only available in platform mode")
		return
	}

	mcpStore := resolveMCPStoreForRead(svc, scope)
	if mcpStore == nil {
		respondError(w, http.StatusServiceUnavailable, "MCP server store not available for scope: "+scope)
		return
	}

	server, err := mcpStore.Get(name)
	if err != nil {
		respondError(w, http.StatusNotFound, "MCP server not found: "+name)
		return
	}

	item := mcpServerToListItem(server, scope)
	respondJSON(w, http.StatusOK, item)
}

// CreateMCPPlatformServerHandler handles POST /api/mcp-platform/servers
func CreateMCPPlatformServerHandler(w http.ResponseWriter, r *http.Request) {
	scope := r.URL.Query().Get("scope")

	svc := store.FromRequest(r)
	if svc == nil || svc.Mode != store.ModePlatform {
		respondError(w, http.StatusServiceUnavailable, "MCP platform servers are only available in platform mode")
		return
	}

	targetStore := resolveMCPStoreForWrite(w, r, svc, scope)
	if targetStore == nil {
		return // auth error already written
	}

	var req MCPServerCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "Name is required")
		return
	}
	if req.Transport == "" {
		req.Transport = "stdio"
	}
	if req.Transport == "stdio" && req.Command == "" {
		respondError(w, http.StatusBadRequest, "Command is required for stdio transport")
		return
	}
	if req.Transport == "sse" && req.URL == "" {
		respondError(w, http.StatusBadRequest, "URL is required for SSE transport")
		return
	}

	// Get user ID for created_by
	createdBy := ""
	if user := GetPlatformUser(r); user != nil {
		createdBy = user.ID
	}

	server := &store.MCPServer{
		Name:      req.Name,
		Command:   req.Command,
		Args:      req.Args,
		Env:       req.Env,
		Transport: req.Transport,
		URL:       req.URL,
		Enabled:   req.Enabled,
		CreatedBy: createdBy,
	}

	if err := targetStore.Save(server); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to save MCP server: "+err.Error())
		return
	}

	// Trigger async tool discovery to populate cached_tools
	go refreshMCPPlatformServer(targetStore, server)

	respondJSON(w, http.StatusCreated, map[string]string{"status": "ok", "name": req.Name})
}

// UpdateMCPPlatformServerHandler handles PUT /api/mcp-platform/servers/{name}
func UpdateMCPPlatformServerHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	scope := r.URL.Query().Get("scope")

	svc := store.FromRequest(r)
	if svc == nil || svc.Mode != store.ModePlatform {
		respondError(w, http.StatusServiceUnavailable, "MCP platform servers are only available in platform mode")
		return
	}

	targetStore := resolveMCPStoreForWrite(w, r, svc, scope)
	if targetStore == nil {
		return // auth error already written
	}

	var req MCPServerCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	// Use URL name for identity (allows rename via body "name" if needed)
	serverName := name
	if req.Name != "" {
		serverName = req.Name
	}

	if req.Transport == "" {
		req.Transport = "stdio"
	}

	// Get user ID for created_by (upsert will keep original on conflict)
	createdBy := ""
	if user := GetPlatformUser(r); user != nil {
		createdBy = user.ID
	}

	server := &store.MCPServer{
		Name:      serverName,
		Command:   req.Command,
		Args:      req.Args,
		Env:       req.Env,
		Transport: req.Transport,
		URL:       req.URL,
		Enabled:   req.Enabled,
		CreatedBy: createdBy,
	}

	if err := targetStore.Save(server); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to update MCP server: "+err.Error())
		return
	}

	// Trigger async tool discovery to refresh cached_tools when config changes
	go refreshMCPPlatformServer(targetStore, server)

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok", "name": serverName})
}

// DeleteMCPPlatformServerHandler handles DELETE /api/mcp-platform/servers/{name}
func DeleteMCPPlatformServerHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	scope := r.URL.Query().Get("scope")

	svc := store.FromRequest(r)
	if svc == nil || svc.Mode != store.ModePlatform {
		respondError(w, http.StatusServiceUnavailable, "MCP platform servers are only available in platform mode")
		return
	}

	targetStore := resolveMCPStoreForWrite(w, r, svc, scope)
	if targetStore == nil {
		return // auth error already written
	}

	if err := targetStore.Delete(name); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to delete MCP server: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ToggleMCPPlatformServerHandler handles PATCH /api/mcp-platform/servers/{name}
// Toggles the enabled state of an MCP server.
func ToggleMCPPlatformServerHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	scope := r.URL.Query().Get("scope")

	svc := store.FromRequest(r)
	if svc == nil || svc.Mode != store.ModePlatform {
		respondError(w, http.StatusServiceUnavailable, "MCP platform servers are only available in platform mode")
		return
	}

	targetStore := resolveMCPStoreForWrite(w, r, svc, scope)
	if targetStore == nil {
		return // auth error already written
	}

	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	// Get existing server
	existing, err := targetStore.Get(name)
	if err != nil {
		respondError(w, http.StatusNotFound, "MCP server not found: "+name)
		return
	}

	existing.Enabled = body.Enabled
	if err := targetStore.Save(existing); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to update MCP server: "+err.Error())
		return
	}

	// Reset the chat agent so it reinitializes with the updated enabled state.
	// Without this, the singleton keeps the old tool configuration.
	GetChatManager().Reset()

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// RefreshMCPPlatformServerHandler handles POST /api/mcp-platform/servers/{name}/refresh
// Triggers async tool discovery for an MCP server.
func RefreshMCPPlatformServerHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	scope := r.URL.Query().Get("scope")

	svc := store.FromRequest(r)
	if svc == nil || svc.Mode != store.ModePlatform {
		respondError(w, http.StatusServiceUnavailable, "MCP platform servers are only available in platform mode")
		return
	}

	targetStore := resolveMCPStoreForWrite(w, r, svc, scope)
	if targetStore == nil {
		return // auth error already written
	}

	// Get the server config
	server, err := targetStore.Get(name)
	if err != nil {
		respondError(w, http.StatusNotFound, "MCP server not found: "+name)
		return
	}

	// Trigger async background refresh
	go refreshMCPPlatformServer(targetStore, server)

	respondJSON(w, http.StatusAccepted, map[string]string{"status": "refresh_started", "name": name})
}

// --- Platform mode helpers ---

// resolveMCPStoreForRead returns the appropriate MCPServerStore for a read operation.
func resolveMCPStoreForRead(svc *store.Services, scope string) store.MCPServerStore {
	switch scope {
	case "team":
		return svc.TeamMCPServers
	case "org":
		return svc.MCPServers
	case "platform":
		return svc.PlatformMCPServers
	default:
		// For merged reads, caller should use listMCPServersPlatform
		return svc.TeamMCPServers
	}
}

// resolveMCPStoreForWrite returns the appropriate MCPServerStore for a write operation
// based on the requested scope. Returns nil and writes an error if auth fails.
func resolveMCPStoreForWrite(w http.ResponseWriter, r *http.Request, svc *store.Services, scope string) store.MCPServerStore {
	switch scope {
	case "platform":
		if svc.PlatformMCPServers == nil {
			respondError(w, http.StatusServiceUnavailable, "Platform MCP server store not available")
			return nil
		}
		if RequirePlatformAdmin(w, r) == nil {
			return nil
		}
		return svc.PlatformMCPServers
	case "team":
		if svc.TeamMCPServers == nil {
			respondError(w, http.StatusServiceUnavailable, "Team MCP server store not available")
			return nil
		}
		if !RequireTeamAdmin(w, r) {
			return nil
		}
		return svc.TeamMCPServers
	case "org":
		if svc.MCPServers == nil {
			respondError(w, http.StatusServiceUnavailable, "Org MCP server store not available")
			return nil
		}
		user := GetPlatformUser(r)
		if user == nil {
			respondError(w, http.StatusUnauthorized, "Authentication required")
			return nil
		}
		if !CanManageOrg(user) {
			respondError(w, http.StatusForbidden, "Organization admin access required to manage org MCP servers")
			return nil
		}
		return svc.MCPServers
	default:
		// No scope specified — default to team
		if svc.TeamMCPServers == nil {
			respondError(w, http.StatusServiceUnavailable, "Team MCP server store not available")
			return nil
		}
		if !RequireTeamAdmin(w, r) {
			return nil
		}
		return svc.TeamMCPServers
	}
}

// listMCPServersPlatform loads MCP servers with three-tier merge: platform → org → team.
// Lower tiers override higher tiers by name.
func listMCPServersPlatform(svc *store.Services, scope string) ([]MCPServerListItem, error) {
	switch scope {
	case "platform":
		return listMCPServersFromStore(svc.PlatformMCPServers, "platform")
	case "team":
		return listMCPServersFromStore(svc.TeamMCPServers, "team")
	case "org":
		return listMCPServersFromStore(svc.MCPServers, "org")
	default:
		return listMCPServersMerged(svc)
	}
}

// listMCPServersFromStore lists MCP servers from a single store.
func listMCPServersFromStore(mcpStore store.MCPServerStore, scope string) ([]MCPServerListItem, error) {
	if mcpStore == nil {
		return []MCPServerListItem{}, nil
	}
	servers, err := mcpStore.List()
	if err != nil {
		return nil, err
	}
	items := make([]MCPServerListItem, 0, len(servers))
	for _, s := range servers {
		item := mcpServerToListItem(&s, scope)
		items = append(items, item)
	}
	sortMCPServerItems(items)
	return items, nil
}

// listMCPServersMerged returns all MCP servers merged: platform → org → team (lower wins by name).
func listMCPServersMerged(svc *store.Services) ([]MCPServerListItem, error) {
	byName := make(map[string]MCPServerListItem)

	// 1. Platform servers as base (inherited by all)
	if svc.PlatformMCPServers != nil {
		platformServers, err := svc.PlatformMCPServers.List()
		if err != nil {
			return nil, err
		}
		for _, s := range platformServers {
			item := mcpServerToListItem(&s, "platform")
			byName[s.Name] = item
		}
	}

	// 2. Org servers override platform by name
	if svc.MCPServers != nil {
		orgServers, err := svc.MCPServers.List()
		if err != nil {
			return nil, err
		}
		for _, s := range orgServers {
			item := mcpServerToListItem(&s, "org")
			byName[s.Name] = item
		}
	}

	// 3. Team servers override org+platform by name
	if svc.TeamMCPServers != nil {
		teamServers, err := svc.TeamMCPServers.List()
		if err != nil {
			return nil, err
		}
		for _, s := range teamServers {
			item := mcpServerToListItem(&s, "team")
			byName[s.Name] = item
		}
	}

	items := make([]MCPServerListItem, 0, len(byName))
	for _, item := range byName {
		items = append(items, item)
	}
	sortMCPServerItems(items)
	return items, nil
}

// mcpServerToListItem converts a store.MCPServer to a MCPServerListItem.
func mcpServerToListItem(s *store.MCPServer, scope string) MCPServerListItem {
	enabled := true
	if s.Enabled != nil {
		enabled = *s.Enabled
	}

	toolCount := 0
	hasTools := false
	if len(s.CachedTools) > 0 {
		var tools []json.RawMessage
		if err := json.Unmarshal(s.CachedTools, &tools); err == nil {
			toolCount = len(tools)
			hasTools = toolCount > 0
		}
	}

	return MCPServerListItem{
		Name:        s.Name,
		Command:     s.Command,
		Args:        s.Args,
		Env:         s.Env,
		Transport:   s.Transport,
		URL:         s.URL,
		Enabled:     enabled,
		Scope:       scope,
		HasTools:    hasTools,
		ToolCount:   toolCount,
		CachedTools: s.CachedTools,
	}
}

// sortMCPServerItems sorts by name alphabetically.
func sortMCPServerItems(items []MCPServerListItem) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
}

// refreshMCPPlatformServer performs async tool discovery for a platform MCP server.
// It starts the MCP server process, discovers its tools, then stores them in the DB.
func refreshMCPPlatformServer(mcpStore store.MCPServerStore, server *store.MCPServer) {
	slog.Info("starting MCP tool discovery for platform server", "server", server.Name)

	tools, err := discoverMCPServerTools(server)
	if err != nil {
		slog.Warn("MCP tool discovery failed for platform server", "server", server.Name, "error", err)
		return
	}

	if len(tools) == 0 {
		slog.Warn("MCP tool discovery returned no tools", "server", server.Name)
		return
	}

	toolsJSON, err := json.Marshal(tools)
	if err != nil {
		slog.Warn("failed to marshal discovered tools", "server", server.Name, "error", err)
		return
	}

	if err := mcpStore.UpdateCachedTools(server.Name, toolsJSON); err != nil {
		slog.Warn("failed to update cached_tools in DB", "server", server.Name, "error", err)
		return
	}

	slog.Info("MCP tool discovery completed", "server", server.Name, "tool_count", len(tools))
}

// discoverMCPServerTools starts an MCP server, lists its tools, and returns them.
// This is the platform-mode equivalent of the personal-mode RefreshSingleServer.
func discoverMCPServerTools(server *store.MCPServer) ([]MCPDiscoveredTool, error) {
	// Convert store.MCPServer to config.MCPServerConfig for the manager
	cfg := mcpServerToConfig(server)

	// Use the MCP manager to discover tools
	mgr := newSingleServerManager(server.Name, cfg)
	if mgr == nil {
		return nil, nil
	}
	defer mgr.Cleanup()

	return mgr.DiscoverTools()
}

// MCPDiscoveredTool represents a tool discovered from an MCP server.
type MCPDiscoveredTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// mcpServerConfigUnchanged returns true if the executable config of two MCP servers
// is the same (command, args, transport, url). Used to skip redundant tool discovery.
func mcpServerConfigUnchanged(existing, updated *store.MCPServer) bool {
	if existing.Command != updated.Command {
		return false
	}
	if existing.Transport != updated.Transport {
		return false
	}
	if existing.URL != updated.URL {
		return false
	}
	if len(existing.Args) != len(updated.Args) {
		return false
	}
	for i := range existing.Args {
		if existing.Args[i] != updated.Args[i] {
			return false
		}
	}
	// If existing has no cached tools, treat as changed so discovery runs
	if len(existing.CachedTools) == 0 {
		return false
	}
	return true
}
