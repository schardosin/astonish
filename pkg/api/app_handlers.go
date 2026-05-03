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
// GET /api/apps
func ListAppsHandler(w http.ResponseWriter, r *http.Request) {
	// Use store abstraction if available, fall back to direct package calls.
	if svc := store.FromRequest(r); svc != nil && svc.Apps != nil {
		items, err := svc.Apps.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"apps": items})
		return
	}

	items, err := apps.ListApps()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"apps": items})
}

// GetAppHandler returns a single app by name including code.
// GET /api/apps/{name}
func GetAppHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]

	if svc := store.FromRequest(r); svc != nil && svc.Apps != nil {
		app, err := svc.Apps.Load(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(app)
		return
	}

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
}

// SaveAppHandler creates or updates an app.
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

	if svc := store.FromRequest(r); svc != nil && svc.Apps != nil {
		// Try to load existing to preserve creation time
		existingAny, _ := svc.Apps.Load(name)

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

		path, err := svc.Apps.Save(app)
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

	// Fallback: direct package calls
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
// DELETE /api/apps/{name}
func DeleteAppHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]

	if svc := store.FromRequest(r); svc != nil && svc.Apps != nil {
		if err := svc.Apps.Delete(name); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		// Drop the per-app PG schema if in platform mode
		if svc.AppStateSQL != nil {
			slug := apps.Slugify(name)
			if err := svc.AppStateSQL.DropSchema(r.Context(), slug); err != nil {
				slog.Debug("failed to drop app PG schema", "app", name, "error", err)
			}
		} else {
			// Personal mode with store — clean up SQLite
			if err := CloseAndDeleteAppDB(name); err != nil {
				slog.Debug("failed to clean up app database", "app", name, "error", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		return
	}

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
