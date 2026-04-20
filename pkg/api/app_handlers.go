package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/apps"
)

// ListAppsHandler returns all saved visual apps.
// GET /api/apps
func ListAppsHandler(w http.ResponseWriter, r *http.Request) {
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

	// Try to load existing to preserve creation time
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
	if err := apps.DeleteApp(name); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}
