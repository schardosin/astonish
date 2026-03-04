package fleet

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// FleetPlan represents a user-customized process derived from a base fleet.
// Custom fleet plans capture the user's preferred workflow (phases, instructions,
// deliverables, preferences) and are reusable across different tasks of the
// same type. Multiple plans can reference the same base fleet.
//
// Plans are stored in ~/.config/astonish/fleet_plans/.
type FleetPlan struct {
	BaseFleet   string              `yaml:"base_fleet" json:"base_fleet"`
	Name        string              `yaml:"name" json:"name"`
	Description string              `yaml:"description,omitempty" json:"description,omitempty"`
	Phases      []FleetPlanPhase    `yaml:"phases" json:"phases"`
	Reviews     map[string][]string `yaml:"reviews,omitempty" json:"reviews,omitempty"`
	Preferences string              `yaml:"preferences,omitempty" json:"preferences,omitempty"`
	Settings    FleetSettings       `yaml:"settings,omitempty" json:"settings,omitempty"`
}

// FleetPlanPhase defines a single phase in a custom fleet plan.
// Every phase has a Primary agent. If Reviewers are present, it becomes a
// multi-agent conversation phase; otherwise it runs as a single-agent phase.
//
// The Agent field is a deprecated alias for Primary (for backward compatibility).
type FleetPlanPhase struct {
	Name         string   `yaml:"name" json:"name"`
	Agent        string   `yaml:"agent,omitempty" json:"agent,omitempty"`         // Deprecated: use Primary instead
	Primary      string   `yaml:"primary,omitempty" json:"primary,omitempty"`     // Agent that executes/produces deliverables
	Reviewers    []string `yaml:"reviewers,omitempty" json:"reviewers,omitempty"` // Optional: agents that review/discuss (enables conversation mode)
	Instructions string   `yaml:"instructions,omitempty" json:"instructions,omitempty"`
	Deliverables []string `yaml:"deliverables,omitempty" json:"deliverables,omitempty"`
	DependsOn    []string `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
}

// NormalizePrimary copies Agent to Primary if Primary is empty (backward compat).
func (p *FleetPlanPhase) NormalizePrimary() {
	if p.Primary == "" && p.Agent != "" {
		p.Primary = p.Agent
	}
}

// IsConversation returns true if this phase uses the multi-agent conversation model.
func (p *FleetPlanPhase) IsConversation() bool {
	return len(p.Reviewers) > 0
}

// GetPrimaryAgent returns the primary agent key for this phase.
func (p *FleetPlanPhase) GetPrimaryAgent() string {
	if p.Primary != "" {
		return p.Primary
	}
	return p.Agent // backward compat fallback
}

// Validate checks that the fleet plan is internally consistent.
// It does not validate base_fleet or agent references (that requires the fleet registry).
func (p *FleetPlan) Validate() error {
	if strings.TrimSpace(p.BaseFleet) == "" {
		return fmt.Errorf("fleet plan: base_fleet is required")
	}
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("fleet plan: name is required")
	}
	if len(p.Phases) == 0 {
		return fmt.Errorf("fleet plan %q: at least one phase is required", p.Name)
	}

	phaseNames := make(map[string]bool)
	for i := range p.Phases {
		phase := &p.Phases[i]

		// Normalize deprecated Agent -> Primary
		phase.NormalizePrimary()

		if strings.TrimSpace(phase.Name) == "" {
			return fmt.Errorf("fleet plan %q: phase %d has no name", p.Name, i+1)
		}

		// Primary is required for all phases
		if strings.TrimSpace(phase.Primary) == "" {
			return fmt.Errorf("fleet plan %q phase %q: primary agent is required", p.Name, phase.Name)
		}

		// Validate reviewers if present (just check non-empty strings; agent existence
		// is checked separately by ValidateAgentRefs)

		if phaseNames[phase.Name] {
			return fmt.Errorf("fleet plan %q: duplicate phase name %q", p.Name, phase.Name)
		}
		phaseNames[phase.Name] = true

		// Validate depends_on references
		for _, dep := range phase.DependsOn {
			if !phaseNames[dep] {
				return fmt.Errorf("fleet plan %q phase %q: depends_on references unknown or later phase %q", p.Name, phase.Name, dep)
			}
		}
	}

	// Validate review references
	for reviewer, targets := range p.Reviews {
		if !phaseNames[reviewer] {
			return fmt.Errorf("fleet plan %q reviews: reviewer %q is not a defined phase", p.Name, reviewer)
		}
		for _, target := range targets {
			if !phaseNames[target] {
				return fmt.Errorf("fleet plan %q reviews: target %q is not a defined phase", p.Name, target)
			}
		}
	}

	return nil
}

// ValidateAgentRefs checks that all agent references in the plan
// correspond to agents defined in the base fleet.
func (p *FleetPlan) ValidateAgentRefs(agentExists func(key string) bool) error {
	for _, phase := range p.Phases {
		primary := phase.GetPrimaryAgent()
		if primary != "" && !agentExists(primary) {
			return fmt.Errorf("fleet plan %q phase %q: primary agent %q not found in base fleet %q",
				p.Name, phase.Name, primary, p.BaseFleet)
		}
		for _, r := range phase.Reviewers {
			if !agentExists(r) {
				return fmt.Errorf("fleet plan %q phase %q: reviewer agent %q not found in base fleet %q",
					p.Name, phase.Name, r, p.BaseFleet)
			}
		}
	}
	return nil
}

// LoadFleetPlan reads and parses a single fleet plan YAML file.
func LoadFleetPlan(path string) (*FleetPlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading fleet plan file %s: %w", path, err)
	}

	var plan FleetPlan
	if err := yaml.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("parsing fleet plan file %s: %w", path, err)
	}

	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("validating fleet plan file %s: %w", path, err)
	}

	return &plan, nil
}

// LoadFleetPlans reads all .yaml/.yml files from the given directory
// and returns a map keyed by filename stem.
func LoadFleetPlans(dir string) (map[string]*FleetPlan, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading fleet plans directory %s: %w", dir, err)
	}

	plans := make(map[string]*FleetPlan)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		path := filepath.Join(dir, name)
		plan, err := LoadFleetPlan(path)
		if err != nil {
			return nil, err
		}

		key := strings.TrimSuffix(name, filepath.Ext(name))
		plans[key] = plan
	}

	return plans, nil
}

// LoadFleetPlansForFleet returns all custom plans that reference the given base fleet key.
func LoadFleetPlansForFleet(dir string, fleetKey string) ([]*FleetPlan, error) {
	allPlans, err := LoadFleetPlans(dir)
	if err != nil {
		return nil, err
	}

	var matching []*FleetPlan
	for _, plan := range allPlans {
		if plan.BaseFleet == fleetKey {
			matching = append(matching, plan)
		}
	}
	return matching, nil
}

// SaveFleetPlan writes a fleet plan to a YAML file.
// The key is used as the filename (without extension).
func SaveFleetPlan(dir string, key string, plan *FleetPlan) error {
	if err := plan.Validate(); err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating fleet plans directory: %w", err)
	}

	data, err := yaml.Marshal(plan)
	if err != nil {
		return fmt.Errorf("marshalling fleet plan %q: %w", plan.Name, err)
	}

	path := filepath.Join(dir, key+".yaml")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing fleet plan file %s: %w", path, err)
	}

	return nil
}

// DeleteFleetPlan removes a fleet plan YAML file from the directory.
func DeleteFleetPlan(dir string, key string) error {
	path := filepath.Join(dir, key+".yaml")
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("fleet plan %q not found", key)
		}
		return fmt.Errorf("deleting fleet plan %q: %w", key, err)
	}
	return nil
}
