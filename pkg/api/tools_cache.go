package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/cache"
	"github.com/schardosin/astonish/pkg/common"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/mcp"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/tools"
)

// ToolsCache holds cached tool information to avoid re-initializing MCP on every request
type ToolsCache struct {
	tools   []ToolInfo
	loaded  bool
	loading bool // Tracks if a load is in progress
	mu      sync.RWMutex
}

var globalToolsCache = &ToolsCache{}

// GetServerStatus returns the status of all MCP servers
func GetServerStatus() []cache.ServerStatus {
	statusesMap := cache.GetServerStatuses()

	statuses := make([]cache.ServerStatus, 0, len(statusesMap))
	for _, status := range statusesMap {
		statuses = append(statuses, status)
	}
	return statuses
}

// SetServerStatus updates the status of a specific MCP server
func SetServerStatus(name string, status cache.ServerStatus) {
	cache.UpdateServerStatus(status)
	// Persist the cache
	if err := cache.SaveCache(); err != nil {
		slog.Warn("failed to save tools cache after status update", "error", err)
	}
}

// ClearServerStatus removes a server from the status map
// Note: This only clears status, not tools. For full removal use cache.RemoveServer
func ClearServerStatus(name string) {
	// We don't have a direct method to clear just status in cache yet,
	// but we can set it to a zero value or "loading" if needed.
	// For now, let's just leave it as this function might not be used or we can implement explicit clear if needed.
	// Actually, let's implement a way to clear status if really needed, but it's mostly used for cleanup.
}

// MCPStatusResponse is the response for GET /api/mcp/status
type MCPStatusResponse struct {
	Servers []cache.ServerStatus `json:"servers"`
}

// MCPStatusHandler handles GET /api/mcp/status
func MCPStatusHandler(w http.ResponseWriter, r *http.Request) {
	// Platform mode: return status from DB store
	if mcpStore := effectiveMCPStore(r); mcpStore != nil {
		servers, err := mcpStore.List(r.Context())
		if err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to load MCP servers: "+err.Error())
			return
		}
		statuses := make([]cache.ServerStatus, 0, len(servers))
		for _, s := range servers {
			status := "configured"
			if !s.IsEnabled() {
				status = "disabled"
			}
			toolCount := 0
			if s.CachedTools != nil && len(s.CachedTools) > 2 { // not empty "[]" or "null"
				// Count tools from cached_tools JSON array
				var tools []json.RawMessage
				if json.Unmarshal(s.CachedTools, &tools) == nil {
					toolCount = len(tools)
				}
			}
			statuses = append(statuses, cache.ServerStatus{
				Name:      s.Name,
				Status:    status,
				ToolCount: toolCount,
			})
		}
		response := MCPStatusResponse{Servers: statuses}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Personal mode: return from in-memory status cache
	statuses := GetServerStatus()

	response := MCPStatusResponse{
		Servers: statuses,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// loadToolsInternal does the actual work of loading tools (must NOT hold the lock)
func loadToolsInternal(ctx context.Context) []ToolInfo {
	slog.Info("starting to load tools")
	now := time.Now().UTC().Format(time.RFC3339)

	// Start with all built-in tools (internal + all category tools)
	var allTools []ToolInfo
	for _, decl := range tools.GetAllFlowToolDeclarations() {
		allTools = append(allTools, ToolInfo{
			Name:        decl.Name,
			Description: decl.Description,
			Source:      decl.Category,
		})
	}

	// Get MCP tools
	mcpManager, err := mcp.NewManager()
	if err != nil {
		slog.Warn("failed to create mcp manager", "error", err)
	} else {
		if err := mcpManager.InitializeToolsets(ctx); err != nil {
			slog.Warn("failed to initialize mcp toolsets", "error", err)
		}

		// Process init results to update server status
		initResults := mcpManager.GetInitResults()
		for _, result := range initResults {
			if !result.Success {
				SetServerStatus(result.Name, cache.ServerStatus{
					Name:      result.Name,
					Status:    "error",
					Error:     result.Error,
					ToolCount: 0,
					LastCheck: now,
				})
			}
		}

		toolsets := mcpManager.GetNamedToolsets()

		// Create minimal context for fetching tools
		minimalCtx := &minimalReadonlyContext{Context: ctx}

		for _, namedToolset := range toolsets {
			serverName := namedToolset.Name
			mcpToolsList, err := namedToolset.Toolset.Tools(minimalCtx)
			if err != nil {
				stderrOutput := mcp.GetStderr(namedToolset.Stderr)
				slog.Warn("failed to get tools from server", "server", serverName, "error", err, "stderr", stderrOutput)
				// Don't persist error status for context cancellation (transient timeout errors)
				// These happen when the refresh times out, not when the server is broken
				if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "context canceled") {
					errMsg := fmt.Sprintf("Failed to list tools: %v", err)
					if stderrOutput != "" && stderrOutput != "no stderr output" {
						errMsg = stderrOutput
					}
					SetServerStatus(serverName, cache.ServerStatus{
						Name:      serverName,
						Status:    "error",
						Error:     errMsg,
						ToolCount: 0,
						LastCheck: now,
					})
				}
				continue
			}

			toolCount := 0
			for _, t := range mcpToolsList {
				allTools = append(allTools, ToolInfo{
					Name:        t.Name(),
					Description: t.Description(),
					Source:      serverName,
				})
				toolCount++
			}

			// Update status to healthy with tool count
			SetServerStatus(serverName, cache.ServerStatus{
				Name:      serverName,
				Status:    "healthy",
				ToolCount: toolCount,
				LastCheck: now,
			})
		}

		// Cleanup after fetching tool info - don't keep MCP servers running
		mcpManager.Cleanup()
	}

	return allTools
}

// InitToolsCache initializes the tools cache at server startup
// It tries to load from persistent cache first for fast startup
func InitToolsCache(ctx context.Context) {
	globalToolsCache.mu.Lock()

	if globalToolsCache.loaded || globalToolsCache.loading {
		globalToolsCache.mu.Unlock()
		return
	}

	globalToolsCache.loading = true
	globalToolsCache.mu.Unlock()

	// Try to load from persistent cache first (fast path)
	persistentCache, err := cache.LoadCache()
	if err == nil && len(persistentCache.Tools) > 0 {
		slog.Info("loading tools from persistent cache", "count", len(persistentCache.Tools))

		// Start with all built-in tool declarations (always authoritative)
		builtinDecls := tools.GetAllFlowToolDeclarations()
		builtinNames := make(map[string]bool, len(builtinDecls))
		var allTools []ToolInfo
		for _, decl := range builtinDecls {
			allTools = append(allTools, ToolInfo{
				Name:        decl.Name,
				Description: decl.Description,
				Source:      decl.Category,
			})
			builtinNames[decl.Name] = true
		}

		// Add MCP tools from persistent cache (skip built-in tools already added)
		for _, t := range persistentCache.Tools {
			if builtinNames[t.Name] {
				continue
			}
			allTools = append(allTools, ToolInfo{
				Name:        t.Name,
				Description: t.Description,
				Source:      t.Source,
			})
		}

		globalToolsCache.mu.Lock()
		globalToolsCache.tools = allTools
		globalToolsCache.loaded = true
		globalToolsCache.loading = false
		globalToolsCache.mu.Unlock()

		slog.Info("tools cache initialized from persistent cache", "count", len(allTools))

		// Validate checksums in background - refresh any changed servers
		go validateAndRefreshChangedServers(ctx, persistentCache)
		return
	}

	// Persistent cache is empty - do full initialization and populate cache
	slog.Info("persistent cache empty or missing, doing full initialization")
	allTools := loadToolsInternal(ctx)

	// Store results
	globalToolsCache.mu.Lock()
	globalToolsCache.tools = allTools
	globalToolsCache.loaded = true
	globalToolsCache.loading = false
	globalToolsCache.mu.Unlock()

	// Also populate persistent cache for next time
	go populatePersistentCache(ctx, allTools)

	slog.Info("tools cache initialized", "count", len(allTools))
}

// populatePersistentCache saves tools to persistent cache grouped by server
func populatePersistentCache(ctx context.Context, allTools []ToolInfo) {
	// Load MCP config for checksum computation
	mcpCfg, err := config.LoadMCPConfig()
	if err != nil {
		slog.Warn("could not load mcp config for checksums", "component", "cache", "error", err)
		mcpCfg = nil
	}

	// Group tools by server
	toolsByServer := make(map[string][]cache.ToolEntry)
	for _, t := range allTools {
		toolsByServer[t.Source] = append(toolsByServer[t.Source], cache.ToolEntry{
			Name:        t.Name,
			Description: t.Description,
			Source:      t.Source,
		})
	}

	// Add each server's tools in one call with computed checksum
	for serverName, tools := range toolsByServer {
		checksum := ""
		if serverName == "internal" {
			// Internal tools use a fixed checksum - they only change with code updates
			checksum = "internal-tools-v1"
		} else if mcpCfg != nil && mcpCfg.MCPServers != nil {
			if serverCfg, ok := mcpCfg.MCPServers[serverName]; ok {
				checksum = cache.ComputeServerChecksum(serverCfg.Command, serverCfg.Args, serverCfg.Env)
			}
		}
		cache.AddServerTools(serverName, tools, checksum)
		slog.Info("added tools for server to persistent cache", "component", "cache", "count", len(tools), "server", serverName, "checksum", checksum[:min(8, len(checksum))])
	}

	if err := cache.SaveCache(); err != nil {
		slog.Warn("failed to save persistent cache", "component", "cache", "error", err)
	} else {

		slog.Info("persistent cache populated from full initialization", "component", "cache", "servers", len(toolsByServer))
	}
}

// RefreshMCPServerHandler handles POST /api/mcp/{server}/refresh
func RefreshMCPServerHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	serverName := vars["name"] // "name" to match definition in handlers.go
	if serverName == "" {
		http.Error(w, "Server name is required", http.StatusBadRequest)
		return
	}

	// Platform mode: refresh by starting the MCP process and discovering tools
	if mcpStore := effectiveMCPStore(r); mcpStore != nil {
		server, err := mcpStore.Get(r.Context(), serverName)
		if err != nil || server == nil {
			respondError(w, http.StatusNotFound, "Server not found")
			return
		}

		// Build a config.MCPServerConfig and use the bridge to discover tools
		serverCfg := config.MCPServerConfig{
			Command:   server.Command,
			Args:      server.Args,
			Env:       server.Env,
			Transport: server.Transport,
			URL:       server.URL,
			Enabled:   server.Enabled,
		}

		// Run tool discovery using the existing mechanism
		servers := map[string]config.MCPServerConfig{serverName: serverCfg}
		go func() {
			bgCtx := context.Background()
			discoveredTools := discoverMCPToolsForPlatform(bgCtx, serverName, servers)
			if discoveredTools != nil {
				if err := mcpStore.UpdateCachedTools(bgCtx, serverName, discoveredTools); err != nil {
					slog.Warn("failed to update cached_tools in store", "server", serverName, "error", err)
				}
			}
		}()

		respondJSON(w, http.StatusOK, map[string]interface{}{"success": true})
		return
	}

	// Personal mode: original logic
	// Update status to loading
	SetServerStatus(serverName, cache.ServerStatus{
		Name:      serverName,
		Status:    "loading",
		ToolCount: 0,
		LastCheck: time.Now().UTC().Format(time.RFC3339),
	})

	// Do the refresh in foreground
	err := RefreshSingleServer(r.Context(), serverName)

	response := map[string]interface{}{}
	if err != nil {
		response["success"] = false
		response["error"] = err.Error()

		// RefreshSingleServer might fail before updating status
		SetServerStatus(serverName, cache.ServerStatus{
			Name:      serverName,
			Status:    "error",
			Error:     err.Error(),
			ToolCount: 0,
			LastCheck: time.Now().UTC().Format(time.RFC3339),
		})
	} else {
		response["success"] = true
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// discoverMCPToolsForPlatform starts an MCP server, discovers its tools, and
// returns the tool declarations as JSON. Returns nil on error (logged).
func discoverMCPToolsForPlatform(ctx context.Context, serverName string, servers map[string]config.MCPServerConfig) json.RawMessage {
	cfg := &config.MCPConfig{MCPServers: servers}
	mgr := mcp.NewManagerFromConfig(cfg)
	defer mgr.Cleanup()

	if err := mgr.InitializeToolsets(ctx); err != nil {
		slog.Warn("platform MCP refresh: failed to initialize", "server", serverName, "error", err)
		return nil
	}

	// Get the tool declarations from the named toolsets
	type toolEntry struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	var toolEntries []toolEntry

	minimalCtx := &minimalReadonlyContext{Context: ctx}
	for _, namedToolset := range mgr.GetNamedToolsets() {
		if namedToolset.Name != serverName {
			continue
		}
		mcpTools, err := namedToolset.Toolset.Tools(minimalCtx)
		if err != nil {
			slog.Warn("platform MCP refresh: failed to get tools", "server", serverName, "error", err)
			return nil
		}
		for _, t := range mcpTools {
			toolEntries = append(toolEntries, toolEntry{
				Name:        t.Name(),
				Description: t.Description(),
			})
		}
	}

	if toolEntries == nil {
		toolEntries = []toolEntry{} // ensure valid JSON "[]"
	}

	data, err := json.Marshal(toolEntries)
	if err != nil {
		slog.Warn("platform MCP refresh: failed to marshal tools", "server", serverName, "error", err)
		return nil
	}

	slog.Info("platform MCP refresh: discovered tools", "server", serverName, "count", len(toolEntries))
	return data
}

// validateAndRefreshChangedServers compares the current MCP config checksums
// against the cached checksums and refreshes any servers that have changed
func validateAndRefreshChangedServers(ctx context.Context, persistentCache *cache.PersistentToolsCache) {
	mcpCfg, err := config.LoadMCPConfig()
	if err != nil {
		slog.Warn("could not load mcp config for validation", "component", "cache", "error", err)
		return
	}
	if mcpCfg.MCPServers == nil {
		return
	}

	// Find servers that need refreshing
	var serversToRefresh []string

	for serverName, serverCfg := range mcpCfg.MCPServers {
		currentChecksum := cache.ComputeServerChecksum(serverCfg.Command, serverCfg.Args, serverCfg.Env)
		cachedChecksum := persistentCache.ServerChecksums[serverName]

		if cachedChecksum == "" {
			slog.Info("server is new, will refresh", "component", "cache", "server", serverName)
			serversToRefresh = append(serversToRefresh, serverName)
		} else if cachedChecksum != currentChecksum {
			slog.Info("server config changed, will refresh", "component", "cache", "server", serverName)
			serversToRefresh = append(serversToRefresh, serverName)
		}
	}

	// Also check for servers in cache that were removed from config
	for serverName := range persistentCache.ServerChecksums {
		if _, exists := mcpCfg.MCPServers[serverName]; !exists {
			slog.Info("server removed from config, removing from cache", "component", "cache", "server", serverName)
			cache.RemoveServer(serverName)
		}
	}

	if len(serversToRefresh) == 0 {
		slog.Info("all servers are up to date, no refresh needed", "component", "cache")
		return
	}

	slog.Info("refreshing servers", "component", "cache", "count", len(serversToRefresh), "servers", serversToRefresh)

	// Initialize MCP manager and refresh each changed server
	mcpManager, err := mcp.NewManager()
	if err != nil {
		slog.Warn("failed to create mcp manager for refresh", "component", "cache", "error", err)
		return
	}

	for _, serverName := range serversToRefresh {
		namedToolset, err := mcpManager.InitializeSingleToolset(ctx, serverName)
		if err != nil {
			slog.Warn("failed to initialize server", "component", "cache", "server", serverName, "error", err)
			continue
		}

		minimalCtx := &minimalReadonlyContext{Context: ctx}
		mcpTools, err := namedToolset.Toolset.Tools(minimalCtx)
		if err != nil {
			slog.Warn("failed to get tools from server", "component", "cache", "server", serverName, "error", err, "stderr", mcp.GetStderr(namedToolset.Stderr))
			continue
		}

		// Update in-memory cache
		var newTools []ToolInfo
		for _, t := range mcpTools {
			newTools = append(newTools, ToolInfo{
				Name:        t.Name(),
				Description: t.Description(),
				Source:      serverName,
			})
		}
		AddServerToolsToCache(serverName, newTools)

		// Update persistent cache (use mcpTools for schema access)
		persistentTools := make([]cache.ToolEntry, 0, len(mcpTools))
		for _, t := range mcpTools {
			persistentTools = append(persistentTools, cache.ToolEntry{
				Name:        t.Name(),
				Description: t.Description(),
				Source:      serverName,
				InputSchema: common.ExtractToolInputSchema(t),
			})
		}
		serverCfg := mcpCfg.MCPServers[serverName]
		checksum := cache.ComputeServerChecksum(serverCfg.Command, serverCfg.Args, serverCfg.Env)
		cache.AddServerTools(serverName, persistentTools, checksum)
		slog.Info("refreshed server", "component", "cache", "server", serverName, "tools", len(newTools))
	}

	if err := cache.SaveCache(); err != nil {
		slog.Warn("failed to save persistent cache after refresh", "component", "cache", "error", err)
	} else {
		slog.Info("persistent cache saved after refresh", "component", "cache", "servers", len(serversToRefresh))
	}
}

// GetCachedTools returns the cached tools list (personal mode only).
// For request-scoped tool lists that respect platform mode, use GetCachedToolsForRequest.
func GetCachedTools() []ToolInfo {
	globalToolsCache.mu.RLock()
	defer globalToolsCache.mu.RUnlock()

	if !globalToolsCache.loaded {
		return nil
	}

	// Return a copy to avoid race conditions
	result := make([]ToolInfo, len(globalToolsCache.tools))
	copy(result, globalToolsCache.tools)
	return result
}

// GetCachedToolsForRequest returns the cached tools appropriate for the request context.
// In platform mode, it reads from the org+team DB stores' CachedTools.
// In personal mode, it returns from the global in-memory cache.
func GetCachedToolsForRequest(r *http.Request) []ToolInfo {
	svc := store.FromRequest(r)
	if svc == nil || svc.Mode != store.ModePlatform {
		return GetCachedTools()
	}

	// Platform mode: build merged tool list from DB stores (team overrides org by name)
	type toolEntry struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	// Collect servers: org first, team overrides
	serverMap := make(map[string]json.RawMessage) // name -> cachedTools
	if svc.MCPServers != nil {
		orgServers, err := svc.MCPServers.List(r.Context())
		if err == nil {
			for _, s := range orgServers {
				if s.IsEnabled() && len(s.CachedTools) > 0 {
					serverMap[s.Name] = s.CachedTools
				}
			}
		}
	}
	if svc.TeamMCPServers != nil {
		teamServers, err := svc.TeamMCPServers.List(r.Context())
		if err == nil {
			for _, s := range teamServers {
				if s.IsEnabled() && len(s.CachedTools) > 0 {
					serverMap[s.Name] = s.CachedTools
				} else {
					// Team disabled server overrides org enabled
					delete(serverMap, s.Name)
				}
			}
		}
	}

	var result []ToolInfo
	// Add all built-in tools (internal + all category tools)
	for _, decl := range tools.GetAllFlowToolDeclarations() {
		result = append(result, ToolInfo{
			Name:        decl.Name,
			Description: decl.Description,
			Source:      decl.Category,
		})
	}

	// Parse each server's cached tools from DB
	for serverName, cachedData := range serverMap {
		var entries []toolEntry
		if json.Unmarshal(cachedData, &entries) == nil {
			for _, e := range entries {
				result = append(result, ToolInfo{
					Name:        e.Name,
					Description: e.Description,
					Source:      serverName,
				})
			}
		}
	}

	// Include installed standard servers (Tavily, Brave, Firecrawl, etc.)
	// that are NOT already in the DB stores. These are configured via the
	// filesystem credential store + config.yaml and may not have been
	// explicitly installed into the team DB.
	for _, srv := range config.GetStandardServers() {
		if serverMap[srv.ID] != nil {
			continue // Already in DB stores — skip
		}
		if !config.IsStandardServerInstalled(srv.ID) {
			continue // Not installed — skip
		}
		// Try to get tool info from the persistent file-based cache
		cachedEntries := cache.GetToolsForServer(srv.ID)
		if len(cachedEntries) > 0 {
			for _, e := range cachedEntries {
				result = append(result, ToolInfo{
					Name:        e.Name,
					Description: e.Description,
					Source:      srv.ID,
				})
			}
		} else {
			// Fallback: use the known web tool names from the standard server definition
			if srv.WebSearchTool != "" {
				if parts := strings.SplitN(srv.WebSearchTool, ":", 2); len(parts) == 2 {
					result = append(result, ToolInfo{
						Name:        parts[1],
						Description: srv.DisplayName + " web search",
						Source:      srv.ID,
					})
				}
			}
			if srv.WebExtractTool != "" {
				if parts := strings.SplitN(srv.WebExtractTool, ":", 2); len(parts) == 2 {
					result = append(result, ToolInfo{
						Name:        parts[1],
						Description: srv.DisplayName + " content extraction",
						Source:      srv.ID,
					})
				}
			}
		}
	}

	return result
}

// AddServerToolsToCache adds tools from a specific server to the cache
// This is used for incremental updates when installing a new MCP server
func AddServerToolsToCache(serverName string, newTools []ToolInfo) {
	globalToolsCache.mu.Lock()
	defer globalToolsCache.mu.Unlock()

	// Add the new tools to the existing cache
	globalToolsCache.tools = append(globalToolsCache.tools, newTools...)
	slog.Info("added tools to cache", "server", serverName, "added", len(newTools), "total", len(globalToolsCache.tools))
}

// RemoveServerToolsFromCache removes all tools from a specific server
// This is used when uninstalling/deleting an MCP server
func RemoveServerToolsFromCache(serverName string) {
	globalToolsCache.mu.Lock()
	defer globalToolsCache.mu.Unlock()

	// Filter out tools from the specified server
	filtered := make([]ToolInfo, 0, len(globalToolsCache.tools))
	removedCount := 0
	for _, t := range globalToolsCache.tools {
		if t.Source != serverName {
			filtered = append(filtered, t)
		} else {
			removedCount++
		}
	}
	globalToolsCache.tools = filtered
	slog.Info("removed tools from cache", "server", serverName, "removed", removedCount, "total", len(globalToolsCache.tools))
}

// RefreshToolsCache forces a refresh of the tools cache
func RefreshToolsCache(ctx context.Context) {
	slog.Info("refresh tools cache called")
	globalToolsCache.mu.Lock()

	// If already loading, skip
	if globalToolsCache.loading {
		slog.Debug("tools cache already loading, skipping refresh")
		globalToolsCache.mu.Unlock()
		return
	}

	globalToolsCache.loading = true
	globalToolsCache.mu.Unlock()

	// Use a channel to implement hard timeout
	done := make(chan []ToolInfo, 1)

	go func() {
		// Create a context with timeout
		timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		done <- loadToolsInternal(timeoutCtx)
	}()

	// Wait for result with hard timeout
	select {
	case allTools := <-done:
		// Store results
		globalToolsCache.mu.Lock()
		globalToolsCache.tools = allTools
		globalToolsCache.loaded = true
		globalToolsCache.loading = false
		globalToolsCache.mu.Unlock()
		slog.Info("tools cache refreshed", "count", len(allTools))

	case <-time.After(35 * time.Second):
		// Hard timeout - just reset the flag and move on
		slog.Warn("tools cache refresh timed out after 35s")
		globalToolsCache.mu.Lock()
		globalToolsCache.loading = false
		globalToolsCache.mu.Unlock()
	}
}

// RefreshSingleServer refreshes/adds a single server to the cache
func RefreshSingleServer(ctx context.Context, serverName string) error {
	slog.Info("refreshing single server", "server", serverName)

	mcpManager, err := mcp.NewManager()
	if err != nil {
		return err
	}
	defer mcpManager.Cleanup()

	namedToolset, err := mcpManager.InitializeSingleToolset(ctx, serverName)
	if err != nil {
		return err
	}

	minimalCtx := &minimalReadonlyContext{Context: ctx}
	mcpTools, err := namedToolset.Toolset.Tools(minimalCtx)
	if err != nil {
		return err
	}

	// Update in-memory cache
	var newTools []ToolInfo
	for _, t := range mcpTools {
		newTools = append(newTools, ToolInfo{
			Name:        t.Name(),
			Description: t.Description(),
			Source:      serverName,
		})
	}

	// Remove old tools for this server first (in case it's an update)
	RemoveServerToolsFromCache(serverName)
	AddServerToolsToCache(serverName, newTools)

	// Update persistent cache
	persistentTools := make([]cache.ToolEntry, 0, len(mcpTools))
	for _, t := range mcpTools {
		persistentTools = append(persistentTools, cache.ToolEntry{
			Name:        t.Name(),
			Description: t.Description(),
			Source:      serverName,
			InputSchema: common.ExtractToolInputSchema(t),
		})
	}
	mcpCfg, err := config.LoadMCPConfig()
	checksum := ""
	if err == nil && mcpCfg.MCPServers != nil {
		serverCfg := mcpCfg.MCPServers[serverName]
		checksum = cache.ComputeServerChecksum(serverCfg.Command, serverCfg.Args, serverCfg.Env)
	}

	cache.AddServerTools(serverName, persistentTools, checksum)
	cache.SaveCache()

	return nil
}
