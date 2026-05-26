package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gorilla/mux"
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

func TestMCPStatusHandler(t *testing.T) {
	cleanup := testSetup(t)
	defer cleanup()

	// Without platform context (no MCP store), handler returns empty servers list
	req, err := http.NewRequest("GET", "/api/mcp/status", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(MCPStatusHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var response struct {
		Servers []cache.ServerStatus `json:"servers"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(response.Servers) != 0 {
		t.Errorf("expected 0 servers without platform context, got %d", len(response.Servers))
	}
}

func TestListServerToolsHandler_Error(t *testing.T) {
	cleanup := testSetup(t)
	defer cleanup()

	// Invalidate cache to ensure it tries to load
	cache.InvalidateCache()

	// Create a request with a non-existent server
	req, err := http.NewRequest("GET", "/api/mcp/nonexistent/tools", nil)
	if err != nil {
		t.Fatal(err)
	}
	req = mux.SetURLVars(req, map[string]string{"serverName": "nonexistent"})

	// Record the response
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(ListServerToolsHandler)
	handler.ServeHTTP(rr, req)

	// Verify results
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var response ListServerToolsResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error == "" {
		t.Error("expected error message for non-existent server, got empty")
	}
}
