package sqlitestore

import (
	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/session"
	"github.com/schardosin/astonish/pkg/store"
)

// Compile-time interface assertions. These ensure the SQLite store types
// satisfy their respective interfaces without any runtime cost.
var (
	// Top-level store
	_ store.PlatformStore   = (*SQLiteStore)(nil)
	_ store.TenantRouter    = (*SQLiteStore)(nil)
	_ store.PlatformBackend = (*SQLiteStore)(nil)

	// Org data store
	_ store.OrgDataStore = (*sqliteOrgDataStore)(nil)

	// Team data store
	_ store.TeamDataStore = (*sqliteTeamDataStore)(nil)

	// Personal data store
	_ store.PersonalDataStore = (*sqlitePersonalDataStore)(nil)

	// Platform-level stores
	_ store.PlatformSettingsStore = (*sqlitePlatformSettingsStore)(nil)
	_ store.OrgSettingsStore      = (*sqliteOrgSettingsStore)(nil)
	_ store.SettingsStore         = (*sqliteSettingsStore)(nil)

	// Entity stores
	_ store.UserStore         = (*sqliteUserStore)(nil)
	_ store.OrganizationStore = (*sqliteOrgStore)(nil)
	_ store.LoginSessionStore = (*sqliteLoginSessionStore)(nil)
	_ store.OIDCProviderStore = (*sqliteOIDCProviderStore)(nil)
	_ store.UserChannelStore  = (*sqliteUserChannelStore)(nil)

	// Team management & audit
	_ store.TeamManagementStore = (*sqliteTeamManagementStore)(nil)
	_ store.AuditStore          = (*sqliteAuditStore)(nil)

	// Content stores
	_ store.SessionStore     = (*sqliteSessionStore)(nil)
	_ store.MemoryStore      = (*sqliteMemoryStore)(nil)
	_ store.SkillStore       = (*sqliteSkillStore)(nil)
	_ store.MCPServerStore   = (*sqliteMCPServerStore)(nil)
	_ store.FlowStore        = (*sqliteFlowStore)(nil)
	_ store.AppStore         = (*sqliteAppStore)(nil)
	_ store.AppStateStore    = (*sqliteAppStateStore)(nil)
	_ store.CredentialStore  = (*sqliteCredentialStore)(nil)

	// Scheduler, fleet, drill
	_ store.SchedulerStore     = (*sqliteSchedulerStore)(nil)
	_ store.FleetTemplateStore = (*sqliteFleetTemplateStore)(nil)
	_ store.FleetPlanStore     = (*sqliteFleetPlanStore)(nil)
	_ store.DrillReportStore   = (*sqliteDrillReportStore)(nil)

	// New stores added for platform parity
	_ agent.ToolVectorStore   = (*SQLiteToolVectorStore)(nil)
	_ session.ThreadIndexer   = (*SQLiteThreadIndex)(nil)
	_ store.LinkCodeStore     = (*SQLiteLinkCodeStore)(nil)
	_ store.LayerStore        = (*SQLiteSandboxLayerStore)(nil)
	_ fleet.MonitorStateStore = (*SQLiteMonitorStateStore)(nil)
)
