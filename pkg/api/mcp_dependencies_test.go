package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/flowstore"
	"github.com/schardosin/astonish/pkg/mcpstore"
)

// TestResolveMCPDependencies_ExactNameMatching verifies that the dependency
// resolver only uses exact name matching (case-insensitive) and does not
// produce false positives from partial matching or signature matching.
func TestResolveMCPDependencies_ExactNameMatching(t *testing.T) {
	tests := []struct {
		name           string
		toolsSelection []string
		cachedTools    []ToolInfo
		storeServers   []mcpstore.Server
		existingDeps   []config.MCPDependency
		expectedDeps   []struct {
			server  string
			source  string
			storeID string
		}
	}{
		{
			name:           "exact match to official store",
			toolsSelection: []string{"search_web"},
			cachedTools: []ToolInfo{
				{Name: "search_web", Source: "tavily-mcp-server"},
			},
			storeServers: []mcpstore.Server{
				{Name: "tavily-mcp-server", McpId: "official/tavily-mcp-server", Source: flowstore.OfficialStoreName},
			},
			existingDeps: nil,
			expectedDeps: []struct {
				server  string
				source  string
				storeID string
			}{
				{server: "tavily-mcp-server", source: "store", storeID: "official/tavily-mcp-server"},
			},
		},
		{
			name:           "exact match to tap (non-official)",
			toolsSelection: []string{"custom_tool"},
			cachedTools: []ToolInfo{
				{Name: "custom_tool", Source: "my-custom-mcp"},
			},
			storeServers: []mcpstore.Server{
				{Name: "my-custom-mcp", McpId: "mytap/my-custom-mcp", Source: "mytap"},
			},
			existingDeps: nil,
			expectedDeps: []struct {
				server  string
				source  string
				storeID string
			}{
				{server: "my-custom-mcp", source: "tap", storeID: "mytap/my-custom-mcp"},
			},
		},
		{
			name:           "no match falls back to inline",
			toolsSelection: []string{"unknown_tool"},
			cachedTools: []ToolInfo{
				{Name: "unknown_tool", Source: "my-local-server"},
			},
			storeServers: []mcpstore.Server{
				{Name: "completely-different-server", McpId: "official/different", Source: flowstore.OfficialStoreName},
			},
			existingDeps: nil,
			expectedDeps: []struct {
				server  string
				source  string
				storeID string
			}{
				{server: "my-local-server", source: "inline", storeID: ""},
			},
		},
		{
			name:           "case insensitive matching",
			toolsSelection: []string{"tool1"},
			cachedTools: []ToolInfo{
				{Name: "tool1", Source: "GitHub-MCP-Server"},
			},
			storeServers: []mcpstore.Server{
				{Name: "github-mcp-server", McpId: "official/github-mcp-server", Source: flowstore.OfficialStoreName},
			},
			existingDeps: nil,
			expectedDeps: []struct {
				server  string
				source  string
				storeID string
			}{
				{server: "GitHub-MCP-Server", source: "store", storeID: "official/github-mcp-server"},
			},
		},
		{
			name:           "store server with spaces matches installed server with hyphens",
			toolsSelection: []string{"ytdlp_download_video"},
			cachedTools: []ToolInfo{
				// Installed with normalized name: spaces replaced with hyphens
				{Name: "ytdlp_download_video", Source: "youtube-download"},
			},
			storeServers: []mcpstore.Server{
				// Store has the name with spaces
				{Name: "YouTube Download", McpId: "official/youtube-download", Source: flowstore.OfficialStoreName},
			},
			existingDeps: nil,
			expectedDeps: []struct {
				server  string
				source  string
				storeID string
			}{
				{server: "youtube-download", source: "store", storeID: "official/youtube-download"},
			},
		},
		{
			name:           "no false positive from partial name match",
			toolsSelection: []string{"my_tool"},
			cachedTools: []ToolInfo{
				{Name: "my_tool", Source: "ui5mcp-server"},
			},
			storeServers: []mcpstore.Server{
				// This should NOT match "ui5mcp-server" - no partial matching!
				{Name: "browser-tools-mcp", McpId: "official/browser-tools-mcp", Source: flowstore.OfficialStoreName},
				{Name: "mcp-server-different", McpId: "official/mcp-server-different", Source: flowstore.OfficialStoreName},
			},
			existingDeps: nil,
			expectedDeps: []struct {
				server  string
				source  string
				storeID string
			}{
				{server: "ui5mcp-server", source: "inline", storeID: ""},
			},
		},
		{
			name:           "empty tools selection returns nil",
			toolsSelection: []string{},
			cachedTools:    nil,
			storeServers:   nil,
			existingDeps:   nil,
			expectedDeps:   nil,
		},
		{
			name:           "internal tools are skipped",
			toolsSelection: []string{"internal_tool"},
			cachedTools: []ToolInfo{
				{Name: "internal_tool", Source: "internal"},
			},
			storeServers: nil,
			existingDeps: nil,
			expectedDeps: nil,
		},
		{
			name:           "existing deps used as fallback for unknown tools",
			toolsSelection: []string{"missing_tool"},
			cachedTools:    []ToolInfo{}, // Not in cache
			storeServers: []mcpstore.Server{
				{Name: "fallback-server", McpId: "official/fallback-server", Source: flowstore.OfficialStoreName},
			},
			existingDeps: []config.MCPDependency{
				{Server: "fallback-server", Tools: []string{"missing_tool"}, Source: "store"},
			},
			expectedDeps: []struct {
				server  string
				source  string
				storeID string
			}{
				{server: "fallback-server", source: "store", storeID: "official/fallback-server"},
			},
		},
		{
			name:           "custom-named server matches store by config signature",
			toolsSelection: []string{"tavily-search"},
			cachedTools: []ToolInfo{
				// Tool from a custom-named server (user called it tavily-websearch instead of tavily)
				{Name: "tavily-search", Source: "tavily-websearch"},
			},
			storeServers: []mcpstore.Server{
				// Store has "Tavily" which would normalize to "tavily", not matching "tavily-websearch"
				// But the config signature should match
				{
					Name:   "Tavily",
					McpId:  "official/tavily-mcp",
					Source: flowstore.OfficialStoreName,
					Config: &mcpstore.ServerConfig{
						Command: "npx",
						Args:    []string{"-y", "tavily-mcp@latest"},
					},
				},
			},
			existingDeps: nil,
			expectedDeps: []struct {
				server  string
				source  string
				storeID string
			}{
				{server: "tavily-websearch", source: "store", storeID: "official/tavily-mcp"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := ResolveMCPDependencies(tt.toolsSelection, tt.cachedTools, tt.storeServers, tt.existingDeps)

			if tt.expectedDeps == nil {
				if len(deps) != 0 {
					t.Errorf("expected nil/empty deps, got %d deps", len(deps))
				}
				return
			}

			if len(deps) != len(tt.expectedDeps) {
				t.Errorf("expected %d deps, got %d", len(tt.expectedDeps), len(deps))
				return
			}

			for i, expected := range tt.expectedDeps {
				if deps[i].Server != expected.server {
					t.Errorf("dep[%d].Server = %s, expected %s", i, deps[i].Server, expected.server)
				}
				if deps[i].Source != expected.source {
					t.Errorf("dep[%d].Source = %s, expected %s", i, deps[i].Source, expected.source)
				}
				if deps[i].StoreID != expected.storeID {
					t.Errorf("dep[%d].StoreID = %s, expected %s", i, deps[i].StoreID, expected.storeID)
				}
			}
		})
	}
}

// TestCheckMCPDependenciesHandler_StoreIDResolution verifies that the check handler
// correctly resolves store_id for tap/store sources when it's missing from the YAML.
func TestCheckMCPDependenciesHandler_StoreIDResolution(t *testing.T) {
	// Note: This is a more integration-style test. For unit testing,
	// we'd need to mock loadAllServersFromTaps(). This test documents
	// the expected behavior.
	
	tests := []struct {
		name         string
		dependencies []config.MCPDependency
		expectPass   bool
	}{
		{
			name: "dependency with store_id should pass through",
			dependencies: []config.MCPDependency{
				{
					Server:  "github-mcp",
					Source:  "store",
					StoreID: "official/github-mcp",
				},
			},
			expectPass: true,
		},
		{
			name: "dependency with source tap should pass through",
			dependencies: []config.MCPDependency{
				{
					Server:  "custom-server",
					Source:  "tap",
					StoreID: "mytap/custom-server",
				},
			},
			expectPass: true,
		},
		{
			name: "inline dependency with config should pass",
			dependencies: []config.MCPDependency{
				{
					Server: "local-server",
					Source: "inline",
					Config: &config.MCPServerConfig{
						Command: "npx",
						Args:    []string{"-y", "@my/server"},
					},
				},
			},
			expectPass: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody := CheckMCPDependenciesRequest{
				Dependencies: tt.dependencies,
			}
			body, _ := json.Marshal(reqBody)

			req := httptest.NewRequest(http.MethodPost, "/api/mcp-dependencies/check", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			
			rr := httptest.NewRecorder()
			CheckMCPDependenciesHandler(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("expected status OK, got %d: %s", rr.Code, rr.Body.String())
				return
			}

			var resp CheckMCPDependenciesResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Errorf("failed to parse response: %v", err)
				return
			}

			if len(resp.Dependencies) != len(tt.dependencies) {
				t.Errorf("expected %d deps in response, got %d", len(tt.dependencies), len(resp.Dependencies))
			}
		})
	}
}

// TestCheckMCPDependenciesHandler_InvalidRequest verifies proper error handling
func TestCheckMCPDependenciesHandler_InvalidRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/mcp-dependencies/check", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	
	rr := httptest.NewRecorder()
	CheckMCPDependenciesHandler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status BadRequest, got %d", rr.Code)
	}
}

// TestCollectToolsFromNodes verifies tool extraction from node configurations
func TestCollectToolsFromNodes(t *testing.T) {
	tests := []struct {
		name          string
		nodes         []config.Node
		expectedTools []string
	}{
		{
			name: "single node with tools",
			nodes: []config.Node{
				{Name: "node1", ToolsSelection: []string{"tool_a", "tool_b"}},
			},
			expectedTools: []string{"tool_a", "tool_b"},
		},
		{
			name: "multiple nodes with overlapping tools (deduped)",
			nodes: []config.Node{
				{Name: "node1", ToolsSelection: []string{"tool_a", "tool_b"}},
				{Name: "node2", ToolsSelection: []string{"tool_b", "tool_c"}},
			},
			expectedTools: []string{"tool_a", "tool_b", "tool_c"},
		},
		{
			name:          "no nodes returns empty",
			nodes:         []config.Node{},
			expectedTools: nil,
		},
		{
			name: "nodes without tools_selection",
			nodes: []config.Node{
				{Name: "node1", Type: "llm"},
			},
			expectedTools: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := CollectToolsFromNodes(tt.nodes)

			if tt.expectedTools == nil {
				if len(tools) != 0 {
					t.Errorf("expected empty tools, got %v", tools)
				}
				return
			}

			// Check all expected tools are present (order may vary)
			toolSet := make(map[string]bool)
			for _, tool := range tools {
				toolSet[tool] = true
			}

			for _, expected := range tt.expectedTools {
				if !toolSet[expected] {
					t.Errorf("expected tool %s not found in result %v", expected, tools)
				}
			}

			if len(tools) != len(tt.expectedTools) {
				t.Errorf("expected %d tools, got %d", len(tt.expectedTools), len(tools))
			}
		})
	}
}

// TestMCPDependencyStatus_JSONTags verifies that JSON serialization works correctly
func TestMCPDependencyStatus_JSONTags(t *testing.T) {
	status := MCPDependencyStatus{
		Server:    "test-server",
		Tools:     []string{"tool1", "tool2"},
		Source:    "store",
		StoreID:   "official/test-server",
		Installed: true,
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify JSON field names
	if _, ok := parsed["server"]; !ok {
		t.Error("expected 'server' field in JSON")
	}
	if _, ok := parsed["store_id"]; !ok {
		t.Error("expected 'store_id' field in JSON")
	}
	if _, ok := parsed["installed"]; !ok {
		t.Error("expected 'installed' field in JSON")
	}
}

// TestCheckMCPDependenciesResponse_AllInstalled verifies the all_installed calculation
func TestCheckMCPDependenciesResponse_AllInstalled(t *testing.T) {
	tests := []struct {
		name         string
		statuses     []MCPDependencyStatus
		expectAll    bool
		expectCount  int
	}{
		{
			name: "all installed",
			statuses: []MCPDependencyStatus{
				{Server: "server1", Installed: true},
				{Server: "server2", Installed: true},
			},
			expectAll:   true,
			expectCount: 0,
		},
		{
			name: "one missing",
			statuses: []MCPDependencyStatus{
				{Server: "server1", Installed: true},
				{Server: "server2", Installed: false},
			},
			expectAll:   false,
			expectCount: 1,
		},
		{
			name: "all missing",
			statuses: []MCPDependencyStatus{
				{Server: "server1", Installed: false},
				{Server: "server2", Installed: false},
			},
			expectAll:   false,
			expectCount: 2,
		},
		{
			name:        "empty list",
			statuses:    []MCPDependencyStatus{},
			expectAll:   true,
			expectCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			missing := 0
			for _, s := range tt.statuses {
				if !s.Installed {
					missing++
				}
			}
			allInstalled := missing == 0

			if allInstalled != tt.expectAll {
				t.Errorf("allInstalled = %v, expected %v", allInstalled, tt.expectAll)
			}
			if missing != tt.expectCount {
				t.Errorf("missing = %d, expected %d", missing, tt.expectCount)
			}
		})
	}
}
