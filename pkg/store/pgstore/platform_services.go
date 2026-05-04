package pgstore

import (
	"context"
	"net/http"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/store"
)

// NewPlatformServices creates a Services instance for platform (multi-tenant) mode.
//
// Unlike personal mode where all stores are directly populated, platform mode
// sets only the Platform and TenantRouter fields at startup. The per-tenant
// stores (Sessions, Memories, Credentials, etc.) are resolved per-request by
// TenantMiddleware, which reads the authenticated user's org/team from the
// request context and populates a request-scoped Services clone.
func NewPlatformServices(ctx context.Context, cfg config.PostgresConfig) (*store.Services, *PGStore, error) {
	pgStore, err := New(ctx, cfg.PlatformDSN, cfg)
	if err != nil {
		return nil, nil, err
	}

	svc := &store.Services{
		Mode:         store.ModePlatform,
		Platform:     pgStore,
		TenantRouter: pgStore,
	}

	return svc, pgStore, nil
}

// TenantContext holds the resolved tenant identity for a request.
// This is populated by auth middleware and consumed by TenantMiddleware.
type TenantContext struct {
	OrgSlug  string
	TeamSlug string
	UserID   string
}

type tenantCtxKey struct{}

// WithTenantContext stores the tenant identity in the request context.
// This should be called by the auth middleware after resolving the user's
// organization and team membership.
func WithTenantContext(ctx context.Context, tc *TenantContext) context.Context {
	return context.WithValue(ctx, tenantCtxKey{}, tc)
}

// TenantContextFrom retrieves the tenant identity from the context.
func TenantContextFrom(ctx context.Context) *TenantContext {
	tc, _ := ctx.Value(tenantCtxKey{}).(*TenantContext)
	return tc
}

// TenantMiddleware is an HTTP middleware that resolves the per-tenant stores
// for each request. It reads the TenantContext (set by auth middleware) and
// creates a request-scoped Services clone with the correct org/team/personal
// stores populated.
//
// This middleware should be placed AFTER auth middleware (which sets TenantContext)
// and AFTER the base store.Middleware (which sets the initial Services).
func TenantMiddleware(pgStore *PGStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			baseSvc := store.FromRequest(r)
			if baseSvc == nil || baseSvc.Mode != store.ModePlatform {
				next.ServeHTTP(w, r)
				return
			}

			tc := TenantContextFrom(r.Context())
			if tc == nil || tc.OrgSlug == "" {
				// No tenant context — pass through (might be a platform-level endpoint)
				next.ServeHTTP(w, r)
				return
			}

			// Resolve org data store
			orgStore, err := pgStore.ForOrg(tc.OrgSlug)
			if err != nil {
				http.Error(w, "failed to resolve organization store", http.StatusInternalServerError)
				return
			}

			// Create a request-scoped clone with tenant-specific stores
			reqSvc := &store.Services{
				Mode:         store.ModePlatform,
				Platform:     baseSvc.Platform,
				TenantRouter: baseSvc.TenantRouter,
				Audit:        orgStore.OrgAudit(),
				Skills:       orgStore.OrgSkills(),
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

			// Wire personal stores for private-first ownership.
			// Sessions: regular chat goes to personal; fleet sub-sessions stay in team.
			// Flows: personal by default; publish-to-team for sharing.
			// Apps: personal by default; publish-to-team for sharing.
			// Credentials: saved from chat go to personal; team creds for shared infra.
			if tc.UserID != "" {
				personalStore := orgStore.ForUser(tc.UserID)
				reqSvc.PersonalSessions = personalStore.Sessions()
				reqSvc.PersonalFlows = personalStore.Flows()
				reqSvc.PersonalApps = personalStore.Apps()
				reqSvc.PersonalCredentials = personalStore.Credentials()
			}

				// Per-user app state: scope to the authenticated user
				// so each user gets their own state for shared team apps.
				if ts, ok := teamStore.(*pgTeamDataStore); ok && tc.UserID != "" {
					reqSvc.AppState = &pgAppStateStore{
						pool:   ts.pool,
						schema: ts.schema(),
						userID: tc.UserID,
					}
					reqSvc.AppStateSQL = &pgAppStateSQLStore{
						pool:       ts.pool,
						teamSchema: ts.schema(),
					}
				} else {
					reqSvc.AppState = teamStore.AppState()
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
