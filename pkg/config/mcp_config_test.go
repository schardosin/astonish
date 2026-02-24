package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func boolPtr(b bool) *bool {
	return &b
}

// TestMergeStandardServers_PreservesDisabledKeyBased verifies that a key-based server
// with Enabled=false keeps that flag when re-merged with a valid API key.
func TestMergeStandardServers_PreservesDisabledKeyBased(t *testing.T) {
	// This test relies on LoadAppConfig which reads the real config.
	// If no config.yaml exists or no Tavily key is set, the key-based branch is skipped,
	// so we verify the flag is preserved structurally.

	cfg := &MCPConfig{
		MCPServers: map[string]MCPServerConfig{
			"tavily": {
				Command: "npx",
				Args:    []string{"-y", "tavily-mcp@latest"},
				Enabled: boolPtr(false),
			},
		},
	}

	mergeStandardServers(cfg)

	// If Tavily key is configured, the entry is re-injected with Enabled preserved.
	// If not configured (CI), the entry is left as-is.
	srv := cfg.MCPServers["tavily"]
	if srv.Enabled == nil || *srv.Enabled {
		// This is OK if LoadAppConfig() had no Tavily key — the key-based branch
		// doesn't touch it. So Enabled should remain false from our initial setup.
		if srv.Enabled == nil {
			t.Fatal("Enabled flag was reset to nil (should stay false)")
		}
		if *srv.Enabled {
			t.Fatal("Enabled flag was flipped to true (should stay false)")
		}
	}
}

// TestMergeStandardServers_CustomServerUntouched verifies that mergeStandardServers
// does not modify entries for non-standard (custom) servers.
func TestMergeStandardServers_CustomServerUntouched(t *testing.T) {
	cfg := &MCPConfig{
		MCPServers: map[string]MCPServerConfig{
			"my-custom-server": {
				Command: "node",
				Args:    []string{"server.js"},
				Enabled: boolPtr(false),
			},
		},
	}

	mergeStandardServers(cfg)

	srv, ok := cfg.MCPServers["my-custom-server"]
	if !ok {
		t.Fatal("custom server entry missing after merge")
	}
	if srv.Enabled == nil {
		t.Fatal("Enabled flag was reset to nil (should be false)")
	}
	if *srv.Enabled {
		t.Fatal("Enabled flag was flipped to true (should be false)")
	}
}

// setupTempConfigDir creates a temporary directory structure that GetConfigDir() will use.
// Returns the config directory path.
func setupTempConfigDir(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()

	if runtime.GOOS == "darwin" {
		// macOS: UserConfigDir() returns $HOME/Library/Application Support
		configDir := filepath.Join(tmpDir, "Library", "Application Support", "astonish")
		if err := os.MkdirAll(configDir, 0755); err != nil {
			t.Fatalf("failed to create temp config dir: %v", err)
		}
		t.Setenv("HOME", tmpDir)
		return configDir
	}
	// Linux/other: UserConfigDir() returns $XDG_CONFIG_HOME or $HOME/.config
	configDir := filepath.Join(tmpDir, "astonish")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create temp config dir: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	return configDir
}

// TestSaveMCPConfig_PersistsDisabledStandardServers verifies that SaveMCPConfig keeps
// disabled standard server entries in mcp_config.json.
func TestSaveMCPConfig_PersistsDisabledStandardServers(t *testing.T) {
	configDir := setupTempConfigDir(t)

	cfg := &MCPConfig{
		MCPServers: map[string]MCPServerConfig{
			"tavily": {
				Command: "npx",
				Args:    []string{"-y", "tavily-mcp@latest"},
				Enabled: boolPtr(false), // explicitly disabled
			},
			"my-custom-server": {
				Command: "node",
				Args:    []string{"server.js"},
			},
		},
	}

	if err := SaveMCPConfig(cfg); err != nil {
		t.Fatalf("SaveMCPConfig failed: %v", err)
	}

	// Verify file was written
	data, err := os.ReadFile(filepath.Join(configDir, "mcp_config.json"))
	if err != nil {
		t.Fatalf("failed to read saved config: %v", err)
	}

	// Load it back raw (no merge)
	loaded, err := LoadMCPConfigRaw()
	if err != nil {
		t.Fatalf("LoadMCPConfigRaw failed: %v", err)
	}

	// Disabled standard server should be persisted
	srv, ok := loaded.MCPServers["tavily"]
	if !ok {
		t.Fatalf("disabled tavily should be persisted, got: %s", string(data))
	}
	if srv.Enabled == nil || *srv.Enabled {
		t.Fatal("tavily Enabled should be false after round-trip")
	}

	// Custom server should always be persisted
	if _, ok := loaded.MCPServers["my-custom-server"]; !ok {
		t.Fatal("custom server should be persisted")
	}
}

// TestSaveMCPConfig_StripsEnabledStandardServers verifies that enabled (or nil-Enabled)
// standard servers are stripped from mcp_config.json since they're managed by config.yaml.
func TestSaveMCPConfig_StripsEnabledStandardServers(t *testing.T) {
	setupTempConfigDir(t)

	cfg := &MCPConfig{
		MCPServers: map[string]MCPServerConfig{
			"tavily": {
				Command: "npx",
				Args:    []string{"-y", "tavily-mcp@latest"},
				Enabled: boolPtr(true), // explicitly enabled → should be stripped
			},
			"brave-search": {
				Command: "npx",
				Args:    []string{"-y", "@brave/brave-search-mcp-server"},
				// Enabled is nil → defaults to true → should be stripped
			},
			"my-custom-server": {
				Command: "node",
				Args:    []string{"server.js"},
			},
		},
	}

	if err := SaveMCPConfig(cfg); err != nil {
		t.Fatalf("SaveMCPConfig failed: %v", err)
	}

	loaded, err := LoadMCPConfigRaw()
	if err != nil {
		t.Fatalf("LoadMCPConfigRaw failed: %v", err)
	}

	if _, ok := loaded.MCPServers["tavily"]; ok {
		t.Fatal("explicitly enabled tavily should be stripped from saved config")
	}
	if _, ok := loaded.MCPServers["brave-search"]; ok {
		t.Fatal("nil-enabled brave-search should be stripped from saved config")
	}
	if _, ok := loaded.MCPServers["my-custom-server"]; !ok {
		t.Fatal("custom server should always be persisted")
	}
}

// TestSaveMCPConfig_RoundTripPreservesDisabledFlag tests the full save → load (raw) → merge cycle:
// a disabled standard server persists through save, is present in raw load, and after merge
// still has Enabled=false.
func TestSaveMCPConfig_RoundTripPreservesDisabledFlag(t *testing.T) {
	setupTempConfigDir(t)

	original := &MCPConfig{
		MCPServers: map[string]MCPServerConfig{
			"tavily": {
				Command: "npx",
				Args:    []string{"-y", "tavily-mcp@latest"},
				Enabled: boolPtr(false),
			},
		},
	}

	// Save
	if err := SaveMCPConfig(original); err != nil {
		t.Fatalf("SaveMCPConfig failed: %v", err)
	}

	// Load raw (simulates reading mcp_config.json)
	raw, err := LoadMCPConfigRaw()
	if err != nil {
		t.Fatalf("LoadMCPConfigRaw failed: %v", err)
	}

	// Merge (simulates LoadMCPConfig)
	mergeStandardServers(raw)

	srv, ok := raw.MCPServers["tavily"]
	if !ok {
		t.Fatal("tavily should exist after merge")
	}
	if srv.Enabled == nil {
		t.Fatal("Enabled should not be nil after round-trip + merge")
	}
	if *srv.Enabled {
		t.Fatal("Enabled should be false after round-trip + merge")
	}
}

// TestIsEnabled_DefaultsToTrue tests the IsEnabled method
func TestIsEnabled_DefaultsToTrue(t *testing.T) {
	tests := []struct {
		name     string
		enabled  *bool
		expected bool
	}{
		{"nil defaults to true", nil, true},
		{"explicit true", boolPtr(true), true},
		{"explicit false", boolPtr(false), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := MCPServerConfig{Enabled: tc.enabled}
			if got := cfg.IsEnabled(); got != tc.expected {
				t.Errorf("IsEnabled() = %v, want %v", got, tc.expected)
			}
		})
	}
}
