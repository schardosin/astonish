package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/SAP/astonish/pkg/store"
)

// --------------------------------------------------------------------------
// Request / response types
// --------------------------------------------------------------------------

// AppPublishRequest is the payload for publishing a personal app to a team.
type AppPublishRequest struct {
	Slug string `json:"slug"`
}

// AppForkRequest is the payload for forking a team/org app to personal.
type AppForkRequest struct {
	Slug   string `json:"slug"`
	Source string `json:"source"` // "team" or "org"
}

// AppPromoteRequest is the payload for promoting a team app to org.
type AppPromoteRequest struct {
	Slug     string `json:"slug"`
	TeamSlug string `json:"team_slug"`
}

// --------------------------------------------------------------------------
// 6.1: Publish personal app to team
// --------------------------------------------------------------------------

// AppPublishToTeamHandler copies a personal app to the team schema.
//
//	POST /api/apps/publish
//
// Platform mode only. The app definition is copied as-is, preserving code
// and version. The published_by column is set to the current user.
func AppPublishToTeamHandler(w http.ResponseWriter, r *http.Request) {
	svc := RequirePlatformServices(w, r)
	if svc == nil {
		return
	}

	pu := RequireAuth(w, r)
	if pu == nil {
		return
	}

	var req AppPublishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Slug == "" {
		respondError(w, http.StatusBadRequest, "slug is required")
		return
	}

	// Load from personal store (prefer svc.PersonalApps, fall back to TenantRouter)
	var personalApps = svc.PersonalApps
	if personalApps == nil {
		if svc.TenantRouter == nil {
			respondError(w, http.StatusServiceUnavailable, "tenant router not available")
			return
		}
		orgStore, err := svc.TenantRouter.ForOrg(pu.OrgSlug)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to resolve org store")
			return
		}
		personalApps = orgStore.ForUser(pu.ID).Apps()
	}
	app, err := personalApps.Load(r.Context(), req.Slug)
	if err != nil {
		respondError(w, http.StatusNotFound, fmt.Sprintf("personal app not found: %v", err))
		return
	}

	// Save to team store
	if svc.Apps == nil {
		respondError(w, http.StatusServiceUnavailable, "team app store not available")
		return
	}
	slug, err := svc.Apps.Save(r.Context(), app)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to publish app to team: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"published": true,
		"slug":      slug,
		"scope":     "team",
		"message":   fmt.Sprintf("App '%s' published to team", slug),
	})
}

// --------------------------------------------------------------------------
// 6.2: Fork team/org app to personal
// --------------------------------------------------------------------------

// AppForkToPersonalHandler copies a team or org app to the user's personal schema.
//
//	POST /api/apps/fork
//
// Platform mode only. Creates a personal copy that the user can modify
// independently.
func AppForkToPersonalHandler(w http.ResponseWriter, r *http.Request) {
	svc := RequirePlatformServices(w, r)
	if svc == nil {
		return
	}

	pu := RequireAuth(w, r)
	if pu == nil {
		return
	}

	var req AppForkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Slug == "" {
		respondError(w, http.StatusBadRequest, "slug is required")
		return
	}
	if req.Source != "team" && req.Source != "org" {
		respondError(w, http.StatusBadRequest, "source must be 'team' or 'org'")
		return
	}

	// Load from source
	var app any
	var err error
	var orgStore store.OrgDataStore
	if req.Source == "team" {
		if svc.Apps == nil {
			respondError(w, http.StatusServiceUnavailable, "team app store not available")
			return
		}
		app, err = svc.Apps.Load(r.Context(), req.Slug)
	} else {
		if svc.TenantRouter == nil {
			respondError(w, http.StatusServiceUnavailable, "tenant router not available")
			return
		}
		orgStore, err = svc.TenantRouter.ForOrg(pu.OrgSlug)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to resolve org store")
			return
		}
		app, err = orgStore.OrgApps().Load(r.Context(), req.Slug)
	}
	if err != nil {
		respondError(w, http.StatusNotFound, fmt.Sprintf("source app not found: %v", err))
		return
	}

	// Save to personal store (prefer svc.PersonalApps, fall back to TenantRouter)
	var personalApps = svc.PersonalApps
	if personalApps == nil {
		if orgStore == nil {
			if svc.TenantRouter == nil {
				respondError(w, http.StatusServiceUnavailable, "tenant router not available")
				return
			}
			orgStore, err = svc.TenantRouter.ForOrg(pu.OrgSlug)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "failed to resolve org store")
				return
			}
		}
		personalApps = orgStore.ForUser(pu.ID).Apps()
	}
	slug, err := personalApps.Save(r.Context(), app)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to fork app: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"forked":  true,
		"slug":    slug,
		"scope":   "personal",
		"source":  req.Source,
		"message": fmt.Sprintf("App '%s' forked from %s to personal", slug, req.Source),
	})
}

// --------------------------------------------------------------------------
// 6.3: Promote team app to org (admin-only)
// --------------------------------------------------------------------------

// AppPromoteToOrgHandler copies a team app to the org-wide app catalog.
//
//	POST /api/apps/promote
//
// Admin-only. The app definition is stored as JSONB in org_apps with
// promoted_by and promoted_from_team metadata.
func AppPromoteToOrgHandler(w http.ResponseWriter, r *http.Request) {
	svc := RequirePlatformServices(w, r)
	if svc == nil {
		return
	}

	pu := RequireOrgAdmin(w, r)
	if pu == nil {
		return
	}

	var req AppPromoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Slug == "" {
		respondError(w, http.StatusBadRequest, "slug is required")
		return
	}
	if req.TeamSlug == "" {
		respondError(w, http.StatusBadRequest, "team_slug is required")
		return
	}

	if svc.TenantRouter == nil {
		respondError(w, http.StatusServiceUnavailable, "tenant router not available")
		return
	}
	orgStore, err := svc.TenantRouter.ForOrg(pu.OrgSlug)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to resolve org store")
		return
	}

	// Load from team store
	teamApps := orgStore.ForTeam(req.TeamSlug).Apps()
	app, err := teamApps.Load(r.Context(), req.Slug)
	if err != nil {
		respondError(w, http.StatusNotFound, fmt.Sprintf("team app not found: %v", err))
		return
	}

	// Enrich with promotion metadata for org storage
	appData, err := json.Marshal(app)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to serialize app")
		return
	}
	var appMap map[string]any
	if err := json.Unmarshal(appData, &appMap); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to parse app data")
		return
	}
	appMap["promotedBy"] = pu.ID
	appMap["promotedFromTeam"] = req.TeamSlug

	// Save to org store
	orgApps := orgStore.OrgApps()
	slug, err := orgApps.Save(r.Context(), appMap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to promote app to org: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"promoted": true,
		"slug":     slug,
		"scope":    "org",
		"message":  fmt.Sprintf("App '%s' promoted from team '%s' to org", slug, req.TeamSlug),
	})
}

// --------------------------------------------------------------------------
// Org app browsing
// --------------------------------------------------------------------------

// ListOrgAppsHandler lists org-level apps.
//
//	GET /api/apps/org
func ListOrgAppsHandler(w http.ResponseWriter, r *http.Request) {
	svc := RequirePlatformServices(w, r)
	if svc == nil {
		return
	}

	pu := RequireAuth(w, r)
	if pu == nil {
		return
	}

	orgStore, err := svc.TenantRouter.ForOrg(pu.OrgSlug)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to resolve org store")
		return
	}

	items, err := orgStore.OrgApps().List(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list org apps: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"apps": items})
}

// DeleteOrgAppHandler removes an org-level app.
//
//	DELETE /api/apps/org/{name}
//
// Admin-only.
func DeleteOrgAppHandler(w http.ResponseWriter, r *http.Request) {
	svc := RequirePlatformServices(w, r)
	if svc == nil {
		return
	}

	pu := RequireOrgAdmin(w, r)
	if pu == nil {
		return
	}

	name := mux.Vars(r)["name"]
	if name == "" {
		respondError(w, http.StatusBadRequest, "app name required")
		return
	}

	orgStore, err := svc.TenantRouter.ForOrg(pu.OrgSlug)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to resolve org store")
		return
	}

	if err := orgStore.OrgApps().Delete(r.Context(), name); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to delete org app: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}
