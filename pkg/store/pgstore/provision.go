package pgstore

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
)

// PostgreSQL roles used by the platform.
const (
	// RolePlatformAdmin owns all databases and runs migrations.
	// RLS does not apply to this role (it is the table owner).
	RolePlatformAdmin = "astonish_platform_admin"

	// RoleApp is the application connection role with restricted privileges.
	// RLS policies are enforced for this role.
	RoleApp = "astonish_app"
)

// EnsureRoles creates the platform roles if they don't already exist.
// This must be run by a superuser or a role with CREATEROLE privilege.
func EnsureRoles(ctx context.Context, conn *pgx.Conn) error {
	for _, role := range []string{RolePlatformAdmin, RoleApp} {
		// CREATE ROLE IF NOT EXISTS is not supported in all PG versions,
		// so we use DO $$ block with exception handling.
		sql := fmt.Sprintf(`
			DO $$
			BEGIN
				CREATE ROLE %s;
			EXCEPTION WHEN duplicate_object THEN
				NULL;
			END $$;`, pgx.Identifier{role}.Sanitize())

		if _, err := conn.Exec(ctx, sql); err != nil {
			return fmt.Errorf("failed to ensure role %s: %w", role, err)
		}
	}

	// Grant LOGIN to the app role
	if _, err := conn.Exec(ctx, fmt.Sprintf(
		`ALTER ROLE %s LOGIN`, pgx.Identifier{RoleApp}.Sanitize(),
	)); err != nil {
		return fmt.Errorf("failed to grant LOGIN to %s: %w", RoleApp, err)
	}

	return nil
}

// ProvisionPlatformDB initializes the platform database.
// The database (astonish_platform) must already exist. This function
// runs migrations and sets up grants for the app role.
func ProvisionPlatformDB(ctx context.Context, conn *pgx.Conn) error {
	// Run platform migrations
	if err := Migrate(ctx, conn, MigrationPlatform, ""); err != nil {
		return fmt.Errorf("platform migrations failed: %w", err)
	}

	// Grant the app role read access to platform tables (needed for auth lookups)
	grants := []string{
		fmt.Sprintf(`GRANT USAGE ON SCHEMA public TO %s`, pgx.Identifier{RoleApp}.Sanitize()),
		fmt.Sprintf(`GRANT SELECT ON ALL TABLES IN SCHEMA public TO %s`, pgx.Identifier{RoleApp}.Sanitize()),
		fmt.Sprintf(`GRANT INSERT, UPDATE ON login_sessions TO %s`, pgx.Identifier{RoleApp}.Sanitize()),
		fmt.Sprintf(`GRANT DELETE ON login_sessions TO %s`, pgx.Identifier{RoleApp}.Sanitize()),
	}

	for _, sql := range grants {
		if _, err := conn.Exec(ctx, sql); err != nil {
			return fmt.Errorf("failed to apply platform grant: %w", err)
		}
	}

	return nil
}

// ProvisionOrgDB creates and initializes a new organization's database.
//
// Parameters:
//   - adminConn: connection to the platform DB (or any DB) with CREATEDB privilege
//   - orgSlug: the organization's slug (used to derive the database name)
//   - platformDSN: DSN for the platform DB (used to derive the org DB DSN)
//
// The function:
//  1. Creates the database astonish_org_{slug}
//  2. Connects to the new database
//  3. Runs org-level migrations (public schema tables)
//  4. Sets up grants for the app role
//
// Returns the DSN for the new org database.
func ProvisionOrgDB(ctx context.Context, adminConn *pgx.Conn, orgSlug, platformDSN string) (string, error) {
	dbName := OrgDBName(orgSlug)

	// CREATE DATABASE cannot run inside a transaction
	createSQL := fmt.Sprintf(`CREATE DATABASE %s`, pgx.Identifier{dbName}.Sanitize())
	if _, err := adminConn.Exec(ctx, createSQL); err != nil {
		// Check if it already exists (23505 = unique_violation for pg_database)
		if !strings.Contains(err.Error(), "already exists") {
			return "", fmt.Errorf("failed to create org database %s: %w", dbName, err)
		}
	}

	// Derive the org DB DSN from the platform DSN
	orgDSN, err := ReplaceDSNDatabase(platformDSN, dbName)
	if err != nil {
		return "", fmt.Errorf("failed to derive org DSN: %w", err)
	}

	// Connect to the new org database
	orgConn, err := pgx.Connect(ctx, orgDSN)
	if err != nil {
		return "", fmt.Errorf("failed to connect to org database %s: %w", dbName, err)
	}
	defer orgConn.Close(ctx)

	// Run org migrations (creates public schema tables)
	if err := Migrate(ctx, orgConn, MigrationOrg, ""); err != nil {
		return "", fmt.Errorf("org migrations failed for %s: %w", dbName, err)
	}

	// Grant the app role connect + basic privileges on the org database
	appRole := pgx.Identifier{RoleApp}.Sanitize()
	grants := []string{
		fmt.Sprintf(`GRANT CONNECT ON DATABASE %s TO %s`, pgx.Identifier{dbName}.Sanitize(), appRole),
		fmt.Sprintf(`GRANT USAGE ON SCHEMA public TO %s`, appRole),
		fmt.Sprintf(`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO %s`, appRole),
		fmt.Sprintf(`GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO %s`, appRole),
		// Audit log: INSERT only (no UPDATE, no DELETE) — revoke then re-grant
		fmt.Sprintf(`REVOKE UPDATE, DELETE ON org_audit_log FROM %s`, appRole),
	}

	for _, sql := range grants {
		if _, err := orgConn.Exec(ctx, sql); err != nil {
			return "", fmt.Errorf("failed to apply org grant on %s: %w", dbName, err)
		}
	}

	// Set default privileges for future tables created by migrations
	if _, err := orgConn.Exec(ctx, fmt.Sprintf(
		`ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %s`,
		appRole,
	)); err != nil {
		return "", fmt.Errorf("failed to set default privileges on %s: %w", dbName, err)
	}
	if _, err := orgConn.Exec(ctx, fmt.Sprintf(
		`ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO %s`,
		appRole,
	)); err != nil {
		return "", fmt.Errorf("failed to set default sequence privileges on %s: %w", dbName, err)
	}

	return orgDSN, nil
}

// ProvisionTeamSchema creates a team schema within an org database and runs
// team-level migrations.
func ProvisionTeamSchema(ctx context.Context, conn *pgx.Conn, teamSlug string) error {
	schemaName := TeamSchemaName(teamSlug)

	// Create the schema
	createSQL := fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, pgx.Identifier{schemaName}.Sanitize())
	if _, err := conn.Exec(ctx, createSQL); err != nil {
		return fmt.Errorf("failed to create team schema %s: %w", schemaName, err)
	}

	// Run team migrations
	if err := Migrate(ctx, conn, MigrationTeam, schemaName); err != nil {
		return fmt.Errorf("team migrations failed for %s: %w", schemaName, err)
	}

	// Grant the app role access to this schema
	appRole := pgx.Identifier{RoleApp}.Sanitize()
	schema := pgx.Identifier{schemaName}.Sanitize()
	grants := []string{
		fmt.Sprintf(`GRANT USAGE ON SCHEMA %s TO %s`, schema, appRole),
		fmt.Sprintf(`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %s TO %s`, schema, appRole),
		fmt.Sprintf(`GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA %s TO %s`, schema, appRole),
		// Team audit log: INSERT only
		fmt.Sprintf(`REVOKE UPDATE, DELETE ON %s.team_audit_log FROM %s`, schema, appRole),
	}

	for _, sql := range grants {
		if _, err := conn.Exec(ctx, sql); err != nil {
			return fmt.Errorf("failed to apply team grant on %s: %w", schemaName, err)
		}
	}

	// Set default privileges for future tables in this schema
	if _, err := conn.Exec(ctx, fmt.Sprintf(
		`ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %s`,
		schema, appRole,
	)); err != nil {
		return fmt.Errorf("failed to set default privileges on %s: %w", schemaName, err)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf(
		`ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT USAGE, SELECT ON SEQUENCES TO %s`,
		schema, appRole,
	)); err != nil {
		return fmt.Errorf("failed to set default sequence privileges on %s: %w", schemaName, err)
	}

	return nil
}

// ProvisionPersonalSchema creates a personal schema within an org database
// and runs personal-level migrations.
func ProvisionPersonalSchema(ctx context.Context, conn *pgx.Conn, userID string) error {
	schemaName := PersonalSchemaName(userID)

	// Create the schema
	createSQL := fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, pgx.Identifier{schemaName}.Sanitize())
	if _, err := conn.Exec(ctx, createSQL); err != nil {
		return fmt.Errorf("failed to create personal schema %s: %w", schemaName, err)
	}

	// Run personal migrations
	if err := Migrate(ctx, conn, MigrationPersonal, schemaName); err != nil {
		return fmt.Errorf("personal migrations failed for %s: %w", schemaName, err)
	}

	// Grant the app role access to this schema
	appRole := pgx.Identifier{RoleApp}.Sanitize()
	schema := pgx.Identifier{schemaName}.Sanitize()
	grants := []string{
		fmt.Sprintf(`GRANT USAGE ON SCHEMA %s TO %s`, schema, appRole),
		fmt.Sprintf(`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %s TO %s`, schema, appRole),
		fmt.Sprintf(`GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA %s TO %s`, schema, appRole),
	}

	for _, sql := range grants {
		if _, err := conn.Exec(ctx, sql); err != nil {
			return fmt.Errorf("failed to apply personal grant on %s: %w", schemaName, err)
		}
	}

	// Set default privileges
	if _, err := conn.Exec(ctx, fmt.Sprintf(
		`ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %s`,
		schema, appRole,
	)); err != nil {
		return fmt.Errorf("failed to set default privileges on %s: %w", schemaName, err)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf(
		`ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT USAGE, SELECT ON SEQUENCES TO %s`,
		schema, appRole,
	)); err != nil {
		return fmt.Errorf("failed to set default sequence privileges on %s: %w", schemaName, err)
	}

	return nil
}

// --- Naming helpers ---

// OrgDBName returns the database name for an organization.
func OrgDBName(orgSlug string) string {
	return "astonish_org_" + sanitizeSlug(orgSlug)
}

// TeamSchemaName returns the schema name for a team.
func TeamSchemaName(teamSlug string) string {
	return "team_" + sanitizeSlug(teamSlug)
}

// PersonalSchemaName returns the schema name for a user's personal data.
func PersonalSchemaName(userID string) string {
	// Replace hyphens in UUIDs with underscores for valid PG identifiers
	return "personal_" + strings.ReplaceAll(userID, "-", "_")
}

// sanitizeSlug removes any characters that aren't alphanumeric or underscore.
func sanitizeSlug(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ReplaceDSNDatabase takes a PostgreSQL DSN and replaces the database name.
// Supports both URL-style (postgres://...) and keyword-style (host=... dbname=...) DSNs.
func ReplaceDSNDatabase(dsn, newDB string) (string, error) {
	// Try URL-style first
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", fmt.Errorf("failed to parse DSN as URL: %w", err)
		}
		u.Path = "/" + newDB
		return u.String(), nil
	}

	// Keyword-style DSN (e.g., "host=localhost dbname=foo")
	parts := strings.Fields(dsn)
	found := false
	for i, part := range parts {
		if strings.HasPrefix(part, "dbname=") {
			parts[i] = "dbname=" + newDB
			found = true
			break
		}
	}
	if !found {
		parts = append(parts, "dbname="+newDB)
	}
	return strings.Join(parts, " "), nil
}
