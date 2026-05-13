package pgstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/store"
)

// pgOrgAppStore implements store.AppStore for org-level apps.
// Org apps use a different schema: definition JSONB instead of code TEXT,
// plus promoted_by and promoted_from_team columns.
type pgOrgAppStore struct {
	pool   *pgxpool.Pool
	schema string
}

func (a *pgOrgAppStore) tableName() string {
	return pgx.Identifier{a.schema, "org_apps"}.Sanitize()
}

func (a *pgOrgAppStore) Save(ctx context.Context, app any) (string, error) {
	data, err := json.Marshal(app)
	if err != nil {
		return "", fmt.Errorf("failed to marshal app: %w", err)
	}

	var fields struct {
		Name         string    `json:"name"`
		Description  string    `json:"description"`
		PromotedBy   string    `json:"promotedBy"`
		PromotedFrom string    `json:"promotedFromTeam"`
		CreatedAt    time.Time `json:"createdAt"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return "", fmt.Errorf("failed to extract app fields: %w", err)
	}

	slug := fields.Name
	if slug == "" {
		return "", fmt.Errorf("app name/slug is required")
	}

	_, err = a.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (slug, name, description, definition, promoted_by, promoted_from_team, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		 ON CONFLICT (slug) DO UPDATE SET
		   name = EXCLUDED.name,
		   description = EXCLUDED.description,
		   definition = EXCLUDED.definition,
		   updated_at = now()`,
		a.tableName()),
		slug, slug, fields.Description, data,
		nilIfEmpty(fields.PromotedBy), nilIfEmpty(fields.PromotedFrom),
		coalesceTime(fields.CreatedAt),
	)
	if err != nil {
		return "", err
	}
	return slug, nil
}

func (a *pgOrgAppStore) Load(ctx context.Context, slug string) (any, error) {
	row := a.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT slug, name, description, definition, promoted_by, promoted_from_team, created_at, updated_at
		 FROM %s WHERE slug = $1`, a.tableName()),
		slug,
	)

	var name, desc string
	var definition []byte
	var promotedBy, promotedFrom *string
	var createdAt, updatedAt time.Time
	var slugResult string
	err := row.Scan(&slugResult, &name, &desc, &definition, &promotedBy, &promotedFrom, &createdAt, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("org app not found: %w", err)
	}

	// Parse the definition JSONB back into a map
	var def map[string]any
	if err := json.Unmarshal(definition, &def); err != nil {
		def = map[string]any{}
	}

	result := map[string]any{
		"name":        name,
		"description": desc,
		"createdAt":   createdAt,
		"updatedAt":   updatedAt,
	}
	// Merge definition fields into the result
	for k, v := range def {
		if _, exists := result[k]; !exists {
			result[k] = v
		}
	}
	if promotedBy != nil {
		result["promotedBy"] = *promotedBy
	}
	if promotedFrom != nil {
		result["promotedFromTeam"] = *promotedFrom
	}
	return result, nil
}

func (a *pgOrgAppStore) Delete(ctx context.Context, slug string) error {
	_, err := a.pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE slug = $1`, a.tableName()),
		slug,
	)
	return err
}

func (a *pgOrgAppStore) List(ctx context.Context) ([]store.AppListItem, error) {
	rows, err := a.pool.Query(ctx, fmt.Sprintf(
		`SELECT slug, description, 1 AS version, updated_at
		 FROM %s ORDER BY updated_at DESC`, a.tableName()),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []store.AppListItem
	for rows.Next() {
		var item store.AppListItem
		if err := rows.Scan(&item.Name, &item.Description, &item.Version, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// coalesceTime returns t if non-zero, otherwise now.
func coalesceTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now()
	}
	return t
}
