package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/apps"
	"github.com/schardosin/astonish/pkg/store"
)

// ListAppsHandler returns all saved visual apps.
// In platform mode, merges personal + team apps with scope annotation.
// GET /api/apps
func ListAppsHandler(w http.ResponseWriter, r *http.Request) {
	// Platform mode: merge personal + team apps (private-first ownership).
	if svc := store.FromRequest(r); svc != nil && svc.Mode == store.ModePlatform && (svc.PersonalApps != nil || svc.Apps != nil) {
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
		return
	}

	// Personal mode: single store (no scope distinction).
	if svc := store.FromRequest(r); svc != nil && svc.Apps != nil {
		items, err := svc.Apps.List(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if items == nil {
			items = []store.AppListItem{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"apps": items})
		return
	}

	// Legacy fallback: filesystem.
	items, err := apps.ListApps()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"apps": items})
}

// GetAppHandler returns a single app by name including code.
// In platform mode, uses scope-aware resolution: personal-first, team fallback.
// GET /api/apps/{name}
func GetAppHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	scope := r.URL.Query().Get("scope") // optional: "personal" or "team"

	if svc := store.FromRequest(r); svc != nil && (svc.PersonalApps != nil || svc.Apps != nil) {
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
			if err != nil && svc.Apps != nil {
				app, err = svc.Apps.Load(r.Context(), name)
			}
		}

		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(app)
		return
	}

	// Personal mode fallback: filesystem.
	app, err := apps.LoadApp(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(app)
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
// In platform mode, saves to personal by default (team with explicit scope).
// PUT /api/apps/{name}
func SaveAppHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]

	var req saveAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Code == "" {
		http.Error(w, "code is required", http.StatusBadRequest)
		return
	}

	// Platform mode: scope-aware save (personal by default, team with explicit scope).
	if svc := store.FromRequest(r); svc != nil && (svc.PersonalApps != nil || svc.Apps != nil) {
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

		path, err := targetStore.Save(r.Context(), app)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"path":   path,
			"name":   name,
		})
		return
	}

	// Fallback: direct package calls (personal mode / filesystem)
	existing, _ := apps.LoadApp(name)

	app := &apps.VisualApp{
		Name:        name,
		Description: req.Description,
		Code:        req.Code,
		Version:     req.Version,
		SessionID:   req.SessionID,
	}

	if existing != nil {
		app.CreatedAt = existing.CreatedAt
	}

	path, err := apps.SaveApp(app)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"path":   path,
		"name":   name,
	})
}

// DeleteAppHandler removes an app.
// In platform mode, uses scope-aware resolution: personal-first, team fallback.
// DELETE /api/apps/{name}
func DeleteAppHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	scope := r.URL.Query().Get("scope") // optional: "personal" or "team"

	if svc := store.FromRequest(r); svc != nil && (svc.PersonalApps != nil || svc.Apps != nil) {
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
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		// Drop the per-app PG schema if in platform mode
		if svc.AppStateSQL != nil {
			slug := apps.Slugify(name)
			if err := svc.AppStateSQL.DropSchema(r.Context(), slug); err != nil {
				slog.Debug("failed to drop app PG schema", "app", name, "error", err)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		return
	}

	// Personal mode fallback: filesystem.
	if err := apps.DeleteApp(name); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Clean up the associated SQLite database (if any)
	if err := CloseAndDeleteAppDB(name); err != nil {
		slog.Debug("failed to clean up app database", "app", name, "error", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}
