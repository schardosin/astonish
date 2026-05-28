package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/schardosin/astonish/pkg/store"
)

// sqliteFlowStore implements store.FlowStore.
type sqliteFlowStore struct {
	db *sql.DB
}

func (s *sqliteFlowStore) ListAllFlows(ctx context.Context) []store.FlowSummary {
	rows, err := s.db.QueryContext(ctx,
		`SELECT name, definition, yaml_content, type FROM flows ORDER BY name`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var flows []store.FlowSummary
	for rows.Next() {
		var name string
		var definition, yamlContent, flowType sql.NullString
		if err := rows.Scan(&name, &definition, &yamlContent, &flowType); err != nil {
			continue
		}
		f := store.FlowSummary{
			Name: name,
			Type: flowType.String,
		}
		// Try to extract description from definition JSON
		if definition.Valid && definition.String != "" {
			var def map[string]interface{}
			if json.Unmarshal([]byte(definition.String), &def) == nil {
				if desc, ok := def["description"].(string); ok {
					f.Description = desc
				}
			}
		}
		// Fallback: extract from yaml_content (used by SaveFlow in platform mode)
		if f.Description == "" && yamlContent.Valid && yamlContent.String != "" {
			f.Description = extractDescriptionFromYAML(yamlContent.String)
		}
		flows = append(flows, f)
	}
	return flows
}

func (s *sqliteFlowStore) ListFlowsByType(ctx context.Context, types []string) []store.FlowSummary {
	if len(types) == 0 {
		return nil
	}

	query := `SELECT name, definition, yaml_content, type FROM flows WHERE type IN (`
	args := make([]interface{}, len(types))
	for i, t := range types {
		if i > 0 {
			query += ","
		}
		query += "?"
		args[i] = t
	}
	query += `) ORDER BY name`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var flows []store.FlowSummary
	for rows.Next() {
		var name string
		var definition, yamlContent, flowType sql.NullString
		if err := rows.Scan(&name, &definition, &yamlContent, &flowType); err != nil {
			continue
		}
		fs := store.FlowSummary{
			Name: name,
			Type: flowType.String,
		}
		// Try definition JSON first, then fallback to yaml_content
		if definition.Valid && definition.String != "" {
			var def map[string]interface{}
			if json.Unmarshal([]byte(definition.String), &def) == nil {
				if desc, ok := def["description"].(string); ok {
					fs.Description = desc
				}
			}
		}
		if fs.Description == "" && yamlContent.Valid && yamlContent.String != "" {
			fs.Description = extractDescriptionFromYAML(yamlContent.String)
		}
		flows = append(flows, fs)
	}
	return flows
}

func (s *sqliteFlowStore) GetFlow(ctx context.Context, name string) (string, error) {
	var yamlContent sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT yaml_content FROM flows WHERE name = ?`, name).Scan(&yamlContent)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("flow %q not found", name)
	}
	if err != nil {
		return "", err
	}
	return yamlContent.String, nil
}

func (s *sqliteFlowStore) SaveFlow(ctx context.Context, name string, yamlContent string) error {
	id := uuid.New().String()
	now := formatTime(time.Now())

	// Try to parse YAML to extract definition and type
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO flows (id, name, yaml_content, type, created_at, updated_at)
		 VALUES (?, ?, ?, 'flow', ?, ?)
		 ON CONFLICT(name) DO UPDATE SET yaml_content = excluded.yaml_content, type = COALESCE(excluded.type, type), updated_at = excluded.updated_at`,
		id, name, yamlContent, now, now)
	return err
}

func (s *sqliteFlowStore) DeleteFlow(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM flows WHERE name = ?`, name)
	return err
}

func (s *sqliteFlowStore) GetTaps(_ context.Context) []store.FlowTap {
	// SQLite store doesn't support taps (remote repositories).
	// Taps are a file-system concept. In SQLite mode, all flows are stored in the database.
	return nil
}

func (s *sqliteFlowStore) AddTap(_ context.Context, _ string, _ string) (string, error) {
	return "", fmt.Errorf("taps are not supported in SQLite mode; save flows directly")
}

func (s *sqliteFlowStore) RemoveTap(_ context.Context, _ string) error {
	return fmt.Errorf("taps are not supported in SQLite mode")
}

func (s *sqliteFlowStore) GetStoreDir(_ context.Context) string {
	return "" // No directory concept in SQLite mode
}

// extractDescriptionFromYAML parses top-level YAML and returns the description field if present.
func extractDescriptionFromYAML(yamlContent string) string {
	if yamlContent == "" {
		return ""
	}
	var m map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlContent), &m); err != nil {
		return ""
	}
	if desc, ok := m["description"].(string); ok {
		return desc
	}
	return ""
}
