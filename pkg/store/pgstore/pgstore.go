package pgstore

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/store"
)

// PGStore is the top-level PostgreSQL store implementation.
// It implements both store.PlatformStore and store.TenantRouter.
type PGStore struct {
	poolMgr     *PoolManager
	platformDSN string
	pgCfg       config.PostgresConfig
	embedFunc   store.EmbedFunc          // optional; propagated to memory stores for hybrid search
	secrets     *PlatformSecretStore     // platform-level secret store (for provider encryption)
}

// New creates a new PGStore connected to the platform database.
// Call Close() when done to release all connection pools.
func New(ctx context.Context, platformDSN string, pgCfg config.PostgresConfig) (*PGStore, error) {
	pm := NewPoolManager(platformDSN, pgCfg)

	// Verify platform DB connectivity
	if _, err := pm.PlatformPool(ctx); err != nil {
		pm.Close()
		return nil, fmt.Errorf("failed to connect to platform database: %w", err)
	}

	return &PGStore{
		poolMgr:     pm,
		platformDSN: platformDSN,
		pgCfg:       pgCfg,
		secrets:     NewPlatformSecretStore(pm),
	}, nil
}

// PoolManager returns the underlying pool manager for direct access.
func (s *PGStore) PoolManager() *PoolManager {
	return s.poolMgr
}

// PlatformSecrets returns the platform-level secret store for instance-wide
// secrets (bot tokens, API keys, etc.) that are not org/team-scoped.
func (s *PGStore) PlatformSecrets() *PlatformSecretStore {
	return s.secrets
}

// InstanceSuffix returns the instance suffix used for database naming.
func (s *PGStore) InstanceSuffix() string {
	return s.pgCfg.InstanceSuffix
}

// PlatformSettings returns the platform-wide settings store.
// Used for provider configuration visible to all orgs and teams.
func (s *PGStore) PlatformSettings() store.PlatformSettingsStore {
	pool, err := s.poolMgr.PlatformPool(context.Background())
	if err != nil {
		return nil
	}
	return NewPGPlatformSettingsStore(pool, s.secrets)
}

// OrgSettings returns the org-level settings store for a given org.
// Used for provider configuration visible to all teams in the org.
func (s *PGStore) OrgSettings(orgSlug string) store.OrgSettingsStore {
	pool, err := s.poolMgr.PlatformPool(context.Background())
	if err != nil {
		return nil
	}
	return NewPGOrgSettingsStore(pool, orgSlug, s.secrets)
}

// PlatformMCPServers returns the platform-level MCP server store.
// Platform MCP servers are inherited by all organizations and teams.
// Env values are encrypted at rest using the platform master key.
func (s *PGStore) PlatformMCPServers() store.MCPServerStore {
	pool, err := s.poolMgr.PlatformPool(context.Background())
	if err != nil {
		slog.Error("failed to get platform pool for MCP servers", "error", err)
		return nil
	}
	return &pgMCPServerStore{
		pool:    pool,
		schema:  "public",
		table:   "platform_mcp_servers",
		secrets: s.secrets,
	}
}

// SetEmbedFunc configures the embedding function used by memory stores for
// vector search. When set, Search() uses hybrid vector+keyword RRF fusion
// and Add() auto-generates embeddings for new memories. When nil (default),
// memory stores fall back to tsvector-only keyword search.
//
// This is called by the launcher after initializing the embedding model
// (HugotEmbedder or cloud provider). It's safe to call at any time;
// subsequent Memories() calls will pick up the new function.
func (s *PGStore) SetEmbedFunc(fn store.EmbedFunc) {
	s.embedFunc = fn
}

// GetEmbedFunc returns the configured embedding function.
// Returns nil if no embedding function has been set.
func (s *PGStore) GetEmbedFunc() store.EmbedFunc {
	return s.embedFunc
}

// --- store.PlatformStore implementation ---

func (s *PGStore) Users() store.UserStore {
	return &pgUserStore{poolMgr: s.poolMgr}
}

func (s *PGStore) Organizations() store.OrganizationStore {
	return &pgOrgStore{poolMgr: s.poolMgr}
}

func (s *PGStore) LoginSessions() store.LoginSessionStore {
	return &pgLoginSessionStore{poolMgr: s.poolMgr}
}

func (s *PGStore) OIDCProviders() store.OIDCProviderStore {
	return &pgOIDCProviderStore{poolMgr: s.poolMgr}
}

func (s *PGStore) UserChannels() store.UserChannelStore {
	return &pgUserChannelStore{poolMgr: s.poolMgr}
}

func (s *PGStore) Close() error {
	s.poolMgr.Close()
	return nil
}

// MigrateAllSchemas runs pending migrations on all existing team and personal
// schemas across all organizations. This should be called at startup to ensure
// that schema changes introduced in new migrations (e.g., adding columns) are
// applied to schemas that were created before those migrations existed.
//
// The function is idempotent — already-applied migrations are skipped via the
// schema_migrations version tracking table.
func (s *PGStore) MigrateAllSchemas(ctx context.Context) error {
	// 0. Run pending platform-level migrations.
	// This ensures new platform tables (e.g. platform_settings, platform_secrets)
	// are created on existing deployments that were bootstrapped before those
	// migrations were added.
	platformPool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return fmt.Errorf("failed to get platform pool: %w", err)
	}
	platConn, err := platformPool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire platform connection for migrations: %w", err)
	}
	if err := Migrate(ctx, platConn.Conn(), MigrationPlatform, ""); err != nil {
		platConn.Release()
		return fmt.Errorf("platform migrations failed: %w", err)
	}
	platConn.Release()

	// 1. Get all active org slugs from the platform database
	rows, err := platformPool.Query(ctx, `SELECT slug FROM organizations WHERE status = 'active'`)
	if err != nil {
		return fmt.Errorf("failed to list organizations: %w", err)
	}
	defer rows.Close()

	var orgSlugs []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return fmt.Errorf("failed to scan org slug: %w", err)
		}
		orgSlugs = append(orgSlugs, slug)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating org slugs: %w", err)
	}

	// 2. For each org, run pending migrations on team and personal schemas
	for _, orgSlug := range orgSlugs {
		if err := s.migrateOrgSchemas(ctx, orgSlug); err != nil {
			slog.Error("failed to migrate schemas for org", "org", orgSlug, "error", err)
			// Continue with other orgs — don't let one broken org block the rest
		}
	}

	return nil
}

// migrateOrgSchemas runs pending migrations on all team and personal schemas
// within a single organization's database.
func (s *PGStore) migrateOrgSchemas(ctx context.Context, orgSlug string) error {
	pool, err := s.poolMgr.OrgPool(ctx, orgSlug)
	if err != nil {
		return fmt.Errorf("failed to get pool for org %s: %w", orgSlug, err)
	}

	// Run org-level migrations first (public schema)
	orgConn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection for org %s: %w", orgSlug, err)
	}
	if err := Migrate(ctx, orgConn.Conn(), MigrationOrg, ""); err != nil {
		orgConn.Release()
		return fmt.Errorf("org migrations failed for %s: %w", orgSlug, err)
	}
	orgConn.Release()

	// Query all schemas in this org database
	schemaRows, err := pool.Query(ctx,
		`SELECT schema_name FROM information_schema.schemata
		 WHERE schema_name LIKE 'team_%' OR schema_name LIKE 'personal_%'
		 ORDER BY schema_name`)
	if err != nil {
		return fmt.Errorf("failed to list schemas for org %s: %w", orgSlug, err)
	}
	defer schemaRows.Close()

	var schemas []string
	for schemaRows.Next() {
		var name string
		if err := schemaRows.Scan(&name); err != nil {
			return fmt.Errorf("failed to scan schema name: %w", err)
		}
		schemas = append(schemas, name)
	}
	if err := schemaRows.Err(); err != nil {
		return fmt.Errorf("error iterating schemas: %w", err)
	}

	// Run migrations on each schema
	for _, schema := range schemas {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			slog.Error("failed to acquire connection for schema migration",
				"org", orgSlug, "schema", schema, "error", err)
			continue
		}

		var level MigrationLevel
		if strings.HasPrefix(schema, "team_") {
			level = MigrationTeam
		} else {
			level = MigrationPersonal
		}

		if err := Migrate(ctx, conn.Conn(), level, schema); err != nil {
			slog.Error("schema migration failed",
				"org", orgSlug, "schema", schema, "level", level, "error", err)
		}

		conn.Release()
	}

	return nil
}

// --- store.TenantRouter implementation ---

func (s *PGStore) ForOrg(orgSlug string) (store.OrgDataStore, error) {
	pool, err := s.poolMgr.OrgPool(context.Background(), orgSlug)
	if err != nil {
		return nil, fmt.Errorf("failed to get pool for org %s: %w", orgSlug, err)
	}

	// Load or create the org's credential encryption key (envelope encryption).
	// Returns nil if ASTONISH_MASTER_KEY is not set (encryption disabled).
	keyMgr := NewOrgEncryptionKeyManager(pool)
	credKey, err := keyMgr.GetOrCreateCredentialKey(context.Background())
	if err != nil {
		slog.Warn("failed to load org credential encryption key, encryption disabled", "org", orgSlug, "error", err)
		credKey = nil
	}

	return &pgOrgDataStore{
		pool:      pool,
		orgSlug:   orgSlug,
		poolMgr:   s.poolMgr,
		embedFunc: s.embedFunc,
		credKey:   credKey,
		secrets:   s.secrets,
	}, nil
}

func (s *PGStore) ProvisionOrg(ctx context.Context, orgID, slug string) error {
	conn, err := s.poolMgr.PlatformConn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	_, err = ProvisionOrgDB(ctx, conn, slug, s.platformDSN, s.pgCfg.InstanceSuffix)
	return err
}

func (s *PGStore) DecommissionOrg(ctx context.Context, orgSlug string) error {
	// Close the pool first
	s.poolMgr.RemovePool(orgSlug)

	// Drop the database
	conn, err := s.poolMgr.PlatformConn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	dbName := OrgDBName(s.pgCfg.InstanceSuffix, orgSlug)
	sanitizedName := pgx.Identifier{dbName}.Sanitize()

	// Force disconnect all sessions before dropping
	_, _ = conn.Exec(ctx,
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`,
		dbName,
	)

	sql := fmt.Sprintf(`DROP DATABASE IF EXISTS %s`, sanitizedName)
	_, err = conn.Exec(ctx, sql)
	return err
}

// --- store.OrgDataStore implementation ---

type pgOrgDataStore struct {
	pool      *pgxpool.Pool
	orgSlug   string
	poolMgr   *PoolManager
	embedFunc store.EmbedFunc
	credKey   []byte // AES-256 credential encryption key (nil = no encryption)
	secrets   *PlatformSecretStore
}

func (o *pgOrgDataStore) ForTeam(teamSlug string) store.TeamDataStore {
	return &pgTeamDataStore{pool: o.pool, teamSlug: teamSlug, orgSlug: o.orgSlug, embedFunc: o.embedFunc, credKey: o.credKey, secrets: o.secrets}
}

func (o *pgOrgDataStore) ForUser(userID string) store.PersonalDataStore {
	return &pgPersonalDataStore{pool: o.pool, userID: userID, embedFunc: o.embedFunc, credKey: o.credKey}
}

func (o *pgOrgDataStore) OrgMemories() store.MemoryStore {
	return &pgMemoryStore{pool: o.pool, schema: "public", tablePrefix: "org_", scope: string(store.MemoryScopeOrg), embedFunc: o.embedFunc, createdByColumn: "promoted_by"}
}

func (o *pgOrgDataStore) OrgSkills() store.SkillStore {
	return &pgSkillStore{pool: o.pool, schema: "public", table: "org_skills"}
}

func (o *pgOrgDataStore) OrgMCPServers() store.MCPServerStore {
	return &pgMCPServerStore{pool: o.pool, schema: "public", table: "org_mcp_servers"}
}

func (o *pgOrgDataStore) OrgApps() store.AppStore {
	return &pgOrgAppStore{pool: o.pool, schema: "public"}
}

func (o *pgOrgDataStore) OrgAudit() store.AuditStore {
	return &pgAuditStore{pool: o.pool, schema: "public", table: "org_audit_log"}
}

func (o *pgOrgDataStore) Teams() store.TeamManagementStore {
	return &pgTeamManagementStore{pool: o.pool}
}

func (o *pgOrgDataStore) ProvisionTeam(ctx context.Context, slug string) error {
	conn, err := o.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	return ProvisionTeamSchema(ctx, conn.Conn(), slug)
}

func (o *pgOrgDataStore) ProvisionPersonalSchema(ctx context.Context, userID string) error {
	conn, err := o.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	return ProvisionPersonalSchema(ctx, conn.Conn(), userID)
}

func (o *pgOrgDataStore) Close() error {
	// Pool is managed by PoolManager, not closed here
	return nil
}

// --- store.TeamDataStore implementation ---

type pgTeamDataStore struct {
	pool      *pgxpool.Pool
	teamSlug  string
	orgSlug   string             // parent org slug (for secret key scoping)
	userID    string             // optional: used for per-user app state scoping
	embedFunc store.EmbedFunc    // optional: for memory store hybrid search
	credKey   []byte             // AES-256 credential encryption key (nil = no encryption)
	secrets   *PlatformSecretStore // optional: for provider secret encryption
}

func (t *pgTeamDataStore) schema() string {
	return TeamSchemaName(t.teamSlug)
}

func (t *pgTeamDataStore) Sessions() store.SessionStore {
	return &pgSessionStore{pool: t.pool, schema: t.schema(), sessions: make(map[string]*pgSession)}
}

func (t *pgTeamDataStore) Memories() store.MemoryStore {
	return &pgMemoryStore{pool: t.pool, schema: t.schema(), tablePrefix: "", scope: string(store.MemoryScopeTeam), embedFunc: t.embedFunc}
}

func (t *pgTeamDataStore) Credentials() store.CredentialStore {
	return &pgCredentialStore{pool: t.pool, schema: t.schema(), encKey: t.credKey, userID: t.userID}
}

func (t *pgTeamDataStore) Apps() store.AppStore {
	return &pgAppStore{pool: t.pool, schema: t.schema(), table: "apps"}
}

func (t *pgTeamDataStore) AppState() store.AppStateStore {
	return &pgAppStateStore{pool: t.pool, schema: t.schema(), userID: t.userID}
}

func (t *pgTeamDataStore) Flows() store.FlowStore {
	return &pgFlowStore{pool: t.pool, schema: t.schema()}
}

func (t *pgTeamDataStore) ScheduledJobs() store.SchedulerStore {
	return &pgSchedulerStore{pool: t.pool, schema: t.schema()}
}

func (t *pgTeamDataStore) FleetTemplates() store.FleetTemplateStore {
	return &pgFleetTemplateStore{pool: t.pool, schema: t.schema()}
}

func (t *pgTeamDataStore) FleetPlans() store.FleetPlanStore {
	return &pgFleetPlanStore{pool: t.pool, schema: t.schema()}
}

func (t *pgTeamDataStore) Audit() store.AuditStore {
	return &pgAuditStore{pool: t.pool, schema: t.schema(), table: "team_audit_log"}
}

func (t *pgTeamDataStore) DrillReports() store.DrillReportStore {
	return &pgDrillReportStore{pool: t.pool, schema: t.schema()}
}

func (t *pgTeamDataStore) Skills() store.SkillStore {
	return &pgSkillStore{pool: t.pool, schema: t.schema(), table: "skills"}
}

func (t *pgTeamDataStore) MCPServers() store.MCPServerStore {
	return &pgMCPServerStore{pool: t.pool, schema: t.schema(), table: "mcp_servers"}
}

func (t *pgTeamDataStore) Settings() store.SettingsStore {
	return &pgSettingsStore{pool: t.pool, teamSlug: t.teamSlug, orgSlug: t.orgSlug, secrets: t.secrets}
}

// --- store.PersonalDataStore implementation ---

type pgPersonalDataStore struct {
	pool      *pgxpool.Pool
	userID    string
	embedFunc store.EmbedFunc
	credKey   []byte // AES-256 credential encryption key (nil = no encryption)
}

func (p *pgPersonalDataStore) schema() string {
	return PersonalSchemaName(p.userID)
}

func (p *pgPersonalDataStore) Memories() store.MemoryStore {
	return &pgMemoryStore{pool: p.pool, schema: p.schema(), tablePrefix: "", scope: string(store.MemoryScopePersonal), embedFunc: p.embedFunc}
}

func (p *pgPersonalDataStore) Apps() store.AppStore {
	return &pgAppStore{pool: p.pool, schema: p.schema(), table: "apps"}
}

func (p *pgPersonalDataStore) Sessions() store.SessionStore {
	return &pgSessionStore{pool: p.pool, schema: p.schema(), sessions: make(map[string]*pgSession)}
}

func (p *pgPersonalDataStore) AppState() store.AppStateStore {
	return &pgAppStateStore{pool: p.pool, schema: p.schema()}
}

func (p *pgPersonalDataStore) Flows() store.FlowStore {
	return &pgFlowStore{pool: p.pool, schema: p.schema()}
}

func (p *pgPersonalDataStore) Credentials() store.CredentialStore {
	return &pgCredentialStore{pool: p.pool, schema: p.schema(), encKey: p.credKey, userID: p.userID}
}
