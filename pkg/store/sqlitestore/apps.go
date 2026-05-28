package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/schardosin/astonish/pkg/store"
)

// sqliteAppStore implements store.AppStore.
type sqliteAppStore struct {
	db    *sql.DB
	table string // "apps" or "org_apps"
	isOrg bool
}

func (s *sqliteAppStore) Save(ctx context.Context, app any) (string, error) {
	data, err := json.Marshal(app)
	if err != nil {
		return "", err
	}

	var fields struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Code        string          `json:"code"`
		Version     int             `json:"version"`
		SessionID   string          `json:"sessionId"`
		DataSources json.RawMessage `json:"dataSources"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return "", err
	}

	slug := fields.Name
	if slug == "" {
		slug = uuid.New().String()
	}

	// Default dataSources to "[]" if empty/null.
	dsJSON := "[]"
	if len(fields.DataSources) > 0 && string(fields.DataSources) != "null" {
		dsJSON = string(fields.DataSources)
	}

	id := uuid.New().String()
	now := formatTime(time.Now())

	_, err = s.db.ExecContext(ctx,
		//nolint:gosec // s.table is a private struct field set at construction from a trusted constant, never from user input
		`INSERT INTO `+s.table+` (id, slug, name, description, code, version, session_id, data_sources, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(slug) DO UPDATE SET
		   name = excluded.name,
		   description = excluded.description,
		   code = excluded.code,
		   version = excluded.version,
		   session_id = excluded.session_id,
		   data_sources = excluded.data_sources,
		   updated_at = excluded.updated_at`,
		id, slug, slug, fields.Description, fields.Code, fields.Version, fields.SessionID, dsJSON, now, now)
	if err != nil {
		return "", err
	}
	return slug, nil
}

func (s *sqliteAppStore) Load(ctx context.Context, slug string) (any, error) {
	var name, desc, code, sessID, createdAt, updatedAt, dsJSON string
	var version int
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(name,''), COALESCE(description,''), COALESCE(code,''), COALESCE(version,1), COALESCE(session_id,''), COALESCE(data_sources,'[]'), created_at, updated_at
		 FROM `+s.table+` WHERE slug = ?`, slug).
		Scan(&name, &desc, &code, &version, &sessID, &dsJSON, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Parse data sources JSON into structured form.
	var dataSources []any
	_ = json.Unmarshal([]byte(dsJSON), &dataSources)
	if dataSources == nil {
		dataSources = []any{}
	}

	return map[string]any{
		"name":        name,
		"description": desc,
		"code":        code,
		"version":     version,
		"sessionId":   sessID,
		"dataSources": dataSources,
		"createdAt":   parseTime(createdAt),
		"updatedAt":   parseTime(updatedAt),
	}, nil
}

func (s *sqliteAppStore) Delete(ctx context.Context, slug string) error {
	//nolint:gosec // s.table is a private struct field set at construction from a trusted constant
	_, err := s.db.ExecContext(ctx, `DELETE FROM `+s.table+` WHERE slug = ?`, slug)
	return err
}

func (s *sqliteAppStore) List(ctx context.Context) ([]store.AppListItem, error) {
	rows, err := s.db.QueryContext(ctx,
		//nolint:gosec // s.table is a private struct field set at construction from a trusted constant
		`SELECT slug, COALESCE(description,''), COALESCE(version,1), updated_at FROM `+s.table+` ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []store.AppListItem
	for rows.Next() {
		var item store.AppListItem
		var updatedAt string
		if err := rows.Scan(&item.Name, &item.Description, &item.Version, &updatedAt); err != nil {
			return nil, err
		}
		item.UpdatedAt = parseTime(updatedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

// sqliteAppStateStore implements store.AppStateStore.
type sqliteAppStateStore struct {
	db     *sql.DB
	userID string
}

func (s *sqliteAppStateStore) Get(ctx context.Context, appSlug, key string) (any, error) {
	var val string
	var err error
	if s.userID != "" {
		err = s.db.QueryRowContext(ctx,
			`SELECT value FROM app_state WHERE app_id = ? AND user_id = ? AND key = ?`,
			appSlug, s.userID, key).Scan(&val)
	} else {
		err = s.db.QueryRowContext(ctx,
			`SELECT value FROM app_state WHERE app_id = ? AND key = ?`,
			appSlug, key).Scan(&val)
	}
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result interface{}
	_ = json.Unmarshal([]byte(val), &result)
	return result, nil
}

func (s *sqliteAppStateStore) Set(ctx context.Context, appSlug, key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if s.userID != "" {
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO app_state (app_id, user_id, key, value, updated_at) VALUES (?, ?, ?, ?, datetime('now'))
			 ON CONFLICT(app_id, user_id, key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
			appSlug, s.userID, key, string(data))
	} else {
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO app_state (app_id, key, value, updated_at) VALUES (?, ?, ?, datetime('now'))
			 ON CONFLICT(app_id, key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
			appSlug, key, string(data))
	}
	return err
}

func (s *sqliteAppStateStore) Delete(ctx context.Context, appSlug, key string) error {
	if s.userID != "" {
		_, err := s.db.ExecContext(ctx,
			`DELETE FROM app_state WHERE app_id = ? AND user_id = ? AND key = ?`,
			appSlug, s.userID, key)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM app_state WHERE app_id = ? AND key = ?`, appSlug, key)
	return err
}

func (s *sqliteAppStateStore) List(ctx context.Context, appSlug string) (map[string]any, error) {
	var rows *sql.Rows
	var err error
	if s.userID != "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT key, value FROM app_state WHERE app_id = ? AND user_id = ?`,
			appSlug, s.userID)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT key, value FROM app_state WHERE app_id = ?`, appSlug)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]any)
	for rows.Next() {
		var key, val string
		if err := rows.Scan(&key, &val); err != nil {
			return nil, err
		}
		var v interface{}
		_ = json.Unmarshal([]byte(val), &v)
		result[key] = v
	}
	return result, rows.Err()
}
