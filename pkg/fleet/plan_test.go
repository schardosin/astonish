package fleet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validPlanYAML = `base_fleet: software-dev
name: "My Dev Process"
description: "Custom development workflow"
phases:
  - name: requirements
    agent: po
    instructions: |
      Focus on acceptance criteria.
    deliverables:
      - Requirements document
    depends_on: []
  - name: design
    agent: architect
    instructions: |
      Use mermaid diagrams.
    deliverables:
      - Technical design
    depends_on: [requirements]
  - name: implementation
    agent: dev
    instructions: |
      Follow existing patterns.
    deliverables:
      - Working code
      - Passing tests
    depends_on: [requirements, design]
reviews:
  design: [requirements]
  implementation: [requirements, design]
preferences: |
  Skip security review for small projects.
settings:
  max_reviews_per_phase: 2
`

func TestLoadFleetPlan_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "my-dev.yaml")
	if err := os.WriteFile(path, []byte(validPlanYAML), 0644); err != nil {
		t.Fatal(err)
		return
	}

	plan, err := LoadFleetPlan(path)
	if err != nil {
		t.Fatalf("LoadFleetPlan failed: %v", err)
		return
	}

	if plan.BaseFleet != "software-dev" {
		t.Errorf("BaseFleet = %q, want %q", plan.BaseFleet, "software-dev")
	}
	if plan.Name != "My Dev Process" {
		t.Errorf("Name = %q, want %q", plan.Name, "My Dev Process")
	}
	if len(plan.Phases) != 3 {
		t.Errorf("Phases count = %d, want 3", len(plan.Phases))
	}
	if plan.Phases[0].Name != "requirements" {
		t.Errorf("Phase[0].Name = %q, want %q", plan.Phases[0].Name, "requirements")
	}
	if plan.Phases[0].Agent != "po" {
		t.Errorf("Phase[0].Agent = %q, want %q", plan.Phases[0].Agent, "po")
	}
	if len(plan.Phases[0].Deliverables) != 1 {
		t.Errorf("Phase[0].Deliverables count = %d, want 1", len(plan.Phases[0].Deliverables))
	}
	if len(plan.Phases[1].DependsOn) != 1 || plan.Phases[1].DependsOn[0] != "requirements" {
		t.Errorf("Phase[1].DependsOn = %v, want [requirements]", plan.Phases[1].DependsOn)
	}
	if len(plan.Phases[2].DependsOn) != 2 {
		t.Errorf("Phase[2].DependsOn count = %d, want 2", len(plan.Phases[2].DependsOn))
	}
	if len(plan.Reviews) != 2 {
		t.Errorf("Reviews count = %d, want 2", len(plan.Reviews))
	}
	if !strings.Contains(plan.Preferences, "Skip security") {
		t.Errorf("Preferences should contain 'Skip security', got %q", plan.Preferences)
	}
	if plan.Settings.MaxReviewsPerPhase != 2 {
		t.Errorf("Settings.MaxReviewsPerPhase = %d, want 2", plan.Settings.MaxReviewsPerPhase)
	}
}

func TestFleetPlan_Validate_MissingBaseFleet(t *testing.T) {
	plan := &FleetPlan{
		Name:   "test",
		Phases: []FleetPlanPhase{{Name: "a", Agent: "dev"}},
	}
	err := plan.Validate()
	if err == nil || !strings.Contains(err.Error(), "base_fleet is required") {
		t.Errorf("expected base_fleet error, got: %v", err)
	}
}

func TestFleetPlan_Validate_MissingName(t *testing.T) {
	plan := &FleetPlan{
		BaseFleet: "software-dev",
		Phases:    []FleetPlanPhase{{Name: "a", Agent: "dev"}},
	}
	err := plan.Validate()
	if err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Errorf("expected name error, got: %v", err)
	}
}

func TestFleetPlan_Validate_NoPhases(t *testing.T) {
	plan := &FleetPlan{
		BaseFleet: "software-dev",
		Name:      "test",
		Phases:    []FleetPlanPhase{},
	}
	err := plan.Validate()
	if err == nil || !strings.Contains(err.Error(), "at least one phase") {
		t.Errorf("expected phases error, got: %v", err)
	}
}

func TestFleetPlan_Validate_MissingPhaseName(t *testing.T) {
	plan := &FleetPlan{
		BaseFleet: "software-dev",
		Name:      "test",
		Phases:    []FleetPlanPhase{{Agent: "dev"}},
	}
	err := plan.Validate()
	if err == nil || !strings.Contains(err.Error(), "has no name") {
		t.Errorf("expected phase name error, got: %v", err)
	}
}

func TestFleetPlan_Validate_MissingPhaseAgent(t *testing.T) {
	plan := &FleetPlan{
		BaseFleet: "software-dev",
		Name:      "test",
		Phases:    []FleetPlanPhase{{Name: "impl"}},
	}
	err := plan.Validate()
	if err == nil || !strings.Contains(err.Error(), "primary agent is required") {
		t.Errorf("expected primary agent error, got: %v", err)
	}
}

func TestFleetPlan_Validate_DuplicatePhaseNames(t *testing.T) {
	plan := &FleetPlan{
		BaseFleet: "software-dev",
		Name:      "test",
		Phases: []FleetPlanPhase{
			{Name: "impl", Agent: "dev"},
			{Name: "impl", Agent: "qa"},
		},
	}
	err := plan.Validate()
	if err == nil || !strings.Contains(err.Error(), "duplicate phase name") {
		t.Errorf("expected duplicate phase error, got: %v", err)
	}
}

func TestFleetPlan_Validate_InvalidDependsOn(t *testing.T) {
	plan := &FleetPlan{
		BaseFleet: "software-dev",
		Name:      "test",
		Phases: []FleetPlanPhase{
			{Name: "impl", Agent: "dev", DependsOn: []string{"nonexistent"}},
		},
	}
	err := plan.Validate()
	if err == nil || !strings.Contains(err.Error(), "unknown or later phase") {
		t.Errorf("expected depends_on error, got: %v", err)
	}
}

func TestFleetPlan_Validate_DependsOnLaterPhase(t *testing.T) {
	plan := &FleetPlan{
		BaseFleet: "software-dev",
		Name:      "test",
		Phases: []FleetPlanPhase{
			{Name: "impl", Agent: "dev", DependsOn: []string{"testing"}},
			{Name: "testing", Agent: "qa"},
		},
	}
	err := plan.Validate()
	if err == nil || !strings.Contains(err.Error(), "unknown or later phase") {
		t.Errorf("expected forward dependency error, got: %v", err)
	}
}

func TestFleetPlan_Validate_InvalidReviewReviewer(t *testing.T) {
	plan := &FleetPlan{
		BaseFleet: "software-dev",
		Name:      "test",
		Phases:    []FleetPlanPhase{{Name: "impl", Agent: "dev"}},
		Reviews:   map[string][]string{"nonexistent": {"impl"}},
	}
	err := plan.Validate()
	if err == nil || !strings.Contains(err.Error(), "reviewer") {
		t.Errorf("expected reviewer error, got: %v", err)
	}
}

func TestFleetPlan_Validate_InvalidReviewTarget(t *testing.T) {
	plan := &FleetPlan{
		BaseFleet: "software-dev",
		Name:      "test",
		Phases:    []FleetPlanPhase{{Name: "impl", Agent: "dev"}},
		Reviews:   map[string][]string{"impl": {"nonexistent"}},
	}
	err := plan.Validate()
	if err == nil || !strings.Contains(err.Error(), "target") {
		t.Errorf("expected review target error, got: %v", err)
	}
}

func TestFleetPlan_ValidateAgentRefs(t *testing.T) {
	plan := &FleetPlan{
		BaseFleet: "software-dev",
		Name:      "test",
		Phases: []FleetPlanPhase{
			{Name: "impl", Agent: "dev"},
			{Name: "testing", Agent: "qa"},
		},
	}

	// All agents exist
	err := plan.ValidateAgentRefs(func(key string) bool {
		return key == "dev" || key == "qa"
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Missing agent
	err = plan.ValidateAgentRefs(func(key string) bool {
		return key == "dev"
	})
	if err == nil || !strings.Contains(err.Error(), "agent \"qa\" not found") {
		t.Errorf("expected agent not found error, got: %v", err)
	}
}

func TestFleetPlanPhase_ConversationValidation(t *testing.T) {
	// Valid conversation phase (primary + reviewers)
	plan := &FleetPlan{
		BaseFleet: "software-dev",
		Name:      "test",
		Phases: []FleetPlanPhase{
			{Name: "design", Primary: "architect", Reviewers: []string{"po"}},
		},
	}
	if err := plan.Validate(); err != nil {
		t.Errorf("expected valid conversation phase, got error: %v", err)
	}

	// Primary without reviewers is now valid (single-agent mode)
	plan2 := &FleetPlan{
		BaseFleet: "software-dev",
		Name:      "test",
		Phases: []FleetPlanPhase{
			{Name: "design", Primary: "architect"},
		},
	}
	if err := plan2.Validate(); err != nil {
		t.Errorf("expected valid single-agent phase with primary, got error: %v", err)
	}

	// Mixed phases (single-agent + conversation)
	plan3 := &FleetPlan{
		BaseFleet: "software-dev",
		Name:      "test",
		Phases: []FleetPlanPhase{
			{Name: "impl", Agent: "dev"},
			{Name: "design", Primary: "architect", Reviewers: []string{"po"}},
		},
	}
	if err := plan3.Validate(); err != nil {
		t.Errorf("expected valid mixed phases, got error: %v", err)
	}
}

func TestFleetPlanPhase_ConversationAgentRefs(t *testing.T) {
	plan := &FleetPlan{
		BaseFleet: "software-dev",
		Name:      "test",
		Phases: []FleetPlanPhase{
			{Name: "design", Primary: "architect", Reviewers: []string{"po", "dev"}},
		},
	}

	// All agents exist
	err := plan.ValidateAgentRefs(func(key string) bool {
		return key == "architect" || key == "po" || key == "dev"
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Missing primary
	err = plan.ValidateAgentRefs(func(key string) bool {
		return key == "po" || key == "dev"
	})
	if err == nil || !strings.Contains(err.Error(), "primary agent \"architect\" not found") {
		t.Errorf("expected primary agent not found error, got: %v", err)
	}

	// Missing reviewer
	err = plan.ValidateAgentRefs(func(key string) bool {
		return key == "architect" || key == "po"
	})
	if err == nil || !strings.Contains(err.Error(), "reviewer agent \"dev\" not found") {
		t.Errorf("expected reviewer agent not found error, got: %v", err)
	}
}

func TestFleetPlanPhase_IsConversation(t *testing.T) {
	// Single-agent via Agent field (backward compat)
	singleAgent := FleetPlanPhase{Name: "impl", Agent: "dev"}
	if singleAgent.IsConversation() {
		t.Error("expected single-agent phase (Agent field) not to be a conversation")
	}
	singleAgent.NormalizePrimary()
	if singleAgent.GetPrimaryAgent() != "dev" {
		t.Errorf("expected GetPrimaryAgent() = %q, got %q", "dev", singleAgent.GetPrimaryAgent())
	}

	// Single-agent via Primary field (no reviewers)
	singlePrimary := FleetPlanPhase{Name: "impl", Primary: "dev"}
	if singlePrimary.IsConversation() {
		t.Error("expected single-agent phase (Primary, no reviewers) not to be a conversation")
	}
	if singlePrimary.GetPrimaryAgent() != "dev" {
		t.Errorf("expected GetPrimaryAgent() = %q, got %q", "dev", singlePrimary.GetPrimaryAgent())
	}

	// Conversation phase (Primary + Reviewers)
	conv := FleetPlanPhase{Name: "design", Primary: "architect", Reviewers: []string{"po"}}
	if !conv.IsConversation() {
		t.Error("expected conversation phase to be identified as conversation")
	}
	if conv.GetPrimaryAgent() != "architect" {
		t.Errorf("expected GetPrimaryAgent() = %q, got %q", "architect", conv.GetPrimaryAgent())
	}
}

func TestLoadFleetPlans(t *testing.T) {
	dir := t.TempDir()

	// Write two plan files
	plan1 := `base_fleet: software-dev
name: "Plan One"
phases:
  - name: impl
    agent: dev
`
	plan2 := `base_fleet: software-dev
name: "Plan Two"
phases:
  - name: design
    agent: architect
  - name: impl
    agent: dev
    depends_on: [design]
`
	if err := os.WriteFile(filepath.Join(dir, "plan-one.yaml"), []byte(plan1), 0644); err != nil {
		t.Fatal(err)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "plan-two.yaml"), []byte(plan2), 0644); err != nil {
		t.Fatal(err)
		return
	}

	plans, err := LoadFleetPlans(dir)
	if err != nil {
		t.Fatalf("LoadFleetPlans failed: %v", err)
		return
	}

	if len(plans) != 2 {
		t.Fatalf("expected 2 plans, got %d", len(plans))
		return
	}
	if plans["plan-one"] == nil || plans["plan-one"].Name != "Plan One" {
		t.Error("plan-one not loaded correctly")
	}
	if plans["plan-two"] == nil || plans["plan-two"].Name != "Plan Two" {
		t.Error("plan-two not loaded correctly")
	}
}

func TestLoadFleetPlans_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	plans, err := LoadFleetPlans(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
		return
	}
	if len(plans) != 0 {
		t.Errorf("expected 0 plans, got %d", len(plans))
	}
}

func TestLoadFleetPlans_NonexistentDir(t *testing.T) {
	plans, err := LoadFleetPlans("/nonexistent/path")
	if err != nil {
		t.Fatalf("unexpected error for nonexistent dir: %v", err)
		return
	}
	if plans != nil {
		t.Errorf("expected nil plans, got %v", plans)
	}
}

func TestLoadFleetPlansForFleet(t *testing.T) {
	dir := t.TempDir()

	plan1 := `base_fleet: software-dev
name: "Dev Plan"
phases:
  - name: impl
    agent: dev
`
	plan2 := `base_fleet: research
name: "Research Plan"
phases:
  - name: research
    agent: researcher
`
	if err := os.WriteFile(filepath.Join(dir, "dev.yaml"), []byte(plan1), 0644); err != nil {
		t.Fatal(err)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "research.yaml"), []byte(plan2), 0644); err != nil {
		t.Fatal(err)
		return
	}

	matching, err := LoadFleetPlansForFleet(dir, "software-dev")
	if err != nil {
		t.Fatalf("LoadFleetPlansForFleet failed: %v", err)
		return
	}
	if len(matching) != 1 {
		t.Fatalf("expected 1 matching plan, got %d", len(matching))
		return
	}
	if matching[0].Name != "Dev Plan" {
		t.Errorf("expected 'Dev Plan', got %q", matching[0].Name)
	}
}

func TestSaveFleetPlan(t *testing.T) {
	dir := t.TempDir()
	plan := &FleetPlan{
		BaseFleet:   "software-dev",
		Name:        "Saved Plan",
		Description: "A test plan",
		Phases: []FleetPlanPhase{
			{Name: "impl", Agent: "dev", Instructions: "Do the work."},
		},
		Preferences: "Be thorough.",
		Settings:    FleetSettings{MaxReviewsPerPhase: 3},
	}

	err := SaveFleetPlan(dir, "saved-plan", plan)
	if err != nil {
		t.Fatalf("SaveFleetPlan failed: %v", err)
		return
	}

	// Verify file was written
	path := filepath.Join(dir, "saved-plan.yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected file to exist")
		return
	}

	// Load it back
	loaded, err := LoadFleetPlan(path)
	if err != nil {
		t.Fatalf("LoadFleetPlan of saved plan failed: %v", err)
		return
	}
	if loaded.Name != "Saved Plan" {
		t.Errorf("Name = %q, want %q", loaded.Name, "Saved Plan")
	}
	if loaded.BaseFleet != "software-dev" {
		t.Errorf("BaseFleet = %q, want %q", loaded.BaseFleet, "software-dev")
	}
	if len(loaded.Phases) != 1 {
		t.Errorf("Phases count = %d, want 1", len(loaded.Phases))
	}
	if loaded.Settings.MaxReviewsPerPhase != 3 {
		t.Errorf("Settings.MaxReviewsPerPhase = %d, want 3", loaded.Settings.MaxReviewsPerPhase)
	}
}

func TestSaveFleetPlan_InvalidPlan(t *testing.T) {
	dir := t.TempDir()
	plan := &FleetPlan{
		Name: "No Base Fleet",
	}
	err := SaveFleetPlan(dir, "invalid", plan)
	if err == nil || !strings.Contains(err.Error(), "base_fleet is required") {
		t.Errorf("expected validation error, got: %v", err)
	}
}

func TestDeleteFleetPlan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "to-delete.yaml")
	if err := os.WriteFile(path, []byte("base_fleet: x\nname: x\nphases:\n  - name: a\n    agent: b\n"), 0644); err != nil {
		t.Fatal(err)
		return
	}

	err := DeleteFleetPlan(dir, "to-delete")
	if err != nil {
		t.Fatalf("DeleteFleetPlan failed: %v", err)
		return
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected file to be deleted")
	}
}

func TestDeleteFleetPlan_NotFound(t *testing.T) {
	dir := t.TempDir()
	err := DeleteFleetPlan(dir, "nonexistent")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found error, got: %v", err)
	}
}
