package pgstore

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/fleet"
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

func (f *pgFleetTemplateStore) GetFleet(ctx context.Context, key string) (any, bool) {
	row := f.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT definition FROM %s WHERE key = $1`, f.tableName()),
		key,
	)

	var defJSON []byte
	if err := row.Scan(&defJSON); err != nil {
		return nil, false
	}

	var cfg fleet.FleetConfig
	if err := json.Unmarshal(defJSON, &cfg); err != nil {
		return nil, false
	}
	return &cfg, true
}

func (f *pgFleetTemplateStore) ListFleets(ctx context.Context) []store.FleetTemplateSummary {
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
		if agents, ok := def["agents"].(map[string]any); ok {
			for agentKey := range agents {
				agentNames = append(agentNames, agentKey)
			}
			sort.Strings(agentNames)
			agentCount = len(agentNames)
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

func (f *pgFleetTemplateStore) Save(ctx context.Context, key string, fleet any) error {
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

func (f *pgFleetTemplateStore) Delete(ctx context.Context, key string) error {
	_, err := f.pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE key = $1`, f.tableName()),
		key,
	)
	return err
}

func (f *pgFleetTemplateStore) Count(ctx context.Context) int {
	var count int
	err := f.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT count(*) FROM %s`, f.tableName()),
	).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

func (f *pgFleetTemplateStore) Reload(ctx context.Context) error {
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

func (f *pgFleetPlanStore) GetPlan(ctx context.Context, key string) (any, bool) {
	row := f.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT definition, created_by FROM %s WHERE key = $1`, f.tableName()),
		key,
	)

	var defJSON []byte
	var createdBy *string
	if err := row.Scan(&defJSON, &createdBy); err != nil {
		return nil, false
	}

	var plan fleet.FleetPlan
	if err := json.Unmarshal(defJSON, &plan); err != nil {
		return nil, false
	}
	plan.Key = key
	if createdBy != nil {
		plan.CreatedBy = *createdBy
	}
	return &plan, true
}

func (f *pgFleetPlanStore) ListPlans(ctx context.Context) []store.FleetPlanSummary {
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
		createdFrom, _ := def["created_from"].(string)

		channelType := "chat"
		if ch, ok := def["channel"].(map[string]any); ok {
			if ct, ok := ch["type"].(string); ok {
				channelType = ct
			}
		}

		var agentNames []string
		if agents, ok := def["agents"].(map[string]any); ok {
			for agentKey := range agents {
				agentNames = append(agentNames, agentKey)
			}
			sort.Strings(agentNames)
		}

		plans = append(plans, store.FleetPlanSummary{
			Key:         key,
			Name:        name,
			Description: desc,
			CreatedFrom: createdFrom,
			ChannelType: channelType,
			AgentCount:  len(agentNames),
			AgentNames:  agentNames,
		})
	}
	return plans
}

func (f *pgFleetPlanStore) Save(ctx context.Context, plan any) error {
	defJSON, err := json.Marshal(plan)
	if err != nil {
		return err
	}

	// Extract key, name, and created_by from the plan
	key := ""
	name := ""
	var createdBy *string
	if fp, ok := plan.(*fleet.FleetPlan); ok {
		key = fp.Key
		name = fp.Name
		if fp.CreatedBy != "" {
			createdBy = &fp.CreatedBy
		}
	} else if m, ok := plan.(map[string]any); ok {
		if k, ok := m["key"].(string); ok {
			key = k
		}
		if n, ok := m["name"].(string); ok {
			name = n
		}
		if cb, ok := m["created_by"].(string); ok && cb != "" {
			createdBy = &cb
		}
	}
	if key == "" {
		return fmt.Errorf("fleet plan must have a key")
	}
	if name == "" {
		name = key
	}

	_, err = f.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (key, name, definition, created_by, updated_at)
		 VALUES ($1, $2, $3, $4, now())
		 ON CONFLICT (key) DO UPDATE SET name = $2, definition = $3, updated_at = now()`,
		f.tableName()),
		key, name, defJSON, createdBy,
	)
	return err
}

func (f *pgFleetPlanStore) Delete(ctx context.Context, key string) error {
	_, err := f.pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE key = $1`, f.tableName()),
		key,
	)
	return err
}

func (f *pgFleetPlanStore) Count(ctx context.Context) int {
	var count int
	err := f.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT count(*) FROM %s`, f.tableName()),
	).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

func (f *pgFleetPlanStore) Reload(ctx context.Context) error {
	// No-op for PG store — data is always read fresh from the database
	return nil
}

func (f *pgFleetPlanStore) GetPlanYAML(ctx context.Context, key string) (string, error) {
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

func (f *pgFleetPlanStore) SavePlanYAML(ctx context.Context, key string, yamlContent string) error {
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

// --- Monitor State Store (DB-backed) ---

// PGMonitorStateStore implements fleet.MonitorStateStore backed by PostgreSQL.
// State is stored in the team's fleet_monitor_state table.
type PGMonitorStateStore struct {
	pool   *pgxpool.Pool
	schema string
}

// NewPGMonitorStateStore creates a DB-backed monitor state store for a team schema.
func NewPGMonitorStateStore(pool *pgxpool.Pool, schema string) *PGMonitorStateStore {
	return &PGMonitorStateStore{pool: pool, schema: schema}
}

func (s *PGMonitorStateStore) tableName() string {
	return pgx.Identifier{s.schema, "fleet_monitor_state"}.Sanitize()
}

func (s *PGMonitorStateStore) LoadState(planKey string) (*fleet.GitHubMonitorState, error) {
	ctx := context.Background()
	var stateJSON []byte
	err := s.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT state FROM %s WHERE plan_key = $1`, s.tableName()),
		planKey,
	).Scan(&stateJSON)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil // no state yet
		}
		return nil, fmt.Errorf("loading monitor state: %w", err)
	}

	var state fleet.GitHubMonitorState
	if err := json.Unmarshal(stateJSON, &state); err != nil {
		return nil, fmt.Errorf("parsing monitor state: %w", err)
	}

	if state.SeenIssues == nil {
		state.SeenIssues = make(map[int]*fleet.SeenIssueState)
	}
	return &state, nil
}

func (s *PGMonitorStateStore) SaveState(planKey string, state *fleet.GitHubMonitorState) error {
	ctx := context.Background()
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshalling monitor state: %w", err)
	}

	_, err = s.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (plan_key, state, updated_at)
		 VALUES ($1, $2, now())
		 ON CONFLICT (plan_key) DO UPDATE SET state = $2, updated_at = now()`,
		s.tableName()),
		planKey, stateJSON,
	)
	if err != nil {
		return fmt.Errorf("saving monitor state: %w", err)
	}
	return nil
}

func (s *PGMonitorStateStore) DeleteState(planKey string) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE plan_key = $1`, s.tableName()),
		planKey,
	)
	if err != nil {
		return fmt.Errorf("deleting monitor state: %w", err)
	}
	return nil
}

// Compile-time check
var _ fleet.MonitorStateStore = (*PGMonitorStateStore)(nil)
