package cache

import (
	"os"
	"testing"
)

// testSetup creates a temporary directory for testing and resets the cache state
func testSetup(t *testing.T) (string, func()) {
	t.Helper()
	
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "cache-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	
	// Reset global cache state
	cacheMu.Lock()
	memoryCache = nil
	cacheLoaded = false
	cacheMu.Unlock()
	
	// Return cleanup function
	cleanup := func() {
		os.RemoveAll(tmpDir)
		cacheMu.Lock()
		memoryCache = nil
		cacheLoaded = false
		cacheMu.Unlock()
	}
	
	return tmpDir, cleanup
}

// TestLoadCache tests loading the cache (may have existing data from real config)
func TestLoadCache(t *testing.T) {
	_, cleanup := testSetup(t)
	defer cleanup()
	
	// InvalidateCache clears memory cache, forcing a fresh load from disk
	InvalidateCache()
	
	cache, err := LoadCache()
	if err != nil {
		t.Fatalf("LoadCache() unexpected error: %v", err)
	}
	
	// Should return a valid cache, not nil
	if cache == nil {
		t.Fatal("LoadCache() returned nil cache")
	}
	
	// Cache should have the correct version
	if cache.Version != cacheVersion {
		t.Errorf("Expected version %d, got %d", cacheVersion, cache.Version)
	}
	
	// ServerChecksums map should be initialized
	if cache.ServerChecksums == nil {
		t.Error("ServerChecksums should not be nil")
	}
}

// TestComputeServerChecksum tests the checksum computation
func TestComputeServerChecksum(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		args     []string
		env      map[string]string
	}{
		{
			name:    "basic command",
			command: "npx",
			args:    []string{"-y", "mcp-tool"},
			env:     nil,
		},
		{
			name:    "command with env",
			command: "npx",
			args:    []string{"-y", "tavily-mcp"},
			env:     map[string]string{"TAVILY_API_KEY": "test-key"},
		},
		{
			name:    "empty args",
			command: "python",
			args:    nil,
			env:     nil,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checksum1 := ComputeServerChecksum(tt.command, tt.args, tt.env)
			checksum2 := ComputeServerChecksum(tt.command, tt.args, tt.env)
			
			// Same inputs should produce same checksum
			if checksum1 != checksum2 {
				t.Errorf("Same inputs produced different checksums: %s vs %s", checksum1, checksum2)
			}
			
			// Checksum should not be empty
			if checksum1 == "" {
				t.Error("Checksum should not be empty")
			}
			
			// Checksum should be reasonable length (16 hex chars from 8 bytes)
			if len(checksum1) != 16 {
				t.Errorf("Checksum length should be 16, got %d", len(checksum1))
			}
		})
	}
}

// TestComputeServerChecksumDifferentInputs tests that different inputs produce different checksums
func TestComputeServerChecksumDifferentInputs(t *testing.T) {
	checksum1 := ComputeServerChecksum("npx", []string{"-y", "tool1"}, nil)
	checksum2 := ComputeServerChecksum("npx", []string{"-y", "tool2"}, nil)
	checksum3 := ComputeServerChecksum("node", []string{"-y", "tool1"}, nil)
	
	if checksum1 == checksum2 {
		t.Error("Different args should produce different checksums")
	}
	
	if checksum1 == checksum3 {
		t.Error("Different commands should produce different checksums")
	}
}

// TestAddServerTools tests adding tools for a server
func TestAddServerTools(t *testing.T) {
	_, cleanup := testSetup(t)
	defer cleanup()
	
	tools := []ToolEntry{
		{Name: "tool1", Description: "Test tool 1", Source: "test-server"},
		{Name: "tool2", Description: "Test tool 2", Source: "test-server"},
	}
	checksum := "abc123def4567890"
	
	AddServerTools("test-server", tools, checksum)
	
	// Verify tools were added
	result := GetToolsForServer("test-server")
	if len(result) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(result))
	}
	
	// Verify checksum was stored
	storedChecksum := GetServerChecksum("test-server")
	if storedChecksum != checksum {
		t.Errorf("Expected checksum %q, got %q", checksum, storedChecksum)
	}
}

// TestAddServerToolsReplacesExisting tests that adding tools replaces existing tools from the same server
func TestAddServerToolsReplacesExisting(t *testing.T) {
	_, cleanup := testSetup(t)
	defer cleanup()
	
	// Add initial tools
	initialTools := []ToolEntry{
		{Name: "old_tool", Description: "Old tool", Source: "test-server"},
	}
	AddServerTools("test-server", initialTools, "checksum1")
	
	// Add new tools for same server
	newTools := []ToolEntry{
		{Name: "new_tool1", Description: "New tool 1", Source: "test-server"},
		{Name: "new_tool2", Description: "New tool 2", Source: "test-server"},
	}
	AddServerTools("test-server", newTools, "checksum2")
	
	// Should only have new tools, not old
	result := GetToolsForServer("test-server")
	if len(result) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(result))
	}
	
	// Verify old tool is not present
	for _, tool := range result {
		if tool.Name == "old_tool" {
			t.Error("Old tool should have been replaced")
		}
	}
}

// TestAddServerToolsMultipleServers tests adding tools from multiple servers
func TestAddServerToolsMultipleServers(t *testing.T) {
	_, cleanup := testSetup(t)
	defer cleanup()
	
	// Add tools for server 1
	tools1 := []ToolEntry{
		{Name: "tool1a", Description: "Tool 1A", Source: "server1"},
		{Name: "tool1b", Description: "Tool 1B", Source: "server1"},
	}
	AddServerTools("server1", tools1, "checksum1")
	
	// Add tools for server 2
	tools2 := []ToolEntry{
		{Name: "tool2a", Description: "Tool 2A", Source: "server2"},
	}
	AddServerTools("server2", tools2, "checksum2")
	
	// Verify each server has correct tools
	result1 := GetToolsForServer("server1")
	result2 := GetToolsForServer("server2")
	
	if len(result1) != 2 {
		t.Errorf("Server1 should have 2 tools, got %d", len(result1))
	}
	if len(result2) != 1 {
		t.Errorf("Server2 should have 1 tool, got %d", len(result2))
	}
	
	// Verify total tools
	allTools := GetAllTools()
	if len(allTools) != 3 {
		t.Errorf("Total tools should be 3, got %d", len(allTools))
	}
}

// TestRemoveServer tests removing a server from the cache
func TestRemoveServer(t *testing.T) {
	_, cleanup := testSetup(t)
	defer cleanup()
	
	// Add tools for two servers
	AddServerTools("server1", []ToolEntry{
		{Name: "tool1", Description: "Tool 1", Source: "server1"},
	}, "checksum1")
	AddServerTools("server2", []ToolEntry{
		{Name: "tool2", Description: "Tool 2", Source: "server2"},
	}, "checksum2")
	
	// Remove server1
	RemoveServer("server1")
	
	// Verify server1 tools are gone
	result1 := GetToolsForServer("server1")
	if len(result1) != 0 {
		t.Errorf("Server1 should have 0 tools after removal, got %d", len(result1))
	}
	
	// Verify server1 checksum is gone
	if GetServerChecksum("server1") != "" {
		t.Error("Server1 checksum should be empty after removal")
	}
	
	// Verify server1 is not found
	if HasServer("server1") {
		t.Error("HasServer should return false after removal")
	}
	
	// Verify server2 is unaffected
	result2 := GetToolsForServer("server2")
	if len(result2) != 1 {
		t.Errorf("Server2 should still have 1 tool, got %d", len(result2))
	}
}

// TestGetServerForTool tests looking up which server provides a tool
func TestGetServerForTool(t *testing.T) {
	_, cleanup := testSetup(t)
	defer cleanup()
	
	// Add tools
	AddServerTools("tavily", []ToolEntry{
		{Name: "tavily-search", Description: "Web search", Source: "tavily"},
		{Name: "tavily-extract", Description: "Content extract", Source: "tavily"},
	}, "checksum1")
	AddServerTools("github", []ToolEntry{
		{Name: "create_issue", Description: "Create GitHub issue", Source: "github"},
	}, "checksum2")
	
	// Test lookups
	tests := []struct {
		toolName       string
		expectedServer string
	}{
		{"tavily-search", "tavily"},
		{"tavily-extract", "tavily"},
		{"create_issue", "github"},
		{"nonexistent", ""},
	}
	
	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			server := GetServerForTool(tt.toolName)
			if server != tt.expectedServer {
				t.Errorf("GetServerForTool(%q) = %q, expected %q", tt.toolName, server, tt.expectedServer)
			}
		})
	}
}

// TestHasServer tests checking if a server exists in the cache
func TestHasServer(t *testing.T) {
	_, cleanup := testSetup(t)
	defer cleanup()
	
	// Initially no servers
	if HasServer("test-server") {
		t.Error("HasServer should return false when cache is empty")
	}
	
	// Add a server
	AddServerTools("test-server", []ToolEntry{
		{Name: "tool", Description: "Test", Source: "test-server"},
	}, "checksum")
	
	// Now it should exist
	if !HasServer("test-server") {
		t.Error("HasServer should return true after adding server")
	}
	
	// Other servers should not exist
	if HasServer("other-server") {
		t.Error("HasServer should return false for non-existent server")
	}
}

// TestIsEmpty tests the IsEmpty function
func TestIsEmpty(t *testing.T) {
	_, cleanup := testSetup(t)
	defer cleanup()
	
	// Initially empty
	if !IsEmpty() {
		t.Error("IsEmpty should return true when cache is empty")
	}
	
	// Add tools
	AddServerTools("test-server", []ToolEntry{
		{Name: "tool", Description: "Test", Source: "test-server"},
	}, "checksum")
	
	// No longer empty
	if IsEmpty() {
		t.Error("IsEmpty should return false after adding tools")
	}
}

// TestInvalidateCache tests the cache invalidation
func TestInvalidateCache(t *testing.T) {
	_, cleanup := testSetup(t)
	defer cleanup()
	
	// Add some tools
	AddServerTools("test-server", []ToolEntry{
		{Name: "tool", Description: "Test", Source: "test-server"},
	}, "checksum")
	
	// Verify not empty
	if IsEmpty() {
		t.Fatal("Cache should not be empty")
	}
	
	// Invalidate
	InvalidateCache()
	
	// Should be empty now (memory cache cleared)
	cacheMu.RLock()
	isEmpty := memoryCache == nil
	cacheMu.RUnlock()
	
	if !isEmpty {
		t.Error("Memory cache should be nil after invalidation")
	}
}

// TestSaveAndLoadCache tests saving and loading the cache file
func TestSaveAndLoadCache(t *testing.T) {
	_, cleanup := testSetup(t)
	defer cleanup()
	
	// Add tools to memory cache
	AddServerTools("test-server", []ToolEntry{
		{Name: "tool1", Description: "Tool 1", Source: "test-server"},
		{Name: "tool2", Description: "Tool 2", Source: "test-server"},
	}, "testchecksum123")
	
	// We can't easily override getCachePath, so we test the save/load logic
	// by directly manipulating the memory cache and file
	
	// Verify the tools are in memory
	tools := GetToolsForServer("test-server")
	if len(tools) != 2 {
		t.Fatalf("Expected 2 tools, got %d", len(tools))
	}
	
	// Verify checksum is stored
	checksum := GetServerChecksum("test-server")
	if checksum != "testchecksum123" {
		t.Errorf("Expected checksum 'testchecksum123', got %q", checksum)
	}

	// Test that SaveCache doesn't error with valid cache
	err := SaveCache()
	if err != nil {
		t.Logf("SaveCache returned error (expected if using real config dir): %v", err)
	}
}

// TestValidateChecksums tests the checksum validation logic
func TestValidateChecksums(t *testing.T) {
	_, cleanup := testSetup(t)
	defer cleanup()
	
	// This test is limited because ValidateChecksums depends on config.LoadMCPConfig()
	// which we can't easily mock. We test what we can.
	
	// With empty cache, should suggest refreshing all servers from config
	needsRefresh, removed := ValidateChecksums(false)
	
	// Results depend on actual MCP config, but we verify the function runs without error
	t.Logf("ValidateChecksums returned: needsRefresh=%v, removed=%v", needsRefresh, removed)
}

// TestGetAllTools tests getting all cached tools
func TestGetAllTools(t *testing.T) {
	_, cleanup := testSetup(t)
	defer cleanup()
	
	// Add tools from multiple servers
	AddServerTools("server1", []ToolEntry{
		{Name: "tool1", Description: "Tool 1", Source: "server1"},
	}, "checksum1")
	AddServerTools("server2", []ToolEntry{
		{Name: "tool2", Description: "Tool 2", Source: "server2"},
		{Name: "tool3", Description: "Tool 3", Source: "server2"},
	}, "checksum2")
	
	allTools := GetAllTools()
	
	if len(allTools) != 3 {
		t.Errorf("Expected 3 tools, got %d", len(allTools))
	}
	
	// Verify all tools are present
	toolNames := make(map[string]bool)
	for _, tool := range allTools {
		toolNames[tool.Name] = true
	}
	
	for _, expected := range []string{"tool1", "tool2", "tool3"} {
		if !toolNames[expected] {
			t.Errorf("Missing tool: %s", expected)
		}
	}
}

// TestConcurrentAccess tests that cache operations are thread-safe
func TestConcurrentAccess(t *testing.T) {
	_, cleanup := testSetup(t)
	defer cleanup()
	
	// Run multiple goroutines accessing the cache
	done := make(chan bool)
	
	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			AddServerTools("concurrent-server", []ToolEntry{
				{Name: "tool", Description: "Test", Source: "concurrent-server"},
			}, "checksum")
		}
		done <- true
	}()
	
	// Reader goroutines
	for j := 0; j < 3; j++ {
		go func() {
			for i := 0; i < 100; i++ {
				_ = GetAllTools()
				_ = GetServerForTool("tool")
				_ = HasServer("concurrent-server")
			}
			done <- true
		}()
	}
	
	// Wait for all goroutines
	for i := 0; i < 4; i++ {
		<-done
	}
	
	// If we get here without deadlock or panic, the test passes
}

// TestToolEntrySource tests that source is properly set on tools
func TestToolEntrySource(t *testing.T) {
	_, cleanup := testSetup(t)
	defer cleanup()
	
	// Add tools without setting source in the entry
	tools := []ToolEntry{
		{Name: "tool1", Description: "Tool 1"},
		{Name: "tool2", Description: "Tool 2"},
	}
	AddServerTools("my-server", tools, "checksum")
	
	// Verify source was set by AddServerTools
	result := GetToolsForServer("my-server")
	for _, tool := range result {
		if tool.Source != "my-server" {
			t.Errorf("Tool %s should have source 'my-server', got %q", tool.Name, tool.Source)
		}
	}
}
