package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestParseSkillFile(t *testing.T) {
	content := []byte(`---
name: test-skill
description: "A test skill for testing"
require_bins: ["echo"]
---

# Test Skill

## Commands
- echo hello
`)

	skill, err := ParseSkillFile("test/SKILL.md", content)
	if err != nil {
		t.Fatalf("ParseSkillFile failed: %v", err)
	}

	if skill.Name != "test-skill" {
		t.Errorf("Name = %q, want %q", skill.Name, "test-skill")
	}
	if skill.Description != "A test skill for testing" {
		t.Errorf("Description = %q, want %q", skill.Description, "A test skill for testing")
	}
	if len(skill.RequireBins) != 1 || skill.RequireBins[0] != "echo" {
		t.Errorf("RequireBins = %v, want [echo]", skill.RequireBins)
	}
	if skill.Content == "" {
		t.Error("Content is empty")
	}
	if skill.FilePath != "test/SKILL.md" {
		t.Errorf("FilePath = %q, want %q", skill.FilePath, "test/SKILL.md")
	}
}

func TestParseSkillFileNoFrontmatter(t *testing.T) {
	content := []byte("# Just a markdown file\n\nNo frontmatter here.\n")
	_, err := ParseSkillFile("test/SKILL.md", content)
	if err == nil {
		t.Fatal("Expected error for missing frontmatter, got nil")
	}
}

func TestParseSkillFileNoName(t *testing.T) {
	content := []byte("---\ndescription: \"test\"\n---\n\n# Content\n")
	_, err := ParseSkillFile("test/SKILL.md", content)
	if err == nil {
		t.Fatal("Expected error for missing name, got nil")
	}
}

func TestParseSkillFileNoDescription(t *testing.T) {
	content := []byte("---\nname: test\n---\n\n# Content\n")
	_, err := ParseSkillFile("test/SKILL.md", content)
	if err == nil {
		t.Fatal("Expected error for missing description, got nil")
	}
}

func TestParseSkillFileTooLarge(t *testing.T) {
	content := make([]byte, 257*1024)
	_, err := ParseSkillFile("test/SKILL.md", content)
	if err == nil {
		t.Fatal("Expected error for oversized file, got nil")
	}
}

func TestIsEligibleWithEcho(t *testing.T) {
	s := Skill{
		Name:        "test",
		Description: "test",
		RequireBins: []string{"echo"},
	}
	// echo should always exist on unix
	if !s.IsEligible() {
		t.Error("Expected echo to be eligible")
	}
}

func TestIsEligibleMissingBin(t *testing.T) {
	s := Skill{
		Name:        "test",
		Description: "test",
		RequireBins: []string{"nonexistent_binary_xyz123"},
	}
	if s.IsEligible() {
		t.Error("Expected missing binary to be ineligible")
	}
}

func TestIsEligibleOSRestriction(t *testing.T) {
	s := Skill{
		Name:        "test",
		Description: "test",
		OS:          []string{"nonexistent_os"},
	}
	if s.IsEligible() {
		t.Error("Expected wrong OS to be ineligible")
	}
}

func TestIsEligibleMissingEnv(t *testing.T) {
	s := Skill{
		Name:        "test",
		Description: "test",
		RequireEnv:  []string{"NONEXISTENT_ENV_VAR_XYZ123"},
	}
	if s.IsEligible() {
		t.Error("Expected missing env var to be ineligible")
	}
}

func TestMissingRequirements(t *testing.T) {
	s := Skill{
		Name:        "test",
		Description: "test",
		RequireBins: []string{"nonexistent_binary_xyz123"},
		RequireEnv:  []string{"NONEXISTENT_ENV_VAR_XYZ123"},
	}
	missing := s.MissingRequirements()
	if len(missing) != 2 {
		t.Errorf("Expected 2 missing, got %d: %v", len(missing), missing)
	}
}

func TestFilterEligible(t *testing.T) {
	allSkills := []Skill{
		{Name: "good", Description: "good", RequireBins: []string{"echo"}},
		{Name: "bad", Description: "bad", RequireBins: []string{"nonexistent_xyz123"}},
	}
	eligible := FilterEligible(allSkills)
	if len(eligible) != 1 {
		t.Errorf("Expected 1 eligible, got %d", len(eligible))
	}
	if eligible[0].Name != "good" {
		t.Errorf("Expected good, got %s", eligible[0].Name)
	}
}

func TestLoadBundledSkills(t *testing.T) {
	byName := make(map[string]*Skill)
	if err := loadBundledSkills(byName); err != nil {
		t.Fatalf("loadBundledSkills failed: %v", err)
	}

	// We ship 10 bundled skills
	if len(byName) != 10 {
		t.Errorf("Expected 10 bundled skills, got %d", len(byName))
	}

	expected := []string{"github", "docker", "git", "npm", "python", "kubernetes", "terraform", "aws", "gcloud", "web-registration"}
	for _, name := range expected {
		if _, ok := byName[name]; !ok {
			t.Errorf("Missing bundled skill: %s", name)
		}
	}

	// Each skill should have content
	for name, skill := range byName {
		if skill.Content == "" {
			t.Errorf("Skill %s has empty content", name)
		}
		if skill.Description == "" {
			t.Errorf("Skill %s has empty description", name)
		}
		if skill.Source != "bundled" {
			t.Errorf("Skill %s has source %q, want %q", name, skill.Source, "bundled")
		}
	}
}

func TestLoadSkillsFromDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a skill directory
	skillDir := filepath.Join(tmpDir, "my-tool")
	os.MkdirAll(skillDir, 0755)
	content := []byte("---\nname: my-tool\ndescription: \"My custom tool\"\n---\n\n# My Tool\n")
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), content, 0644)

	byName := make(map[string]*Skill)
	loadSkillsFromDir(tmpDir, "user", byName)

	if len(byName) != 1 {
		t.Fatalf("Expected 1 skill, got %d", len(byName))
	}
	skill := byName["my-tool"]
	if skill == nil {
		t.Fatal("Skill 'my-tool' not found")
	}
	if skill.Source != "user" {
		t.Errorf("Source = %q, want %q", skill.Source, "user")
	}
}

func TestLoadSkillsOverride(t *testing.T) {
	// Create two directories with same skill name
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	skillDir1 := filepath.Join(dir1, "overlap")
	os.MkdirAll(skillDir1, 0755)
	os.WriteFile(filepath.Join(skillDir1, "SKILL.md"), []byte("---\nname: overlap\ndescription: \"From dir1\"\n---\n\n# V1\n"), 0644)

	skillDir2 := filepath.Join(dir2, "overlap")
	os.MkdirAll(skillDir2, 0755)
	os.WriteFile(filepath.Join(skillDir2, "SKILL.md"), []byte("---\nname: overlap\ndescription: \"From dir2\"\n---\n\n# V2\n"), 0644)

	// Load with dir2 as extra — should override dir1
	result, err := LoadSkills(dir1, []string{dir2}, "", nil)
	if err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}

	// Find the overlap skill
	found := false
	for _, s := range result {
		if s.Name == "overlap" {
			found = true
			if s.Description != "From dir2" {
				t.Errorf("Expected override from dir2, got description: %s", s.Description)
			}
		}
	}
	if !found {
		t.Error("Skill 'overlap' not found in results")
	}
}

func TestLoadSkillsAllowlist(t *testing.T) {
	tmpDir := t.TempDir()

	for _, name := range []string{"alpha", "beta"} {
		skillDir := filepath.Join(tmpDir, name)
		os.MkdirAll(skillDir, 0755)
		content := []byte(fmt.Sprintf("---\nname: %s\ndescription: \"Skill %s\"\n---\n\n# %s\n", name, name, name))
		os.WriteFile(filepath.Join(skillDir, "SKILL.md"), content, 0644)
	}

	result, err := LoadSkills(tmpDir, nil, "", []string{"alpha"})
	if err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("Expected 1 skill with allowlist, got %d", len(result))
	}
	if result[0].Name != "alpha" {
		t.Errorf("Expected alpha, got %s", result[0].Name)
	}
}

func TestLoadSkillsNonexistentDir(t *testing.T) {
	// Should not error on nonexistent directories
	result, err := LoadSkills("/nonexistent/path", nil, "", nil)
	if err != nil {
		t.Fatalf("Expected no error for nonexistent dir, got: %v", err)
	}
	// Should still have bundled skills
	if len(result) == 0 {
		t.Error("Expected bundled skills even with nonexistent user dir")
	}
}
