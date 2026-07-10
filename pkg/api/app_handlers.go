package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/apps"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/provider"
	"github.com/schardosin/astonish/pkg/store"
)

// ListAppsHandler returns all saved visual apps.
// Merges personal + team apps with scope annotation.
// GET /api/apps
func ListAppsHandler(w http.ResponseWriter, r *http.Request) {
	svc := store.FromRequest(r)
	if svc == nil || (svc.PersonalApps == nil && svc.Apps == nil) {
		respondError(w, http.StatusInternalServerError, "app store not available")
		return
	}

	var merged []store.AppListItem

	// Personal apps first (user's private apps)
	if svc.PersonalApps != nil {
		items, err := svc.PersonalApps.List(r.Context())
		if err != nil {
			slog.Warn("failed to list personal apps", "error", err)
		} else {
			for i := range items {
				items[i].Scope = "personal"
			}
			merged = append(merged, items...)
		}
	}

	// Team apps (shared/published apps)
	if svc.Apps != nil {
		teamItems, err := svc.Apps.List(r.Context())
		if err != nil {
			slog.Warn("failed to list team apps", "error", err)
		} else {
			for i := range teamItems {
				teamItems[i].Scope = "team"
			}
			merged = append(merged, teamItems...)
		}
	}

	if merged == nil {
		merged = []store.AppListItem{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"apps": merged})
}

// GetAppHandler returns a single app by name including code.
// Uses scope-aware resolution: personal-first, team fallback.
// GET /api/apps/{name}
func GetAppHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	scope := r.URL.Query().Get("scope") // optional: "personal" or "team"

	svc := store.FromRequest(r)
	if svc == nil || (svc.PersonalApps == nil && svc.Apps == nil) {
		respondError(w, http.StatusInternalServerError, "app store not available")
		return
	}

	var app any
	var err error

	// Explicit scope requested
	if scope == "team" && svc.Apps != nil {
		app, err = svc.Apps.Load(r.Context(), name)
	} else if scope == "personal" && svc.PersonalApps != nil {
		app, err = svc.PersonalApps.Load(r.Context(), name)
	} else {
		// Default: try personal first, then team
		if svc.PersonalApps != nil {
			app, err = svc.PersonalApps.Load(r.Context(), name)
		}
		if (app == nil || err != nil) && svc.Apps != nil {
			app, err = svc.Apps.Load(r.Context(), name)
		}
	}

	if err != nil || app == nil {
		msg := "app not found"
		if err != nil {
			msg = err.Error()
		}
		respondError(w, http.StatusNotFound, msg)
		return
	}

	// Per-app pin overlay: empty = not pinned; effective values come from the
	// cascade (platform → org → team → user-default → app-pin). Reports the
	// would-be effective config; no LLM swap. See DECISION-3 in
	// .omo/notepads/per-chat-app-model-pin/decisions.md.
	pinnedProvider, pinnedModel := readAppPin(r, name)
	effectiveProvider, effectiveModel := resolveAppEffective(r, pinnedProvider, pinnedModel)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"app":               app,
		"pinnedProvider":    pinnedProvider,
		"pinnedModel":       pinnedModel,
		"effectiveProvider": effectiveProvider,
		"effectiveModel":    effectiveModel,
	})
}

// saveAppRequest is the JSON body for PUT /api/apps/{name}.
type saveAppRequest struct {
	Description string `json:"description"`
	Code        string `json:"code"`
	Version     int    `json:"version"`
	SessionID   string `json:"sessionId"`
	Scope       string `json:"scope,omitempty"` // "personal" (default) or "team"
}

// SaveAppHandler creates or updates an app.
// Saves to personal by default (team with explicit scope).
// PUT /api/apps/{name}
func SaveAppHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]

	var req saveAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Code == "" {
		respondError(w, http.StatusBadRequest, "code is required")
		return
	}

	svc := store.FromRequest(r)
	if svc == nil || (svc.PersonalApps == nil && svc.Apps == nil) {
		respondError(w, http.StatusInternalServerError, "app store not available")
		return
	}

	// Choose target store
	var targetStore store.AppStore
	if req.Scope == "team" && svc.Apps != nil {
		targetStore = svc.Apps
	} else if svc.PersonalApps != nil {
		targetStore = svc.PersonalApps
	} else {
		targetStore = svc.Apps
	}

	// Try to load existing to preserve creation time
	existingAny, _ := targetStore.Load(r.Context(), name)

	app := &apps.VisualApp{
		Name:        name,
		Description: req.Description,
		Code:        req.Code,
		Version:     req.Version,
		SessionID:   req.SessionID,
	}
	if existing, ok := existingAny.(*apps.VisualApp); ok && existing != nil {
		app.CreatedAt = existing.CreatedAt
	}

	slug, err := targetStore.Save(r.Context(), app)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"path":   slug,
		"name":   name,
	})
}

// DeleteAppHandler removes an app.
// Uses scope-aware resolution: personal-first, team fallback.
// DELETE /api/apps/{name}
func DeleteAppHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	scope := r.URL.Query().Get("scope") // optional: "personal" or "team"

	svc := store.FromRequest(r)
	if svc == nil || (svc.PersonalApps == nil && svc.Apps == nil) {
		respondError(w, http.StatusInternalServerError, "app store not available")
		return
	}

	var err error

	// Explicit scope requested
	if scope == "team" && svc.Apps != nil {
		err = svc.Apps.Delete(r.Context(), name)
	} else if scope == "personal" && svc.PersonalApps != nil {
		err = svc.PersonalApps.Delete(r.Context(), name)
	} else {
		// Default: try personal first, then team
		if svc.PersonalApps != nil {
			err = svc.PersonalApps.Delete(r.Context(), name)
		}
		if err != nil && svc.Apps != nil {
			err = svc.Apps.Delete(r.Context(), name)
		}
	}

	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	// Drop the per-app state database (SQLite .db file or PG schema)
	if svc.AppStateSQL != nil {
		slug := apps.Slugify(name)
		if err := svc.AppStateSQL.DropSchema(r.Context(), slug); err != nil {
			slog.Debug("failed to drop app state schema", "app", name, "error", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}

// patchAppModelRequest is the JSON body for PATCH /api/apps/{name}/model.
// Empty strings clear the pin (see pkg/store/settings.go AppPin contract).
type patchAppModelRequest struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// PatchAppModelHandler persists a per-app provider/model pin.
// PATCH /api/apps/{name}/model
// Body: {"provider":"...","model":"..."} — empty strings clear the pin.
// Platform mode only; requires an authenticated platform user.
func PatchAppModelHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if name == "" {
		respondError(w, http.StatusBadRequest, "app name required")
		return
	}

	svc := RequirePlatformServices(w, r)
	if svc == nil {
		return
	}
	pu := RequireAuth(w, r)
	if pu == nil {
		return
	}

	var req patchAppModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if svc.TenantRouter == nil {
		respondError(w, http.StatusServiceUnavailable, "tenant router not available")
		return
	}
	orgStore, err := svc.TenantRouter.ForOrg(pu.OrgSlug)
	if err != nil || orgStore == nil {
		respondError(w, http.StatusInternalServerError, "failed to resolve org store")
		return
	}

	// Personal-first, team fallback: SetAppPin must target the store that owns
	// the app slug (mirrors GetAppHandler's resolution order).
	scope := ""
	if personalStore := orgStore.ForUser(pu.ID); personalStore != nil {
		if _, perr := personalStore.Apps().Load(r.Context(), name); perr == nil {
			scope = "personal"
		}
	}
	if scope == "" && pu.TeamSlug != "" {
		if teamStore := orgStore.ForTeam(pu.TeamSlug); teamStore != nil {
			if _, terr := teamStore.Apps().Load(r.Context(), name); terr == nil {
				scope = "team"
			}
		}
	}

	var setErr error
	switch scope {
	case "personal":
		setErr = orgStore.ForUser(pu.ID).SetAppPin(r.Context(), name, req.Provider, req.Model)
	case "team":
		setErr = orgStore.ForTeam(pu.TeamSlug).SetAppPin(r.Context(), name, req.Provider, req.Model)
	default:
		respondError(w, http.StatusNotFound, "app not found")
		return
	}
	if setErr != nil {
		if strings.Contains(setErr.Error(), "not found") {
			respondError(w, http.StatusNotFound, "app not found")
			return
		}
		respondError(w, http.StatusInternalServerError, setErr.Error())
		return
	}

	effectiveProvider, effectiveModel := resolveAppEffective(r, req.Provider, req.Model)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"pinnedProvider":    req.Provider,
		"pinnedModel":       req.Model,
		"effectiveProvider": effectiveProvider,
		"effectiveModel":    effectiveModel,
	})
}

// readAppPin returns the persisted provider/model pin for an app, or empty
// strings when unpinned, unavailable, or unauthenticated. Never returns an
// error — empty means "no pin", matching ApplyProviderOverride's no-op
// contract. Personal-first, team fallback.
func readAppPin(r *http.Request, appName string) (pinnedProvider, pinnedModel string) {
	svc := store.FromRequest(r)
	if svc == nil || svc.TenantRouter == nil {
		return "", ""
	}
	pu := GetPlatformUser(r)
	if pu == nil || pu.OrgSlug == "" {
		return "", ""
	}
	orgStore, err := svc.TenantRouter.ForOrg(pu.OrgSlug)
	if err != nil || orgStore == nil {
		return "", ""
	}
	if pu.ID != "" {
		if personalStore := orgStore.ForUser(pu.ID); personalStore != nil {
			if pin, perr := personalStore.AppPin(r.Context(), appName); perr == nil && pin != nil {
				return pin.Provider, pin.Model
			}
		}
	}
	if pu.TeamSlug != "" {
		if teamStore := orgStore.ForTeam(pu.TeamSlug); teamStore != nil {
			if pin, terr := teamStore.AppPin(r.Context(), appName); terr == nil && pin != nil {
				return pin.Provider, pin.Model
			}
		}
	}
	return "", ""
}

// resolveAppEffective applies the app-pin overlay on top of the cascade
// (platform → org → team → user-default → app-pin) and returns the resulting
// provider/model. Empty pin args are no-ops (cascade wins).
func resolveAppEffective(r *http.Request, pinnedProvider, pinnedModel string) (effectiveProvider, effectiveModel string) {
	cfg := effectiveAppConfig(r)
	if cfg == nil {
		cfg = &config.AppConfig{}
	}
	cfg = provider.ApplyProviderOverride(cfg, pinnedProvider, pinnedModel)
	return cfg.General.DefaultProvider, cfg.General.DefaultModel
}
