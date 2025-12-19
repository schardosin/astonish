package api

import (
	"context"
	"log"
	"sync"
	"time"

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

// loadToolsInternal does the actual work of loading tools (must NOT hold the lock)
func loadToolsInternal(ctx context.Context) []ToolInfo {
	log.Printf("loadToolsInternal: Starting to load tools...")
	var allTools []ToolInfo

	// Get internal tools
	internalTools, err := tools.GetInternalTools()
	if err != nil {
		log.Printf("Warning: failed to get internal tools: %v", err)
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
		log.Printf("Warning: failed to create MCP manager: %v", err)
	} else {
		if err := mcpManager.InitializeToolsets(ctx); err != nil {
			log.Printf("Warning: failed to initialize MCP toolsets: %v", err)
		} else {
			toolsets := mcpManager.GetToolsets()

			// Create minimal context for fetching tools
			minimalCtx := &minimalReadonlyContext{Context: ctx}

			for _, toolset := range toolsets {
				serverName := toolset.Name()
				mcpToolsList, err := toolset.Tools(minimalCtx)
				if err != nil {
					log.Printf("Warning: failed to get tools from %s: %v", serverName, err)
					continue
				}
				for _, t := range mcpToolsList {
					allTools = append(allTools, ToolInfo{
						Name:        t.Name(),
						Description: t.Description(),
						Source:      serverName,
					})
				}
			}
		}
	}

	return allTools
}

// InitToolsCache initializes the tools cache at server startup
func InitToolsCache(ctx context.Context) {
	globalToolsCache.mu.Lock()

	if globalToolsCache.loaded || globalToolsCache.loading {
		globalToolsCache.mu.Unlock()
		return
	}

	globalToolsCache.loading = true
	globalToolsCache.mu.Unlock()

	// Load tools without holding the lock
	allTools := loadToolsInternal(ctx)

	// Store results
	globalToolsCache.mu.Lock()
	globalToolsCache.tools = allTools
	globalToolsCache.loaded = true
	globalToolsCache.loading = false
	globalToolsCache.mu.Unlock()

	log.Printf("Tools cache initialized with %d tools", len(allTools))
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
	log.Printf("Added %d tools from server '%s' to cache (total: %d)", len(newTools), serverName, len(globalToolsCache.tools))
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
	log.Printf("Removed %d tools from server '%s' from cache (total: %d)", removedCount, serverName, len(globalToolsCache.tools))
}

// RefreshToolsCache forces a refresh of the tools cache
func RefreshToolsCache(ctx context.Context) {
	log.Printf("RefreshToolsCache: Called, attempting to refresh...")
	globalToolsCache.mu.Lock()

	// If already loading, skip
	if globalToolsCache.loading {
		log.Printf("RefreshToolsCache: Already loading, skipping")
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
		log.Printf("Tools cache refreshed with %d tools", len(allTools))

	case <-time.After(35 * time.Second):
		// Hard timeout - just reset the flag and move on
		log.Printf("RefreshToolsCache: TIMEOUT after 35s, resetting loading flag")
		globalToolsCache.mu.Lock()
		globalToolsCache.loading = false
		globalToolsCache.mu.Unlock()
	}
}
