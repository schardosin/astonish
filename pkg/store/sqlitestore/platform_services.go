package sqlitestore

import (
	"context"
	"net/http"

	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/pgstore"
)

// NewPlatformServices creates a Services instance for platform mode backed by SQLite.
//
// This parallels pgstore.NewPlatformServices. Per-tenant stores are resolved
// per-request by TenantMiddleware, which reads the authenticated user's org/team
// from the request context and populates a request-scoped Services clone.
func NewPlatformServices(ctx context.Context, dataDir string) (*store.Services, *SQLiteStore, error) {
	sqlStore, err := New(ctx, dataDir)
	if err != nil {
		return nil, nil, err
	}

	svc := &store.Services{
		Mode:             store.ModePlatform,
		Platform:         sqlStore,
		TenantRouter:     sqlStore,
		PlatformSettings: sqlStore.PlatformSettings(),
	}

	return svc, sqlStore, nil
}

// TenantMiddleware is an HTTP middleware that resolves per-tenant stores for SQLite.
// It mirrors the pgstore.TenantMiddleware behavior: reads TenantContext from auth
// middleware and creates a request-scoped Services clone with org/team/personal
// stores populated from the appropriate SQLite databases.
//
// This middleware should be placed AFTER auth middleware (which sets TenantContext)
// and AFTER the base store.Middleware (which sets the initial Services).
func TenantMiddleware(sqlStore *SQLiteStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			baseSvc := store.FromRequest(r)
			if baseSvc == nil || baseSvc.Mode != store.ModePlatform {
				next.ServeHTTP(w, r)
				return
			}

			tc := pgstore.TenantContextFrom(r.Context())
			if tc == nil || tc.OrgSlug == "" {
				// No tenant context — pass through (platform-level endpoint)
				next.ServeHTTP(w, r)
				return
			}

			// Resolve org data store
			orgStore, err := sqlStore.ForOrg(tc.OrgSlug)
			if err != nil {
				http.Error(w, "failed to resolve organization store", http.StatusInternalServerError)
				return
			}

			// Create a request-scoped clone with tenant-specific stores
			reqSvc := &store.Services{
				Mode:               store.ModePlatform,
				Platform:           baseSvc.Platform,
				TenantRouter:       baseSvc.TenantRouter,
				Audit:              orgStore.OrgAudit(),
				Skills:             orgStore.OrgSkills(),
				MCPServers:         orgStore.OrgMCPServers(),
				PlatformMCPServers: sqlStore.PlatformMCPServers(),
				PlatformSettings:   sqlStore.PlatformSettings(),
				OrgSettings:        sqlStore.OrgSettings(tc.OrgSlug),
			}

			// Populate team-scoped stores if team is known
			if tc.TeamSlug != "" {
				teamStore := orgStore.ForTeam(tc.TeamSlug)
				reqSvc.Sessions = teamStore.Sessions()
				reqSvc.Memory = teamStore.Memories()
				reqSvc.Credentials = teamStore.Credentials()
				reqSvc.Apps = teamStore.Apps()
				reqSvc.Flows = teamStore.Flows()
				reqSvc.Scheduler = teamStore.ScheduledJobs()
				reqSvc.FleetTemplates = teamStore.FleetTemplates()
				reqSvc.FleetPlans = teamStore.FleetPlans()
				reqSvc.DrillReports = teamStore.DrillReports()
				reqSvc.TeamSkills = teamStore.Skills()
				reqSvc.TeamMCPServers = teamStore.MCPServers()
				reqSvc.Settings = teamStore.Settings()
				reqSvc.AppState = teamStore.AppState()
				reqSvc.AppStateSQL = sqlStore.AppStateSQL()

				// Wire personal stores for private-first ownership
				if tc.UserID != "" {
					personalStore := orgStore.ForUser(tc.UserID)
					reqSvc.PersonalSessions = personalStore.Sessions()
					reqSvc.PersonalFlows = personalStore.Flows()
					reqSvc.PersonalApps = personalStore.Apps()
					reqSvc.PersonalCredentials = personalStore.Credentials()
				}

				// Build three-tier memory searcher (personal + team + org)
				var personalMem store.MemoryStore
				if tc.UserID != "" {
					personalMem = orgStore.ForUser(tc.UserID).Memories()
				}
				reqSvc.MemorySearcher = store.NewThreeTierSearcher(store.ThreeTierMemoryStoreConfig{
					Personal: personalMem,
					Team:     teamStore.Memories(),
					Org:      orgStore.OrgMemories(),
				})
			}

			// Inject into request context
			ctx := store.WithServices(r.Context(), reqSvc)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
