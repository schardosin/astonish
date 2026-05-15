package store

import (
	"context"
	"net/http"
)

type contextKey string

const servicesKey contextKey = "astonish_services"
const credStoreKey contextKey = "astonish_credential_store"
const memoryStoreKey contextKey = "astonish_memory_store"
const memorySearcherKey contextKey = "astonish_memory_searcher"
const flowStoreKey contextKey = "astonish_flow_store"
const drillReportStoreKey contextKey = "astonish_drill_report_store"
const sessionIDKey contextKey = "astonish_session_id"

// WithServices returns a new context containing the Services instance.
func WithServices(ctx context.Context, svc *Services) context.Context {
	return context.WithValue(ctx, servicesKey, svc)
}

// FromContext retrieves the Services instance from the context.
// Returns nil if no Services is present (e.g., in personal mode before
// Services is wired, or in tests).
func FromContext(ctx context.Context) *Services {
	svc, _ := ctx.Value(servicesKey).(*Services)
	return svc
}

// FromRequest retrieves the Services instance from an HTTP request's context.
// This is a convenience wrapper for handler functions.
func FromRequest(r *http.Request) *Services {
	return FromContext(r.Context())
}

// Middleware returns an HTTP middleware that injects the Services instance
// into every request's context. This should be applied early in the
// middleware chain (after auth, before handlers).
func Middleware(svc *Services) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := WithServices(r.Context(), svc)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// WithCredentialStore returns a new context containing a CredentialStore.
// This is used to propagate the tenant-scoped credential store into the
// ADK runner context so that tool functions can access it without globals.
func WithCredentialStore(ctx context.Context, cs CredentialStore) context.Context {
	return context.WithValue(ctx, credStoreKey, cs)
}

// CredentialStoreFromContext retrieves the CredentialStore from a context.
// Returns nil if no CredentialStore is present (personal mode or tests).
// Tool functions should call this and fall back to the package-level global
// credential store when nil.
func CredentialStoreFromContext(ctx context.Context) CredentialStore {
	cs, _ := ctx.Value(credStoreKey).(CredentialStore)
	return cs
}

// WithMemoryStore returns a new context containing a tenant-scoped MemoryStore.
// Used to propagate the PG team memory store into the ADK runner context.
func WithMemoryStore(ctx context.Context, ms MemoryStore) context.Context {
	return context.WithValue(ctx, memoryStoreKey, ms)
}

// MemoryStoreFromContext retrieves the MemoryStore from a context.
// Returns nil if no MemoryStore is present (personal mode or tests).
func MemoryStoreFromContext(ctx context.Context) MemoryStore {
	ms, _ := ctx.Value(memoryStoreKey).(MemoryStore)
	return ms
}

// WithThreeTierSearcher returns a new context containing a ThreeTierSearcher.
// Used to propagate the cross-tier memory searcher into the ADK runner context.
func WithThreeTierSearcher(ctx context.Context, ts ThreeTierSearcher) context.Context {
	return context.WithValue(ctx, memorySearcherKey, ts)
}

// ThreeTierSearcherFromContext retrieves the ThreeTierSearcher from a context.
// Returns nil if no ThreeTierSearcher is present (personal mode or tests).
func ThreeTierSearcherFromContext(ctx context.Context) ThreeTierSearcher {
	ts, _ := ctx.Value(memorySearcherKey).(ThreeTierSearcher)
	return ts
}

// WithFlowStore returns a new context containing a tenant-scoped FlowStore.
// Used to propagate the PG flow store into the ADK runner context so that
// drill tools (save_drill, list_drills, etc.) can read/write flows from the
// database rather than the local filesystem in platform mode.
func WithFlowStore(ctx context.Context, fs FlowStore) context.Context {
	return context.WithValue(ctx, flowStoreKey, fs)
}

// FlowStoreFromContext retrieves the FlowStore from a context.
// Returns nil if no FlowStore is present (personal mode or tests).
func FlowStoreFromContext(ctx context.Context) FlowStore {
	fs, _ := ctx.Value(flowStoreKey).(FlowStore)
	return fs
}

// WithDrillReportStore returns a new context containing a tenant-scoped DrillReportStore.
// Used to propagate the PG drill report store into the ADK runner context so that
// the run_drill tool can persist execution results to the database in platform mode.
func WithDrillReportStore(ctx context.Context, rs DrillReportStore) context.Context {
	return context.WithValue(ctx, drillReportStoreKey, rs)
}

// DrillReportStoreFromContext retrieves the DrillReportStore from a context.
// Returns nil if no DrillReportStore is present (personal mode or tests).
func DrillReportStoreFromContext(ctx context.Context) DrillReportStore {
	rs, _ := ctx.Value(drillReportStoreKey).(DrillReportStore)
	return rs
}

const skillStoresKey contextKey = "astonish_skill_stores"
const schedulerStoreKey contextKey = "astonish_scheduler_store"
const mcpServerStoresKey contextKey = "astonish_mcp_server_stores"

// SkillStores holds references to both org and team skill stores
// for use in tool context injection.
type SkillStores struct {
	Org  SkillStore // org-level skill store (nil if not in platform mode)
	Team SkillStore // team-level skill store (nil if not in platform mode)
}

// WithSkillStores returns a new context containing the SkillStores.
// Used to propagate tenant-scoped skill stores into the ADK runner context
// so that the skill_lookup tool can resolve skills dynamically per-request.
func WithSkillStores(ctx context.Context, ss *SkillStores) context.Context {
	return context.WithValue(ctx, skillStoresKey, ss)
}

// SkillStoresFromContext retrieves the SkillStores from a context.
// Returns nil if no SkillStores is present (personal mode or tests).
func SkillStoresFromContext(ctx context.Context) *SkillStores {
	if ctx == nil {
		return nil
	}
	ss, _ := ctx.Value(skillStoresKey).(*SkillStores)
	return ss
}

// WithSchedulerStore returns a new context containing a tenant-scoped SchedulerStore.
// Used to propagate the team's scheduler store into the ADK runner context so that
// the schedule_job and list_scheduled_jobs tools can operate on the correct team's jobs.
func WithSchedulerStore(ctx context.Context, ss SchedulerStore) context.Context {
	return context.WithValue(ctx, schedulerStoreKey, ss)
}

// SchedulerStoreFromContext retrieves the SchedulerStore from a context.
// Returns nil if no SchedulerStore is present (personal mode or tests).
func SchedulerStoreFromContext(ctx context.Context) SchedulerStore {
	if ctx == nil {
		return nil
	}
	ss, _ := ctx.Value(schedulerStoreKey).(SchedulerStore)
	return ss
}

// MCPServerStores holds references to both org and team MCP server stores
// for use in tool context injection.
type MCPServerStores struct {
	Org  MCPServerStore // org-level MCP server store (nil if not in platform mode)
	Team MCPServerStore // team-level MCP server store (nil if not in platform mode)
}

// WithMCPServerStores returns a new context containing the MCPServerStores.
// Used to propagate tenant-scoped MCP server stores into the ADK runner context
// so that the agent can resolve MCP servers dynamically per-request.
func WithMCPServerStores(ctx context.Context, ms *MCPServerStores) context.Context {
	return context.WithValue(ctx, mcpServerStoresKey, ms)
}

// MCPServerStoresFromContext retrieves the MCPServerStores from a context.
// Returns nil if no MCPServerStores is present (personal mode or tests).
func MCPServerStoresFromContext(ctx context.Context) *MCPServerStores {
	if ctx == nil {
		return nil
	}
	ms, _ := ctx.Value(mcpServerStoresKey).(*MCPServerStores)
	return ms
}

const fleetTemplateStoreKey contextKey = "astonish_fleet_template_store"
const fleetPlanStoreKey contextKey = "astonish_fleet_plan_store"

// WithFleetTemplateStore returns a new context containing a tenant-scoped FleetTemplateStore.
// Used to propagate the PG fleet template store into the ADK runner context so that
// fleet tools can resolve templates from the database in platform mode.
func WithFleetTemplateStore(ctx context.Context, fs FleetTemplateStore) context.Context {
	return context.WithValue(ctx, fleetTemplateStoreKey, fs)
}

// FleetTemplateStoreFromContext retrieves the FleetTemplateStore from a context.
// Returns nil if no FleetTemplateStore is present (personal mode or tests).
func FleetTemplateStoreFromContext(ctx context.Context) FleetTemplateStore {
	if ctx == nil {
		return nil
	}
	fs, _ := ctx.Value(fleetTemplateStoreKey).(FleetTemplateStore)
	return fs
}

// WithFleetPlanStore returns a new context containing a tenant-scoped FleetPlanStore.
// Used to propagate the PG fleet plan store into the ADK runner context so that
// fleet tools can read/write plans from the database in platform mode.
func WithFleetPlanStore(ctx context.Context, fs FleetPlanStore) context.Context {
	return context.WithValue(ctx, fleetPlanStoreKey, fs)
}

// FleetPlanStoreFromContext retrieves the FleetPlanStore from a context.
// Returns nil if no FleetPlanStore is present (personal mode or tests).
func FleetPlanStoreFromContext(ctx context.Context) FleetPlanStore {
	if ctx == nil {
		return nil
	}
	fs, _ := ctx.Value(fleetPlanStoreKey).(FleetPlanStore)
	return fs
}

const sandboxTemplateKey contextKey = "astonish_sandbox_template"
const sandboxLayerChainKey contextKey = "astonish_sandbox_layer_chain"
const sessionServiceKey contextKey = "astonish_session_service"
const userIDKey contextKey = "astonish_user_id"

// WithSandboxTemplate returns a new context containing the team's sandbox
// template name. Used to propagate the team's custom container template into
// the ADK runner context so that NodeTool can create containers with the
// correct template instead of always using @base.
func WithSandboxTemplate(ctx context.Context, tpl string) context.Context {
	return context.WithValue(ctx, sandboxTemplateKey, tpl)
}

// SandboxTemplateFromContext retrieves the sandbox template name from a context.
// Returns "" if no template is present (personal mode, tests, or team has no
// custom template — which causes the sandbox to fall back to @base).
func SandboxTemplateFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	tpl, _ := ctx.Value(sandboxTemplateKey).(string)
	return tpl
}

// WithSandboxLayerChain returns a new context containing the resolved overlay
// layer chain (ordered oldest-first, e.g. ["@base", "<sha256>"]). This is the
// pre-resolved chain that the K8s backend passes directly to
// SessionSpec.LayerChain, bypassing the template-name-to-SHA indirection.
//
// When present, backends MUST use this chain instead of treating the template
// name as a literal layer ID.
func WithSandboxLayerChain(ctx context.Context, chain []string) context.Context {
	return context.WithValue(ctx, sandboxLayerChainKey, chain)
}

// SandboxLayerChainFromContext retrieves the resolved layer chain from context.
// Returns nil if no chain is present (personal mode, Incus backend, or team
// has no custom template). Callers should fall back to the template name if nil.
func SandboxLayerChainFromContext(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	chain, _ := ctx.Value(sandboxLayerChainKey).([]string)
	return chain
}

// WithSessionService returns a new context containing a tenant-scoped SessionStore.
// Used to propagate the per-request session service (e.g., pgstore PersonalSessions)
// into the ADK runner context so that sub-agents (delegate_tasks) create child
// sessions in the correct store rather than the factory-time default.
func WithSessionService(ctx context.Context, ss SessionStore) context.Context {
	return context.WithValue(ctx, sessionServiceKey, ss)
}

// SessionServiceFromContext retrieves the SessionStore from a context.
// Returns nil if no SessionStore is present (personal mode or tests).
// SubAgentManager checks this to prefer the per-request store over its default.
func SessionServiceFromContext(ctx context.Context) SessionStore {
	if ctx == nil {
		return nil
	}
	ss, _ := ctx.Value(sessionServiceKey).(SessionStore)
	return ss
}

// WithUserID returns a new context containing the effective user ID.
// Used to propagate the per-request platform user ID (UUID) into the ADK runner
// context so that sub-agents create child sessions with the correct user_id
// (required by pgstore where user_id is a UUID column).
func WithUserID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, userIDKey, id)
}

// UserIDFromContext retrieves the user ID from a context.
// Returns "" if no user ID is present (personal mode, tests, or non-platform contexts).
func UserIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(userIDKey).(string)
	return id
}

// SystemUserID is the nil UUID used for system-initiated sessions (fleet plans
// without an owner, scheduler jobs, and other headless execution contexts).
// It represents "the platform acting autonomously" when no human user is
// associated with the action. Universally recognizable as a system identity.
const SystemUserID = "00000000-0000-0000-0000-000000000000"

// --- Disabled Tools (per-team tool restrictions) ---

type disabledToolsKey struct{}

// WithDisabledTools attaches a set of disabled tool names to the context.
// Tools in this set will be filtered from the agent's tool list per-request.
func WithDisabledTools(ctx context.Context, names []string) context.Context {
	if len(names) == 0 {
		return ctx
	}
	return context.WithValue(ctx, disabledToolsKey{}, names)
}

// DisabledToolsFromContext retrieves the disabled tool names from the context.
// Returns nil if no restrictions are set (personal mode or unrestricted team).
func DisabledToolsFromContext(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	names, _ := ctx.Value(disabledToolsKey{}).([]string)
	return names
}

// --- Tenant Identity (org/team slug propagation) ---

type orgSlugKey struct{}
type teamSlugKey struct{}

// WithOrgSlug attaches the organization slug to the context.
// Used to propagate tenant identity into the ADK runner context.
func WithOrgSlug(ctx context.Context, slug string) context.Context {
	return context.WithValue(ctx, orgSlugKey{}, slug)
}

// OrgSlugFromContext retrieves the organization slug from the context.
// Returns "" if not in platform mode or if the context lacks tenant identity.
func OrgSlugFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	s, _ := ctx.Value(orgSlugKey{}).(string)
	return s
}

// WithTeamSlug attaches the team slug to the context.
// Used to propagate tenant identity into the ADK runner context.
func WithTeamSlug(ctx context.Context, slug string) context.Context {
	return context.WithValue(ctx, teamSlugKey{}, slug)
}

// TeamSlugFromContext retrieves the team slug from the context.
// Returns "" if not in platform mode or if the context lacks tenant identity.
func TeamSlugFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	s, _ := ctx.Value(teamSlugKey{}).(string)
	return s
}

// --- Run Job Function (for scheduler test execution) ---

// RunJobFunc executes a scheduled job by ID and returns its output.
// This is injected into the tool context so that the schedule_job tool can
// trigger test execution without going through the unauthenticated HTTP bridge.
type RunJobFunc func(ctx context.Context, jobID string) (string, error)

type runJobFuncKey struct{}

// WithRunJobFunc injects a RunJobFunc into the context.
func WithRunJobFunc(ctx context.Context, fn RunJobFunc) context.Context {
	return context.WithValue(ctx, runJobFuncKey{}, fn)
}

// RunJobFuncFromContext retrieves the RunJobFunc from the context.
// Returns nil if not available (personal mode or not injected).
func RunJobFuncFromContext(ctx context.Context) RunJobFunc {
	if ctx == nil {
		return nil
	}
	fn, _ := ctx.Value(runJobFuncKey{}).(RunJobFunc)
	return fn
}

// WithSessionID returns a new context containing the active session ID.
// This is used to tag memories created during a session.
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDKey, sessionID)
}

// SessionIDFromContext retrieves the active session ID from the context.
// Returns empty string if no session ID is present.
func SessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	s, _ := ctx.Value(sessionIDKey).(string)
	return s
}

// --- Memory Merge Function (cross-session dedup) ---

// MemorySaveOrMergeFunc is a function that saves a memory entry, performing
// cross-session deduplication and LLM-based merge when an existing entry with
// a related category already exists. If no merge function is in context,
// callers should fall back to a raw memStore.Add().
type MemorySaveOrMergeFunc func(ctx context.Context, memStore MemoryStore, entry MemoryEntry) error

type memorySaveOrMergeKey struct{}

// WithMemorySaveOrMerge injects a MemorySaveOrMergeFunc into the context.
// This is set by the launcher when wiring the ChatRunner in platform mode,
// allowing the memory_save tool to perform cross-session dedup without
// needing direct access to the LLM or agent.MemoryMerger.
func WithMemorySaveOrMerge(ctx context.Context, fn MemorySaveOrMergeFunc) context.Context {
	return context.WithValue(ctx, memorySaveOrMergeKey{}, fn)
}

// MemorySaveOrMergeFromContext retrieves the MemorySaveOrMergeFunc from context.
// Returns nil if not available (personal mode or not injected).
func MemorySaveOrMergeFromContext(ctx context.Context) MemorySaveOrMergeFunc {
	if ctx == nil {
		return nil
	}
	fn, _ := ctx.Value(memorySaveOrMergeKey{}).(MemorySaveOrMergeFunc)
	return fn
}
