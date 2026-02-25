package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncSkillsToMemory(t *testing.T) {
	memDir := t.TempDir()
	allSkills := []Skill{
		{Name: "echo-skill", Description: "Echo skill", Content: "# Echo\n\nUse echo.", RequireBins: []string{"echo"}},
	}

	err := SyncSkillsToMemory(allSkills, memDir)
	if err != nil {
		t.Fatalf("SyncSkillsToMemory failed: %v", err)
	}

	// Check the file was created
	outPath := filepath.Join(memDir, "skills", "echo-skill.md")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("Failed to read synced skill file: %v", err)
	}

	content := string(data)
	if content == "" {
		t.Error("Synced file is empty")
	}

	// Should contain skill name in header
	if !contains(content, "# Skill: echo-skill") {
		t.Error("Synced file missing skill header")
	}
}

func TestSyncSkillsToMemoryIdempotent(t *testing.T) {
	memDir := t.TempDir()
	allSkills := []Skill{
		{Name: "echo-skill", Description: "Echo skill", Content: "# Echo", RequireBins: []string{"echo"}},
	}

	// Sync twice
	SyncSkillsToMemory(allSkills, memDir)
	SyncSkillsToMemory(allSkills, memDir)

	// File should still exist and be valid
	outPath := filepath.Join(memDir, "skills", "echo-skill.md")
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("File missing after second sync: %v", err)
	}
}

func TestSyncSkillsToMemoryCleanup(t *testing.T) {
	memDir := t.TempDir()

	// Sync with one skill
	SyncSkillsToMemory([]Skill{
		{Name: "keep", Description: "Keep", Content: "# Keep", RequireBins: []string{"echo"}},
	}, memDir)

	// Write an orphan file
	orphanPath := filepath.Join(memDir, "skills", "orphan.md")
	os.WriteFile(orphanPath, []byte("orphan"), 0644)

	// Sync again — orphan should be removed
	SyncSkillsToMemory([]Skill{
		{Name: "keep", Description: "Keep", Content: "# Keep", RequireBins: []string{"echo"}},
	}, memDir)

	if _, err := os.Stat(orphanPath); err == nil {
		t.Error("Orphan file should have been removed")
	}
}

func TestSyncSkillsIneligibleSkipped(t *testing.T) {
	memDir := t.TempDir()
	allSkills := []Skill{
		{Name: "missing-bin", Description: "Missing", Content: "# Missing", RequireBins: []string{"nonexistent_xyz123"}},
	}

	SyncSkillsToMemory(allSkills, memDir)

	outPath := filepath.Join(memDir, "skills", "missing-bin.md")
	if _, err := os.Stat(outPath); err == nil {
		t.Error("Ineligible skill should not be synced")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
