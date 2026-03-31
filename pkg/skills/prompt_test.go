package skills

import (
	"strings"
	"testing"
)

func TestBuildSkillIndexEmpty(t *testing.T) {
	result := BuildSkillIndex(nil)
	if result != "" {
		t.Errorf("Expected empty string for nil skills, got %q", result)
	}
}

func TestBuildSkillIndexNoEligible(t *testing.T) {
	allSkills := []Skill{
		{Name: "missing", Description: "Missing", RequireBins: []string{"nonexistent_xyz123"}},
	}
	result := BuildSkillIndex(allSkills)
	// Ineligible skills should still appear for discoverability
	if result == "" {
		t.Error("Expected non-empty index even with only ineligible skills")
	}
	if !strings.Contains(result, "missing") {
		t.Error("Ineligible skill should appear in index")
	}
	if !strings.Contains(result, "setup required") {
		t.Error("Ineligible skill should be marked with setup required")
	}
}

func TestBuildSkillIndex(t *testing.T) {
	allSkills := []Skill{
		{Name: "echo-tool", Description: "Echo operations", RequireBins: []string{"echo"}},
	}
	result := BuildSkillIndex(allSkills)

	if !strings.Contains(result, "## Available Skills") {
		t.Error("Missing header")
	}
	if !strings.Contains(result, "echo-tool") {
		t.Error("Missing skill name")
	}
	if !strings.Contains(result, "Echo operations") {
		t.Error("Missing skill description")
	}
	if !strings.Contains(result, "skill_lookup") {
		t.Error("Missing skill_lookup tool reference")
	}
}

func TestBuildSkillIndexMultiple(t *testing.T) {
	allSkills := []Skill{
		{Name: "alpha", Description: "Alpha tool", RequireBins: []string{"echo"}},
		{Name: "beta", Description: "Beta tool", RequireBins: []string{"echo"}},
		{Name: "missing", Description: "Missing", RequireBins: []string{"nonexistent_xyz123"}},
	}
	result := BuildSkillIndex(allSkills)

	if !strings.Contains(result, "alpha") {
		t.Error("Missing alpha skill")
	}
	if !strings.Contains(result, "beta") {
		t.Error("Missing beta skill")
	}
	// Ineligible skills should appear with setup-required marker
	if !strings.Contains(result, "missing") {
		t.Error("Ineligible skill should appear in index for discoverability")
	}
	if !strings.Contains(result, "setup required") {
		t.Error("Ineligible skill should be marked with setup required")
	}
}
