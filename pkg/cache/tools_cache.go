package cache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/schardosin/astonish/pkg/config"
)

const (
	cacheFileName = "tools_cache.json"
	cacheVersion  = 2
)

// ToolEntry represents a single tool in the cache
type ToolEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"` // MCP server name or "internal"
}

// ServerStatus represents the health and status of an MCP server
type ServerStatus struct {
	Name      string `json:"name"`
	Status    string `json:"status"` // "healthy", "error", "loading"
	Error     string `json:"error,omitempty"`
	ToolCount int    `json:"tool_count"`
	LastCheck string `json:"last_check"`
}

// PersistentToolsCache is the structure stored in the cache file
type PersistentToolsCache struct {
	Version         int                     `json:"version"`
	LastUpdated     time.Time               `json:"lastUpdated"`
	Tools           []ToolEntry             `json:"tools"`
	ServerChecksums map[string]string       `json:"serverChecksums"` // server name -> config checksum
	ServerStatuses  map[string]ServerStatus `json:"serverStatuses"`  // server name -> status
}

// Global in-memory copy for fast access
var (
	memoryCache    *PersistentToolsCache
	cacheMu        sync.RWMutex
	cacheLoaded    bool
	customCacheDir string
)

// SetCacheDir sets a custom directory for the cache file (used for testing)
func SetCacheDir(dir string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	customCacheDir = dir
	memoryCache = nil
	cacheLoaded = false
}

// getCachePath returns the path to the cache file using OS config directory
func getCachePath() (string, error) {
	if customCacheDir != "" {
		return filepath.Join(customCacheDir, cacheFileName), nil
	}
	configDir, err := config.GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, cacheFileName), nil
}

// LoadCache loads the cache from disk into memory
// Returns an empty cache if file doesn't exist
func LoadCache() (*PersistentToolsCache, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	// If already loaded, return memory copy
	if cacheLoaded && memoryCache != nil {
		return memoryCache, nil
	}

	cachePath, err := getCachePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(cachePath)
	if os.IsNotExist(err) {
		// No cache file - return empty cache
		memoryCache = &PersistentToolsCache{
			Version:         cacheVersion,
			LastUpdated:     time.Now(),
			Tools:           []ToolEntry{},
			ServerChecksums: make(map[string]string),
			ServerStatuses:  make(map[string]ServerStatus),
		}
		cacheLoaded = true
		return memoryCache, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read cache file: %w", err)
	}

	var cache PersistentToolsCache
	if err := json.Unmarshal(data, &cache); err != nil {
		// Corrupt cache - return empty
		memoryCache = &PersistentToolsCache{
			Version:         cacheVersion,
			LastUpdated:     time.Now(),
			Tools:           []ToolEntry{},
			ServerChecksums: make(map[string]string),
			ServerStatuses:  make(map[string]ServerStatus),
		}
		cacheLoaded = true
		return memoryCache, nil
	}

	// Initialize maps if nil
	if cache.ServerChecksums == nil {
		cache.ServerChecksums = make(map[string]string)
	}
	if cache.ServerStatuses == nil {
		cache.ServerStatuses = make(map[string]ServerStatus)
	}

	memoryCache = &cache
	cacheLoaded = true
	return memoryCache, nil
}

// SaveCache saves the current cache to disk
func SaveCache() error {
	cacheMu.RLock()
	if memoryCache == nil {
		cacheMu.RUnlock()
		return nil
	}
	
	memoryCache.LastUpdated = time.Now()
	data, err := json.MarshalIndent(memoryCache, "", "  ")
	cacheMu.RUnlock()
	
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}

	cachePath, err := getCachePath()
	if err != nil {
		return err
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	return nil
}

// GetServerForTool looks up which server provides a tool
// Returns empty string if not found
func GetServerForTool(toolName string) string {
	cacheMu.RLock()
	defer cacheMu.RUnlock()

	if memoryCache == nil {
		return ""
	}

	for _, tool := range memoryCache.Tools {
		if tool.Name == toolName {
			return tool.Source
		}
	}
	return ""
}

// GetToolsForServer returns all tools from a specific server
func GetToolsForServer(serverName string) []ToolEntry {
	cacheMu.RLock()
	defer cacheMu.RUnlock()

	if memoryCache == nil {
		return nil
	}

	var result []ToolEntry
	for _, tool := range memoryCache.Tools {
		if tool.Source == serverName {
			result = append(result, tool)
		}
	}
	return result
}

// GetAllTools returns all cached tools
func GetAllTools() []ToolEntry {
	cacheMu.RLock()
	defer cacheMu.RUnlock()

	if memoryCache == nil {
		return nil
	}

	result := make([]ToolEntry, len(memoryCache.Tools))
	copy(result, memoryCache.Tools)
	return result
}

// AddServerTools adds or updates tools for a server
// Also updates the checksum for change detection
func AddServerTools(serverName string, tools []ToolEntry, configChecksum string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	if memoryCache == nil {
		memoryCache = &PersistentToolsCache{
			Version:         cacheVersion,
			LastUpdated:     time.Now(),
			Tools:           []ToolEntry{},
			ServerChecksums: make(map[string]string),
			ServerStatuses:  make(map[string]ServerStatus),
		}
	}

	// Remove existing tools from this server
	filtered := make([]ToolEntry, 0, len(memoryCache.Tools))
	for _, t := range memoryCache.Tools {
		if t.Source != serverName {
			filtered = append(filtered, t)
		}
	}

	// Add new tools
	for _, t := range tools {
		t.Source = serverName // Ensure source is set
		filtered = append(filtered, t)
	}

	memoryCache.Tools = filtered
	memoryCache.ServerChecksums[serverName] = configChecksum
}

// RemoveServer removes all tools from a server
func RemoveServer(serverName string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	if memoryCache == nil {
		return
	}

	// Remove tools
	filtered := make([]ToolEntry, 0, len(memoryCache.Tools))
	for _, t := range memoryCache.Tools {
		if t.Source != serverName {
			filtered = append(filtered, t)
		}
	}
	memoryCache.Tools = filtered

	// Remove checksum and status
	delete(memoryCache.ServerChecksums, serverName)
	delete(memoryCache.ServerStatuses, serverName)
}

// UpdateServerStatus updates the status for a server
func UpdateServerStatus(status ServerStatus) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	if memoryCache == nil {
		memoryCache = &PersistentToolsCache{
			Version:         cacheVersion,
			LastUpdated:     time.Now(),
			Tools:           []ToolEntry{},
			ServerChecksums: make(map[string]string),
			ServerStatuses:  make(map[string]ServerStatus),
		}
	}

	memoryCache.ServerStatuses[status.Name] = status
}

// GetServerStatuses returns all server statuses
func GetServerStatuses() map[string]ServerStatus {
	cacheMu.RLock()
	defer cacheMu.RUnlock()

	if memoryCache == nil {
		return map[string]ServerStatus{}
	}

	// Return copy
	result := make(map[string]ServerStatus)
	for k, v := range memoryCache.ServerStatuses {
		result[k] = v
	}
	return result
}

// GetServerChecksum returns the stored checksum for a server
func GetServerChecksum(serverName string) string {
	cacheMu.RLock()
	defer cacheMu.RUnlock()

	if memoryCache == nil || memoryCache.ServerChecksums == nil {
		return ""
	}
	return memoryCache.ServerChecksums[serverName]
}

// ComputeServerChecksum computes a checksum for a server config
// Used to detect if server config changed
func ComputeServerChecksum(command string, args []string, env map[string]string) string {
	data := fmt.Sprintf("%s|%v|%v", command, args, env)
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash[:8]) // Short hash
}

// IsEmpty returns true if cache has no tools
func IsEmpty() bool {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	return memoryCache == nil || len(memoryCache.Tools) == 0
}

// HasServer returns true if cache has tools from this server
func HasServer(serverName string) bool {
	cacheMu.RLock()
	defer cacheMu.RUnlock()

	if memoryCache == nil || memoryCache.ServerChecksums == nil {
		return false
	}
	_, ok := memoryCache.ServerChecksums[serverName]
	return ok
}

// InvalidateCache clears the in-memory cache, forcing next LoadCache to read from disk
func InvalidateCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	memoryCache = nil
	cacheLoaded = false
}

// ValidateChecksums compares current MCP config checksums against cached checksums
// Returns lists of servers that need refreshing and servers that were removed
func ValidateChecksums(verbose bool) (needsRefresh []string, removed []string) {
	mcpCfg, err := config.LoadMCPConfig()
	if err != nil {
		if verbose {
			fmt.Printf("[Cache] Warning: Could not load MCP config for validation: %v\n", err)
		}
		return nil, nil
	}
	if mcpCfg.MCPServers == nil {
		return nil, nil
	}

	cacheMu.RLock()
	defer cacheMu.RUnlock()

	if memoryCache == nil {
		// No cache loaded yet
		for serverName := range mcpCfg.MCPServers {
			needsRefresh = append(needsRefresh, serverName)
		}
		return needsRefresh, nil
	}

	// Find servers that need refreshing
	for serverName, serverCfg := range mcpCfg.MCPServers {
		currentChecksum := ComputeServerChecksum(serverCfg.Command, serverCfg.Args, serverCfg.Env)
		cachedChecksum := memoryCache.ServerChecksums[serverName]

		if cachedChecksum == "" {
			if verbose {
				fmt.Printf("[Cache] Server '%s' is new (not in cache), will refresh\n", serverName)
			}
			needsRefresh = append(needsRefresh, serverName)
		} else if cachedChecksum != currentChecksum {
			if verbose {
				fmt.Printf("[Cache] Server '%s' config changed (checksum mismatch), will refresh\n", serverName)
			}
			needsRefresh = append(needsRefresh, serverName)
		}
	}

	// Find servers that were removed from config
	for serverName := range memoryCache.ServerChecksums {
		if _, exists := mcpCfg.MCPServers[serverName]; !exists {
			if verbose {
				fmt.Printf("[Cache] Server '%s' was removed from config\n", serverName)
			}
			removed = append(removed, serverName)
		}
	}

	return needsRefresh, removed
}
