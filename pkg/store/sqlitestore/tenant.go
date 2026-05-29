package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/schardosin/astonish/pkg/store"
)

// sqliteOrgDataStore implements store.OrgDataStore for a single organization.
type sqliteOrgDataStore struct {
	slug      string
	db        *sql.DB // org.db
	dir       string  // org directory path
	embedFunc store.EmbedFunc
	secrets   *SQLitePlatformSecretStore
	teamPools sync.Map // team_slug → *sqliteTeamDataStore
	userPools sync.Map // user_id → *sqlitePersonalDataStore
}

func (o *sqliteOrgDataStore) ForTeam(teamSlug string) store.TeamDataStore {
	if cached, ok := o.teamPools.Load(teamSlug); ok {
		return cached.(*sqliteTeamDataStore)
	}

	dbPath := filepath.Join(o.dir, "teams", teamSlug+".db")
	db, err := openDB(dbPath)
	if err != nil {
		slog.Error("open team database", "team", teamSlug, "error", err)
		return nil
	}

	ts := &sqliteTeamDataStore{
		db:        db,
		teamSlug:  teamSlug,
		orgSlug:   o.slug,
		embedFunc: o.embedFunc,
		secrets:   o.secrets,
	}
	o.teamPools.Store(teamSlug, ts)
	return ts
}

func (o *sqliteOrgDataStore) ForUser(userID string) store.PersonalDataStore {
	if cached, ok := o.userPools.Load(userID); ok {
		return cached.(*sqlitePersonalDataStore)
	}

	dbPath := filepath.Join(o.dir, "personal", userID+".db")
	db, err := openDB(dbPath)
	if err != nil {
		slog.Error("open personal database", "user", userID, "error", err)
		return nil
	}

	ps := &sqlitePersonalDataStore{
		db:        db,
		userID:    userID,
		embedFunc: o.embedFunc,
	}
	o.userPools.Store(userID, ps)
	return ps
}

func (o *sqliteOrgDataStore) OrgMemories() store.MemoryStore {
	return &sqliteMemoryStore{
		db:              o.db,
		table:           "org_memories",
		ftsTable:        "org_memories_fts",
		scope:           string(store.MemoryScopeOrg),
		embedFunc:       o.embedFunc,
		createdByColumn: "promoted_by",
		vecIndex:        newVectorIndex(),
	}
}

func (o *sqliteOrgDataStore) OrgSkills() store.SkillStore {
	return &sqliteSkillStore{db: o.db, table: "org_skills", filesTable: "org_skill_files"}
}

func (o *sqliteOrgDataStore) OrgMCPServers() store.MCPServerStore {
	return &sqliteMCPServerStore{db: o.db, table: "org_mcp_servers"}
}

func (o *sqliteOrgDataStore) OrgApps() store.AppStore {
	return &sqliteAppStore{db: o.db, table: "org_apps", isOrg: true}
}

func (o *sqliteOrgDataStore) OrgAudit() store.AuditStore {
	return &sqliteAuditStore{db: o.db, table: "org_audit_log"}
}

func (o *sqliteOrgDataStore) Teams() store.TeamManagementStore {
	return &sqliteTeamManagementStore{db: o.db}
}

func (o *sqliteOrgDataStore) ProvisionTeam(ctx context.Context, slug string) error {
	dbPath := filepath.Join(o.dir, "teams", slug+".db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0750); err != nil {
		return fmt.Errorf("create team directory: %w", err)
	}

	db, err := openDB(dbPath)
	if err != nil {
		return fmt.Errorf("create team database: %w", err)
	}
	defer db.Close()

	if err := migrate(ctx, db, migrationTeam); err != nil {
		return fmt.Errorf("migrate team database: %w", err)
	}

	slog.Info("provisioned team database", "org", o.slug, "team", slug)
	return nil
}

func (o *sqliteOrgDataStore) ProvisionPersonalSchema(ctx context.Context, userID string) error {
	// Sanitize userID for use as filename (replace problematic chars)
	safeID := strings.ReplaceAll(userID, "/", "_")
	dbPath := filepath.Join(o.dir, "personal", safeID+".db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0750); err != nil {
		return fmt.Errorf("create personal directory: %w", err)
	}

	db, err := openDB(dbPath)
	if err != nil {
		return fmt.Errorf("create personal database: %w", err)
	}
	defer db.Close()

	if err := migrate(ctx, db, migrationPersonal); err != nil {
		return fmt.Errorf("migrate personal database: %w", err)
	}

	slog.Info("provisioned personal database", "org", o.slug, "user", userID)
	return nil
}

func (o *sqliteOrgDataStore) Close() error {
	// Close all team stores
	o.teamPools.Range(func(key, value any) bool {
		if ts, ok := value.(*sqliteTeamDataStore); ok {
			ts.db.Close()
		}
		return true
	})
	// Close all personal stores
	o.userPools.Range(func(key, value any) bool {
		if ps, ok := value.(*sqlitePersonalDataStore); ok {
			ps.db.Close()
		}
		return true
	})
	// Close org db
	if o.db != nil {
		return o.db.Close()
	}
	return nil
}

// --- store.TeamDataStore ---

type sqliteTeamDataStore struct {
	db        *sql.DB
	teamSlug  string
	orgSlug   string
	userID    string // set per-request for app state scoping
	embedFunc store.EmbedFunc
	secrets   *SQLitePlatformSecretStore
}

func (t *sqliteTeamDataStore) Sessions() store.SessionStore {
	return &sqliteSessionStore{db: t.db}
}

func (t *sqliteTeamDataStore) Memories() store.MemoryStore {
	return &sqliteMemoryStore{
		db:              t.db,
		table:           "memories",
		ftsTable:        "memories_fts",
		scope:           string(store.MemoryScopeTeam),
		embedFunc:       t.embedFunc,
		createdByColumn: "created_by",
		vecIndex:        newVectorIndex(),
	}
}

func (t *sqliteTeamDataStore) Credentials() store.CredentialStore {
	return &sqliteCredentialStore{db: t.db}
}

func (t *sqliteTeamDataStore) Apps() store.AppStore {
	return &sqliteAppStore{db: t.db, table: "apps"}
}

func (t *sqliteTeamDataStore) AppState() store.AppStateStore {
	return &sqliteAppStateStore{db: t.db, userID: t.userID}
}

func (t *sqliteTeamDataStore) Flows() store.FlowStore {
	return &sqliteFlowStore{db: t.db}
}

func (t *sqliteTeamDataStore) Skills() store.SkillStore {
	return &sqliteSkillStore{db: t.db, table: "skills", filesTable: "skill_files"}
}

func (t *sqliteTeamDataStore) MCPServers() store.MCPServerStore {
	return &sqliteMCPServerStore{db: t.db, table: "mcp_servers"}
}

func (t *sqliteTeamDataStore) ScheduledJobs() store.SchedulerStore {
	return &sqliteSchedulerStore{db: t.db}
}

func (t *sqliteTeamDataStore) FleetTemplates() store.FleetTemplateStore {
	return &sqliteFleetTemplateStore{db: t.db}
}

func (t *sqliteTeamDataStore) FleetPlans() store.FleetPlanStore {
	return &sqliteFleetPlanStore{db: t.db}
}

func (t *sqliteTeamDataStore) DrillReports() store.DrillReportStore {
	return &sqliteDrillReportStore{db: t.db}
}

func (t *sqliteTeamDataStore) Settings() store.SettingsStore {
	return &sqliteSettingsStore{db: t.db, orgSlug: t.orgSlug, teamSlug: t.teamSlug, secrets: t.secrets}
}

func (t *sqliteTeamDataStore) Audit() store.AuditStore {
	return &sqliteAuditStore{db: t.db, table: "team_audit_log"}
}

// --- store.PersonalDataStore ---

type sqlitePersonalDataStore struct {
	db        *sql.DB
	userID    string
	embedFunc store.EmbedFunc
}

func (p *sqlitePersonalDataStore) Memories() store.MemoryStore {
	return &sqliteMemoryStore{
		db:              p.db,
		table:           "memories",
		ftsTable:        "memories_fts",
		scope:           string(store.MemoryScopePersonal),
		embedFunc:       p.embedFunc,
		createdByColumn: "created_by",
		vecIndex:        newVectorIndex(),
	}
}

func (p *sqlitePersonalDataStore) Apps() store.AppStore {
	return &sqliteAppStore{db: p.db, table: "apps"}
}

func (p *sqlitePersonalDataStore) Sessions() store.SessionStore {
	return &sqliteSessionStore{db: p.db}
}

func (p *sqlitePersonalDataStore) AppState() store.AppStateStore {
	return &sqliteAppStateStore{db: p.db}
}

func (p *sqlitePersonalDataStore) Flows() store.FlowStore {
	return &sqliteFlowStore{db: p.db}
}

func (p *sqlitePersonalDataStore) Credentials() store.CredentialStore {
	return &sqliteCredentialStore{db: p.db}
}
