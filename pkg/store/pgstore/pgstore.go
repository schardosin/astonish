package pgstore

import (
	"context"
	"fmt"

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
	}, nil
}

// PoolManager returns the underlying pool manager for direct access.
func (s *PGStore) PoolManager() *PoolManager {
	return s.poolMgr
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

func (s *PGStore) Close() error {
	s.poolMgr.Close()
	return nil
}

// --- store.TenantRouter implementation ---

func (s *PGStore) ForOrg(orgSlug string) (store.OrgDataStore, error) {
	pool, err := s.poolMgr.OrgPool(context.Background(), orgSlug)
	if err != nil {
		return nil, fmt.Errorf("failed to get pool for org %s: %w", orgSlug, err)
	}
	return &pgOrgDataStore{
		pool:    pool,
		orgSlug: orgSlug,
		poolMgr: s.poolMgr,
	}, nil
}

func (s *PGStore) ProvisionOrg(ctx context.Context, orgID, slug string) error {
	conn, err := s.poolMgr.PlatformConn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	_, err = ProvisionOrgDB(ctx, conn, slug, s.platformDSN)
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

	dbName := OrgDBName(orgSlug)
	// Force disconnect all sessions before dropping
	_, _ = conn.Exec(ctx, fmt.Sprintf(
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s'`,
		dbName,
	))

	sql := fmt.Sprintf(`DROP DATABASE IF EXISTS %s`, fmt.Sprintf(`"%s"`, dbName))
	_, err = conn.Exec(ctx, sql)
	return err
}

// --- store.OrgDataStore implementation ---

type pgOrgDataStore struct {
	pool    *pgxpool.Pool
	orgSlug string
	poolMgr *PoolManager
}

func (o *pgOrgDataStore) ForTeam(teamSlug string) store.TeamDataStore {
	return &pgTeamDataStore{pool: o.pool, teamSlug: teamSlug}
}

func (o *pgOrgDataStore) ForUser(userID string) store.PersonalDataStore {
	return &pgPersonalDataStore{pool: o.pool, userID: userID}
}

func (o *pgOrgDataStore) OrgMemories() store.MemoryStore {
	return &pgMemoryStore{pool: o.pool, schema: "public", tablePrefix: "org_", scope: string(store.MemoryScopeOrg)}
}

func (o *pgOrgDataStore) OrgSkills() store.SkillStore {
	return &pgSkillStore{pool: o.pool, schema: "public", table: "org_skills"}
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
	pool     *pgxpool.Pool
	teamSlug string
	userID   string // optional: used for per-user app state scoping
}

func (t *pgTeamDataStore) schema() string {
	return TeamSchemaName(t.teamSlug)
}

func (t *pgTeamDataStore) Sessions() store.SessionStore {
	return &pgSessionStore{pool: t.pool, schema: t.schema(), sessions: make(map[string]*pgSession)}
}

func (t *pgTeamDataStore) Memories() store.MemoryStore {
	return &pgMemoryStore{pool: t.pool, schema: t.schema(), tablePrefix: "", scope: string(store.MemoryScopeTeam)}
}

func (t *pgTeamDataStore) Credentials() store.CredentialStore {
	return &pgCredentialStore{pool: t.pool, schema: t.schema()}
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

// --- store.PersonalDataStore implementation ---

type pgPersonalDataStore struct {
	pool   *pgxpool.Pool
	userID string
}

func (p *pgPersonalDataStore) schema() string {
	return PersonalSchemaName(p.userID)
}

func (p *pgPersonalDataStore) Memories() store.MemoryStore {
	return &pgMemoryStore{pool: p.pool, schema: p.schema(), tablePrefix: "", scope: string(store.MemoryScopePersonal)}
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
