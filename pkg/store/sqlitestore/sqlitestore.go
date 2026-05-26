package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/session"
	"github.com/schardosin/astonish/pkg/store"
)

// SQLiteStore is the top-level SQLite store implementation.
// It implements both store.PlatformStore and store.TenantRouter.
type SQLiteStore struct {
	dataDir      string
	platformDB   *sql.DB
	embedFunc    store.EmbedFunc
	secrets      *SQLitePlatformSecretStore
	orgPools     sync.Map // org_slug → *sqliteOrgDataStore (lazy-loaded)
	buildMu      sync.Mutex // shared build lock for sandbox template builds
	appStateSQL  *sqliteAppStateSQLStore // per-app SQL databases
}

// New creates a new SQLiteStore rooted at dataDir.
// It opens (or creates) the platform database and runs pending migrations.
// Returns an error if the platform database cannot be initialized.
func New(ctx context.Context, dataDir string) (*SQLiteStore, error) {
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	platformPath := filepath.Join(dataDir, "platform.db")
	db, err := openDB(platformPath)
	if err != nil {
		return nil, fmt.Errorf("open platform database: %w", err)
	}

	if err := migrate(ctx, db, migrationPlatform); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate platform database: %w", err)
	}

	secrets := NewSQLitePlatformSecretStore(db)

	return &SQLiteStore{
		dataDir:     dataDir,
		platformDB:  db,
		secrets:     secrets,
		appStateSQL: newSQLiteAppStateSQLStore(dataDir),
	}, nil
}

// NeedsBootstrap returns true if no users exist in the platform database.
// This indicates a fresh installation that needs the setup wizard.
func (s *SQLiteStore) NeedsBootstrap(ctx context.Context) (bool, error) {
	var count int
	err := s.platformDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check user count: %w", err)
	}
	return count == 0, nil
}

// SetEmbedFunc configures the embedding function used by memory stores.
func (s *SQLiteStore) SetEmbedFunc(fn store.EmbedFunc) {
	s.embedFunc = fn
}

// GetEmbedFunc returns the configured embedding function.
func (s *SQLiteStore) GetEmbedFunc() store.EmbedFunc {
	return s.embedFunc
}

// InstanceSuffix returns an empty string for SQLite mode.
// SQLite uses directory-based isolation, not database naming conventions.
func (s *SQLiteStore) InstanceSuffix() string {
	return ""
}

// Close closes all database connections.
func (s *SQLiteStore) Close() error {
	// Close all org stores
	s.orgPools.Range(func(key, value any) bool {
		if org, ok := value.(*sqliteOrgDataStore); ok {
			org.Close()
		}
		return true
	})

	// Close all app state SQL databases
	if s.appStateSQL != nil {
		s.appStateSQL.CloseAll()
	}

	if s.platformDB != nil {
		return s.platformDB.Close()
	}
	return nil
}

// --- store.PlatformStore implementation ---

func (s *SQLiteStore) Users() store.UserStore {
	return &sqliteUserStore{db: s.platformDB}
}

func (s *SQLiteStore) Organizations() store.OrganizationStore {
	return &sqliteOrgStore{db: s.platformDB}
}

func (s *SQLiteStore) LoginSessions() store.LoginSessionStore {
	return &sqliteLoginSessionStore{db: s.platformDB}
}

func (s *SQLiteStore) OIDCProviders() store.OIDCProviderStore {
	return &sqliteOIDCProviderStore{db: s.platformDB}
}

func (s *SQLiteStore) UserChannels() store.UserChannelStore {
	return &sqliteUserChannelStore{db: s.platformDB}
}

// PlatformSettings returns the platform-wide settings store.
func (s *SQLiteStore) PlatformSettings() store.PlatformSettingsStore {
	return &sqlitePlatformSettingsStore{db: s.platformDB, secrets: s.secrets}
}

// PlatformMCPServers returns the platform-level MCP server store.
func (s *SQLiteStore) PlatformMCPServers() store.MCPServerStore {
	return &sqliteMCPServerStore{db: s.platformDB, table: "platform_mcp_servers"}
}

// OrgSettings returns the org-level settings store for the given org slug.
func (s *SQLiteStore) OrgSettings(orgSlug string) store.OrgSettingsStore {
	return &sqliteOrgSettingsStore{
		db:      s.platformDB,
		orgSlug: orgSlug,
		secrets: s.secrets,
	}
}

// Secrets returns the platform secret store for external callers (e.g., daemon secret getter).
func (s *SQLiteStore) Secrets() *SQLitePlatformSecretStore {
	return s.secrets
}

// SecretGetter returns a function that resolves secrets from the platform
// secrets table. Implements store.PlatformBackend.
func (s *SQLiteStore) SecretGetter() func(string) string {
	if s.secrets == nil {
		return func(string) string { return "" }
	}
	return s.secrets.GetSecret
}

// SandboxLayers returns a LayerStore backed by the platform database.
func (s *SQLiteStore) SandboxLayers() store.LayerStore {
	return NewSQLiteSandboxLayerStore(s.platformDB)
}

// SandboxTemplates returns a SandboxTemplateStore backed by the platform database.
func (s *SQLiteStore) SandboxTemplates() store.SandboxTemplateStore {
	return NewSQLiteSandboxTemplateStore(s.platformDB, &s.buildMu)
}

// CleanupExpired removes expired transient records (device sessions, link codes).
func (s *SQLiteStore) CleanupExpired(ctx context.Context) error {
	_, _ = s.platformDB.ExecContext(ctx, `DELETE FROM device_sessions WHERE expires_at < datetime('now')`)
	_, _ = s.platformDB.ExecContext(ctx, `DELETE FROM pending_link_codes WHERE expires_at < datetime('now')`)
	return nil
}

// NewToolVectorStore creates a ToolVectorStore for semantic tool discovery.
// Returns (nil, nil) if the embedding function is not configured.
func (s *SQLiteStore) NewToolVectorStore(ctx context.Context) (agent.ToolVectorStore, error) {
	if s.embedFunc == nil {
		return nil, nil
	}
	return NewSQLiteToolVectorStore(s.platformDB, s.embedFunc)
}

// NewThreadIndex creates a thread indexer for email session routing.
func (s *SQLiteStore) NewThreadIndex() session.ThreadIndexer {
	return NewSQLiteThreadIndex(s.platformDB)
}

// NewLinkCodeStore creates a link code store for channel verification.
func (s *SQLiteStore) NewLinkCodeStore() store.LinkCodeStore {
	return NewSQLiteLinkCodeStore(s.platformDB)
}

// NewMonitorStateStore creates a monitor state store for fleet plan monitors.
func (s *SQLiteStore) NewMonitorStateStore(orgSlug, teamSlug string) fleet.MonitorStateStore {
	return s.newMonitorStateStoreForTeam(orgSlug, teamSlug)
}

// AppStateSQL returns the per-app SQL store (per-app .db files in {dataDir}/apps/).
func (s *SQLiteStore) AppStateSQL() store.AppStateSQLStore {
	return s.appStateSQL
}

// --- store.TenantRouter implementation ---

func (s *SQLiteStore) ForOrg(orgSlug string) (store.OrgDataStore, error) {
	if cached, ok := s.orgPools.Load(orgSlug); ok {
		return cached.(*sqliteOrgDataStore), nil
	}

	orgDir := filepath.Join(s.dataDir, "orgs", orgSlug)
	orgDBPath := filepath.Join(orgDir, "org.db")

	// Check if org directory exists
	if _, err := os.Stat(orgDBPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("organization %q not provisioned", orgSlug)
	}

	db, err := openDB(orgDBPath)
	if err != nil {
		return nil, fmt.Errorf("open org database %q: %w", orgSlug, err)
	}

	// Run pending migrations on the org database
	if err := migrate(context.Background(), db, migrationOrg); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate org database %q: %w", orgSlug, err)
	}

	orgStore := &sqliteOrgDataStore{
		slug:      orgSlug,
		db:        db,
		dir:       orgDir,
		embedFunc: s.embedFunc,
		secrets:   s.secrets,
	}
	s.orgPools.Store(orgSlug, orgStore)
	return orgStore, nil
}

func (s *SQLiteStore) ProvisionOrg(ctx context.Context, orgID, slug string) error {
	orgDir := filepath.Join(s.dataDir, "orgs", slug)
	if err := os.MkdirAll(orgDir, 0750); err != nil {
		return fmt.Errorf("create org directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(orgDir, "teams"), 0750); err != nil {
		return fmt.Errorf("create teams directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(orgDir, "personal"), 0750); err != nil {
		return fmt.Errorf("create personal directory: %w", err)
	}

	db, err := openDB(filepath.Join(orgDir, "org.db"))
	if err != nil {
		return fmt.Errorf("create org database: %w", err)
	}
	defer db.Close()

	if err := migrate(ctx, db, migrationOrg); err != nil {
		return fmt.Errorf("migrate org database: %w", err)
	}

	slog.Info("provisioned org database", "slug", slug, "dir", orgDir)
	return nil
}

func (s *SQLiteStore) DecommissionOrg(ctx context.Context, orgSlug string) error {
	// Close the cached store if open
	if cached, ok := s.orgPools.LoadAndDelete(orgSlug); ok {
		if org, ok := cached.(*sqliteOrgDataStore); ok {
			org.Close()
		}
	}

	// Remove the org directory entirely
	orgDir := filepath.Join(s.dataDir, "orgs", orgSlug)
	if err := os.RemoveAll(orgDir); err != nil {
		return fmt.Errorf("remove org directory: %w", err)
	}

	slog.Info("decommissioned org", "slug", orgSlug)
	return nil
}

// MigrateAll runs pending migrations on all org, team, and personal databases.
func (s *SQLiteStore) MigrateAll(ctx context.Context) error {
	orgsDir := filepath.Join(s.dataDir, "orgs")
	entries, err := os.ReadDir(orgsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("list orgs directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		orgSlug := entry.Name()
		if err := s.migrateOrg(ctx, orgSlug); err != nil {
			slog.Error("failed to migrate org", "org", orgSlug, "error", err)
		}
	}
	return nil
}

func (s *SQLiteStore) migrateOrg(ctx context.Context, orgSlug string) error {
	orgDir := filepath.Join(s.dataDir, "orgs", orgSlug)

	// Migrate org.db
	orgDB, err := openDB(filepath.Join(orgDir, "org.db"))
	if err != nil {
		return err
	}
	if err := migrate(ctx, orgDB, migrationOrg); err != nil {
		orgDB.Close()
		return fmt.Errorf("org migrations: %w", err)
	}
	orgDB.Close()

	// Migrate team databases
	teamsDir := filepath.Join(orgDir, "teams")
	teamEntries, _ := os.ReadDir(teamsDir)
	for _, te := range teamEntries {
		if te.IsDir() || filepath.Ext(te.Name()) != ".db" {
			continue
		}
		teamDB, err := openDB(filepath.Join(teamsDir, te.Name()))
		if err != nil {
			slog.Error("open team db for migration", "file", te.Name(), "error", err)
			continue
		}
		if err := migrate(ctx, teamDB, migrationTeam); err != nil {
			slog.Error("team migration failed", "file", te.Name(), "error", err)
		}
		teamDB.Close()
	}

	// Migrate personal databases
	personalDir := filepath.Join(orgDir, "personal")
	personalEntries, _ := os.ReadDir(personalDir)
	for _, pe := range personalEntries {
		if pe.IsDir() || filepath.Ext(pe.Name()) != ".db" {
			continue
		}
		pDB, err := openDB(filepath.Join(personalDir, pe.Name()))
		if err != nil {
			slog.Error("open personal db for migration", "file", pe.Name(), "error", err)
			continue
		}
		if err := migrate(ctx, pDB, migrationPersonal); err != nil {
			slog.Error("personal migration failed", "file", pe.Name(), "error", err)
		}
		pDB.Close()
	}

	return nil
}
