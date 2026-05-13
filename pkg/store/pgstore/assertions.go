package pgstore

import "github.com/schardosin/astonish/pkg/store"

// Compile-time interface assertions.
// These ensure all PG store types satisfy their respective interfaces at build time.

// Top-level stores
var _ store.PlatformStore = (*PGStore)(nil)
var _ store.TenantRouter = (*PGStore)(nil)

// Org/Team/Personal data stores
var _ store.OrgDataStore = (*pgOrgDataStore)(nil)
var _ store.TeamDataStore = (*pgTeamDataStore)(nil)
var _ store.PersonalDataStore = (*pgPersonalDataStore)(nil)

// Platform-level stores
var _ store.UserStore = (*pgUserStore)(nil)
var _ store.OrganizationStore = (*pgOrgStore)(nil)
var _ store.LoginSessionStore = (*pgLoginSessionStore)(nil)

// Data stores
var _ store.SessionStore = (*pgSessionStore)(nil)
var _ store.MemoryStore = (*pgMemoryStore)(nil)
var _ store.CredentialStore = (*pgCredentialStore)(nil)
var _ store.AppStore = (*pgAppStore)(nil)
var _ store.AppStore = (*pgOrgAppStore)(nil)
var _ store.AppStateStore = (*pgAppStateStore)(nil)
var _ store.FlowStore = (*pgFlowStore)(nil)
var _ store.SchedulerStore = (*pgSchedulerStore)(nil)
var _ store.FleetTemplateStore = (*pgFleetTemplateStore)(nil)
var _ store.FleetPlanStore = (*pgFleetPlanStore)(nil)
var _ store.SkillStore = (*pgSkillStore)(nil)
var _ store.MCPServerStore = (*pgMCPServerStore)(nil)
var _ store.AuditStore = (*pgAuditStore)(nil)
var _ store.TeamManagementStore = (*pgTeamManagementStore)(nil)
