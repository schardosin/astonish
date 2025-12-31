package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/cache"
)

func TestMCPStatusHandler(t *testing.T) {
	cleanup := testSetup(t)
	defer cleanup()

	// 1. Set up some data in cache
	SetServerStatus("test-server-1", cache.ServerStatus{
		Name:      "test-server-1",
		Status:    "healthy",
		ToolCount: 10,
		LastCheck: "2024-12-31T12:00:00Z",
	})
	SetServerStatus("test-server-2", cache.ServerStatus{
		Name:      "test-server-2",
		Status:    "error",
		Error:     "Bad config",
		ToolCount: 0,
		LastCheck: "2024-12-31T12:00:00Z",
	})

	// 2. Create a request
	req, err := http.NewRequest("GET", "/api/mcp/status", nil)
	if err != nil {
		t.Fatal(err)
	}

	// 3. Record the response
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(MCPStatusHandler)
	handler.ServeHTTP(rr, req)

	// 4. Verify results
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var response struct {
		Servers []cache.ServerStatus `json:"servers"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(response.Servers) != 2 {
		t.Errorf("expected 2 servers, got %d", len(response.Servers))
	}

	found1 := false
	found2 := false
	for _, s := range response.Servers {
		if s.Name == "test-server-1" && s.Status == "healthy" {
			found1 = true
		}
		if s.Name == "test-server-2" && s.Status == "error" && s.Error == "Bad config" {
			found2 = true
		}
	}

	if !found1 || !found2 {
		t.Error("servers not found in response or have wrong status")
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
