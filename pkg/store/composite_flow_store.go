package store

import "context"

// compositeFlowStore wraps a personal and team FlowStore.
// Reads resolve personal-first with team fallback.
// Writes (SaveFlow) always go to the personal store.
// Taps are team-scoped and delegated to the team store.
type compositeFlowStore struct {
	personal FlowStore
	team     FlowStore
}

// NewCompositeFlowStore creates a FlowStore that resolves personal-first, team-fallback.
// Writes (SaveFlow) always go to the personal store.
// If either store is nil, the other is returned directly (no wrapping).
func NewCompositeFlowStore(personal, team FlowStore) FlowStore {
	if personal == nil {
		return team
	}
	if team == nil {
		return personal
	}
	return &compositeFlowStore{personal: personal, team: team}
}

func (c *compositeFlowStore) GetFlow(ctx context.Context, name string) (string, error) {
	// Try personal first. Also check for empty content — some store
	// implementations return ("", nil) for not-found instead of an error.
	yaml, err := c.personal.GetFlow(ctx, name)
	if err == nil && yaml != "" {
		return yaml, nil
	}
	return c.team.GetFlow(ctx, name)
}

func (c *compositeFlowStore) SaveFlow(ctx context.Context, name string, yamlContent string) error {
	return c.personal.SaveFlow(ctx, name, yamlContent)
}

func (c *compositeFlowStore) DeleteFlow(ctx context.Context, name string) error {
	// Try personal first; if not found there, try team.
	err := c.personal.DeleteFlow(ctx, name)
	if err == nil {
		return nil
	}
	return c.team.DeleteFlow(ctx, name)
}

func (c *compositeFlowStore) ListAllFlows(ctx context.Context) []FlowSummary {
	var merged []FlowSummary

	personalFlows := c.personal.ListAllFlows(ctx)
	for i := range personalFlows {
		if personalFlows[i].Scope == "" {
			personalFlows[i].Scope = "personal"
		}
	}
	merged = append(merged, personalFlows...)

	// Build a set of personal flow names to deduplicate.
	personalNames := make(map[string]struct{}, len(personalFlows))
	for _, f := range personalFlows {
		personalNames[f.Name] = struct{}{}
	}

	teamFlows := c.team.ListAllFlows(ctx)
	for i := range teamFlows {
		// Skip team flows that have a personal override.
		if _, exists := personalNames[teamFlows[i].Name]; exists {
			continue
		}
		if teamFlows[i].Scope == "" {
			teamFlows[i].Scope = "team"
		}
		merged = append(merged, teamFlows[i])
	}

	return merged
}

func (c *compositeFlowStore) ListFlowsByType(ctx context.Context, types []string) []FlowSummary {
	var merged []FlowSummary

	personalFlows := c.personal.ListFlowsByType(ctx, types)
	for i := range personalFlows {
		if personalFlows[i].Scope == "" {
			personalFlows[i].Scope = "personal"
		}
	}
	merged = append(merged, personalFlows...)

	// Build a set of personal flow names to deduplicate.
	personalNames := make(map[string]struct{}, len(personalFlows))
	for _, f := range personalFlows {
		personalNames[f.Name] = struct{}{}
	}

	teamFlows := c.team.ListFlowsByType(ctx, types)
	for i := range teamFlows {
		if _, exists := personalNames[teamFlows[i].Name]; exists {
			continue
		}
		if teamFlows[i].Scope == "" {
			teamFlows[i].Scope = "team"
		}
		merged = append(merged, teamFlows[i])
	}

	return merged
}

// Taps are team-scoped — delegate to team store.

func (c *compositeFlowStore) GetTaps(ctx context.Context) []FlowTap {
	return c.team.GetTaps(ctx)
}

func (c *compositeFlowStore) AddTap(ctx context.Context, urlOrShorthand string, alias string) (string, error) {
	return c.team.AddTap(ctx, urlOrShorthand, alias)
}

func (c *compositeFlowStore) RemoveTap(ctx context.Context, name string) error {
	return c.team.RemoveTap(ctx, name)
}

// GetStoreDir returns the personal store dir (where saves go).
func (c *compositeFlowStore) GetStoreDir(ctx context.Context) string {
	return c.personal.GetStoreDir(ctx)
}
