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

// --- Fuzzy heading matching tests ---

func TestAppend_FuzzyHeadingMatch_ProxmoxVariations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MEMORY.md")

	m := &Manager{Path: path}

	// First write creates "Proxmox Server"
	m.Append("Proxmox Server", "- IP: 192.168.1.200", false)
	// Second write with a similar-but-different heading should merge
	m.Append("Proxmox Server Configuration", "- Node: proxmox", false)
	// Third write with yet another variation
	m.Append("Proxmox Server Connection", "- Auth: PVE API Token", false)

	content, err := m.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Should only have one ## heading (the original "Proxmox Server")
	count := strings.Count(content, "## ")
	if count != 1 {
		t.Errorf("expected 1 section heading, got %d in:\n%s", count, content)
	}

	// All content should be present under the single heading
	if !strings.Contains(content, "192.168.1.200") {
		t.Errorf("missing original content:\n%s", content)
	}
	if !strings.Contains(content, "Node: proxmox") {
		t.Errorf("missing second append content:\n%s", content)
	}
	if !strings.Contains(content, "Auth: PVE API Token") {
		t.Errorf("missing third append content:\n%s", content)
	}

	// The heading should be preserved from the first write
	if !strings.Contains(content, "## Proxmox Server\n") {
		t.Errorf("heading should be preserved from first write:\n%s", content)
	}
}

func TestAppend_FuzzyHeadingMatch_BrowserVariations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MEMORY.md")

	m := &Manager{Path: path}

	m.Append("Browser Infrastructure", "- Port: 9222", false)
	// "Browser Service Configuration" — after removing stop words
	// (service, configuration), this becomes {browser} vs {browser, infrastructure}
	// which is 1/2 = 0.5 — exactly at threshold
	m.Append("Browser Service Configuration", "- Chromium backend", false)

	content, err := m.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Should merge into one section
	count := strings.Count(content, "## ")
	if count != 1 {
		t.Errorf("expected 1 section heading, got %d in:\n%s", count, content)
	}
	if !strings.Contains(content, "Port: 9222") || !strings.Contains(content, "Chromium backend") {
		t.Errorf("both entries should be present:\n%s", content)
	}
}

func TestAppend_FuzzyHeadingMatch_NoFalsePositive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MEMORY.md")

	m := &Manager{Path: path}

	// These should NOT be merged — they are different topics
	m.Append("Reddit", "- Username: testuser", false)
	m.Append("Gmail / Google Account", "- Email: test@gmail.com", false)
	m.Append("Infrastructure", "- Server: 10.0.0.1", false)

	content, err := m.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Should have exactly 3 separate sections
	count := strings.Count(content, "## ")
	if count != 3 {
		t.Errorf("expected 3 section headings, got %d in:\n%s", count, content)
	}
}

func TestAppend_FuzzyHeadingMatch_PreservesFirstHeading(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MEMORY.md")

	m := &Manager{Path: path}

	m.Append("GitHub Repositories", "- Repo: astonish", false)
	m.Append("GitHub Repository", "- Repo: juicytrade", false)

	content, err := m.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Should merge and keep the first heading name
	count := strings.Count(content, "## ")
	if count != 1 {
		t.Errorf("expected 1 section heading, got %d in:\n%s", count, content)
	}
	if !strings.Contains(content, "## GitHub Repositories") {
		t.Errorf("should preserve first heading:\n%s", content)
	}
	if !strings.Contains(content, "astonish") || !strings.Contains(content, "juicytrade") {
		t.Errorf("both entries should be present:\n%s", content)
	}
}

func TestAppend_FuzzyOverwriteViaFuzzyMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MEMORY.md")

	m := &Manager{Path: path}

	// Create the section
	m.Append("Proxmox Server", "- Old info: outdated", false)

	// Overwrite via a fuzzy-matching heading
	m.Append("Proxmox Server Configuration", "- New info: corrected", true)

	content, err := m.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Old content should be gone (overwritten)
	if strings.Contains(content, "Old info: outdated") {
		t.Errorf("old content should have been overwritten:\n%s", content)
	}
	// New content should be present
	if !strings.Contains(content, "New info: corrected") {
		t.Errorf("new content should be present:\n%s", content)
	}
	// Should still only have one section
	count := strings.Count(content, "## ")
	if count != 1 {
		t.Errorf("expected 1 section heading, got %d in:\n%s", count, content)
	}
}

// --- Subset heading matching tests ---

func TestAppend_SubsetMatch_ShortExistingHeading(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MEMORY.md")

	m := &Manager{Path: path}

	// "Proxmox" (1 word) should absorb "Proxmox API Access" (3 words)
	// because {proxmox} is a subset of {proxmox, api, access}
	m.Append("Proxmox", "- Hostname: proxmox.local", false)
	m.Append("Proxmox API Access", "- Auth: PVE token", false)
	m.Append("Proxmox Storage Architecture", "- local-zfs: 1.7TB", false)

	content, err := m.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	count := strings.Count(content, "## ")
	if count != 1 {
		t.Errorf("expected 1 section heading, got %d in:\n%s", count, content)
	}
	if !strings.Contains(content, "## Proxmox\n") {
		t.Errorf("should preserve first heading:\n%s", content)
	}
	if !strings.Contains(content, "proxmox.local") || !strings.Contains(content, "PVE token") || !strings.Contains(content, "local-zfs") {
		t.Errorf("all entries should be present:\n%s", content)
	}
}

func TestAppend_SubsetMatch_NewIsSubsetOfExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MEMORY.md")

	m := &Manager{Path: path}

	// Existing heading is more specific, new heading is more general
	// {browser} is a subset of {browser, infrastructure}
	m.Append("Browser Infrastructure", "- Port: 9222", false)
	m.Append("Browser", "- CDP protocol", false)

	content, err := m.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	count := strings.Count(content, "## ")
	if count != 1 {
		t.Errorf("expected 1 section heading, got %d in:\n%s", count, content)
	}
	if !strings.Contains(content, "Port: 9222") || !strings.Contains(content, "CDP protocol") {
		t.Errorf("both entries should be present:\n%s", content)
	}
}

func TestAppend_SubsetMatch_NoFalsePositive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MEMORY.md")

	m := &Manager{Path: path}

	// These share no words — should NOT merge
	m.Append("Proxmox", "- Server info", false)
	m.Append("Reddit", "- Account info", false)
	m.Append("GitHub", "- Repo info", false)

	content, err := m.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	count := strings.Count(content, "## ")
	if count != 3 {
		t.Errorf("expected 3 separate sections, got %d in:\n%s", count, content)
	}
}

func TestIsSubset(t *testing.T) {
	tests := []struct {
		name     string
		a        map[string]bool
		b        map[string]bool
		expected bool
	}{
		{
			"a subset of b",
			map[string]bool{"proxmox": true},
			map[string]bool{"proxmox": true, "api": true, "access": true},
			true,
		},
		{
			"equal sets",
			map[string]bool{"proxmox": true, "server": true},
			map[string]bool{"proxmox": true, "server": true},
			true,
		},
		{
			"a not subset of b",
			map[string]bool{"proxmox": true, "server": true},
			map[string]bool{"reddit": true, "account": true},
			false,
		},
		{
			"partial overlap not subset",
			map[string]bool{"proxmox": true, "api": true},
			map[string]bool{"proxmox": true, "storage": true},
			false,
		},
		{
			"empty a returns false",
			map[string]bool{},
			map[string]bool{"proxmox": true},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSubset(tt.a, tt.b)
			if got != tt.expected {
				t.Errorf("isSubset(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

// --- headingWords and jaccardSimilarity unit tests ---

func TestHeadingWords(t *testing.T) {
	tests := []struct {
		heading  string
		expected map[string]bool
	}{
		{
			"Proxmox Server Configuration",
			// "configuration" is a stop word, "server" has 6 chars so no stem
			map[string]bool{"proxmox": true, "server": true},
		},
		{
			"Browser Service Configuration",
			// "service" and "configuration" are stop words
			map[string]bool{"browser": true},
		},
		{
			"GitHub Repositories",
			// "repositories" (12 chars, ends in -ies) -> "repository"
			map[string]bool{"github": true, "repository": true},
		},
		{
			"GitHub Repository Details",
			// "details" is a stop word, "repository" (10 chars, no trailing s)
			map[string]bool{"github": true, "repository": true},
		},
		{
			"Proxmox environment (container control)",
			// "environment" is a stop word, parens are split chars
			map[string]bool{"proxmox": true, "container": true, "control": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.heading, func(t *testing.T) {
			got := headingWords(tt.heading)
			if len(got) != len(tt.expected) {
				t.Errorf("headingWords(%q) = %v, want %v", tt.heading, got, tt.expected)
				return
			}
			for w := range tt.expected {
				if !got[w] {
					t.Errorf("headingWords(%q) missing word %q, got %v", tt.heading, w, got)
				}
			}
		})
	}
}

func TestJaccardSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a    map[string]bool
		b    map[string]bool
		min  float64
		max  float64
	}{
		{
			"identical",
			map[string]bool{"proxmox": true, "server": true},
			map[string]bool{"proxmox": true, "server": true},
			1.0, 1.0,
		},
		{
			"disjoint",
			map[string]bool{"reddit": true},
			map[string]bool{"gmail": true, "google": true},
			0.0, 0.0,
		},
		{
			"partial overlap",
			map[string]bool{"proxmox": true, "server": true},
			map[string]bool{"proxmox": true, "api": true},
			0.3, 0.4, // 1/3 = 0.333
		},
		{
			"subset",
			map[string]bool{"browser": true},
			map[string]bool{"browser": true, "infrastructure": true},
			0.49, 0.51, // 1/2 = 0.5
		},
		{
			"both empty",
			map[string]bool{},
			map[string]bool{},
			0.0, 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := jaccardSimilarity(tt.a, tt.b)
			if score < tt.min || score > tt.max {
				t.Errorf("jaccardSimilarity(%v, %v) = %f, want [%f, %f]", tt.a, tt.b, score, tt.min, tt.max)
			}
		})
	}
}

// --- AppendToFile tests (knowledge tier) ---

func TestAppendToFile_CreatesFileWithSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tools", "proxmox.md")

	err := AppendToFile(path, "API Patterns", "- GET /nodes/proxmox/status", false, false)
	if err != nil {
		t.Fatalf("AppendToFile failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "## API Patterns") {
		t.Errorf("missing section heading:\n%s", content)
	}
	if !strings.Contains(content, "GET /nodes/proxmox/status") {
		t.Errorf("missing content:\n%s", content)
	}
}

func TestAppendToFile_DeduplicatesLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tools", "proxmox.md")

	AppendToFile(path, "API Patterns", "- GET /nodes/proxmox/status", false, false)
	AppendToFile(path, "API Patterns", "- GET /nodes/proxmox/status", false, false)

	data, _ := os.ReadFile(path)
	content := string(data)

	count := strings.Count(content, "GET /nodes/proxmox/status")
	if count != 1 {
		t.Errorf("expected 1 occurrence, got %d in:\n%s", count, content)
	}
}

func TestAppendToFile_FuzzyMergesSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tools", "proxmox.md")

	AppendToFile(path, "Proxmox Configuration", "- Host: proxmox.local", false, false)
	AppendToFile(path, "Proxmox Config Details", "- Node: proxmox", false, false)

	data, _ := os.ReadFile(path)
	content := string(data)

	// Should merge into one section
	count := strings.Count(content, "## ")
	if count != 1 {
		t.Errorf("expected 1 section heading, got %d in:\n%s", count, content)
	}
	if !strings.Contains(content, "Host: proxmox.local") || !strings.Contains(content, "Node: proxmox") {
		t.Errorf("both entries should be present:\n%s", content)
	}
}

func TestAppendToFile_OverwriteSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tools", "proxmox.md")

	AppendToFile(path, "Storage", "- local: 1TB", false, false)
	AppendToFile(path, "Storage", "- local-zfs: 2TB", true, false)

	data, _ := os.ReadFile(path)
	content := string(data)

	if strings.Contains(content, "local: 1TB") {
		t.Errorf("old content should have been overwritten:\n%s", content)
	}
	if !strings.Contains(content, "local-zfs: 2TB") {
		t.Errorf("new content should be present:\n%s", content)
	}
}

func TestAppendToFile_PreservesOtherSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "knowledge.md")

	AppendToFile(path, "API Patterns", "- GET /status", false, false)
	AppendToFile(path, "Workarounds", "- Use -sk flag", false, false)
	AppendToFile(path, "API Patterns", "- GET /lxc", false, false)

	data, _ := os.ReadFile(path)
	content := string(data)

	if !strings.Contains(content, "## API Patterns") {
		t.Errorf("missing API Patterns section:\n%s", content)
	}
	if !strings.Contains(content, "## Workarounds") {
		t.Errorf("missing Workarounds section:\n%s", content)
	}
	if !strings.Contains(content, "GET /status") || !strings.Contains(content, "GET /lxc") {
		t.Errorf("API Patterns should have both entries:\n%s", content)
	}
	if !strings.Contains(content, "Use -sk flag") {
		t.Errorf("Workarounds content missing:\n%s", content)
	}
}

// --- GetSectionContent tests ---

func TestGetSectionContent_Found(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MEMORY.md")
	os.WriteFile(path, []byte("## Proxmox\n- Hostname: proxmox.local\n- Node: proxmox\n\n## Browser\n- Port: 9222\n"), 0644)

	content, found := GetSectionContent(path, "Proxmox")
	if !found {
		t.Fatal("expected to find Proxmox section")
	}
	if !strings.Contains(content, "Hostname: proxmox.local") {
		t.Errorf("missing content:\n%s", content)
	}
	if !strings.Contains(content, "Node: proxmox") {
		t.Errorf("missing content:\n%s", content)
	}
	// Should not contain Browser section content
	if strings.Contains(content, "Port: 9222") {
		t.Errorf("should only return Proxmox section, not Browser:\n%s", content)
	}
}

func TestGetSectionContent_FuzzyMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MEMORY.md")
	os.WriteFile(path, []byte("## Proxmox\n- Hostname: proxmox.local\n"), 0644)

	// "Proxmox API Access" should fuzzy-match to "Proxmox" via subset matching
	content, found := GetSectionContent(path, "Proxmox API Access")
	if !found {
		t.Fatal("expected fuzzy match to find Proxmox section")
	}
	if !strings.Contains(content, "Hostname: proxmox.local") {
		t.Errorf("missing content:\n%s", content)
	}
}

func TestGetSectionContent_NotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MEMORY.md")
	os.WriteFile(path, []byte("## Proxmox\n- Hostname: proxmox.local\n"), 0644)

	_, found := GetSectionContent(path, "Kubernetes")
	if found {
		t.Error("should not find a Kubernetes section")
	}
}

func TestGetSectionContent_FileDoesNotExist(t *testing.T) {
	_, found := GetSectionContent("/nonexistent/path/MEMORY.md", "Anything")
	if found {
		t.Error("should return false for nonexistent file")
	}
}
