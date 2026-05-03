package store

// FleetTemplateSummary is a summary of a fleet template.
type FleetTemplateSummary struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	AgentCount  int      `json:"agent_count"`
	AgentNames  []string `json:"agent_names"`
}

// FleetTemplateStore manages fleet template definitions.
//
// In personal mode, this wraps the existing fleet.Registry.
// In platform mode, templates are stored in the team's schema.
type FleetTemplateStore interface {
	// GetFleet returns a fleet config by key.
	// The returned value is the concrete *fleet.FleetConfig.
	GetFleet(key string) (any, bool)

	// ListFleets returns summaries of all fleet templates.
	ListFleets() []FleetTemplateSummary

	// Save persists a fleet template.
	Save(key string, fleet any) error

	// Delete removes a fleet template.
	Delete(key string) error

	// Count returns the number of fleet templates.
	Count() int

	// Reload re-reads fleet templates from the backing store.
	Reload() error
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
	GetPlan(key string) (any, bool)

	// ListPlans returns summaries of all fleet plans.
	ListPlans() []FleetPlanSummary

	// Save persists a fleet plan.
	Save(plan any) error

	// Delete removes a fleet plan.
	Delete(key string) error

	// Count returns the number of fleet plans.
	Count() int

	// Reload re-reads fleet plans from the backing store.
	Reload() error

	// GetPlanYAML returns the raw YAML content for a fleet plan.
	// Returns the YAML string and nil error, or empty string and error if not found.
	GetPlanYAML(key string) (string, error)

	// SavePlanYAML persists a fleet plan from raw YAML content.
	// The YAML is parsed, validated, and stored.
	SavePlanYAML(key string, yamlContent string) error
}
