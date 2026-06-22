package store

import (
	"context"
	"time"
)

// AppListItem is a summary of a saved app.
type AppListItem struct {
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Version     int       `json:"version"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Scope       string    `json:"scope,omitempty"` // "personal" or "team" (platform mode only)
}

// AppStore manages generative UI app definitions.
//
// In personal mode, this wraps the existing apps package functions.
// In platform mode, apps are stored in the appropriate schema.
type AppStore interface {
	// Save persists an app definition. Returns the slug.
	Save(ctx context.Context, app any) (string, error)

	// Load retrieves an app by slug.
	Load(ctx context.Context, slug string) (any, error)

	// Delete removes an app by slug.
	Delete(ctx context.Context, slug string) error

	// List returns summaries of all apps.
	List(ctx context.Context) ([]AppListItem, error)
}

// AppStateStore manages per-app persistent state (key-value pairs).
//
// In personal mode, this wraps the existing SQLite-based app state.
// In platform mode, state is stored per (app, user) in PostgreSQL.
type AppStateStore interface {
	// Get retrieves a value by key for the given app.
	Get(ctx context.Context, appSlug, key string) (any, error)

	// Set stores a value by key for the given app.
	Set(ctx context.Context, appSlug, key string, value any) error

	// Delete removes a key for the given app.
	Delete(ctx context.Context, appSlug, key string) error

	// List returns all keys for the given app.
	List(ctx context.Context, appSlug string) (map[string]any, error)
}

// AppStateSQLStore provides raw SQL execution against per-app databases.
//
// In platform mode each app gets its own PostgreSQL schema (e.g.
// team_general_app_todo_app) within the org database. The app's SQL runs
// with search_path set to that schema, so table names need no rewriting.
//
// In personal mode this interface is nil — SQLite handles it directly.
type AppStateSQLStore interface {
	// EnsureSchema creates the per-app schema if it doesn't exist.
	EnsureSchema(ctx context.Context, appSlug string) error

	// Query executes a read-only SQL statement within the app's schema.
	Query(ctx context.Context, appSlug, sql string, params ...any) ([]map[string]any, error)

	// Exec executes a write/DDL SQL statement within the app's schema.
	// Returns (rowsAffected, lastInsertId, error).
	Exec(ctx context.Context, appSlug, sql string, params ...any) (int64, int64, error)

	// DropSchema removes the per-app schema and all its tables.
	DropSchema(ctx context.Context, appSlug string) error

	// DropSchemasWithPrefix removes all app schemas matching a prefix
	// (used for cleaning up session-scoped app databases).
	DropSchemasWithPrefix(ctx context.Context, prefix string) error
}
