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

// pgAppStore implements store.AppStore for PostgreSQL.
type pgAppStore struct {
	pool   *pgxpool.Pool
	schema string
	table  string // "apps" for team/personal, "org_apps" for org-level
}

func (a *pgAppStore) tableName() string {
	return pgx.Identifier{a.schema, a.table}.Sanitize()
}

func (a *pgAppStore) Save(ctx context.Context, app any) (string, error) {
	// The app parameter is a map or struct. Extract fields via JSON round-trip.
	data, err := json.Marshal(app)
	if err != nil {
		return "", fmt.Errorf("failed to marshal app: %w", err)
	}
	var fields struct {
		Name        string    `json:"name"`
		Description string    `json:"description"`
		Code        string    `json:"code"`
		Version     int       `json:"version"`
		SessionID   string    `json:"sessionId"`
		CreatedAt   time.Time `json:"createdAt"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return "", fmt.Errorf("failed to extract app fields: %w", err)
	}

	slug := fields.Name
	if slug == "" {
		return "", fmt.Errorf("app name/slug is required")
	}

	createdAt := fields.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	_, err = a.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (slug, name, description, code, version, session_id, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		 ON CONFLICT (slug) DO UPDATE SET
		   name = EXCLUDED.name,
		   description = EXCLUDED.description,
		   code = EXCLUDED.code,
		   version = EXCLUDED.version,
		   session_id = EXCLUDED.session_id,
		   updated_at = now()`,
		a.tableName()),
		slug, slug, fields.Description, fields.Code, fields.Version,
		fields.SessionID, createdAt,
	)
	if err != nil {
		return "", err
	}
	return slug, nil
}

func (a *pgAppStore) Load(ctx context.Context, slug string) (any, error) {
	row := a.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT slug, name, description, code, version, session_id, created_at, updated_at
		 FROM %s WHERE slug = $1`, a.tableName()),
		slug,
	)

	var name, desc, code, sessID string
	var version int
	var createdAt, updatedAt time.Time
	var slugResult string
	err := row.Scan(&slugResult, &name, &desc, &code, &version, &sessID, &createdAt, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("app not found: %w", err)
	}

	return map[string]any{
		"name":        name,
		"description": desc,
		"code":        code,
		"version":     version,
		"sessionId":   sessID,
		"createdAt":   createdAt,
		"updatedAt":   updatedAt,
	}, nil
}

func (a *pgAppStore) Delete(ctx context.Context, slug string) error {
	_, err := a.pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE slug = $1`, a.tableName()),
		slug,
	)
	return err
}

func (a *pgAppStore) List(ctx context.Context) ([]store.AppListItem, error) {
	rows, err := a.pool.Query(ctx, fmt.Sprintf(
		`SELECT slug, description, version, updated_at
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

// --- pgAppStateStore implements store.AppStateStore ---

type pgAppStateStore struct {
	pool   *pgxpool.Pool
	schema string
	userID string // scopes state to a specific user; empty for personal mode
}

func (s *pgAppStateStore) tableName() string {
	return pgx.Identifier{s.schema, "app_state"}.Sanitize()
}

func (s *pgAppStateStore) appsTable() string {
	return pgx.Identifier{s.schema, "apps"}.Sanitize()
}

func (s *pgAppStateStore) Get(ctx context.Context, appSlug, key string) (any, error) {
	var row pgx.Row
	if s.userID != "" {
		row = s.pool.QueryRow(ctx, fmt.Sprintf(
			`SELECT value FROM %s WHERE app_id = (
				SELECT id FROM %s WHERE slug = $1
			) AND user_id = $3 AND key = $2`, s.tableName(), s.appsTable()),
			appSlug, key, s.userID,
		)
	} else {
		row = s.pool.QueryRow(ctx, fmt.Sprintf(
			`SELECT value FROM %s WHERE app_id = (
				SELECT id FROM %s WHERE slug = $1
			) AND key = $2`, s.tableName(), s.appsTable()),
			appSlug, key,
		)
	}

	var value []byte
	if err := row.Scan(&value); err != nil {
		return nil, err
	}
	var result any
	if err := json.Unmarshal(value, &result); err != nil {
		return string(value), nil
	}
	return result, nil
}

func (s *pgAppStateStore) Set(ctx context.Context, appSlug, key string, value any) error {
	valueJSON, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if s.userID != "" {
		_, err = s.pool.Exec(ctx, fmt.Sprintf(
			`INSERT INTO %s (app_id, user_id, key, value, updated_at)
			 VALUES (
				(SELECT id FROM %s WHERE slug = $1),
				$4::UUID,
				$2, $3, now()
			 )
			 ON CONFLICT (app_id, user_id, key) DO UPDATE SET value = $3, updated_at = now()`,
			s.tableName(), s.appsTable()),
			appSlug, key, valueJSON, s.userID,
		)
	} else {
		_, err = s.pool.Exec(ctx, fmt.Sprintf(
			`INSERT INTO %s (app_id, key, value, updated_at)
			 VALUES (
				(SELECT id FROM %s WHERE slug = $1),
				$2, $3, now()
			 )
			 ON CONFLICT (app_id, key) DO UPDATE SET value = $3, updated_at = now()`,
			s.tableName(), s.appsTable()),
			appSlug, key, valueJSON,
		)
	}
	return err
}

func (s *pgAppStateStore) Delete(ctx context.Context, appSlug, key string) error {
	if s.userID != "" {
		_, err := s.pool.Exec(ctx, fmt.Sprintf(
			`DELETE FROM %s WHERE app_id = (
				SELECT id FROM %s WHERE slug = $1
			) AND user_id = $3 AND key = $2`, s.tableName(), s.appsTable()),
			appSlug, key, s.userID,
		)
		return err
	}
	_, err := s.pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE app_id = (
			SELECT id FROM %s WHERE slug = $1
		) AND key = $2`, s.tableName(), s.appsTable()),
		appSlug, key,
	)
	return err
}

func (s *pgAppStateStore) List(ctx context.Context, appSlug string) (map[string]any, error) {
	var rows pgx.Rows
	var err error
	if s.userID != "" {
		rows, err = s.pool.Query(ctx, fmt.Sprintf(
			`SELECT key, value FROM %s WHERE app_id = (
				SELECT id FROM %s WHERE slug = $1
			) AND user_id = $2`, s.tableName(), s.appsTable()),
			appSlug, s.userID,
		)
	} else {
		rows, err = s.pool.Query(ctx, fmt.Sprintf(
			`SELECT key, value FROM %s WHERE app_id = (
				SELECT id FROM %s WHERE slug = $1
			)`, s.tableName(), s.appsTable()),
			appSlug,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]any)
	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		var v any
		if err := json.Unmarshal(value, &v); err != nil {
			result[key] = string(value)
		} else {
			result[key] = v
		}
	}
	return result, rows.Err()
}
