package pgstore

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/schardosin/astonish/pkg/config"
)

// BuildDSN constructs a PostgreSQL connection string from individual components.
func BuildDSN(host string, port int, user, password, dbname, sslmode string) string {
	u := &url.URL{
		Scheme: "postgres",
		Host:   fmt.Sprintf("%s:%d", host, port),
		Path:   "/" + dbname,
	}
	if password != "" {
		u.User = url.UserPassword(user, password)
	} else {
		u.User = url.User(user)
	}
	if sslmode != "" {
		q := u.Query()
		q.Set("sslmode", sslmode)
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// BootstrapPlatform creates the platform database (if it does not already
// exist), ensures the required PostgreSQL roles, and runs platform-level
// migrations. This is the single entry point used by:
//   - The CLI setup wizard (`astonish setup`)
//   - The CLI command (`astonish platform init`)
//   - The UI setup API (`POST /api/platform/init`)
//   - The daemon auto-init on first startup
//
// The platformDSN should point to any database on the target server (the actual
// platform DB name is derived from the suffix). The user must have CREATEDB privilege.
// The suffix parameter namespaces the instance; empty string means legacy naming.
func BootstrapPlatform(ctx context.Context, platformDSN, suffix string) error {
	// Step 1: Connect to the default "postgres" database to run CREATE DATABASE.
	// CREATE DATABASE cannot be run inside a transaction or on the target DB itself.
	adminDSN, err := ReplaceDSNDatabase(platformDSN, "postgres")
	if err != nil {
		return fmt.Errorf("failed to derive admin DSN: %w", err)
	}

	adminConn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}

	// Step 2: Create the platform database if it doesn't exist.
	dbName := config.PlatformDBName(suffix)
	createSQL := fmt.Sprintf(`CREATE DATABASE %s`, pgx.Identifier{dbName}.Sanitize())
	if _, execErr := adminConn.Exec(ctx, createSQL); execErr != nil {
		if !strings.Contains(execErr.Error(), "already exists") {
			adminConn.Close(ctx)
			return fmt.Errorf("failed to create platform database: %w", execErr)
		}
		// Database already exists — that's fine.
	}

	// Step 3: Ensure platform roles on the admin connection.
	if err := EnsureRoles(ctx, adminConn); err != nil {
		adminConn.Close(ctx)
		return fmt.Errorf("failed to create roles: %w", err)
	}
	adminConn.Close(ctx)

	// Step 4: Connect to the platform database and run migrations.
	platDSN, err := ReplaceDSNDatabase(platformDSN, dbName)
	if err != nil {
		return fmt.Errorf("failed to derive platform DSN: %w", err)
	}

	platConn, err := pgx.Connect(ctx, platDSN)
	if err != nil {
		return fmt.Errorf("failed to connect to platform database: %w", err)
	}
	defer platConn.Close(ctx)

	if err := ProvisionPlatformDB(ctx, platConn); err != nil {
		return fmt.Errorf("failed to provision platform database: %w", err)
	}

	return nil
}

// PlatformDBExists checks whether a platform database with the given suffix
// already exists on the PostgreSQL host. Used for collision detection when
// generating a new instance suffix.
func PlatformDBExists(ctx context.Context, anyDSN, suffix string) (bool, error) {
	adminDSN, err := ReplaceDSNDatabase(anyDSN, "postgres")
	if err != nil {
		return false, err
	}
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		return false, err
	}
	defer conn.Close(ctx)

	dbName := config.PlatformDBName(suffix)
	var exists bool
	err = conn.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)`,
		dbName,
	).Scan(&exists)
	return exists, err
}
