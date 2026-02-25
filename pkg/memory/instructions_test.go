package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstructionsPath(t *testing.T) {
	path := InstructionsPath("/some/dir")
	expected := filepath.Join("/some/dir", "INSTRUCTIONS.md")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestLoadInstructions_NonExistent(t *testing.T) {
	dir := t.TempDir()
	content, err := LoadInstructions(dir)
	if err != nil {
		t.Fatalf("expected no error for non-existent file, got: %v", err)
	}
	if content != "" {
		t.Errorf("expected empty string, got: %q", content)
	}
}

func TestEnsureInstructions_CreatesDefault(t *testing.T) {
	dir := t.TempDir()

	created, err := EnsureInstructions(dir)
	if err != nil {
		t.Fatalf("EnsureInstructions failed: %v", err)
	}
	if !created {
		t.Error("expected file to be created")
	}

	// Verify file exists and contains default content
	content, err := LoadInstructions(dir)
	if err != nil {
		t.Fatalf("LoadInstructions failed: %v", err)
	}
	if content != DefaultInstructions {
		t.Errorf("expected default instructions, got: %q", content)
	}
}

func TestEnsureInstructions_NoOverwrite(t *testing.T) {
	dir := t.TempDir()

	// Write custom content first
	customContent := "# My Custom Instructions\n\nDo things differently.\n"
	path := InstructionsPath(dir)
	if err := os.WriteFile(path, []byte(customContent), 0644); err != nil {
		t.Fatalf("failed to write custom file: %v", err)
	}

	created, err := EnsureInstructions(dir)
	if err != nil {
		t.Fatalf("EnsureInstructions failed: %v", err)
	}
	if created {
		t.Error("expected file NOT to be created (already exists)")
	}

	// Verify original content preserved
	content, err := LoadInstructions(dir)
	if err != nil {
		t.Fatalf("LoadInstructions failed: %v", err)
	}
	if content != customContent {
		t.Errorf("expected custom content preserved, got: %q", content)
	}
}

func TestEnsureInstructions_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "memory")

	created, err := EnsureInstructions(dir)
	if err != nil {
		t.Fatalf("EnsureInstructions failed: %v", err)
	}
	if !created {
		t.Error("expected file to be created")
	}

	// Verify nested directory was created
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("expected directory to be created")
	}
}

func TestDefaultInstructions_HasExpectedSections(t *testing.T) {
	expectedSections := []string{
		"## General Behavior",
		"## Permissions",
		"## Communication Style",
		"## Memory Guidelines",
	}
	for _, section := range expectedSections {
		if !strings.Contains(DefaultInstructions, section) {
			t.Errorf("DefaultInstructions missing section: %s", section)
		}
	}
}
