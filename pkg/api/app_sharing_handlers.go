package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/schardosin/astonish/pkg/store"
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
	svc := store.FromRequest(r)
	if svc == nil || svc.Mode != store.ModePlatform {
		http.Error(w, "platform mode required", http.StatusBadRequest)
		return
	}

	pu := GetPlatformUser(r)
	if pu == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}

	var req AppPublishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Slug == "" {
		http.Error(w, "slug is required", http.StatusBadRequest)
		return
	}

	// Load from personal store (prefer svc.PersonalApps, fall back to TenantRouter)
	var personalApps store.AppStore
	if svc.PersonalApps != nil {
		personalApps = svc.PersonalApps
	} else {
		if svc.TenantRouter == nil {
			http.Error(w, "tenant router not available", http.StatusServiceUnavailable)
			return
		}
		orgStore, err := svc.TenantRouter.ForOrg(pu.OrgSlug)
		if err != nil {
			http.Error(w, "failed to resolve org store", http.StatusInternalServerError)
			return
		}
		personalApps = orgStore.ForUser(pu.ID).Apps()
	}
	app, err := personalApps.Load(req.Slug)
	if err != nil {
		http.Error(w, fmt.Sprintf("personal app not found: %v", err), http.StatusNotFound)
		return
	}

	// Save to team store
	if svc.Apps == nil {
		http.Error(w, "team app store not available", http.StatusServiceUnavailable)
		return
	}
	slug, err := svc.Apps.Save(app)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to publish app to team: %v", err), http.StatusInternalServerError)
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
	svc := store.FromRequest(r)
	if svc == nil || svc.Mode != store.ModePlatform {
		http.Error(w, "platform mode required", http.StatusBadRequest)
		return
	}

	pu := GetPlatformUser(r)
	if pu == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}

	var req AppForkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Slug == "" {
		http.Error(w, "slug is required", http.StatusBadRequest)
		return
	}
	if req.Source != "team" && req.Source != "org" {
		http.Error(w, "source must be 'team' or 'org'", http.StatusBadRequest)
		return
	}

	// Load from source
	var app any
	var err error
	var orgStore store.OrgDataStore
	if req.Source == "team" {
		if svc.Apps == nil {
			http.Error(w, "team app store not available", http.StatusServiceUnavailable)
			return
		}
		app, err = svc.Apps.Load(req.Slug)
	} else {
		if svc.TenantRouter == nil {
			http.Error(w, "tenant router not available", http.StatusServiceUnavailable)
			return
		}
		orgStore, err = svc.TenantRouter.ForOrg(pu.OrgSlug)
		if err != nil {
			http.Error(w, "failed to resolve org store", http.StatusInternalServerError)
			return
		}
		app, err = orgStore.OrgApps().Load(req.Slug)
	}
	if err != nil {
		http.Error(w, fmt.Sprintf("source app not found: %v", err), http.StatusNotFound)
		return
	}

	// Save to personal store (prefer svc.PersonalApps, fall back to TenantRouter)
	var personalApps store.AppStore
	if svc.PersonalApps != nil {
		personalApps = svc.PersonalApps
	} else {
		if orgStore == nil {
			if svc.TenantRouter == nil {
				http.Error(w, "tenant router not available", http.StatusServiceUnavailable)
				return
			}
			orgStore, err = svc.TenantRouter.ForOrg(pu.OrgSlug)
			if err != nil {
				http.Error(w, "failed to resolve org store", http.StatusInternalServerError)
				return
			}
		}
		personalApps = orgStore.ForUser(pu.ID).Apps()
	}
	slug, err := personalApps.Save(app)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to fork app: %v", err), http.StatusInternalServerError)
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
	svc := store.FromRequest(r)
	if svc == nil || svc.Mode != store.ModePlatform {
		http.Error(w, "platform mode required", http.StatusBadRequest)
		return
	}

	pu := GetPlatformUser(r)
	if pu == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if !CanManageOrg(pu) {
		http.Error(w, "admin role required for app promotion", http.StatusForbidden)
		return
	}

	var req AppPromoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Slug == "" {
		http.Error(w, "slug is required", http.StatusBadRequest)
		return
	}
	if req.TeamSlug == "" {
		http.Error(w, "team_slug is required", http.StatusBadRequest)
		return
	}

	if svc.TenantRouter == nil {
		http.Error(w, "tenant router not available", http.StatusServiceUnavailable)
		return
	}
	orgStore, err := svc.TenantRouter.ForOrg(pu.OrgSlug)
	if err != nil {
		http.Error(w, "failed to resolve org store", http.StatusInternalServerError)
		return
	}

	// Load from team store
	teamApps := orgStore.ForTeam(req.TeamSlug).Apps()
	app, err := teamApps.Load(req.Slug)
	if err != nil {
		http.Error(w, fmt.Sprintf("team app not found: %v", err), http.StatusNotFound)
		return
	}

	// Enrich with promotion metadata for org storage
	appData, err := json.Marshal(app)
	if err != nil {
		http.Error(w, "failed to serialize app", http.StatusInternalServerError)
		return
	}
	var appMap map[string]any
	if err := json.Unmarshal(appData, &appMap); err != nil {
		http.Error(w, "failed to parse app data", http.StatusInternalServerError)
		return
	}
	appMap["promotedBy"] = pu.ID
	appMap["promotedFromTeam"] = req.TeamSlug

	// Save to org store
	orgApps := orgStore.OrgApps()
	slug, err := orgApps.Save(appMap)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to promote app to org: %v", err), http.StatusInternalServerError)
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
	svc := store.FromRequest(r)
	if svc == nil || svc.Mode != store.ModePlatform {
		http.Error(w, "platform mode required", http.StatusBadRequest)
		return
	}

	pu := GetPlatformUser(r)
	if pu == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}

	orgStore, err := svc.TenantRouter.ForOrg(pu.OrgSlug)
	if err != nil {
		http.Error(w, "failed to resolve org store", http.StatusInternalServerError)
		return
	}

	items, err := orgStore.OrgApps().List()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to list org apps: %v", err), http.StatusInternalServerError)
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
	svc := store.FromRequest(r)
	if svc == nil || svc.Mode != store.ModePlatform {
		http.Error(w, "platform mode required", http.StatusBadRequest)
		return
	}

	pu := GetPlatformUser(r)
	if pu == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if !CanManageOrg(pu) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}

	name := mux.Vars(r)["name"]
	if name == "" {
		http.Error(w, "app name required", http.StatusBadRequest)
		return
	}

	orgStore, err := svc.TenantRouter.ForOrg(pu.OrgSlug)
	if err != nil {
		http.Error(w, "failed to resolve org store", http.StatusInternalServerError)
		return
	}

	if err := orgStore.OrgApps().Delete(name); err != nil {
		http.Error(w, fmt.Sprintf("failed to delete org app: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}
