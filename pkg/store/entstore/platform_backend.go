package entstore

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	teament "github.com/schardosin/astonish/ent/team"
	"github.com/schardosin/astonish/ent/platform/loginsession"
	"github.com/schardosin/astonish/ent/platform/pendinglinkcode"
	"github.com/schardosin/astonish/pkg/store"
)

// --- store.TenantRouter ---
// ForOrg, ProvisionOrg, DecommissionOrg are implemented in tenant_router.go.

// --- store.PlatformBackend additional methods ---

// PlatformSettings is implemented in platform_settings.go.
// OrgSettings is implemented in platform_settings.go.
// PlatformMCPServers is implemented in mcp_servers.go.
// SandboxLayers is implemented in sandbox.go.
// SandboxTemplates is implemented in sandbox.go.
// SecretGetter is implemented in platform_settings.go.

func (s *Store) SandboxLayers() store.LayerStore {
	return &layerStore{client: s.platformClient}
}

func (s *Store) SandboxTemplates() store.SandboxTemplateStore {
	return s.sandboxTemplates
}

func (s *Store) MigrateAll(ctx context.Context) error {
	// TODO: implement migration runner
	return fmt.Errorf("entstore: MigrateAll not yet implemented")
}

func (s *Store) CleanupExpired(ctx context.Context) error {
	now := time.Now()

	// Delete expired login sessions.
	if _, err := s.platformClient.LoginSession.Delete().
		Where(loginsession.ExpiresAtLT(now)).
		Exec(ctx); err != nil {
		return fmt.Errorf("cleanup login sessions: %w", err)
	}

	// Delete expired link codes.
	if _, err := s.platformClient.PendingLinkCode.Delete().
		Where(pendinglinkcode.ExpiresAtLT(now)).
		Exec(ctx); err != nil {
		return fmt.Errorf("cleanup link codes: %w", err)
	}

	return nil
}

// AllSandboxSessionIDs returns all known sandbox session IDs across all teams.
// Used by the K8s GC reconciler to detect orphan pods.
func (s *Store) AllSandboxSessionIDs(ctx context.Context) (map[string]bool, error) {
	teamSchemas, err := s.ListTeamSchemas(ctx)
	if err != nil {
		return nil, fmt.Errorf("list team schemas: %w", err)
	}

	result := make(map[string]bool)
	for _, ts := range teamSchemas {
		sessStore := s.sandboxSessionsForSchema(ts.orgDBName, ts.schemaName)
		if sessStore == nil {
			continue
		}
		sessions, err := sessStore.List(ctx, store.SandboxSessionFilter{})
		if err != nil {
			// Non-fatal: schema might not have sandbox_sessions table yet.
			slog.Debug("AllSandboxSessionIDs: list failed", "db", ts.orgDBName, "schema", ts.schemaName, "error", err)
			continue
		}
		for _, sess := range sessions {
			result[sess.SessionID] = true
		}
	}
	return result, nil
}

// SandboxSessionsForTeam returns a SandboxSessionStore for the given org and
// team. For PostgreSQL mode, opens a connection to the team schema within the
// org database and returns an Ent-backed store. For SQLite mode, returns nil.
func (s *Store) SandboxSessionsForTeam(_ context.Context, orgSlug, teamSlug string) store.SandboxSessionStore {
	if s.dialect != DialectPostgres {
		return nil
	}

	orgDB := s.orgDBName(orgSlug)
	schema := teamSchemaName(teamSlug)
	return s.sandboxSessionsForSchema(orgDB, schema)
}

// teamSchemaRef represents a team schema within an org database.
type teamSchemaRef struct {
	orgDBName  string
	schemaName string
}

// ListTeamSchemas returns all team schemas across all active orgs.
// Used by sandbox audit and GC reconciler to iterate all sandbox sessions.
func (s *Store) ListTeamSchemas(ctx context.Context) ([]teamSchemaRef, error) {
	if s.dialect != DialectPostgres {
		return nil, fmt.Errorf("ListTeamSchemas only supported on PostgreSQL")
	}

	// Get all active orgs to find their databases.
	orgs, err := s.platformClient.Organization.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query orgs: %w", err)
	}

	var refs []teamSchemaRef
	for _, org := range orgs {
		orgDB := org.DbName
		if orgDB == "" {
			orgDB = s.orgDBName(org.Slug)
		}

		// Connect to the org database and list team schemas.
		dsn, err := s.deriveDSN(orgDB)
		if err != nil {
			slog.Debug("ListTeamSchemas: derive DSN failed", "org", org.Slug, "error", err)
			continue
		}
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			slog.Debug("ListTeamSchemas: open failed", "org", org.Slug, "error", err)
			continue
		}

		rows, err := db.QueryContext(ctx,
			`SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'team_%' ORDER BY schema_name`)
		if err != nil {
			db.Close()
			slog.Debug("ListTeamSchemas: query failed", "org", org.Slug, "error", err)
			continue
		}
		for rows.Next() {
			var schema string
			if err := rows.Scan(&schema); err != nil {
				continue
			}
			refs = append(refs, teamSchemaRef{orgDBName: orgDB, schemaName: schema})
		}
		rows.Close()
		db.Close()
	}

	return refs, nil
}

// sandboxSessionsForSchema opens a connection to the given team schema within
// the given org database and returns a SandboxSessionStore. Returns nil if the
// connection fails or if sandbox_sessions table doesn't exist.
func (s *Store) sandboxSessionsForSchema(orgDBName, schemaName string) store.SandboxSessionStore {
	dsn, err := s.deriveSchemaAwareDSN(orgDBName, schemaName)
	if err != nil {
		slog.Debug("sandboxSessionsForSchema: derive DSN failed", "db", orgDBName, "schema", schemaName, "error", err)
		return nil
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		slog.Debug("sandboxSessionsForSchema: open failed", "db", orgDBName, "schema", schemaName, "error", err)
		return nil
	}

	// Check if sandbox_sessions table exists in this schema.
	var exists bool
	err = db.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM information_schema.tables
		WHERE table_schema = $1 AND table_name = 'sandbox_sessions'
	)`, schemaName).Scan(&exists)
	if err != nil || !exists {
		db.Close()
		return nil
	}

	drv := entsql.OpenDB(dialect.Postgres, db)
	client := teament.NewClient(teament.Driver(drv))

	return &teamSandboxSessionStore{client: client}
}

// PruneStaleSandboxSessions removes sandbox_sessions records whose
// chat_session_id does not correspond to an existing chat session in the same
// team schema. These stale records accumulate when session deletion previously
// failed to clean up the sandbox registry (before the PG-backed
// TryDestroySession fix).
//
// Returns the total number of records deleted across all team schemas.
func (s *Store) PruneStaleSandboxSessions(ctx context.Context) (int, error) {
	teamSchemas, err := s.ListTeamSchemas(ctx)
	if err != nil {
		return 0, fmt.Errorf("PruneStaleSandboxSessions: list team schemas: %w", err)
	}

	total := 0
	for _, ts := range teamSchemas {
		dsn, err := s.deriveSchemaAwareDSN(ts.orgDBName, ts.schemaName)
		if err != nil {
			slog.Debug("PruneStaleSandboxSessions: derive DSN failed", "db", ts.orgDBName, "schema", ts.schemaName, "error", err)
			continue
		}

		db, err := sql.Open("pgx", dsn)
		if err != nil {
			slog.Debug("PruneStaleSandboxSessions: open failed", "db", ts.orgDBName, "schema", ts.schemaName, "error", err)
			continue
		}

		// Delete sandbox_sessions whose chat_session_id has no matching row in sessions.
		// The special 'team-template-*' rows (used for template builds) have
		// chat_session_id = id, and there's never a matching chat session for them.
		// They are intentionally pruned here — they represent build artifacts, not
		// live sessions.
		res, err := db.ExecContext(ctx, `
			DELETE FROM sandbox_sessions ss
			WHERE NOT EXISTS (
				SELECT 1 FROM sessions s WHERE s.id = ss.chat_session_id
			)
		`)
		db.Close()
		if err != nil {
			slog.Warn("PruneStaleSandboxSessions: delete failed", "db", ts.orgDBName, "schema", ts.schemaName, "error", err)
			continue
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			slog.Info("PruneStaleSandboxSessions: pruned stale records",
				"db", ts.orgDBName, "schema", ts.schemaName, "pruned", n)
		}
		total += int(n)
	}
	return total, nil
}

// ListIdleSandboxSessions returns all running (non-pinned) sandbox sessions
// across all team schemas whose last_active_at is before the given cutoff.
// Used by the idle watchdog to find sessions that should be evicted.
func (s *Store) ListIdleSandboxSessions(ctx context.Context, cutoff time.Time) ([]store.SandboxSession, error) {
	teamSchemas, err := s.ListTeamSchemas(ctx)
	if err != nil {
		return nil, fmt.Errorf("ListIdleSandboxSessions: list team schemas: %w", err)
	}

	var result []store.SandboxSession
	for _, ts := range teamSchemas {
		sessStore := s.sandboxSessionsForSchema(ts.orgDBName, ts.schemaName)
		if sessStore == nil {
			continue
		}
		sessions, lErr := sessStore.List(ctx, store.SandboxSessionFilter{State: store.SandboxSessionStateRunning})
		if lErr != nil {
			slog.Debug("ListIdleSandboxSessions: list failed", "db", ts.orgDBName, "schema", ts.schemaName, "error", lErr)
			continue
		}
		for _, sess := range sessions {
			if sess.Pinned {
				continue
			}
			if sess.LastActiveAt.IsZero() || sess.LastActiveAt.Before(cutoff) {
				result = append(result, *sess)
			}
		}
	}
	return result, nil
}

// MarkSandboxSessionEvicted finds the given sandbox session (by SessionID)
// across all team schemas and sets its state to Evicted, clearing container/pod
// names. Used by the idle watchdog after pod deletion.
func (s *Store) MarkSandboxSessionEvicted(ctx context.Context, sessionID string) error {
	teamSchemas, err := s.ListTeamSchemas(ctx)
	if err != nil {
		return fmt.Errorf("MarkSandboxSessionEvicted: list team schemas: %w", err)
	}

	for _, ts := range teamSchemas {
		sessStore := s.sandboxSessionsForSchema(ts.orgDBName, ts.schemaName)
		if sessStore == nil {
			continue
		}
		sess, gErr := sessStore.Get(ctx, sessionID)
		if gErr != nil || sess == nil {
			continue
		}
		sess.State = store.SandboxSessionStateEvicted
		sess.ContainerName = ""
		sess.PodName = ""
		return sessStore.Put(ctx, sess)
	}
	return fmt.Errorf("MarkSandboxSessionEvicted: session %q not found", sessionID)
}


