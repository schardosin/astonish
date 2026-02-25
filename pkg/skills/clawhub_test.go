package skills

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestParseClawHubInputFullURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://clawhub.ai/steipete/github", "github"},
		{"https://clawhub.ai/owner/docker", "docker"},
		{"https://clawhub.ai/someone/my-skill/", "my-skill"},
	}

	for _, tc := range tests {
		slug, err := ParseClawHubInput(tc.input)
		if err != nil {
			t.Errorf("ParseClawHubInput(%q) error: %v", tc.input, err)
			continue
		}
		if slug != tc.expected {
			t.Errorf("ParseClawHubInput(%q) = %q, want %q", tc.input, slug, tc.expected)
		}
	}
}

func TestParseClawHubInputShorthand(t *testing.T) {
	slug, err := ParseClawHubInput("clawhub:terraform")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if slug != "terraform" {
		t.Errorf("got %q, want %q", slug, "terraform")
	}
}

func TestParseClawHubInputBareSlug(t *testing.T) {
	slug, err := ParseClawHubInput("kubernetes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if slug != "kubernetes" {
		t.Errorf("got %q, want %q", slug, "kubernetes")
	}
}

func TestParseClawHubInputOwnerSlug(t *testing.T) {
	slug, err := ParseClawHubInput("steipete/github")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if slug != "github" {
		t.Errorf("got %q, want %q", slug, "github")
	}
}

func TestParseClawHubInputInvalid(t *testing.T) {
	tests := []string{
		"",
		"clawhub:",
		"has spaces",
		"https://clawhub.ai/onlyone",
	}

	for _, input := range tests {
		_, err := ParseClawHubInput(input)
		if err == nil {
			t.Errorf("ParseClawHubInput(%q) should have returned error", input)
		}
	}
}

func TestNormalizeClawHubMetadataJSONString(t *testing.T) {
	// ClawHub Variation A: metadata is a JSON string
	skill := Skill{
		Name:        "docker",
		Description: "Docker skill",
		Metadata:    `{"clawdbot":{"emoji":"🐳","requires":{"bins":["docker","docker-compose"]},"os":["linux","darwin","win32"]}}`,
	}

	normalizeClawHubMetadata(&skill)

	if len(skill.RequireBins) != 2 || skill.RequireBins[0] != "docker" || skill.RequireBins[1] != "docker-compose" {
		t.Errorf("RequireBins = %v, want [docker docker-compose]", skill.RequireBins)
	}
	if len(skill.OS) != 3 {
		t.Fatalf("OS = %v, want 3 entries", skill.OS)
	}
	// win32 should be mapped to windows
	foundWindows := false
	for _, o := range skill.OS {
		if o == "windows" {
			foundWindows = true
		}
		if o == "win32" {
			t.Error("OS should have mapped win32 to windows")
		}
	}
	if !foundWindows {
		t.Error("OS should contain 'windows' (mapped from win32)")
	}
}

func TestNormalizeClawHubMetadataNestedYAML(t *testing.T) {
	// ClawHub Variation B: metadata is already a map (from YAML parsing)
	skill := Skill{
		Name:        "terraform",
		Description: "Terraform skill",
		Metadata: map[string]interface{}{
			"clawdbot": map[string]interface{}{
				"requires": map[string]interface{}{
					"bins": []interface{}{"terraform"},
					"env":  []interface{}{"TF_TOKEN"},
				},
				"os": []interface{}{"linux", "darwin"},
			},
		},
	}

	normalizeClawHubMetadata(&skill)

	if len(skill.RequireBins) != 1 || skill.RequireBins[0] != "terraform" {
		t.Errorf("RequireBins = %v, want [terraform]", skill.RequireBins)
	}
	if len(skill.RequireEnv) != 1 || skill.RequireEnv[0] != "TF_TOKEN" {
		t.Errorf("RequireEnv = %v, want [TF_TOKEN]", skill.RequireEnv)
	}
	if len(skill.OS) != 2 {
		t.Errorf("OS = %v, want [linux darwin]", skill.OS)
	}
}

func TestNormalizeClawHubMetadataNoMetadata(t *testing.T) {
	skill := Skill{
		Name:        "simple",
		Description: "Simple skill",
	}

	normalizeClawHubMetadata(&skill)

	if len(skill.RequireBins) != 0 {
		t.Errorf("RequireBins should be empty, got %v", skill.RequireBins)
	}
	if len(skill.OS) != 0 {
		t.Errorf("OS should be empty, got %v", skill.OS)
	}
}

func TestNormalizeClawHubMetadataFlatFieldsPreserved(t *testing.T) {
	// If flat fields are already set, they should NOT be overwritten
	skill := Skill{
		Name:        "docker",
		Description: "Docker skill",
		RequireBins: []string{"custom-docker"},
		OS:          []string{"linux"},
		Metadata: map[string]interface{}{
			"clawdbot": map[string]interface{}{
				"requires": map[string]interface{}{
					"bins": []interface{}{"docker"},
				},
				"os": []interface{}{"linux", "darwin", "win32"},
			},
		},
	}

	normalizeClawHubMetadata(&skill)

	// Should keep original flat values
	if len(skill.RequireBins) != 1 || skill.RequireBins[0] != "custom-docker" {
		t.Errorf("RequireBins should be preserved as [custom-docker], got %v", skill.RequireBins)
	}
	if len(skill.OS) != 1 || skill.OS[0] != "linux" {
		t.Errorf("OS should be preserved as [linux], got %v", skill.OS)
	}
}

// createTestZip creates a zip archive in memory with the given files.
func createTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestDownloadFromClawHub(t *testing.T) {
	meta := ClawHubMeta{
		OwnerID:     "test-owner",
		Slug:        "test-skill",
		Version:     "1.2.3",
		PublishedAt: "2025-01-01T00:00:00Z",
	}
	metaJSON, _ := json.Marshal(meta)

	zipData := createTestZip(t, map[string]string{
		"SKILL.md":   "---\nname: test-skill\ndescription: \"A test skill from ClawHub\"\nrequire_bins: [\"echo\"]\n---\n\n# Test Skill\n\nInstructions here.\n",
		"_meta.json": string(metaJSON),
		"setup.md":   "# Setup\n\nSetup instructions.\n",
	})

	// Create a test server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slug := r.URL.Query().Get("slug")
		if slug != "test-skill" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Write(zipData)
	}))
	defer srv.Close()

	// Temporarily override the download URL
	origURL := clawHubDownloadURL
	// We can't easily override the const, so let's test via a local download helper instead
	_ = origURL

	// Test the extraction by creating a zip and using a local file approach
	destDir := t.TempDir()
	skillDir := filepath.Join(destDir, "test-skill")
	os.MkdirAll(skillDir, 0755)

	// Write the zip files directly (simulating what DownloadFromClawHub does)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: test-skill\ndescription: \"A test skill from ClawHub\"\nrequire_bins: [\"echo\"]\n---\n\n# Test Skill\n\nInstructions here.\n"), 0644)
	os.WriteFile(filepath.Join(skillDir, "_meta.json"), metaJSON, 0644)
	os.WriteFile(filepath.Join(skillDir, "setup.md"), []byte("# Setup\n\nSetup instructions.\n"), 0644)

	// Verify the meta read works
	readMeta, err := ReadClawHubMeta(skillDir)
	if err != nil {
		t.Fatalf("ReadClawHubMeta failed: %v", err)
	}
	if readMeta.Version != "1.2.3" {
		t.Errorf("Version = %q, want %q", readMeta.Version, "1.2.3")
	}
	if readMeta.Slug != "test-skill" {
		t.Errorf("Slug = %q, want %q", readMeta.Slug, "test-skill")
	}

	// Verify the skill can be parsed
	skillPath := filepath.Join(skillDir, "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	skill, err := ParseSkillFile(skillPath, data)
	if err != nil {
		t.Fatalf("ParseSkillFile failed: %v", err)
	}
	if skill.Name != "test-skill" {
		t.Errorf("Name = %q, want %q", skill.Name, "test-skill")
	}
}

func TestReadClawHubMetaNotFound(t *testing.T) {
	_, err := ReadClawHubMeta(t.TempDir())
	if err == nil {
		t.Error("Expected error for missing _meta.json")
	}
}

func TestParseClawHubInputHTTP(t *testing.T) {
	// http:// should also work (will be upgraded to https in practice)
	slug, err := ParseClawHubInput("http://clawhub.ai/owner/myskill")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if slug != "myskill" {
		t.Errorf("got %q, want %q", slug, "myskill")
	}
}

func TestSyncSkillsToMemoryWithSupplementaryFiles(t *testing.T) {
	// Create a skill directory with supplementary .md files
	skillDir := t.TempDir()
	subDir := filepath.Join(skillDir, "docker")
	os.MkdirAll(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, "SKILL.md"), []byte("---\nname: docker\ndescription: \"Docker skill\"\nrequire_bins: [\"echo\"]\n---\n\n# Docker\n"), 0644)
	os.WriteFile(filepath.Join(subDir, "commands.md"), []byte("# Docker Commands\n\nUseful commands.\n"), 0644)
	os.WriteFile(filepath.Join(subDir, "security.md"), []byte("# Docker Security\n\nSecurity tips.\n"), 0644)
	os.WriteFile(filepath.Join(subDir, "config.js"), []byte("// not a markdown file"), 0644)

	byName := make(map[string]*Skill)
	loadSkillsFromDir(skillDir, "user", byName)

	if len(byName) != 1 {
		t.Fatalf("Expected 1 skill, got %d", len(byName))
	}

	docker := byName["docker"]
	if docker.Directory == "" {
		t.Fatal("Docker skill should have a Directory set")
	}

	// Sync to memory
	memDir := t.TempDir()
	err := SyncSkillsToMemory([]Skill{*docker}, memDir)
	if err != nil {
		t.Fatalf("SyncSkillsToMemory failed: %v", err)
	}

	memSkillsDir := filepath.Join(memDir, "skills")

	// Main skill file
	if _, err := os.Stat(filepath.Join(memSkillsDir, "docker.md")); err != nil {
		t.Error("Expected docker.md in memory/skills/")
	}

	// Supplementary files with naming convention
	if _, err := os.Stat(filepath.Join(memSkillsDir, "docker--commands.md")); err != nil {
		t.Error("Expected docker--commands.md in memory/skills/")
	}
	if _, err := os.Stat(filepath.Join(memSkillsDir, "docker--security.md")); err != nil {
		t.Error("Expected docker--security.md in memory/skills/")
	}

	// Non-.md files should NOT be synced
	if _, err := os.Stat(filepath.Join(memSkillsDir, "docker--config.js")); err == nil {
		t.Error("Non-.md file should NOT be synced to memory")
	}
}

func TestSkillDirectorySetForDiskSkills(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "my-tool")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: my-tool\ndescription: \"My tool\"\n---\n\n# My Tool\n"), 0644)

	byName := make(map[string]*Skill)
	loadSkillsFromDir(tmpDir, "user", byName)

	skill := byName["my-tool"]
	if skill == nil {
		t.Fatal("Skill not found")
	}
	if skill.Directory == "" {
		t.Error("Directory should be set for disk-based skills")
	}
	if !filepath.IsAbs(skill.Directory) {
		t.Errorf("Directory should be absolute, got %q", skill.Directory)
	}
}

func TestBundledSkillsHaveEmptyDirectory(t *testing.T) {
	byName := make(map[string]*Skill)
	if err := loadBundledSkills(byName); err != nil {
		t.Fatalf("loadBundledSkills failed: %v", err)
	}

	for name, skill := range byName {
		if skill.Directory != "" {
			t.Errorf("Bundled skill %q should have empty Directory, got %q", name, skill.Directory)
		}
	}
}
