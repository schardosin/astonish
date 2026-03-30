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
	var allTools []ToolInfo
	now := time.Now().UTC().Format(time.RFC3339)

	// Get internal tools
	internalTools, err := tools.GetInternalTools()
	if err != nil {
		slog.Warn("failed to get internal tools", "error", err)
	} else {
		for _, t := range internalTools {
			allTools = append(allTools, ToolInfo{
				Name:        t.Name(),
				Description: t.Description(),
				Source:      "internal",
			})
		}
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

		// Convert cache.ToolEntry to api.ToolInfo
		var allTools []ToolInfo
		hasInternalTools := false
		for _, t := range persistentCache.Tools {
			if t.Source == "internal" {
				hasInternalTools = true
			}
			allTools = append(allTools, ToolInfo{
				Name:        t.Name,
				Description: t.Description,
				Source:      t.Source,
			})
		}

		// If persistent cache doesn't have internal tools (old cache format),
		// add them now and update the cache
		if !hasInternalTools {
			slog.Info("adding internal tools to cache")
			internalTools, err := tools.GetInternalTools()
			if err == nil {
				for _, t := range internalTools {
					allTools = append(allTools, ToolInfo{
						Name:        t.Name(),
						Description: t.Description(),
						Source:      "internal",
					})
				}
				// Update persistent cache with internal tools
				go func() {
					internalEntries := make([]cache.ToolEntry, 0, len(internalTools))
					for _, t := range internalTools {
						internalEntries = append(internalEntries, cache.ToolEntry{
							Name:        t.Name(),
							Description: t.Description(),
							Source:      "internal",
							InputSchema: common.ExtractToolInputSchema(t),
						})
					}
					cache.AddServerTools("internal", internalEntries, "internal-tools-v1")
					cache.SaveCache()
					slog.Info("added internal tools to persistent cache", "component", "cache", "count", len(internalEntries))
				}()
			}
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
		// RefreshSingleServer updates status to healthy on success
		// But let's verify if we need to do anything else.
		// It updates cache status.
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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

// GetCachedTools returns the cached tools list
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
