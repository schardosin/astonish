package pgstore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/store"
)

// pgFleetTemplateStore implements store.FleetTemplateStore for PostgreSQL.
type pgFleetTemplateStore struct {
	pool   *pgxpool.Pool
	schema string
}

func (f *pgFleetTemplateStore) tableName() string {
	return pgx.Identifier{f.schema, "fleet_templates"}.Sanitize()
}

func (f *pgFleetTemplateStore) GetFleet(key string) (any, bool) {
	ctx := context.Background()
	row := f.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT definition FROM %s WHERE key = $1`, f.tableName()),
		key,
	)

	var defJSON []byte
	if err := row.Scan(&defJSON); err != nil {
		return nil, false
	}

	var def any
	if err := json.Unmarshal(defJSON, &def); err != nil {
		return nil, false
	}
	return def, true
}

func (f *pgFleetTemplateStore) ListFleets() []store.FleetTemplateSummary {
	ctx := context.Background()
	rows, err := f.pool.Query(ctx, fmt.Sprintf(
		`SELECT key, name, definition FROM %s ORDER BY name`, f.tableName()),
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var templates []store.FleetTemplateSummary
	for rows.Next() {
		var key, name string
		var defJSON []byte
		if err := rows.Scan(&key, &name, &defJSON); err != nil {
			continue
		}

		var def map[string]any
		_ = json.Unmarshal(defJSON, &def)

		desc, _ := def["description"].(string)
		agentCount := 0
		var agentNames []string
		if agents, ok := def["agents"].([]any); ok {
			agentCount = len(agents)
			for _, a := range agents {
				if am, ok := a.(map[string]any); ok {
					if n, ok := am["name"].(string); ok {
						agentNames = append(agentNames, n)
					}
				}
			}
		}

		templates = append(templates, store.FleetTemplateSummary{
			Key:         key,
			Name:        name,
			Description: desc,
			AgentCount:  agentCount,
			AgentNames:  agentNames,
		})
	}
	return templates
}

func (f *pgFleetTemplateStore) Save(key string, fleet any) error {
	ctx := context.Background()
	defJSON, err := json.Marshal(fleet)
	if err != nil {
		return err
	}

	name := key
	if m, ok := fleet.(map[string]any); ok {
		if n, ok := m["name"].(string); ok && n != "" {
			name = n
		}
	}

	_, err = f.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (key, name, definition, updated_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (key) DO UPDATE SET name = $2, definition = $3, updated_at = now()`,
		f.tableName()),
		key, name, defJSON,
	)
	return err
}

func (f *pgFleetTemplateStore) Delete(key string) error {
	ctx := context.Background()
	_, err := f.pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE key = $1`, f.tableName()),
		key,
	)
	return err
}

func (f *pgFleetTemplateStore) Count() int {
	ctx := context.Background()
	var count int
	err := f.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT count(*) FROM %s`, f.tableName()),
	).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

func (f *pgFleetTemplateStore) Reload() error {
	// No-op for PG store — data is always read fresh from the database
	return nil
}

// --- pgFleetPlanStore implements store.FleetPlanStore ---

type pgFleetPlanStore struct {
	pool   *pgxpool.Pool
	schema string
}

func (f *pgFleetPlanStore) tableName() string {
	return pgx.Identifier{f.schema, "fleet_plans"}.Sanitize()
}

func (f *pgFleetPlanStore) GetPlan(key string) (any, bool) {
	ctx := context.Background()
	row := f.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT definition FROM %s WHERE key = $1`, f.tableName()),
		key,
	)

	var defJSON []byte
	if err := row.Scan(&defJSON); err != nil {
		return nil, false
	}

	var def any
	if err := json.Unmarshal(defJSON, &def); err != nil {
		return nil, false
	}
	return def, true
}

func (f *pgFleetPlanStore) ListPlans() []store.FleetPlanSummary {
	ctx := context.Background()
	rows, err := f.pool.Query(ctx, fmt.Sprintf(
		`SELECT key, name, active, definition FROM %s ORDER BY name`, f.tableName()),
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var plans []store.FleetPlanSummary
	for rows.Next() {
		var key, name string
		var active bool
		var defJSON []byte
		if err := rows.Scan(&key, &name, &active, &defJSON); err != nil {
			continue
		}

		var def map[string]any
		_ = json.Unmarshal(defJSON, &def)
		desc, _ := def["description"].(string)

		plans = append(plans, store.FleetPlanSummary{
			Key:         key,
			Name:        name,
			Description: desc,
		})
	}
	return plans
}

func (f *pgFleetPlanStore) Save(plan any) error {
	ctx := context.Background()
	defJSON, err := json.Marshal(plan)
	if err != nil {
		return err
	}

	// Extract key and name from the plan
	key := ""
	name := ""
	if m, ok := plan.(map[string]any); ok {
		if k, ok := m["key"].(string); ok {
			key = k
		}
		if n, ok := m["name"].(string); ok {
			name = n
		}
	}
	if key == "" {
		return fmt.Errorf("fleet plan must have a key")
	}
	if name == "" {
		name = key
	}

	_, err = f.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (key, name, definition, updated_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (key) DO UPDATE SET name = $2, definition = $3, updated_at = now()`,
		f.tableName()),
		key, name, defJSON,
	)
	return err
}

func (f *pgFleetPlanStore) Delete(key string) error {
	ctx := context.Background()
	_, err := f.pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE key = $1`, f.tableName()),
		key,
	)
	return err
}

func (f *pgFleetPlanStore) Count() int {
	ctx := context.Background()
	var count int
	err := f.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT count(*) FROM %s`, f.tableName()),
	).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

func (f *pgFleetPlanStore) Reload() error {
	// No-op for PG store — data is always read fresh from the database
	return nil
}

func (f *pgFleetPlanStore) GetPlanYAML(key string) (string, error) {
	ctx := context.Background()
	var yamlContent *string
	var defJSON []byte
	err := f.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT yaml_content, definition FROM %s WHERE key = $1`, f.tableName()),
		key,
	).Scan(&yamlContent, &defJSON)
	if err != nil {
		return "", fmt.Errorf("fleet plan %q not found: %w", key, err)
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

func (f *pgFleetPlanStore) SavePlanYAML(key string, yamlContent string) error {
	ctx := context.Background()

	// Parse YAML to extract a JSON definition for the JSONB column.
	defJSON := []byte(`{}`)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(yamlContent), &parsed); err == nil {
		defJSON, _ = json.Marshal(parsed)
	}

	// Extract name from parsed definition
	name := key
	if n, ok := parsed["name"].(string); ok && n != "" {
		name = n
	}

	_, err := f.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (key, name, definition, yaml_content, updated_at)
		 VALUES ($1, $2, $3, $4, now())
		 ON CONFLICT (key) DO UPDATE SET name = $2, definition = $3, yaml_content = $4, updated_at = now()`,
		f.tableName()),
		key, name, defJSON, yamlContent,
	)
	return err
}
