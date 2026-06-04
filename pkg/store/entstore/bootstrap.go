package entstore

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"

	platformmigrate "github.com/schardosin/astonish/ent/platform/migrate"
	"github.com/schardosin/astonish/pkg/store/pgutil"
)

// BootstrapPlatform creates the platform database if it doesn't exist,
// ensures PG roles, runs Ent schema auto-migration, and applies PG-specific
// extras (extensions, specialized indexes, triggers, RLS, grants, seed data).
//
// For SQLite this simply creates the store and runs Schema.Create().
func BootstrapPlatform(ctx context.Context, cfg Config) error {
	if cfg.DSN == "" {
		return fmt.Errorf("DSN is required")
	}

	if !strings.HasPrefix(cfg.DSN, "postgres://") && !strings.HasPrefix(cfg.DSN, "postgresql://") {
		// SQLite: create store + auto-migrate.
		s, err := New(ctx, cfg)
		if err != nil {
			return fmt.Errorf("create store: %w", err)
		}
		defer s.Close()

		if err := s.platformClient.Schema.Create(ctx,
			platformmigrate.WithDropColumn(true),
			platformmigrate.WithDropIndex(true),
		); err != nil {
			return fmt.Errorf("schema create: %w", err)
		}
		return nil
	}

	// PostgreSQL path:
	// 1. Connect to "postgres" database to create the target DB and roles.
	dbName, adminDSN, err := parsePGDSN(cfg.DSN)
	if err != nil {
		return fmt.Errorf("parse DSN: %w", err)
	}

	adminDB, err := sql.Open("pgx", adminDSN)
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer adminDB.Close()

	// Ensure roles exist.
	if err := pgutil.EnsureRoles(ctx, adminDB); err != nil {
		return fmt.Errorf("ensure roles: %w", err)
	}

	// Create the platform database if it doesn't exist.
	var exists bool
	err = adminDB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check database existence: %w", err)
	}

	if !exists {
		if strings.ContainsAny(dbName, `"'\;`) {
			return fmt.Errorf("invalid database name: %s", dbName)
		}
		if _, err := adminDB.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE "%s"`, dbName)); err != nil {
			return fmt.Errorf("create database: %w", err)
		}
	}

	// 2. Open the target database and run Ent schema migration.
	s, err := New(ctx, cfg)
	if err != nil {
		return fmt.Errorf("open new database: %w", err)
	}
	defer s.Close()

	if err := s.platformClient.Schema.Create(ctx,
		platformmigrate.WithDropColumn(true),
		platformmigrate.WithDropIndex(true),
	); err != nil {
		return fmt.Errorf("schema create: %w", err)
	}

	// 3. Apply PG-specific extras (extensions, indexes, triggers, grants, seed).
	if err := s.applyPGExtras(ctx, ScopePlatform, s.platformDB); err != nil {
		return fmt.Errorf("apply platform pg extras: %w", err)
	}

	// 4. Apply grants.
	if err := pgutil.ApplyGrants(ctx, s.platformDB, "platform"); err != nil {
		return fmt.Errorf("apply platform grants: %w", err)
	}

	return nil
}

// parsePGDSN extracts the database name from a PostgreSQL DSN and returns
// the database name and a modified DSN pointing to the "postgres" database.
func parsePGDSN(dsn string) (dbName string, adminDSN string, err error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", "", fmt.Errorf("parse URL: %w", err)
	}

	dbName = strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		return "", "", fmt.Errorf("no database name in DSN")
	}

	// Replace the path with /postgres for the admin connection.
	u.Path = "/postgres"
	adminDSN = u.String()

	return dbName, adminDSN, nil
}
