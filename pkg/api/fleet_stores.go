package api

import (
	"context"

	"github.com/schardosin/astonish/pkg/store"
)

// FleetStores bundles all tenant-scoped stores that a fleet session needs
// in its run context. In platform mode, these are resolved from the team's
// data store. In personal mode, this is nil (tools fall back to filesystem).
//
// This struct ensures fleet sessions have the same store access as chat
// sessions and scheduled jobs — enabling drill lookup, credential resolution,
// memory search, skill loading, and all other platform-mode capabilities.
type FleetStores struct {
	Flows          store.FlowStore
	DrillReports   store.DrillReportStore
	Credentials    store.CredentialStore
	Skills         *store.SkillStores
	MCPServers     *store.MCPServerStores
	Memory         store.MemoryStore
	MemorySearcher store.ThreeTierSearcher
	Scheduler      store.SchedulerStore
	FleetTemplates store.FleetTemplateStore
	FleetPlans     store.FleetPlanStore

	// MemorySaveOrMerge is the cross-session memory merge function.
	// When set, memory_save operations use LLM-based dedup/merge instead of raw inserts.
	MemorySaveOrMerge store.MemorySaveOrMergeFunc
}

// FleetStoresFromTeam builds a FleetStores from a TeamDataStore and OrgDataStore.
// Used in the daemon/headless path where we have direct access to the data stores
// (e.g., fleet starter/recover closures in daemon/run.go).
//
// platformMCP, when non-nil, makes platform-tier MCP servers (e.g. standard
// servers like Tavily installed at scope=platform) visible to the fleet's chat
// agents. Pass nil only in personal-mode tests.
func FleetStoresFromTeam(team store.TeamDataStore, org store.OrgDataStore, platformMCP store.MCPServerStore) *FleetStores {
	if team == nil {
		return nil
	}
	fs := &FleetStores{
		Flows:          team.Flows(),
		DrillReports:   team.DrillReports(),
		Credentials:    team.Credentials(),
		Memory:         team.Memories(),
		Scheduler:      team.ScheduledJobs(),
		FleetTemplates: team.FleetTemplates(),
		FleetPlans:     team.FleetPlans(),
		Skills: &store.SkillStores{
			Team: team.Skills(),
		},
		MCPServers: &store.MCPServerStores{
			Platform: platformMCP,
			Team:     team.MCPServers(),
		},
	}

	// Include org-level stores when available
	if org != nil {
		fs.Skills.Org = org.OrgSkills()
		fs.MCPServers.Org = org.OrgMCPServers()
	}

	return fs
}

// FleetStoresFromServices builds a FleetStores from a request-scoped Services.
// Used in the Studio UI handler path where stores come from HTTP middleware
// (TenantMiddleware populates the per-request Services with team-scoped stores).
func FleetStoresFromServices(svc *store.Services) *FleetStores {
	if svc == nil || svc.Mode != store.ModePlatform {
		return nil
	}
	fs := &FleetStores{
		Flows:          svc.Flows,
		DrillReports:   svc.DrillReports,
		Credentials:    svc.Credentials,
		Memory:         svc.Memory,
		MemorySearcher: svc.MemorySearcher,
		Scheduler:      svc.Scheduler,
		FleetTemplates: svc.FleetTemplates,
		FleetPlans:     svc.FleetPlans,
	}

	if svc.Skills != nil || svc.TeamSkills != nil {
		fs.Skills = &store.SkillStores{
			Org:  svc.Skills,
			Team: svc.TeamSkills,
		}
	}

	if svc.PlatformMCPServers != nil || svc.MCPServers != nil || svc.TeamMCPServers != nil {
		fs.MCPServers = &store.MCPServerStores{
			Platform: svc.PlatformMCPServers,
			Org:      svc.MCPServers,
			Team:     svc.TeamMCPServers,
		}
	}

	return fs
}

// InjectIntoContext enriches the given context with all non-nil stores from
// this FleetStores. Returns the enriched context. Safe to call on a nil receiver
// (returns the original context unchanged — personal mode no-op).
func (fs *FleetStores) InjectIntoContext(ctx context.Context) context.Context {
	if fs == nil {
		return ctx
	}

	if fs.Flows != nil {
		ctx = store.WithFlowStore(ctx, fs.Flows)
	}
	if fs.DrillReports != nil {
		ctx = store.WithDrillReportStore(ctx, fs.DrillReports)
	}
	if fs.Credentials != nil {
		ctx = store.WithCredentialStore(ctx, fs.Credentials)
	}
	if fs.Skills != nil {
		ctx = store.WithSkillStores(ctx, fs.Skills)
	}
	if fs.MCPServers != nil {
		ctx = store.WithMCPServerStores(ctx, fs.MCPServers)
	}
	if fs.Memory != nil {
		ctx = store.WithMemoryStore(ctx, fs.Memory)
	}
	if fs.MemorySearcher != nil {
		ctx = store.WithThreeTierSearcher(ctx, fs.MemorySearcher)
	}
	if fs.Scheduler != nil {
		ctx = store.WithSchedulerStore(ctx, fs.Scheduler)
	}
	if fs.FleetTemplates != nil {
		ctx = store.WithFleetTemplateStore(ctx, fs.FleetTemplates)
	}
	if fs.FleetPlans != nil {
		ctx = store.WithFleetPlanStore(ctx, fs.FleetPlans)
	}
	if fs.MemorySaveOrMerge != nil {
		ctx = store.WithMemorySaveOrMerge(ctx, fs.MemorySaveOrMerge)
	}

	return ctx
}
