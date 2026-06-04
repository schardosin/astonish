package entstore

import (
	"context"
	"fmt"

	"gopkg.in/yaml.v3"

	teament "github.com/schardosin/astonish/ent/team"
	"github.com/schardosin/astonish/ent/team/flow"
	"github.com/schardosin/astonish/pkg/store"
)

// teamFlowStore implements store.FlowStore using the Ent team client.
type teamFlowStore struct {
	client *teament.Client
}

var _ store.FlowStore = (*teamFlowStore)(nil)

func (s *teamFlowStore) ListAllFlows(ctx context.Context) []store.FlowSummary {
	flows, err := s.client.Flow.Query().
		Order(flow.ByName()).
		All(ctx)
	if err != nil {
		return nil
	}
	return flowsToSummaries(flows)
}

func (s *teamFlowStore) ListFlowsByType(ctx context.Context, types []string) []store.FlowSummary {
	if len(types) == 0 {
		return nil
	}
	flows, err := s.client.Flow.Query().
		Where(flow.TypeIn(types...)).
		Order(flow.ByName()).
		All(ctx)
	if err != nil {
		return nil
	}
	return flowsToSummaries(flows)
}

func (s *teamFlowStore) GetFlow(ctx context.Context, name string) (string, error) {
	f, err := s.client.Flow.Query().
		Where(flow.NameEQ(name)).
		Only(ctx)
	if err != nil {
		if teament.IsNotFound(err) {
			return "", fmt.Errorf("flow %q not found", name)
		}
		return "", fmt.Errorf("entstore: FlowStore.GetFlow: %w", err)
	}

	// Prefer yaml_content if available.
	if f.YamlContent != nil && *f.YamlContent != "" {
		return *f.YamlContent, nil
	}

	// Fall back to marshaling definition.
	data, err := yaml.Marshal(f.Definition)
	if err != nil {
		return "", fmt.Errorf("entstore: FlowStore.GetFlow: marshal definition: %w", err)
	}
	return string(data), nil
}

func (s *teamFlowStore) SaveFlow(ctx context.Context, name string, yamlContent string) error {
	// Parse YAML to extract definition and metadata.
	var definition map[string]any
	if err := yaml.Unmarshal([]byte(yamlContent), &definition); err != nil {
		definition = map[string]any{"name": name}
	}

	flowType := ""
	if t, ok := definition["type"].(string); ok {
		flowType = t
	}

	// Try update first.
	n, err := s.client.Flow.Update().
		Where(flow.NameEQ(name)).
		SetDefinition(definition).
		SetYamlContent(yamlContent).
		SetType(flowType).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("entstore: FlowStore.SaveFlow: update: %w", err)
	}
	if n == 0 {
		_, err = s.client.Flow.Create().
			SetName(name).
			SetDefinition(definition).
			SetYamlContent(yamlContent).
			SetType(flowType).
			Save(ctx)
		if err != nil {
			return fmt.Errorf("entstore: FlowStore.SaveFlow: create: %w", err)
		}
	}
	return nil
}

func (s *teamFlowStore) DeleteFlow(ctx context.Context, name string) error {
	_, err := s.client.Flow.Delete().
		Where(flow.NameEQ(name)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("entstore: FlowStore.DeleteFlow: %w", err)
	}
	return nil
}

func (s *teamFlowStore) GetTaps(_ context.Context) []store.FlowTap {
	// Taps are a file-system concept; not applicable in DB-backed mode.
	return nil
}

func (s *teamFlowStore) AddTap(_ context.Context, _ string, _ string) (string, error) {
	return "", fmt.Errorf("entstore: taps are not supported in database-backed mode")
}

func (s *teamFlowStore) RemoveTap(_ context.Context, _ string) error {
	return fmt.Errorf("entstore: taps are not supported in database-backed mode")
}

func (s *teamFlowStore) GetStoreDir(_ context.Context) string {
	return ""
}

// flowsToSummaries converts Ent Flow entities to FlowSummary DTOs.
func flowsToSummaries(flows []*teament.Flow) []store.FlowSummary {
	summaries := make([]store.FlowSummary, 0, len(flows))
	for _, f := range flows {
		summary := store.FlowSummary{
			Name:      f.Name,
			Type:      f.Type,
			Installed: true,
		}
		if desc, ok := f.Definition["description"].(string); ok {
			summary.Description = desc
		}
		if tags, ok := f.Definition["tags"].([]any); ok {
			for _, t := range tags {
				if s, ok := t.(string); ok {
					summary.Tags = append(summary.Tags, s)
				}
			}
		}
		if suite, ok := f.Definition["suite"].(string); ok {
			summary.Suite = suite
		}
		summaries = append(summaries, summary)
	}
	return summaries
}
