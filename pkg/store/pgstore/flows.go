package pgstore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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

func (f *pgFlowStore) ListAllFlows() []store.FlowSummary {
	ctx := context.Background()
	rows, err := f.pool.Query(ctx, fmt.Sprintf(
		`SELECT name, definition FROM %s ORDER BY name`, f.tableName()),
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var flows []store.FlowSummary
	for rows.Next() {
		var name string
		var defJSON []byte
		if err := rows.Scan(&name, &defJSON); err != nil {
			continue
		}

		var def map[string]any
		_ = json.Unmarshal(defJSON, &def)

		desc, _ := def["description"].(string)
		flows = append(flows, store.FlowSummary{
			Name:        name,
			Description: desc,
			Installed:   true, // PG-stored flows are always "installed"
		})
	}
	return flows
}

func (f *pgFlowStore) GetTaps() []store.FlowTap {
	// PG mode doesn't use taps — flows are stored directly in the database
	return nil
}

func (f *pgFlowStore) AddTap(_ string, _ string) (string, error) {
	return "", fmt.Errorf("taps are not supported in platform mode; flows are stored in the database")
}

func (f *pgFlowStore) RemoveTap(_ string) error {
	return fmt.Errorf("taps are not supported in platform mode")
}

func (f *pgFlowStore) GetStoreDir() string {
	// PG mode doesn't use a local store directory
	return ""
}

// SaveFlow inserts or updates a flow in the database.
// Used by the migration engine to import flows from file-based storage.
func (f *pgFlowStore) SaveFlow(ctx context.Context, name string, definition any) error {
	defJSON, err := json.Marshal(definition)
	if err != nil {
		return fmt.Errorf("failed to marshal flow definition: %w", err)
	}

	_, err = f.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (name, definition, created_at, updated_at)
		 VALUES ($1, $2, now(), now())
		 ON CONFLICT (name) DO UPDATE SET definition = $2, updated_at = now()`,
		f.tableName()),
		name, defJSON,
	)
	return err
}
