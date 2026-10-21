package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/SAP/astonish/pkg/cache"
	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/store"
	"github.com/gorilla/mux"
)

func mcpBoolPtr(b bool) *bool { return &b }

// TestGetCachedToolsForRequest_IncludesFileMCPServers verifies that the
// advertised tool list (the one the model sees via the system prompt and
// GET /api/tools) includes config-file (mcp_config.json) MCP servers, whose
// tools live in the shared on-disk tools cache rather than a per-tenant DB
// column. This is the base layer parity that GetMCPServersHandler,
// loadMCPConfigForRequest, and loadMCPConfig already have.
func TestGetCachedToolsForRequest_IncludesFileMCPServers(t *testing.T) {
	writeMCPConfigFile(t, `{
		"mcpServers": {
			"custom-a": {"command": "npx", "args": ["-y", "custom-a-mcp"], "transport": "stdio", "enabled": true}
		}
	}`)

	cache.SetCacheDir(t.TempDir())
	t.Cleanup(func() { cache.SetCacheDir("") })
	cache.AddServerTools("custom-a", []cache.ToolEntry{
		{Name: "custom_a_tool", Description: "Tool from config-file server", Source: "custom-a"},
	}, "checksum-a")

	svc := &store.Services{Mode: store.ModePlatform, TeamMCPServers: newMemMCPStore()}
	req := httptest.NewRequest("GET", "/api/tools", nil)
	req = req.WithContext(store.WithServices(req.Context(), svc))

	tools := GetCachedToolsForRequest(req)
	if !hasNamedTool(tools, "custom_a_tool", "custom-a") {
		t.Fatalf("expected config-file server tool 'custom_a_tool' (source custom-a) in advertised tools, got: %v", namedTools(tools))
	}
}

// TestGetCachedToolsForRequest_DBServerOverridesFileServer verifies a same-named
// DB (team) server overrides the config-file base entry by name.
func TestGetCachedToolsForRequest_DBServerOverridesFileServer(t *testing.T) {
	writeMCPConfigFile(t, `{
		"mcpServers": {
			"custom-a": {"command": "npx", "args": ["-y", "custom-a-mcp"], "transport": "stdio", "enabled": true}
		}
	}`)

	cache.SetCacheDir(t.TempDir())
	t.Cleanup(func() { cache.SetCacheDir("") })
	cache.AddServerTools("custom-a", []cache.ToolEntry{
		{Name: "file_tool", Description: "from file", Source: "custom-a"},
	}, "checksum-a")

	dbTools, _ := json.Marshal([]map[string]any{{"name": "db_tool", "description": "from db"}})
	svc := &store.Services{
		Mode: store.ModePlatform,
		TeamMCPServers: newMemMCPStore(store.MCPServer{
			Name: "custom-a", Enabled: mcpBoolPtr(true), CachedTools: dbTools,
		}),
	}
	req := httptest.NewRequest("GET", "/api/tools", nil)
	req = req.WithContext(store.WithServices(req.Context(), svc))

	tools := GetCachedToolsForRequest(req)
	if !hasNamedTool(tools, "db_tool", "custom-a") {
		t.Fatalf("expected DB server tool 'db_tool' to override file server, got: %v", namedTools(tools))
	}
	if hasNamedTool(tools, "file_tool", "custom-a") {
		t.Fatalf("file-server tool 'file_tool' should have been overridden by same-named DB server, got: %v", namedTools(tools))
	}
}

// TestGetCachedToolsForRequest_DisabledFileServerExcluded verifies a disabled
// config-file server does not contribute tools to the advertised list.
func TestGetCachedToolsForRequest_DisabledFileServerExcluded(t *testing.T) {
	writeMCPConfigFile(t, `{
		"mcpServers": {
			"custom-a": {"command": "npx", "transport": "stdio", "enabled": false}
		}
	}`)

	cache.SetCacheDir(t.TempDir())
	t.Cleanup(func() { cache.SetCacheDir("") })
	cache.AddServerTools("custom-a", []cache.ToolEntry{
		{Name: "custom_a_tool", Description: "should not appear", Source: "custom-a"},
	}, "checksum-a")

	svc := &store.Services{Mode: store.ModePlatform, TeamMCPServers: newMemMCPStore()}
	req := httptest.NewRequest("GET", "/api/tools", nil)
	req = req.WithContext(store.WithServices(req.Context(), svc))

	tools := GetCachedToolsForRequest(req)
	if hasNamedTool(tools, "custom_a_tool", "custom-a") {
		t.Fatalf("disabled config-file server tool should be excluded, got: %v", namedTools(tools))
	}
}

// fileRefreshStore records the exact selected-store lookup while retaining the
// normal nil,nil miss semantics of memMCPStore.
type fileRefreshStore struct {
	*memMCPStore
	getNames []string
	getErr   error
	onGet    func()
}

func (s *fileRefreshStore) Get(ctx context.Context, name string) (*store.MCPServer, error) {
	s.getNames = append(s.getNames, name)
	if s.onGet != nil {
		s.onGet()
	}
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.memMCPStore.Get(ctx, name)
}

// This matrix proves handler authorization/selection, not live discovery. Both
// async discovery paths are deduplicated before starting a goroutine, so even a
// forbidden fallback regression cannot launch a process or contact a server.
func TestRefreshMCPServerHandler_FileFallbackScopeMatrix(t *testing.T) {
	const fileName = "file-refresh-scope-matrix"
	const absentName = "file-refresh-not-configured"
	for _, name := range []string{fileName, absentName} {
		if _, loaded := discoveryInFlight.LoadOrStore(name, struct{}{}); loaded {
			t.Fatalf("unexpected discovery fixture collision for %q", name)
		}
		t.Cleanup(func() { discoveryInFlight.Delete(name) })
	}

	for _, tt := range []struct {
		name            string
		scope           mcpWriteScope
		serverName      string
		platformAdmin   bool
		storeError      bool
		missingPlatform bool
		dbRow           bool
		wantStatus      int
	}{
		{name: "explicit_platform_nil_row_uses_actual_file", scope: mcpScopePlatform, platformAdmin: true, wantStatus: http.StatusOK},
		{name: "default_nil_row_cannot_use_actual_file", wantStatus: http.StatusNotFound},
		{name: "team_nil_row_cannot_use_actual_file", scope: mcpScopeTeam, wantStatus: http.StatusNotFound},
		{name: "org_nil_row_cannot_use_actual_file", scope: mcpScopeOrg, wantStatus: http.StatusNotFound},
		{name: "platform_store_error_cannot_use_actual_file", scope: mcpScopePlatform, platformAdmin: true, storeError: true, wantStatus: http.StatusNotFound},
		{name: "missing_platform_store_cannot_use_actual_file", scope: mcpScopePlatform, platformAdmin: true, missingPlatform: true, wantStatus: http.StatusServiceUnavailable},
		{name: "platform_requires_actual_file_name", scope: mcpScopePlatform, platformAdmin: true, serverName: absentName, wantStatus: http.StatusNotFound},
		{name: "org_admin_cannot_refresh_platform_file", scope: mcpScopePlatform, wantStatus: http.StatusForbidden},
		{name: "platform_same_name_db_row_precedes_file", scope: mcpScopePlatform, platformAdmin: true, dbRow: true, wantStatus: http.StatusOK},
		{name: "default_same_name_db_row_precedes_file", dbRow: true, wantStatus: http.StatusOK},
		{name: "team_same_name_db_row_precedes_file", scope: mcpScopeTeam, dbRow: true, wantStatus: http.StatusOK},
		{name: "org_same_name_db_row_precedes_file", scope: mcpScopeOrg, dbRow: true, wantStatus: http.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", root)
			t.Setenv("XDG_CONFIG_HOME", root)
			t.Setenv("SUDO_USER", "")
			dir, err := config.GetConfigDir()
			if err != nil || dir != filepath.Join(root, "astonish") {
				t.Fatalf("config directory must be test-owned: %q, error %v", dir, err)
			}
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(dir, "mcp_config.json")
			if err := os.WriteFile(configPath, []byte(`{"mcpServers":{"file-refresh-scope-matrix":{"command":"must-not-run-file-mcp","transport":"stdio","enabled":true}}}`), 0600); err != nil {
				t.Fatal(err)
			}
			if _, ok := config.FileMCPServers()[fileName]; !ok {
				t.Fatal("fixture must contain the actual requested file server")
			}
			cacheDir := t.TempDir()
			cache.SetCacheDir(cacheDir)
			t.Cleanup(func() { cache.SetCacheDir("") })
			cache.AddServerTools(fileName, []cache.ToolEntry{{Name: "existing_file_tool", Source: fileName}}, "original-file-checksum")
			if err := cache.SaveCache(); err != nil {
				t.Fatal(err)
			}
			snapshotCache := func() ([]byte, []byte) {
				t.Helper()
				cached, err := cache.LoadCache()
				if err != nil {
					t.Fatal(err)
				}
				memory, err := json.Marshal(cached)
				if err != nil {
					t.Fatal(err)
				}
				disk, err := os.ReadFile(filepath.Join(cacheDir, "tools_cache.json"))
				if err != nil {
					t.Fatal(err)
				}
				return memory, disk
			}
			beforeMemory, beforeDisk := snapshotCache()

			team := &fileRefreshStore{memMCPStore: newMemMCPStore()}
			org := &fileRefreshStore{memMCPStore: newMemMCPStore()}
			platform := &fileRefreshStore{memMCPStore: newMemMCPStore()}
			svc := &store.Services{Mode: store.ModePlatform, TeamMCPServers: team, MCPServers: org, PlatformMCPServers: platform}
			selected := team
			switch tt.scope {
			case mcpScopeOrg:
				selected = org
			case mcpScopePlatform:
				selected = platform
			}
			var expectedDB []byte
			if tt.dbRow {
				selected.servers[fileName] = store.MCPServer{
					Name: fileName, Command: "must-not-run-db-mcp", Transport: "stdio",
					CachedTools: json.RawMessage(`[{"name":"existing_db_tool"}]`),
				}
				var err error
				expectedDB, err = json.Marshal(selected.servers)
				if err != nil {
					t.Fatal(err)
				}
				// Start with a real same-name file entry, then remove it at the
				// DB lookup boundary. A 200 now cannot be a false-positive file
				// fallback: the returned DB row must be sufficient on its own.
				selected.onGet = func() {
					if err := os.Remove(configPath); err != nil {
						t.Fatal(err)
					}
				}
			}
			if tt.storeError {
				selected.getErr = errors.New("file refresh lookup failed")
			}
			if tt.missingPlatform {
				svc.PlatformMCPServers = nil
			}
			name := tt.serverName
			if name == "" {
				name = fileName
			}
			user := &PlatformUser{ID: "file-refresh-admin", Role: "admin"}
			if tt.platformAdmin {
				user.PlatformRole = "superadmin"
			}
			url := "/api/mcp/" + name + "/refresh"
			if tt.scope != "" {
				url += "?scope=" + string(tt.scope)
			}
			req := httptest.NewRequest(http.MethodPost, url, nil)
			req = mux.SetURLVars(req, map[string]string{"name": name})
			req = req.WithContext(WithPlatformUser(store.WithServices(req.Context(), svc), user))
			response := httptest.NewRecorder()
			RefreshMCPServerHandler(response, req)
			if response.Code != tt.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", response.Code, tt.wantStatus, response.Body.String())
			}
			if tt.wantStatus == http.StatusOK {
				var body struct {
					Success bool `json:"success"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || !body.Success {
					t.Fatalf("expected success response, got %s (decode error %v)", response.Body.String(), err)
				}
			}
			for _, s := range []*fileRefreshStore{team, org, platform} {
				var wantNames []string
				if s == selected && !tt.missingPlatform && tt.wantStatus != http.StatusForbidden {
					wantNames = []string{name}
				}
				if !reflect.DeepEqual(s.getNames, wantNames) {
					t.Errorf("store lookup names=%v want=%v", s.getNames, wantNames)
				}
				if tt.dbRow && s == selected {
					gotDB, err := json.Marshal(s.servers)
					if err != nil || !bytes.Equal(gotDB, expectedDB) {
						t.Errorf("refresh changed the selected DB row: %s (error %v)", gotDB, err)
					}
				} else if len(s.servers) != 0 {
					t.Error("refresh unexpectedly changed DB rows")
				}
			}
			afterMemory, afterDisk := snapshotCache()
			if !bytes.Equal(beforeMemory, afterMemory) || !bytes.Equal(beforeDisk, afterDisk) {
				t.Error("selection-only refresh changed shared cache memory or disk")
			}
			if _, ok := discoveryInFlight.Load(name); !ok {
				t.Error("refresh removed the preseeded discovery guard")
			}
		})
	}
}

func hasNamedTool(tools []ToolInfo, name, source string) bool {
	for _, ti := range tools {
		if ti.Name == name && ti.Source == source {
			return true
		}
	}
	return false
}

func namedTools(tools []ToolInfo) []string {
	out := make([]string, 0, len(tools))
	for _, ti := range tools {
		out = append(out, ti.Name+"@"+ti.Source)
	}
	return out
}
