package store

// Services is the central dependency container for Astonish.
//
// It replaces the package-level Set*/Get* globals that were previously used
// to wire dependencies between the daemon, launchers, API handlers, and tools.
//
// In personal mode, all stores are backed by the local filesystem.
// In platform mode, stores are backed by PostgreSQL with per-org/team scoping.
//
// Services is created once at startup (in daemon.Run or launcher entry points)
// and passed to all consumers via function parameters or request context.
type Services struct {
	// Mode indicates the deployment mode.
	Mode DeploymentMode

	// Sessions provides access to the team-scoped session store.
	// In platform mode this holds fleet sub-sessions (team-shared).
	// Regular chat sessions use PersonalSessions instead.
	Sessions SessionStore

	// PersonalSessions provides access to the user's private session store.
	// Regular chat sessions are always created here. Fleet sub-sessions
	// remain in the team-scoped Sessions store.
	PersonalSessions SessionStore

	// Memory provides access to the vector + BM25 memory search system.
	Memory MemoryStore

	// MemoryMgr provides higher-level memory operations (load/append MEMORY.md).
	MemoryMgr MemoryManager

	// Credentials provides access to the team-scoped encrypted credential store.
	// In platform mode, these are credentials shared across the team (app-to-app auth).
	Credentials CredentialStore

	// PersonalCredentials provides access to the user's private credential store.
	// Credentials saved from chat go here by default (user identity credentials).
	// Users can explicitly publish credentials to team when sharing is needed.
	PersonalCredentials CredentialStore

	// Apps provides access to team-shared generative UI app definitions.
	// In platform mode, these are apps that have been published to the team.
	Apps AppStore

	// PersonalApps provides access to the user's private app definitions.
	// Apps are created here by default and explicitly published to team when ready.
	PersonalApps AppStore

	// AppState provides access to per-app persistent state.
	AppState AppStateStore

	// AppStateSQL provides raw SQL execution against per-app PostgreSQL schemas.
	// Only populated in platform mode; nil in personal mode (SQLite used directly).
	AppStateSQL AppStateSQLStore

	// Flows provides access to team-shared flow/agent definitions.
	// In platform mode, these are flows that have been published to the team.
	Flows FlowStore

	// PersonalFlows provides access to the user's private flow/agent definitions.
	// Flows are created here by default and explicitly published to team when ready.
	PersonalFlows FlowStore

	// Scheduler provides access to scheduled job persistence.
	Scheduler SchedulerStore

	// FleetTemplates provides access to fleet template definitions.
	FleetTemplates FleetTemplateStore

	// FleetPlans provides access to fleet plan definitions.
	FleetPlans FleetPlanStore

	// Skills provides access to org-level skill definitions.
	// In platform mode, these are skills shared across all teams in the org.
	Skills SkillStore

	// TeamSkills provides access to team-scoped skill definitions.
	// Team skills override org skills of the same name. Team admins manage these.
	TeamSkills SkillStore

	// MCPServers provides access to org-level MCP server configurations.
	// In platform mode, these are MCP servers shared across all teams in the org.
	MCPServers MCPServerStore

	// TeamMCPServers provides access to team-scoped MCP server configurations.
	// Team MCP servers override org MCP servers of the same name. Team admins manage these.
	TeamMCPServers MCPServerStore

	// DrillReports provides access to drill test report persistence.
	DrillReports DrillReportStore

	// Audit provides audit logging.
	Audit AuditStore

	// MemorySearcher provides cross-tier memory search in platform mode.
	// In personal mode this is nil (use Memory directly).
	MemorySearcher ThreeTierSearcher

	// Platform-only fields (nil in personal mode).
	Platform     PlatformStore
	TenantRouter TenantRouter
}

// DeploymentMode indicates whether Astonish is running in personal or platform mode.
type DeploymentMode string

const (
	// ModePersonal is the default single-user mode with file-based storage.
	ModePersonal DeploymentMode = "personal"

	// ModePlatform is the multi-tenant mode with PostgreSQL storage.
	ModePlatform DeploymentMode = "platform"
)
