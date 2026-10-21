package api

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/config"
)

func TestSQLitePlatformInitHandler_SanitizesStorageFailures(t *testing.T) {
	for _, tc := range []struct {
		name, mode, wantError string
		prepare               func(*testing.T, string, string)
	}{
		{
			name: "config_load", mode: config.AuthModeNone, wantError: "Failed to load config",
			prepare: func(t *testing.T, configDir, _ string) {
				if err := os.MkdirAll(filepath.Join(configDir, "config.yaml"), 0700); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "config_save_none", mode: config.AuthModeNone, wantError: "Platform initialized but failed to save config",
			prepare: blockSQLiteSetupConfigSave,
		},
		{
			name: "config_save_builtin", mode: config.AuthModeBuiltin, wantError: "Platform initialized but failed to save config",
			prepare: blockSQLiteSetupConfigSave,
		},
		{
			name: "bootstrap", mode: config.AuthModeNone, wantError: "Failed to initialize SQLite database",
			prepare: func(t *testing.T, _, dataDir string) {
				if err := os.MkdirAll(filepath.Join(dataDir, "platform.db"), 0700); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "local_workspace", mode: config.AuthModeNone, wantError: "Failed to create local workspace",
			prepare: blockSQLiteSetupOrgDirectory,
		},
		{
			name: "org_provision", mode: config.AuthModeBuiltin, wantError: "Failed to provision org",
			prepare: blockSQLiteSetupOrgDirectory,
		},
		{
			name: "team_provision", mode: config.AuthModeBuiltin, wantError: "Failed to provision team",
			prepare: func(t *testing.T, _, dataDir string) {
				if err := os.MkdirAll(filepath.Join(dataDir, "orgs", "fresh", "teams", "general.db"), 0700); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configDir, dataDir := sqliteFreshSetupPaths(t)
			tc.prepare(t, configDir, dataDir)
			w := sqliteFreshSetupRequest(t, dataDir, tc.mode, "unit-bootstrap-secret")
			var response PlatformInitResponse
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusInternalServerError || response.Success || response.RestartRequired || response.Error != tc.wantError {
				t.Fatalf("expected bounded failure %q, got HTTP %d: %+v", tc.wantError, w.Code, response)
			}
		})
	}
}

func TestSQLitePlatformInitHandler_ErrorResponsesAreStatic(t *testing.T) {
	// Cover hard-to-inject branches (including team membership) without adding
	// mutable production hooks to the setup path. Behavioral cases above prove
	// that real filesystem/database failures also reach sanitized responses.
	f, err := parser.ParseFile(token.NewFileSet(), "platform_setup_handlers.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	errorResponses := 0
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "sqlitePlatformInitHandler" {
			continue
		}
		found = true
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "Error" {
				return true
			}
			errorResponses++
			if value, ok := kv.Value.(*ast.BasicLit); !ok || value.Kind != token.STRING {
				t.Error("SQLite setup errors must be fixed messages, not raw error or request details")
			}
			return true
		})
	}
	if !found {
		t.Fatal("SQLite setup handler not found")
	}
	// Keep coverage of the current error branches even if the handler is
	// extracted again. Additional responses must also pass the static check.
	const minErrorResponses = 24
	if errorResponses < minErrorResponses {
		t.Fatalf("examined %d SQLite setup error responses, want at least %d; check the handler inspection target", errorResponses, minErrorResponses)
	}
}

type sqliteSetupErrorReader struct{}

func (sqliteSetupErrorReader) Read([]byte) (int, error) {
	return 0, errors.New("synthetic-private-body-reader-detail")
}

func TestSQLitePlatformInitHandler_SanitizesRequestFailures(t *testing.T) {
	for _, tc := range []struct {
		name, wantError string
		body            io.Reader
	}{
		{name: "body_read", wantError: "Invalid request body", body: sqliteSetupErrorReader{}},
		{name: "auth_mode", wantError: "Unsupported auth mode", body: strings.NewReader(`{"auth_mode":"` + strings.Repeat("synthetic-private-detail", 100) + `"}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sqliteFreshSetupPaths(t)
			r := httptest.NewRequest(http.MethodPost, "/api/platform/init/sqlite", tc.body)
			r.Header.Set("X-Astonish-Bootstrap-Token", "unit-bootstrap-secret")
			w := httptest.NewRecorder()
			SQLitePlatformInitHandler(w, r)
			var response PlatformInitResponse
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusBadRequest || response.Error != tc.wantError {
				t.Fatalf("expected bounded validation failure: HTTP %d: %+v", w.Code, response)
			}
		})
	}
}

func blockSQLiteSetupConfigSave(t *testing.T, configDir, _ string) {
	t.Helper()
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	// A dangling link reads as absent but cannot be written when its parent
	// target directory is absent. Both paths are confined to the test directory.
	if err := os.Symlink(filepath.Join(configDir, "missing", "config.yaml"), filepath.Join(configDir, "config.yaml")); err != nil {
		t.Fatal(err)
	}
}

func blockSQLiteSetupOrgDirectory(t *testing.T, _, dataDir string) {
	t.Helper()
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "orgs"), []byte("synthetic filesystem obstruction"), 0600); err != nil {
		t.Fatal(err)
	}
}
