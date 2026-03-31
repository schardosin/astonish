package tools

import (
	"testing"

	"github.com/schardosin/astonish/pkg/skills"
)

func TestSkillLookupFound(t *testing.T) {
	allSkills := []skills.Skill{
		{Name: "test-skill", Description: "A test skill", Content: "# Test\n\nHello world.", RequireBins: []string{"echo"}},
	}

	fn := SkillLookup(allSkills)
	result, err := fn(nil, SkillLookupArgs{Name: "test-skill"})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("Unexpected error in result: %s", result.Error)
	}
	if result.Name != "test-skill" {
		t.Errorf("Name = %q, want %q", result.Name, "test-skill")
	}
	if result.Content != "# Test\n\nHello world." {
		t.Errorf("Content = %q, want expected content", result.Content)
	}
}

func TestSkillLookupNotFound(t *testing.T) {
	allSkills := []skills.Skill{
		{Name: "existing", Description: "Exists", Content: "content", RequireBins: []string{"echo"}},
	}

	fn := SkillLookup(allSkills)
	result, err := fn(nil, SkillLookupArgs{Name: "nonexistent"})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Error("Expected error for nonexistent skill")
	}
	if !containsStr(result.Error, "existing") {
		t.Errorf("Error should list available skills, got: %s", result.Error)
	}
}

func TestSkillLookupEmptyName(t *testing.T) {
	fn := SkillLookup(nil)
	result, err := fn(nil, SkillLookupArgs{Name: ""})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Error("Expected error for empty name")
	}
}

func TestSkillLookupIneligibleReturnsWithMissingReqs(t *testing.T) {
	allSkills := []skills.Skill{
		{Name: "missing-bin", Description: "Missing", Content: "content", RequireBins: []string{"nonexistent_xyz123"}},
	}

	fn := SkillLookup(allSkills)
	result, err := fn(nil, SkillLookupArgs{Name: "missing-bin"})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Errorf("Ineligible skill should be found, not return error: %s", result.Error)
	}
	if result.Name != "missing-bin" {
		t.Errorf("Name = %q, want %q", result.Name, "missing-bin")
	}
	if result.Content != "content" {
		t.Errorf("Content = %q, want %q", result.Content, "content")
	}
	if len(result.MissingRequirements) == 0 {
		t.Error("Expected MissingRequirements to be populated for ineligible skill")
	}
	if !containsStr(result.MissingRequirements[0], "nonexistent_xyz123") {
		t.Errorf("MissingRequirements should mention missing binary, got: %v", result.MissingRequirements)
	}
}

func TestNewSkillLookupTool(t *testing.T) {
	allSkills := []skills.Skill{
		{Name: "test", Description: "Test", Content: "content", RequireBins: []string{"echo"}},
	}

	toolInst, err := NewSkillLookupTool(allSkills)
	if err != nil {
		t.Fatalf("NewSkillLookupTool failed: %v", err)
	}
	if toolInst.Name() != "skill_lookup" {
		t.Errorf("Name = %q, want %q", toolInst.Name(), "skill_lookup")
	}
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
