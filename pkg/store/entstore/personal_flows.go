package entstore

import (
	"context"
	"fmt"

	"gopkg.in/yaml.v3"

	personalent "github.com/schardosin/astonish/ent/personal"
	"github.com/schardosin/astonish/ent/personal/flow"
	"github.com/schardosin/astonish/pkg/store"
)

// personalFlowStore implements store.FlowStore for personal scope.
type personalFlowStore struct {
	client *personalent.Client
}

var _ store.FlowStore = (*personalFlowStore)(nil)

func (fs *personalFlowStore) ListAllFlows(ctx context.Context) []store.FlowSummary {
	ents, err := fs.client.Flow.Query().
		Where(flow.TypeNotIn("drill", "drill_suite", "test", "test_suite")).
		Order(flow.ByName()).
		All(ctx)
	if err != nil {
		return nil
	}

	summaries := make([]store.FlowSummary, len(ents))
	for i, e := range ents {
		summaries[i] = entPersonalFlowToSummary(e)
	}
	return summaries
}

func (fs *personalFlowStore) ListFlowsByType(ctx context.Context, types []string) []store.FlowSummary {
	q := fs.client.Flow.Query()
	if len(types) > 0 {
		q = q.Where(flow.TypeIn(types...))
	}
	q = q.Order(flow.ByName())

	ents, err := q.All(ctx)
	if err != nil {
		return nil
	}

	summaries := make([]store.FlowSummary, len(ents))
	for i, e := range ents {
		summaries[i] = entPersonalFlowToSummary(e)
	}
	return summaries
}

func (fs *personalFlowStore) GetFlow(ctx context.Context, name string) (string, error) {
	ent, err := fs.client.Flow.Query().
		Where(flow.NameEQ(name)).
		Only(ctx)
	if err != nil {
		if personalent.IsNotFound(err) {
			return "", fmt.Errorf("flow %q not found", name)
		}
		return "", err
	}

	// Return yaml_content if available, otherwise serialize definition.
	if ent.YamlContent != nil {
		return *ent.YamlContent, nil
	}
	return "", nil
}

func (fs *personalFlowStore) SaveFlow(ctx context.Context, name string, yamlContent string) error {
	// Parse YAML to extract metadata (description, type) for listing.
	var def map[string]any
	_ = yaml.Unmarshal([]byte(yamlContent), &def)

	flowType := ""
	if t, ok := def["type"].(string); ok {
		flowType = t
	}

	// Check if flow exists.
	existing, err := fs.client.Flow.Query().
		Where(flow.NameEQ(name)).
		Only(ctx)
	if err != nil && !personalent.IsNotFound(err) {
		return err
	}

	if existing != nil {
		update := existing.Update().
			SetYamlContent(yamlContent)
		if def != nil {
			update.SetDefinition(def)
		}
		if flowType != "" {
			update.SetType(flowType)
		}
		return update.Exec(ctx)
	}

	create := fs.client.Flow.Create().
		SetName(name).
		SetYamlContent(yamlContent)
	if def != nil {
		create.SetDefinition(def)
	}
	if flowType != "" {
		create.SetType(flowType)
	}
	_, err = create.Save(ctx)
	return err
}

func (fs *personalFlowStore) DeleteFlow(ctx context.Context, name string) error {
	_, err := fs.client.Flow.Delete().
		Where(flow.NameEQ(name)).
		Exec(ctx)
	return err
}

func (fs *personalFlowStore) GetTaps(ctx context.Context) []store.FlowTap {
	// Personal flow store does not manage taps in the DB.
	return nil
}

func (fs *personalFlowStore) AddTap(ctx context.Context, urlOrShorthand string, alias string) (string, error) {
	return "", errNotImpl
}

func (fs *personalFlowStore) RemoveTap(ctx context.Context, name string) error {
	return errNotImpl
}

func (fs *personalFlowStore) GetStoreDir(ctx context.Context) string {
	return ""
}

func entPersonalFlowToSummary(e *personalent.Flow) store.FlowSummary {
	s := store.FlowSummary{
		Name:      e.Name,
		Type:      e.Type,
		Scope:     "personal",
		Installed: true,
	}
	if e.Definition != nil {
		if desc, ok := e.Definition["description"].(string); ok {
			s.Description = desc
		}
		if suite, ok := e.Definition["suite"].(string); ok {
			s.Suite = suite
		}
		if tags, ok := e.Definition["tags"].([]any); ok {
			for _, t := range tags {
				if tag, ok := t.(string); ok {
					s.Tags = append(s.Tags, tag)
				}
			}
		}
	}
	return s
}
