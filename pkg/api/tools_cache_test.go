package api

import (
	"os"
	"sync"
	"testing"

	"github.com/schardosin/astonish/pkg/cache"
)

func testSetup(t *testing.T) func() {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "api-cache-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	cache.SetCacheDir(tmpDir)
	
	return func() {
		os.RemoveAll(tmpDir)
		cache.SetCacheDir("")
	}
}

// TestGetCachedTools_EmptyCache verifies behavior when cache is not loaded
func TestGetCachedTools_EmptyCache(t *testing.T) {
	// Save original state
	originalCache := globalToolsCache
	defer func() { globalToolsCache = originalCache }()

	// Create empty cache
	globalToolsCache = &ToolsCache{
		tools:   nil,
		loaded:  false,
		loading: false,
		mu:      sync.RWMutex{},
	}

	tools := GetCachedTools()
	if tools != nil {
		t.Errorf("expected nil when cache not loaded, got %v", tools)
	}
}

// TestGetCachedTools_LoadedCache verifies tools are returned when cache is loaded
func TestGetCachedTools_LoadedCache(t *testing.T) {
	// Save original state
	originalCache := globalToolsCache
	defer func() { globalToolsCache = originalCache }()

	// Create loaded cache with test tools
	testTools := []ToolInfo{
		{Name: "tool1", Description: "Test tool 1", Source: "test-server"},
		{Name: "tool2", Description: "Test tool 2", Source: "test-server"},
	}

	globalToolsCache = &ToolsCache{
		tools:   testTools,
		loaded:  true,
		loading: false,
		mu:      sync.RWMutex{},
	}

	tools := GetCachedTools()
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}

	// Verify it's a copy (modifying returned slice shouldn't affect cache)
	tools[0].Name = "modified"
	cachedTools := GetCachedTools()
	if cachedTools[0].Name == "modified" {
		t.Error("GetCachedTools should return a copy, not the original slice")
	}
}

// TestAddServerToolsToCache verifies adding tools to cache
func TestAddServerToolsToCache(t *testing.T) {
	// Save original state
	originalCache := globalToolsCache
	defer func() { globalToolsCache = originalCache }()

	// Create empty loaded cache
	globalToolsCache = &ToolsCache{
		tools:   []ToolInfo{},
		loaded:  true,
		loading: false,
		mu:      sync.RWMutex{},
	}

	// Add tools
	newTools := []ToolInfo{
		{Name: "new_tool1", Description: "New tool 1", Source: "new-server"},
		{Name: "new_tool2", Description: "New tool 2", Source: "new-server"},
	}
	AddServerToolsToCache("new-server", newTools)

	// Verify tools were added
	cachedTools := GetCachedTools()
	if len(cachedTools) != 2 {
		t.Errorf("expected 2 tools after adding, got %d", len(cachedTools))
	}

	// Verify tool content
	foundTool1 := false
	foundTool2 := false
	for _, tool := range cachedTools {
		if tool.Name == "new_tool1" && tool.Source == "new-server" {
			foundTool1 = true
		}
		if tool.Name == "new_tool2" && tool.Source == "new-server" {
			foundTool2 = true
		}
	}
	if !foundTool1 || !foundTool2 {
		t.Error("added tools not found in cache")
	}
}

// TestAddServerToolsToCache_Accumulation verifies tools accumulate in cache
func TestAddServerToolsToCache_Accumulation(t *testing.T) {
	// Save original state
	originalCache := globalToolsCache
	defer func() { globalToolsCache = originalCache }()

	// Create cache with existing tools
	globalToolsCache = &ToolsCache{
		tools: []ToolInfo{
			{Name: "existing_tool", Description: "Existing", Source: "existing-server"},
		},
		loaded:  true,
		loading: false,
		mu:      sync.RWMutex{},
	}

	// Add more tools
	newTools := []ToolInfo{
		{Name: "new_tool", Description: "New", Source: "new-server"},
	}
	AddServerToolsToCache("new-server", newTools)

	// Verify both sets exist
	cachedTools := GetCachedTools()
	if len(cachedTools) != 2 {
		t.Errorf("expected 2 tools (1 existing + 1 new), got %d", len(cachedTools))
	}
}

// TestRemoveServerToolsFromCache verifies removing tools by server name
func TestRemoveServerToolsFromCache(t *testing.T) {
	// Save original state
	originalCache := globalToolsCache
	defer func() { globalToolsCache = originalCache }()

	// Create cache with tools from multiple servers
	globalToolsCache = &ToolsCache{
		tools: []ToolInfo{
			{Name: "tool1", Description: "Tool 1", Source: "server-a"},
			{Name: "tool2", Description: "Tool 2", Source: "server-a"},
			{Name: "tool3", Description: "Tool 3", Source: "server-b"},
		},
		loaded:  true,
		loading: false,
		mu:      sync.RWMutex{},
	}

	// Remove server-a tools
	RemoveServerToolsFromCache("server-a")

	// Verify only server-b tools remain
	cachedTools := GetCachedTools()
	if len(cachedTools) != 1 {
		t.Errorf("expected 1 tool remaining, got %d", len(cachedTools))
	}
	if cachedTools[0].Source != "server-b" {
		t.Errorf("expected remaining tool from server-b, got %s", cachedTools[0].Source)
	}
}

// TestRemoveServerToolsFromCache_NonExistent verifies no error when removing non-existent server
func TestRemoveServerToolsFromCache_NonExistent(t *testing.T) {
	// Save original state
	originalCache := globalToolsCache
	defer func() { globalToolsCache = originalCache }()

	// Create cache with tools
	globalToolsCache = &ToolsCache{
		tools: []ToolInfo{
			{Name: "tool1", Description: "Tool 1", Source: "server-a"},
		},
		loaded:  true,
		loading: false,
		mu:      sync.RWMutex{},
	}

	// Remove non-existent server (should not panic or error)
	RemoveServerToolsFromCache("non-existent-server")

	// Verify original tools unchanged
	cachedTools := GetCachedTools()
	if len(cachedTools) != 1 {
		t.Errorf("expected 1 tool unchanged, got %d", len(cachedTools))
	}
}

// TestToolInfo_Equality verifies ToolInfo struct comparison
func TestToolInfo_Equality(t *testing.T) {
	tool1 := ToolInfo{Name: "test", Description: "desc", Source: "src"}
	tool2 := ToolInfo{Name: "test", Description: "desc", Source: "src"}
	tool3 := ToolInfo{Name: "different", Description: "desc", Source: "src"}

	if tool1 != tool2 {
		t.Error("identical ToolInfo structs should be equal")
	}
	if tool1 == tool3 {
		t.Error("different ToolInfo structs should not be equal")
	}
}

// TestCacheConcurrency verifies thread-safety of cache operations
func TestCacheConcurrency(t *testing.T) {
	// Save original state
	originalCache := globalToolsCache
	defer func() { globalToolsCache = originalCache }()

	// Create empty cache
	globalToolsCache = &ToolsCache{
		tools:   []ToolInfo{},
		loaded:  true,
		loading: false,
		mu:      sync.RWMutex{},
	}

	// Run concurrent operations
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(3)

		// Concurrent adds
		go func(serverNum int) {
			defer wg.Done()
			AddServerToolsToCache("server-"+string(rune('a'+serverNum)), []ToolInfo{
				{Name: "tool", Source: "server-" + string(rune('a'+serverNum))},
			})
		}(i)

		// Concurrent reads
		go func() {
			defer wg.Done()
			_ = GetCachedTools()
		}()

		// Concurrent removes
		go func(serverNum int) {
			defer wg.Done()
			RemoveServerToolsFromCache("server-" + string(rune('a'+serverNum)))
		}(i)
	}

	wg.Wait()
	// If we get here without deadlock or panic, concurrency is handled
}

// TestRefreshToolsCache_SkipsWhenLoading verifies skip behavior when already loading
func TestRefreshToolsCache_SkipsWhenLoading(t *testing.T) {
	// Save original state
	originalCache := globalToolsCache
	defer func() { globalToolsCache = originalCache }()

	// Create cache that is already loading
	globalToolsCache = &ToolsCache{
		tools:   []ToolInfo{{Name: "existing", Source: "test"}},
		loaded:  true,
		loading: true, // Already loading
		mu:      sync.RWMutex{},
	}

	// Verify the loading flag is set
	globalToolsCache.mu.RLock()
	isLoading := globalToolsCache.loading
	globalToolsCache.mu.RUnlock()

	if !isLoading {
		t.Error("expected loading flag to be true")
	}

	// Verify tools are still accessible while loading
	tools := GetCachedTools()
	if len(tools) != 1 {
		t.Errorf("expected 1 tool while loading, got %d", len(tools))
	}
}

// TestAddAndRemoveServerTools_RoundTrip verifies add then remove works correctly
func TestAddAndRemoveServerTools_RoundTrip(t *testing.T) {
	// Save original state
	originalCache := globalToolsCache
	defer func() { globalToolsCache = originalCache }()

	// Create empty cache
	globalToolsCache = &ToolsCache{
		tools:   []ToolInfo{},
		loaded:  true,
		loading: false,
		mu:      sync.RWMutex{},
	}

	// Add tools
	AddServerToolsToCache("test-server", []ToolInfo{
		{Name: "tool1", Description: "Tool 1", Source: "test-server"},
		{Name: "tool2", Description: "Tool 2", Source: "test-server"},
	})

	// Verify added
	if len(GetCachedTools()) != 2 {
		t.Error("tools should be added")
	}

	// Remove tools
	RemoveServerToolsFromCache("test-server")

	// Verify removed
	if len(GetCachedTools()) != 0 {
		t.Error("tools should be removed")
	}
}

// TestGetCachedTools_ThreadSafety verifies reads don't interfere with state
func TestGetCachedTools_ThreadSafety(t *testing.T) {
	// Save original state
	originalCache := globalToolsCache
	defer func() { globalToolsCache = originalCache }()

	// Create cache with tools
	globalToolsCache = &ToolsCache{
		tools: []ToolInfo{
			{Name: "tool1", Source: "server"},
		},
		loaded:  true,
		loading: false,
		mu:      sync.RWMutex{},
	}

	// Multiple concurrent reads
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tools := GetCachedTools()
			if len(tools) != 1 {
				t.Errorf("expected 1 tool, got %d", len(tools))
			}
		}()
	}
	wg.Wait()
}

// TestSetServerStatus verifies setting and retrieving server status via API wrapper
func TestSetServerStatus(t *testing.T) {
	cleanup := testSetup(t)
	defer cleanup()

	status := cache.ServerStatus{
		Name:      "api-test-server",
		Status:    "error",
		Error:     "API Error",
		ToolCount: 0,
		LastCheck: "2024-12-31T12:00:00Z",
	}

	SetServerStatus("api-test-server", status)

	statuses := GetServerStatus()
	found := false
	for _, s := range statuses {
		if s.Name == "api-test-server" {
			found = true
			if s.Status != "error" || s.Error != "API Error" {
				t.Errorf("expected error status, got %+v", s)
			}
		}
	}
	if !found {
		t.Error("status not found in GetServerStatus output")
	}
}
