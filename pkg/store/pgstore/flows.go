package pgstore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"

	"github.com/schardosin/astonish/pkg/store"
)

// pgFlowStore implements store.FlowStore for PostgreSQL.
type pgFlowStore struct {
	pool   *pgxpool.Pool
	schema string
}

func (f *pgFlowStore) tableName() string {
	return pgx.Identifier{f.schema, "flows"}.Sanitize()
}

func (f *pgFlowStore) ListAllFlows(ctx context.Context) []store.FlowSummary {
	rows, err := f.pool.Query(ctx, fmt.Sprintf(
		`SELECT name, type, definition, yaml_content FROM %s WHERE type = '' ORDER BY name`, f.tableName()),
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var flows []store.FlowSummary
	for rows.Next() {
		var name, flowType string
		var defJSON []byte
		var yamlContent *string
		if err := rows.Scan(&name, &flowType, &defJSON, &yamlContent); err != nil {
			continue
		}

		var def map[string]any
		_ = json.Unmarshal(defJSON, &def)

		desc, _ := def["description"].(string)
		suite, _ := def["suite"].(string)

		var tags []string
		if rawTags, ok := def["tags"]; ok {
			if arr, ok := rawTags.([]any); ok {
				for _, t := range arr {
					if s, ok := t.(string); ok {
						tags = append(tags, s)
					}
				}
			}
		}

		flows = append(flows, store.FlowSummary{
			Name:        name,
			Description: desc,
			Type:        flowType,
			Suite:       suite,
			Tags:        tags,
			Installed:   true,
		})
	}
	return flows
}

func (f *pgFlowStore) ListFlowsByType(ctx context.Context, types []string) []store.FlowSummary {
	if len(types) == 0 {
		return nil
	}
	rows, err := f.pool.Query(ctx, fmt.Sprintf(
		`SELECT name, type, definition, yaml_content FROM %s WHERE type = ANY($1) ORDER BY name`,
		f.tableName()),
		types,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var flows []store.FlowSummary
	for rows.Next() {
		var name, flowType string
		var defJSON []byte
		var yamlContent *string
		if err := rows.Scan(&name, &flowType, &defJSON, &yamlContent); err != nil {
			continue
		}

		var def map[string]any
		_ = json.Unmarshal(defJSON, &def)

		desc, _ := def["description"].(string)
		suite, _ := def["suite"].(string)

		var tags []string
		if rawTags, ok := def["tags"]; ok {
			if arr, ok := rawTags.([]any); ok {
				for _, t := range arr {
					if s, ok := t.(string); ok {
						tags = append(tags, s)
					}
				}
			}
		}

		flows = append(flows, store.FlowSummary{
			Name:        name,
			Description: desc,
			Type:        flowType,
			Suite:       suite,
			Tags:        tags,
			Installed:   true,
		})
	}
	return flows
}

func (f *pgFlowStore) GetFlow(ctx context.Context, name string) (string, error) {
	var yamlContent *string
	var defJSON []byte
	err := f.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT yaml_content, definition FROM %s WHERE name = $1`, f.tableName()),
		name,
	).Scan(&yamlContent, &defJSON)
	if err != nil {
		return "", fmt.Errorf("flow %q not found: %w", name, err)
	}

	// Prefer raw YAML if available; fall back to pretty-printing the JSON definition.
	if yamlContent != nil && *yamlContent != "" {
		return *yamlContent, nil
	}

	// Fallback: re-serialize the JSONB definition as indented JSON.
	var pretty json.RawMessage
	if err := json.Unmarshal(defJSON, &pretty); err == nil {
		indented, _ := json.MarshalIndent(pretty, "", "  ")
		return string(indented), nil
	}
	return string(defJSON), nil
}

func (f *pgFlowStore) SaveFlow(ctx context.Context, name string, yamlContent string) error {
	// Parse YAML to extract type and a JSON definition for the JSONB column.
	defJSON := []byte(`{}`)
	flowType := ""
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(yamlContent), &parsed); err == nil {
		if t, ok := parsed["type"].(string); ok {
			flowType = t
		}
		jsonBytes, err := json.Marshal(parsed)
		if err == nil {
			defJSON = jsonBytes
		}
	}

	_, err := f.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (name, type, definition, yaml_content, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, now(), now())
		 ON CONFLICT (name) DO UPDATE SET type = $2, definition = $3, yaml_content = $4, updated_at = now()`,
		f.tableName()),
		name, flowType, defJSON, yamlContent,
	)
	return err
}

func (f *pgFlowStore) DeleteFlow(ctx context.Context, name string) error {
	tag, err := f.pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE name = $1`, f.tableName()),
		name,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("flow %q not found", name)
	}
	return nil
}

// SaveFlowDefinition inserts or updates a flow using a structured definition.
// Used by the migration engine to import flows from file-based storage.
func (f *pgFlowStore) SaveFlowDefinition(ctx context.Context, name string, definition any, yamlContent string) error {
	defJSON, err := json.Marshal(definition)
	if err != nil {
		return fmt.Errorf("failed to marshal flow definition: %w", err)
	}

	// Extract type from the definition map.
	flowType := ""
	if m, ok := definition.(map[string]any); ok {
		if t, ok := m["type"].(string); ok {
			flowType = t
		}
	}

	_, err = f.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (name, type, definition, yaml_content, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, now(), now())
		 ON CONFLICT (name) DO UPDATE SET type = $2, definition = $3, yaml_content = $4, updated_at = now()`,
		f.tableName()),
		name, flowType, defJSON, nilIfEmpty(yamlContent),
	)
	return err
}

func (f *pgFlowStore) GetTaps(ctx context.Context) []store.FlowTap {
	// PG mode doesn't use taps — flows are stored directly in the database
	return nil
}

func (f *pgFlowStore) AddTap(ctx context.Context, _ string, _ string) (string, error) {
	return "", fmt.Errorf("taps are not supported in platform mode; flows are stored in the database")
}

func (f *pgFlowStore) RemoveTap(ctx context.Context, _ string) error {
	return fmt.Errorf("taps are not supported in platform mode")
}

func (f *pgFlowStore) GetStoreDir(ctx context.Context) string {
	// PG mode doesn't use a local store directory
	return ""
}
