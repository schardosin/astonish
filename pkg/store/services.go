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

	// Sessions provides access to the session store.
	// This wraps the ADK session.Service and adds Astonish metadata operations.
	Sessions SessionStore

	// Memory provides access to the vector + BM25 memory search system.
	Memory MemoryStore

	// MemoryMgr provides higher-level memory operations (load/append MEMORY.md).
	MemoryMgr MemoryManager

	// Credentials provides access to the encrypted credential store.
	Credentials CredentialStore

	// Apps provides access to generative UI app definitions.
	Apps AppStore

	// AppState provides access to per-app persistent state.
	AppState AppStateStore

	// AppStateSQL provides raw SQL execution against per-app PostgreSQL schemas.
	// Only populated in platform mode; nil in personal mode (SQLite used directly).
	AppStateSQL AppStateSQLStore

	// Flows provides access to flow/agent definitions and the tap registry.
	Flows FlowStore

	// Scheduler provides access to scheduled job persistence.
	Scheduler SchedulerStore

	// FleetTemplates provides access to fleet template definitions.
	FleetTemplates FleetTemplateStore

	// FleetPlans provides access to fleet plan definitions.
	FleetPlans FleetPlanStore

	// Skills provides access to operational knowledge skills.
	Skills SkillStore

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
