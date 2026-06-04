// Package entstore provides a unified database store implementation using
// Ent (entgo.io) as the ORM layer. It supports both PostgreSQL and SQLite
// backends through a single codebase, with dialect selection at connection time.
//
// This package replaces the dual pgstore/sqlitestore implementations with a
// single store that uses generated Ent clients for all database operations.
//
// Architecture:
//   - Store wraps Ent clients for all 4 scopes (platform, org, team, personal)
//   - Dialect (PG vs SQLite) is determined at connection time from the DSN
//   - Multi-tenancy uses connection-level schema isolation (PG search_path)
//   - Migrations are applied via Atlas (embedded SQL from pkg/store/migrations/)
package entstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	platforment "github.com/schardosin/astonish/ent/platform"
	"github.com/schardosin/astonish/pkg/store"

	_ "modernc.org/sqlite" // Register the pure-Go sqlite driver.
)

// Dialect represents the database backend type.
type Dialect string

const (
	DialectPostgres Dialect = "postgres"
	DialectSQLite   Dialect = "sqlite"
)

// Store is the top-level unified store implementation.
// It satisfies store.PlatformBackend.
type Store struct {
	dialect        Dialect
	platformClient *platforment.Client
	platformDB     *sql.DB

	// Platform DSN for deriving org/team/personal connections.
	platformDSN string

	// For PG multi-tenancy: instance suffix for database naming.
	instanceSuffix string

	// SQLite data directory (for SQLite mode).
	dataDir string

	// Embedding function for vector search (optional).
	embedFunc store.EmbedFunc

	// Singleton sub-stores that carry state.
	sandboxTemplates *sandboxTemplateStore

	// Cached org data stores (map[string]*orgDataStore).
	orgClients sync.Map
}

// Config holds configuration for creating a new Store.
type Config struct {
	// DSN is the primary database connection string.
	// For PG: "postgres://user:pass@host:port/dbname?sslmode=prefer"
	// For SQLite: "file:/path/to/data.db" or ":memory:"
	DSN string

	// InstanceSuffix is used for PG database naming (e.g., "wjw3p6").
	// Only relevant for PostgreSQL mode.
	InstanceSuffix string

	// DataDir is the base directory for SQLite databases.
	// Only relevant for SQLite mode.
	DataDir string
}

// New creates a new unified Store from the given configuration.
// The dialect is auto-detected from the DSN.
func New(ctx context.Context, cfg Config) (*Store, error) {
	d := detectDialect(cfg.DSN)

	s := &Store{
		dialect:        d,
		platformDSN:    cfg.DSN,
		instanceSuffix: cfg.InstanceSuffix,
		dataDir:        cfg.DataDir,
	}

	// Open the platform database connection.
	var err error
	switch d {
	case DialectPostgres:
		err = s.openPostgres(ctx, cfg.DSN)
	case DialectSQLite:
		err = s.openSQLite(ctx, cfg.DSN)
	default:
		return nil, fmt.Errorf("unsupported dialect: %s", d)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to open platform database: %w", err)
	}

	// Initialize singleton sub-stores.
	s.sandboxTemplates = &sandboxTemplateStore{client: s.platformClient}

	return s, nil
}

// openPostgres opens a PostgreSQL connection and creates the Ent client.
func (s *Store) openPostgres(ctx context.Context, dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("sql.Open: %w", err)
	}

	// Verify connectivity.
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("ping: %w", err)
	}

	// Create Ent client using the sql.DB directly.
	drv := entsql.OpenDB(dialect.Postgres, db)
	client := platforment.NewClient(platforment.Driver(drv))

	s.platformDB = db
	s.platformClient = client
	return nil
}

// openSQLite opens a SQLite connection and creates the Ent client.
// Uses modernc.org/sqlite (pure Go, CGO_ENABLED=0 compatible).
func (s *Store) openSQLite(ctx context.Context, dsn string) error {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("sql.Open: %w", err)
	}

	// Enable WAL mode and foreign keys for SQLite.
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return fmt.Errorf("pragma %q: %w", pragma, err)
		}
	}

	// Verify connectivity.
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("ping: %w", err)
	}

	// Create Ent client using the sql.DB directly.
	drv := entsql.OpenDB(dialect.SQLite, db)
	client := platforment.NewClient(platforment.Driver(drv))

	s.platformDB = db
	s.platformClient = client
	return nil
}

// Close releases all database connections.
func (s *Store) Close() error {
	if s.platformClient != nil {
		if err := s.platformClient.Close(); err != nil {
			return err
		}
	}
	return nil
}

// InstanceSuffix returns the instance suffix for database naming.
func (s *Store) InstanceSuffix() string {
	return s.instanceSuffix
}

// SetEmbedFunc configures the embedding function for vector search.
func (s *Store) SetEmbedFunc(fn store.EmbedFunc) {
	s.embedFunc = fn
}

// GetEmbedFunc returns the configured embedding function.
func (s *Store) GetEmbedFunc() store.EmbedFunc {
	return s.embedFunc
}

// Dialect returns the detected database dialect (Postgres or SQLite).
func (s *Store) Dialect() Dialect {
	return s.dialect
}

// PlatformClient returns the Ent platform client for direct use.
// This is an escape hatch for callers that need low-level access.
func (s *Store) PlatformClient() *platforment.Client {
	return s.platformClient
}

// PlatformDB returns the raw *sql.DB for health checks and direct queries.
func (s *Store) PlatformDB() *sql.DB {
	return s.platformDB
}

// detectDialect auto-detects the database dialect from the DSN string.
func detectDialect(dsn string) Dialect {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		return DialectPostgres
	}
	return DialectSQLite
}
