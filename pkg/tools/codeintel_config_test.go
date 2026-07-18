package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGetInternalTools_CodeIntelDisabled(t *testing.T) {
	configDir := setupToolsTempConfigDir(t)
	cfgPath := filepath.Join(configDir, "config.yaml")
	content := "codeintel:\n  enabled: false\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := GetInternalTools()
	if err != nil {
		t.Fatalf("GetInternalTools: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range got {
		names[tool.Name()] = true
	}
	for _, name := range []string{"repo_map", "code_definition", "code_references"} {
		if names[name] {
			t.Errorf("expected %s absent when codeintel.enabled=false", name)
		}
	}
	if !names["grep_search"] || !names["read_file"] {
		t.Fatal("expected core tools to remain registered")
	}
}

func TestGetInternalTools_CodeIntelEnabledByDefault(t *testing.T) {
	_ = setupToolsTempConfigDir(t)
	// No config.yaml → LoadAppConfig returns empty config; IsEnabled defaults true.
	got, err := GetInternalTools()
	if err != nil {
		t.Fatalf("GetInternalTools: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range got {
		names[tool.Name()] = true
	}
	for _, name := range []string{"repo_map", "code_definition", "code_references"} {
		if !names[name] {
			t.Errorf("expected %s registered by default", name)
		}
	}
}

func TestGetInternalTools_LibraryPathSetsEnv(t *testing.T) {
	configDir := setupToolsTempConfigDir(t)
	libPath := filepath.Join(t.TempDir(), "libastonish-treesitter.so")
	if err := os.WriteFile(libPath, []byte("placeholder"), 0644); err != nil {
		t.Fatal(err)
	}
	content := "codeintel:\n  library_path: " + libPath + "\n"
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ASTONISH_TREESITTER_LIB", "")

	if _, err := GetInternalTools(); err != nil {
		t.Fatalf("GetInternalTools: %v", err)
	}
	if got := os.Getenv("ASTONISH_TREESITTER_LIB"); got != libPath {
		t.Fatalf("ASTONISH_TREESITTER_LIB=%q, want %q", got, libPath)
	}
}

func setupToolsTempConfigDir(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	if runtime.GOOS == "darwin" {
		configDir := filepath.Join(tmpDir, "Library", "Application Support", "astonish")
		if err := os.MkdirAll(configDir, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		t.Setenv("HOME", tmpDir)
		t.Setenv("XDG_CONFIG_HOME", "")
		return configDir
	}
	configDir := filepath.Join(tmpDir, "astonish")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	return configDir
}
