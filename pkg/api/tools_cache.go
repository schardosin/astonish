package api

import (
	"context"
	"log"
	"sync"

	"github.com/schardosin/astonish/pkg/mcp"
	"github.com/schardosin/astonish/pkg/tools"
)

// ToolsCache holds cached tool information to avoid re-initializing MCP on every request
type ToolsCache struct {
	tools     []ToolInfo
	loaded    bool
	mu        sync.RWMutex
}

var globalToolsCache = &ToolsCache{}

// InitToolsCache initializes the tools cache at server startup
func InitToolsCache(ctx context.Context) {
	globalToolsCache.mu.Lock()
	defer globalToolsCache.mu.Unlock()
	
	if globalToolsCache.loaded {
		return
	}
	
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
	
	globalToolsCache.tools = allTools
	globalToolsCache.loaded = true
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

// RefreshToolsCache forces a refresh of the tools cache
func RefreshToolsCache(ctx context.Context) {
	globalToolsCache.mu.Lock()
	globalToolsCache.loaded = false
	globalToolsCache.mu.Unlock()
	
	InitToolsCache(ctx)
}
