package fleet

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultSetupProfileKey is used when a template does not specify setup_profile.
	DefaultSetupProfileKey = "generic"
)

// SetupProfile defines reusable plan-creation steps and field schema for a domain.
type SetupProfile struct {
	Key              string                    `yaml:"key" json:"key"`
	Name             string                    `yaml:"name" json:"name"`
	Description      string                    `yaml:"description,omitempty" json:"description,omitempty"`
	Domain           string                    `yaml:"domain,omitempty" json:"domain,omitempty"`
	PinnedToolGroups []string                  `yaml:"pinned_tool_groups,omitempty" json:"pinned_tool_groups,omitempty"`
	IntroPrompt      string                    `yaml:"intro_prompt,omitempty" json:"intro_prompt,omitempty"`
	WizardPrompt     string                    `yaml:"wizard_prompt,omitempty" json:"wizard_prompt,omitempty"` // deprecated: use step prompt fields
	ChannelTypes     map[string]ChannelTypeDef `yaml:"channel_types,omitempty" json:"channel_types,omitempty"`
	Steps            []SetupStep               `yaml:"steps" json:"steps"`
}

// SetupStep is one logical stage in plan setup.
type SetupStep struct {
	ID               string         `yaml:"id" json:"id"`
	Title            string         `yaml:"title" json:"title"`
	Type             string         `yaml:"type" json:"type"`
	Icon             string         `yaml:"icon,omitempty" json:"icon,omitempty"`
	Summary          string         `yaml:"summary,omitempty" json:"summary,omitempty"`
	Required         bool           `yaml:"required,omitempty" json:"required,omitempty"`
	When             string         `yaml:"when,omitempty" json:"when,omitempty"`
	Fields           []SetupField   `yaml:"fields,omitempty" json:"fields,omitempty"`
	Defaults         map[string]any `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	Provisioner      string         `yaml:"provisioner,omitempty" json:"provisioner,omitempty"`
	Inputs           []SetupBinding `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	Outputs          []SetupBinding `yaml:"outputs,omitempty" json:"outputs,omitempty"`
	Prompt           string         `yaml:"prompt,omitempty" json:"prompt,omitempty"`
	Content          string         `yaml:"content,omitempty" json:"content,omitempty"`
	Guidance         string         `yaml:"guidance,omitempty" json:"guidance,omitempty"` // deprecated: use prompt
	PinnedToolGroups []string       `yaml:"pinned_tool_groups,omitempty" json:"pinned_tool_groups,omitempty"`
	Tools            []string       `yaml:"tools,omitempty" json:"tools,omitempty"`
}

// SetupField describes a single captured value within a step.
type SetupField struct {
	ID       string              `yaml:"id" json:"id"`
	Label    string              `yaml:"label" json:"label"`
	Type     string              `yaml:"type" json:"type"`
	Required bool                `yaml:"required,omitempty" json:"required,omitempty"`
	When     string              `yaml:"when,omitempty" json:"when,omitempty"`
	Options  []SetupFieldOption  `yaml:"options,omitempty" json:"options,omitempty"`
	MapsTo   string              `yaml:"maps_to" json:"maps_to"`
	Default  any                 `yaml:"default,omitempty" json:"default,omitempty"`
	Hint     string              `yaml:"hint,omitempty" json:"hint,omitempty"`
}

// SetupFieldOption is one choice for enum fields.
type SetupFieldOption struct {
	Value string `yaml:"value" json:"value"`
	Label string `yaml:"label" json:"label"`
}

// SetupBinding maps a profile input/output to a collected or plan path.
type SetupBinding struct {
	From string `yaml:"from,omitempty" json:"from,omitempty"`
	To   string `yaml:"to" json:"to"`
}

// SetupCollected holds user-provided values keyed by step ID, then field ID.
type SetupCollected map[string]map[string]any

// SetupProfileSummary is a list entry for setup profiles.
type SetupProfileSummary struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Domain      string `json:"domain,omitempty"`
	StepCount   int    `json:"step_count"`
	Source      string `json:"source,omitempty"`
}

// ProfileForTemplate returns the setup profile key for a fleet template.
func ProfileForTemplate(cfg *FleetConfig) string {
	if cfg == nil {
		return DefaultSetupProfileKey
	}
	key := strings.TrimSpace(cfg.SetupProfileKey)
	if key != "" {
		return key
	}
	if cfg.PlanWizard != nil && strings.TrimSpace(cfg.PlanWizard.SystemPrompt) != "" {
		return DefaultSetupProfileKey
	}
	return DefaultSetupProfileKey
}

// ParseSetupProfileYAML parses a setup profile from YAML bytes.
func ParseSetupProfileYAML(data []byte) (*SetupProfile, error) {
	var profile SetupProfile
	if err := yaml.Unmarshal(data, &profile); err != nil {
		return nil, fmt.Errorf("parsing setup profile: %w", err)
	}
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	return &profile, nil
}

// Validate checks a setup profile definition.
func (p *SetupProfile) Validate() error {
	if p == nil {
		return fmt.Errorf("setup profile is nil")
	}
	if strings.TrimSpace(p.Key) == "" {
		return fmt.Errorf("setup profile key is required")
	}
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("setup profile name is required")
	}
	if len(p.Steps) == 0 {
		return fmt.Errorf("setup profile %q has no steps", p.Key)
	}
	seen := map[string]struct{}{}
	for i, step := range p.Steps {
		id := strings.TrimSpace(step.ID)
		if id == "" {
			return fmt.Errorf("step %d: id is required", i+1)
		}
		if _, dup := seen[id]; dup {
			return fmt.Errorf("duplicate step id %q", id)
		}
		seen[id] = struct{}{}
		if strings.TrimSpace(step.Title) == "" {
			return fmt.Errorf("step %q: title is required", id)
		}
		if strings.TrimSpace(step.Type) == "" {
			return fmt.Errorf("step %q: type is required", id)
		}
	}
	return nil
}

// CloneSetupProfile returns a deep copy of a setup profile with a new key and name.
func CloneSetupProfile(src *SetupProfile, newKey, newName string) (*SetupProfile, error) {
	if src == nil {
		return nil, fmt.Errorf("source profile is required")
	}
	newKey = strings.TrimSpace(newKey)
	if newKey == "" {
		return nil, fmt.Errorf("new key is required")
	}
	data, err := yaml.Marshal(src)
	if err != nil {
		return nil, fmt.Errorf("clone setup profile: %w", err)
	}
	clone, err := ParseSetupProfileYAML(data)
	if err != nil {
		return nil, err
	}
	clone.Key = newKey
	if strings.TrimSpace(newName) != "" {
		clone.Name = strings.TrimSpace(newName)
	} else if clone.Name != "" {
		clone.Name = clone.Name + " Copy"
	} else {
		clone.Name = newKey
	}
	return clone, nil
}

// SetupProfileToYAML serializes a setup profile to YAML bytes.
func SetupProfileToYAML(p *SetupProfile) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("setup profile is nil")
	}
	return yaml.Marshal(p)
}

// DefaultSetupProfileTemplate returns a minimal custom profile scaffold.
func DefaultSetupProfileTemplate(key, name string) *SetupProfile {
	return &SetupProfile{
		Key:  key,
		Name: name,
		Description: "Custom setup profile",
		Domain: "custom",
		PinnedToolGroups: []string{"fleet", "credentials"},
		Steps: []SetupStep{
			{
				ID: "identity", Title: "Plan identity", Type: "form", Required: true,
				Fields: []SetupField{
					{ID: "key", Label: "Plan key", Type: "string", Required: true, MapsTo: "plan.key"},
					{ID: "name", Label: "Plan name", Type: "string", Required: true, MapsTo: "plan.name"},
					{ID: "description", Label: "Description", Type: "string", MapsTo: "plan.description"},
				},
			},
			{
				ID: "channel", Title: "Communication channel", Type: "form", Required: true,
				Fields: []SetupField{
					{ID: "type", Label: "Channel type", Type: "enum", Required: true, MapsTo: "plan.channel.type", Default: "chat",
						Options: []SetupFieldOption{
							{Value: "chat", Label: "Chat (manual start)"},
							{Value: "github_issues", Label: "GitHub Issues"},
						}},
				},
			},
			{ID: "review", Title: "Review and save", Type: "review", Required: true,
				Prompt: "Call validate_fleet_plan then save_fleet_plan with all collected configuration.",
				Tools:  []string{"validate_fleet_plan", "save_fleet_plan", "update_setup_draft"},
			},
		},
		IntroPrompt: "Guide the user through plan identity, channel configuration, then validate and save the fleet plan.",
	}
}

// StepByID returns the step with the given ID.
func (p *SetupProfile) StepByID(id string) (*SetupStep, bool) {
	for i := range p.Steps {
		if p.Steps[i].ID == id {
			return &p.Steps[i], true
		}
	}
	return nil, false
}
