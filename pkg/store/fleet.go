package store

import "context"

// FleetTemplateSummary is a summary of a fleet template.
type FleetTemplateSummary struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	AgentCount  int      `json:"agent_count"`
	AgentNames  []string `json:"agent_names"`
	// Source is "bundled" for Astonish-shipped templates or "custom" for DB-backed ones.
	// Optional; set by ListFleets implementations that distinguish the two.
	Source string `json:"source,omitempty"`
}

// FleetTemplateStore manages fleet template definitions.
//
// In personal mode, this wraps the existing fleet.Registry.
// In platform mode, templates are stored in the team's schema.
type FleetTemplateStore interface {
	// GetFleet returns a fleet config by key.
	// The returned value is the concrete *fleet.FleetConfig.
	GetFleet(ctx context.Context, key string) (any, bool)

	// ListFleets returns summaries of all fleet templates.
	ListFleets(ctx context.Context) []FleetTemplateSummary

	// Save persists a fleet template.
	Save(ctx context.Context, key string, fleet any) error

	// Delete removes a fleet template.
	Delete(ctx context.Context, key string) error

	// Count returns the number of fleet templates.
	Count(ctx context.Context) int

	// Reload re-reads fleet templates from the backing store.
	Reload(ctx context.Context) error
}

// FleetPlanSummary is a summary of a fleet plan.
type FleetPlanSummary struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	CreatedFrom string   `json:"created_from,omitempty"`
	ChannelType string   `json:"channel_type"`
	AgentCount  int      `json:"agent_count"`
	AgentNames  []string `json:"agent_names"`
}

// FleetPlanStore manages fleet plan definitions.
//
// In personal mode, this wraps the existing fleet.PlanRegistry.
// In platform mode, plans are stored in the team's schema.
type FleetPlanStore interface {
	// GetPlan returns a fleet plan by key.
	// The returned value is the concrete *fleet.FleetPlan.
	GetPlan(ctx context.Context, key string) (any, bool)

	// ListPlans returns summaries of all fleet plans.
	ListPlans(ctx context.Context) []FleetPlanSummary

	// Save persists a fleet plan.
	Save(ctx context.Context, plan any) error

	// Delete removes a fleet plan.
	Delete(ctx context.Context, key string) error

	// Count returns the number of fleet plans.
	Count(ctx context.Context) int

	// Reload re-reads fleet plans from the backing store.
	Reload(ctx context.Context) error

	// GetPlanYAML returns the raw YAML content for a fleet plan.
	// Returns the YAML string and nil error, or empty string and error if not found.
	GetPlanYAML(ctx context.Context, key string) (string, error)

	// SavePlanYAML persists a fleet plan from raw YAML content.
	// The YAML is parsed, validated, and stored.
	SavePlanYAML(ctx context.Context, key string, yamlContent string) error
}

// FleetSetupProfileSummary is a list entry for setup profiles.
type FleetSetupProfileSummary struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Domain      string `json:"domain,omitempty"`
	StepCount   int    `json:"step_count"`
	Source      string `json:"source,omitempty"`
}

// FleetSetupProfileStore manages reusable fleet setup profile definitions.
type FleetSetupProfileStore interface {
	GetProfile(ctx context.Context, key string) (any, bool)
	ListProfiles(ctx context.Context) []FleetSetupProfileSummary
	Save(ctx context.Context, key string, profile any) error
	Delete(ctx context.Context, key string) error
}

// FleetSetupDraft holds in-progress setup collected values.
type FleetSetupDraft struct {
	ID              string         `json:"id"`
	TemplateKey     string         `json:"template_key"`
	SetupProfileKey string         `json:"setup_profile_key"`
	Collected       map[string]any `json:"collected"`
	CurrentStep     string         `json:"current_step,omitempty"`
	CreatedAt       string         `json:"created_at,omitempty"`
	UpdatedAt       string         `json:"updated_at,omitempty"`
}

// FleetSetupDraftStore persists in-progress plan setup state.
type FleetSetupDraftStore interface {
	Create(ctx context.Context, draft *FleetSetupDraft) error
	Get(ctx context.Context, id string) (*FleetSetupDraft, bool)
	Update(ctx context.Context, draft *FleetSetupDraft) error
	Delete(ctx context.Context, id string) error
}
