package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/schardosin/astonish/pkg/config"
)

// --- cleanPGError Tests ---

func TestCleanPGError_RemovesConnectionString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "connection string at end",
			input: "failed to connect: postgres://user:pass@host:5432/db",
			want:  "failed to connect: [redacted]",
		},
		{
			name:  "connection string in middle",
			input: "error connecting to postgres://admin:secret@db.example.com:5432/astonish_platform more text",
			want:  "error connecting to [redacted] more text",
		},
		{
			name:  "no connection string",
			input: "some ordinary error message",
			want:  "some ordinary error message",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "connection string with special chars",
			input: "failed: postgres://u:p%40ss@host:5432/db?sslmode=require",
			want:  "failed: [redacted]",
		},
		{
			name:  "connection string with tab after",
			input: "err postgres://x:y@h:5432/d\tnext",
			want:  "err [redacted]\tnext",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanPGError(tt.input)
			if got != tt.want {
				t.Errorf("cleanPGError(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- DeploymentModeHandler Tests ---

// setupTestConfigDir creates a temp XDG_CONFIG_HOME with an optional config.yaml
// and returns a cleanup function.
func setupTestConfigDir(t *testing.T, cfg *config.AppConfig) func() {
	t.Helper()
	tmpDir := t.TempDir()
	astonishDir := filepath.Join(tmpDir, "astonish")
	if err := os.MkdirAll(astonishDir, 0755); err != nil {
		t.Fatalf("failed to create test config dir: %v", err)
	}

	origXDG := os.Getenv("XDG_CONFIG_HOME")

	os.Setenv("XDG_CONFIG_HOME", tmpDir)

	if cfg != nil {
		data, err := yaml.Marshal(cfg)
		if err != nil {
			t.Fatalf("failed to marshal config: %v", err)
		}
		if err := os.WriteFile(filepath.Join(astonishDir, "config.yaml"), data, 0644); err != nil {
			t.Fatalf("failed to write config: %v", err)
		}
	}

	return func() {
		if origXDG != "" {
			os.Setenv("XDG_CONFIG_HOME", origXDG)
		} else {
			os.Unsetenv("XDG_CONFIG_HOME")
		}
	}
}

func TestDeploymentModeHandler_PersonalMode(t *testing.T) {
	cleanup := setupTestConfigDir(t, &config.AppConfig{
		Storage: config.StorageConfig{Backend: ""},
	})
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/platform/mode", nil)
	w := httptest.NewRecorder()
	DeploymentModeHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode error: %v", err)
	}

	if resp["mode"] != "personal" {
		t.Errorf("mode = %q, want %q", resp["mode"], "personal")
	}
}

func TestDeploymentModeHandler_PlatformMode(t *testing.T) {
	cleanup := setupTestConfigDir(t, &config.AppConfig{
		Storage: config.StorageConfig{
			Backend: "postgres",
			Postgres: config.PostgresConfig{
				PlatformDSN: "postgres://u:p@h:5432/db",
			},
		},
	})
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/platform/mode", nil)
	w := httptest.NewRecorder()
	DeploymentModeHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode error: %v", err)
	}

	if resp["mode"] != "platform" {
		t.Errorf("mode = %q, want %q", resp["mode"], "platform")
	}
	if resp["configured"] != true {
		t.Errorf("configured = %v, want true", resp["configured"])
	}
}

func TestDeploymentModeHandler_NoConfigFile(t *testing.T) {
	// No config file written — should default to personal mode
	cleanup := setupTestConfigDir(t, nil)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/platform/mode", nil)
	w := httptest.NewRecorder()
	DeploymentModeHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["mode"] != "personal" {
		t.Errorf("mode = %q, want %q", resp["mode"], "personal")
	}
}

// --- PlatformInitHandler Tests ---

func TestPlatformInitHandler_AlreadyConfigured(t *testing.T) {
	cleanup := setupTestConfigDir(t, &config.AppConfig{
		Storage: config.StorageConfig{
			Backend: "postgres",
			Postgres: config.PostgresConfig{
				PlatformDSN: "postgres://u:p@h:5432/db",
			},
		},
	})
	defer cleanup()

	body := `{"host":"localhost","port":5432,"user":"admin","password":"pass"}`
	req := httptest.NewRequest("POST", "/api/platform/init", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	PlatformInitHandler(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}

	var resp PlatformInitResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !strings.Contains(resp.Error, "already configured") {
		t.Errorf("error = %q, want to contain 'already configured'", resp.Error)
	}
}

func TestPlatformInitHandler_MissingUser(t *testing.T) {
	cleanup := setupTestConfigDir(t, &config.AppConfig{
		Storage: config.StorageConfig{Backend: ""},
	})
	defer cleanup()

	body := `{"host":"localhost","port":5432,"user":"","password":"pass"}`
	req := httptest.NewRequest("POST", "/api/platform/init", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	PlatformInitHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}

	var resp PlatformInitResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !strings.Contains(resp.Error, "username is required") {
		t.Errorf("error = %q, want to contain 'username is required'", resp.Error)
	}
}

func TestPlatformInitHandler_MissingPassword(t *testing.T) {
	cleanup := setupTestConfigDir(t, &config.AppConfig{
		Storage: config.StorageConfig{Backend: ""},
	})
	defer cleanup()

	body := `{"host":"localhost","port":5432,"user":"admin","password":""}`
	req := httptest.NewRequest("POST", "/api/platform/init", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	PlatformInitHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}

	var resp PlatformInitResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !strings.Contains(resp.Error, "password is required") {
		t.Errorf("error = %q, want to contain 'password is required'", resp.Error)
	}
}

func TestPlatformInitHandler_InvalidSlug(t *testing.T) {
	cleanup := setupTestConfigDir(t, &config.AppConfig{
		Storage: config.StorageConfig{Backend: ""},
	})
	defer cleanup()

	body := `{"host":"localhost","port":5432,"user":"admin","password":"pass","org_slug":"INVALID SLUG!"}`
	req := httptest.NewRequest("POST", "/api/platform/init", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	PlatformInitHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}

	var resp PlatformInitResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !strings.Contains(resp.Error, "lowercase alphanumeric") {
		t.Errorf("error = %q, want to contain 'lowercase alphanumeric'", resp.Error)
	}
}

func TestPlatformInitHandler_InvalidJSON(t *testing.T) {
	cleanup := setupTestConfigDir(t, &config.AppConfig{
		Storage: config.StorageConfig{Backend: ""},
	})
	defer cleanup()

	body := `{invalid json`
	req := httptest.NewRequest("POST", "/api/platform/init", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	PlatformInitHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}

	var resp PlatformInitResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !strings.Contains(resp.Error, "Invalid request body") {
		t.Errorf("error = %q, want to contain 'Invalid request body'", resp.Error)
	}
}

func TestPlatformInitHandler_DefaultsApplied(t *testing.T) {
	// This test validates that default values are applied for optional fields.
	// It won't succeed at actually connecting (no real PG), but we can test
	// that it gets past validation. We expect a 500 because PG isn't available.
	cleanup := setupTestConfigDir(t, &config.AppConfig{
		Storage: config.StorageConfig{Backend: ""},
	})
	defer cleanup()

	// Minimal valid request — host/port/ssl/org all omitted (will use defaults)
	body := `{"user":"admin","password":"pass"}`
	req := httptest.NewRequest("POST", "/api/platform/init", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	PlatformInitHandler(w, req)

	// Should not be 400 (validation should pass with defaults applied)
	// It will be 500 because there's no real PostgreSQL to connect to
	if w.Code == http.StatusBadRequest {
		var resp PlatformInitResponse
		json.Unmarshal(w.Body.Bytes(), &resp)
		t.Fatalf("unexpected 400: %s (defaults should handle missing fields)", resp.Error)
	}

	// Accept 500 (can't connect to PG) — the key is that validation passed
	if w.Code != http.StatusInternalServerError {
		t.Logf("status = %d (expected 500 since no PG is available)", w.Code)
	}
}

func TestPlatformInitHandler_ValidSlugCharacters(t *testing.T) {
	cleanup := setupTestConfigDir(t, &config.AppConfig{
		Storage: config.StorageConfig{Backend: ""},
	})
	defer cleanup()

	validSlugs := []string{"my-org", "org_1", "test123", "a", "org-name-with-hyphens"}

	for _, slug := range validSlugs {
		t.Run(slug, func(t *testing.T) {
			body := `{"user":"admin","password":"pass","org_slug":"` + slug + `"}`
			req := httptest.NewRequest("POST", "/api/platform/init", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			PlatformInitHandler(w, req)

			// Should not be rejected with 400 for slug validation
			if w.Code == http.StatusBadRequest {
				var resp PlatformInitResponse
				json.Unmarshal(w.Body.Bytes(), &resp)
				if strings.Contains(resp.Error, "slug") {
					t.Errorf("valid slug %q was rejected: %s", slug, resp.Error)
				}
			}
		})
	}
}

func TestPlatformInitHandler_InvalidSlugCharacters(t *testing.T) {
	cleanup := setupTestConfigDir(t, &config.AppConfig{
		Storage: config.StorageConfig{Backend: ""},
	})
	defer cleanup()

	invalidSlugs := []string{"UPPERCASE", "has space", "special!", "with.dots", "slash/bad"}

	for _, slug := range invalidSlugs {
		t.Run(slug, func(t *testing.T) {
			body := `{"user":"admin","password":"pass","org_slug":"` + slug + `"}`
			req := httptest.NewRequest("POST", "/api/platform/init", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			PlatformInitHandler(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("invalid slug %q was NOT rejected (status=%d)", slug, w.Code)
			}
		})
	}
}
