package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_NonExistentFile(t *testing.T) {
	m := &Manager{Path: filepath.Join(t.TempDir(), "does_not_exist.md")}
	content, err := m.Load()
	if err != nil {
		t.Fatalf("expected no error for non-existent file, got: %v", err)
	}
	if content != "" {
		t.Errorf("expected empty string, got: %q", content)
	}
}

func TestAppend_CreatesFileAndSection(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{Path: filepath.Join(dir, "memory", "MEMORY.md")}

	err := m.Append("Infrastructure", "- Proxmox server: 192.168.1.200 (user: root)", false)
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	content, err := m.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if !strings.Contains(content, "## Infrastructure") {
		t.Errorf("expected section heading, got:\n%s", content)
	}
	if !strings.Contains(content, "192.168.1.200") {
		t.Errorf("expected IP in content, got:\n%s", content)
	}
}

func TestAppend_AddsToExistingSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MEMORY.md")

	// Seed file with an existing section
	initial := "## Infrastructure\n- Server A: 10.0.0.1\n"
	os.WriteFile(path, []byte(initial), 0644)

	m := &Manager{Path: path}
	err := m.Append("Infrastructure", "- Server B: 10.0.0.2", false)
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	content, err := m.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if !strings.Contains(content, "Server A") {
		t.Errorf("lost existing content:\n%s", content)
	}
	if !strings.Contains(content, "Server B") {
		t.Errorf("new content not added:\n%s", content)
	}
}

func TestAppend_DeduplicatesExactLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MEMORY.md")

	m := &Manager{Path: path}

	// Add the same line twice
	m.Append("Infrastructure", "- Proxmox server: 192.168.1.200", false)
	m.Append("Infrastructure", "- Proxmox server: 192.168.1.200", false)

	content, err := m.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	count := strings.Count(content, "192.168.1.200")
	if count != 1 {
		t.Errorf("expected 1 occurrence, got %d in:\n%s", count, content)
	}
}

func TestAppend_MultipleSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MEMORY.md")

	m := &Manager{Path: path}

	m.Append("Infrastructure", "- Server: 10.0.0.1", false)
	m.Append("Preferences", "- Timezone: America/New_York", false)
	m.Append("Infrastructure", "- Router: 10.0.0.254", false)

	content, err := m.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if !strings.Contains(content, "## Infrastructure") {
		t.Errorf("missing Infrastructure section:\n%s", content)
	}
	if !strings.Contains(content, "## Preferences") {
		t.Errorf("missing Preferences section:\n%s", content)
	}
	if !strings.Contains(content, "Router") {
		t.Errorf("missing Router entry:\n%s", content)
	}

	// Infrastructure section should have both entries
	infraIdx := strings.Index(content, "## Infrastructure")
	prefIdx := strings.Index(content, "## Preferences")
	if infraIdx > prefIdx {
		t.Errorf("Infrastructure should come before Preferences (order preserved)")
	}

	infraSection := content[infraIdx:prefIdx]
	if !strings.Contains(infraSection, "Server") || !strings.Contains(infraSection, "Router") {
		t.Errorf("Infrastructure section missing entries:\n%s", infraSection)
	}
}

func TestAppend_CaseInsensitiveHeading(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MEMORY.md")

	m := &Manager{Path: path}

	m.Append("Infrastructure", "- Server A: 10.0.0.1", false)
	m.Append("infrastructure", "- Server B: 10.0.0.2", false)

	content, err := m.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Should only have one ## Infrastructure heading (case preserved from first write)
	count := strings.Count(content, "## ")
	if count != 1 {
		t.Errorf("expected 1 section heading, got %d in:\n%s", count, content)
	}
	if !strings.Contains(content, "Server A") || !strings.Contains(content, "Server B") {
		t.Errorf("both entries should be present:\n%s", content)
	}
}

func TestAppend_PreservesPreamble(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MEMORY.md")

	// Seed with a preamble (# heading + text before any ## section)
	initial := "# Memory\n\nThis file stores persistent knowledge.\n\n## Facts\n- Fact one\n"
	os.WriteFile(path, []byte(initial), 0644)

	m := &Manager{Path: path}
	m.Append("Facts", "- Fact two", false)

	content, err := m.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if !strings.Contains(content, "# Memory") {
		t.Errorf("preamble lost:\n%s", content)
	}
	if !strings.Contains(content, "This file stores persistent knowledge.") {
		t.Errorf("preamble text lost:\n%s", content)
	}
	if !strings.Contains(content, "Fact one") || !strings.Contains(content, "Fact two") {
		t.Errorf("facts missing:\n%s", content)
	}
}

func TestParseSections_EmptyContent(t *testing.T) {
	sl := parseSections("")
	if len(sl.sections) != 0 {
		t.Errorf("expected 0 sections, got %d", len(sl.sections))
	}
}

func TestParseSections_MultipleHeadings(t *testing.T) {
	content := "## Alpha\n- a1\n- a2\n\n## Beta\n- b1\n"
	sl := parseSections(content)

	if len(sl.sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(sl.sections))
	}
	if sl.sections[0].heading != "Alpha" {
		t.Errorf("expected 'Alpha', got %q", sl.sections[0].heading)
	}
	if sl.sections[1].heading != "Beta" {
		t.Errorf("expected 'Beta', got %q", sl.sections[1].heading)
	}
	if len(sl.sections[0].lines) != 2 {
		t.Errorf("expected 2 lines in Alpha, got %d: %v", len(sl.sections[0].lines), sl.sections[0].lines)
	}
}

func TestDefaultPath(t *testing.T) {
	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath failed: %v", err)
	}
	if !strings.HasSuffix(path, filepath.Join("astonish", "memory", "MEMORY.md")) {
		t.Errorf("unexpected path: %s", path)
	}
}

func TestAppend_OverwriteSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MEMORY.md")

	m := &Manager{Path: path}

	// Seed with initial data
	m.Append("Infrastructure - Proxmox LXC", "- openclaw (VMID 107): IP 192.168.1.233, SSH as root (passwordless key auth)", false)

	// Overwrite the section with corrected info
	m.Append("Infrastructure - Proxmox LXC", "- openclaw (VMID 107): IP 192.168.1.233, SSH as root, password: 554252", true)

	content, err := m.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Old line should be gone
	if strings.Contains(content, "passwordless key auth") {
		t.Errorf("old line should have been replaced:\n%s", content)
	}
	// New line should be present
	if !strings.Contains(content, "password: 554252") {
		t.Errorf("new line should be present:\n%s", content)
	}
	// Should only have one occurrence of openclaw
	count := strings.Count(content, "openclaw")
	if count != 1 {
		t.Errorf("expected 1 occurrence of openclaw, got %d in:\n%s", count, content)
	}
}

func TestAppend_OverwritePreservesOtherSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MEMORY.md")

	m := &Manager{Path: path}

	// Seed with two sections
	m.Append("Infrastructure", "- Server: 10.0.0.1", false)
	m.Append("Preferences", "- Timezone: America/New_York", false)

	// Overwrite only Infrastructure
	m.Append("Infrastructure", "- Server: 10.0.0.2 (corrected)", true)

	content, err := m.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Old Infrastructure content should be gone
	if strings.Contains(content, "10.0.0.1") {
		t.Errorf("old server IP should have been replaced:\n%s", content)
	}
	// New Infrastructure content should be present
	if !strings.Contains(content, "10.0.0.2 (corrected)") {
		t.Errorf("new server IP should be present:\n%s", content)
	}
	// Preferences section should be untouched
	if !strings.Contains(content, "America/New_York") {
		t.Errorf("Preferences section should be preserved:\n%s", content)
	}
}
