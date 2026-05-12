package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/flowstore"
	"github.com/schardosin/astonish/pkg/store"
)

// validTapURL matches GitHub repository URL formats accepted by AddTap:
//   - "owner" or "owner/repo" (public GitHub shorthand)
//   - "github.com/owner/repo"
//   - "github.enterprise.com/owner/repo"
//
// Only alphanumeric, hyphens, underscores, dots, and slashes are allowed.
// This prevents SSRF by ensuring the URL cannot contain crafted hostnames
// targeting internal services.
var validTapURL = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,253}(/[a-zA-Z0-9][a-zA-Z0-9._-]{0,99}){0,2}$`)

// FlowStoreListResponse is the response for GET /api/flow-store
type FlowStoreListResponse struct {
	Taps  []TapInfo  `json:"taps"`
	Flows []FlowInfo `json:"flows"`
}

// TapInfo represents a tap for the UI
type TapInfo struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	IsOfficial bool   `json:"isOfficial"`
}

// FlowInfo represents a flow for the UI
type FlowInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	TapName     string   `json:"tapName"`
	Installed   bool     `json:"installed"`
	FullName    string   `json:"fullName"` // tap/flow or just flow for official
}

// AddTapRequest is the request for POST /api/flow-store/taps
type AddTapRequest struct {
	URL   string `json:"url"`   // e.g., "company" or "company/repo"
	Alias string `json:"alias"` // Optional custom name
}

// ListFlowStoreHandler handles GET /api/flow-store
// Returns all taps and flows from the catalog.
// In platform mode, the "installed" flag reflects the team's database.
// In personal mode, it reflects the local filesystem cache.
func ListFlowStoreHandler(w http.ResponseWriter, r *http.Request) {
	fs, err := flowstore.NewStore()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to initialize flow store: "+err.Error())
		return
	}

	// Update manifests
	if err := fs.UpdateAllManifests(); err != nil {
		slog.Warn("failed to update flow store manifests", "error", err)
	}

	// Get taps
	var taps []TapInfo
	for _, tap := range fs.GetAllTaps() {
		taps = append(taps, TapInfo{
			Name:       tap.Name,
			URL:        tap.URL,
			IsOfficial: tap.Name == flowstore.OfficialStoreName,
		})
	}

	// In platform mode, build a set of installed flow names from the team's DB.
	var dbFlowNames map[string]bool
	if svc := store.FromRequest(r); svc != nil && svc.Flows != nil {
		dbFlows := svc.Flows.ListAllFlows(r.Context())
		dbFlowNames = make(map[string]bool, len(dbFlows))
		for _, f := range dbFlows {
			dbFlowNames[f.Name] = true
		}
	}

	// Get flows from catalog
	var flows []FlowInfo
	for _, f := range fs.ListAllFlows() {
		fullName := f.Name
		if f.TapName != flowstore.OfficialStoreName {
			fullName = f.TapName + "/" + f.Name
		}

		installed := f.Installed // personal mode: filesystem-based
		if dbFlowNames != nil {
			// Platform mode: check the team's database
			installed = dbFlowNames[f.Name]
		}

		flows = append(flows, FlowInfo{
			Name:        f.Name,
			Description: f.Description,
			Tags:        f.Tags,
			TapName:     f.TapName,
			Installed:   installed,
			FullName:    fullName,
		})
	}

	respondJSON(w, http.StatusOK, FlowStoreListResponse{
		Taps:  taps,
		Flows: flows,
	})
}

// ListTapsHandler handles GET /api/flow-store/taps
func ListTapsHandler(w http.ResponseWriter, r *http.Request) {
	store, err := flowstore.NewStore()
	if err != nil {
		http.Error(w, "Failed to initialize flow store: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var taps []TapInfo
	for _, tap := range store.GetAllTaps() {
		taps = append(taps, TapInfo{
			Name:       tap.Name,
			URL:        tap.URL,
			IsOfficial: tap.Name == flowstore.OfficialStoreName,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"taps": taps,
	})
}

// AddTapHandler handles POST /api/flow-store/taps
func AddTapHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireTeamAdmin(w, r) {
		return
	}

	var req AddTapRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}

	// Strip protocol prefix before validation (parseTapURL also strips these)
	cleanURL := strings.TrimPrefix(strings.TrimPrefix(req.URL, "https://"), "http://")
	cleanURL = strings.TrimSuffix(strings.TrimSuffix(cleanURL, ".git"), "/")
	if !validTapURL.MatchString(cleanURL) {
		http.Error(w, "Invalid tap URL format: must be a GitHub repository (e.g. owner/repo or github.com/owner/repo)", http.StatusBadRequest)
		return
	}

	store, err := flowstore.NewStore()
	if err != nil {
		http.Error(w, "Failed to initialize flow store: "+err.Error(), http.StatusInternalServerError)
		return
	}

	tapName, err := store.AddTap(cleanURL, req.Alias)
	if err != nil {
		http.Error(w, "Failed to add tap: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"tapName": tapName,
		"message": "Tap added successfully",
	})
}

// RemoveTapHandler handles DELETE /api/flow-store/taps/{name}
func RemoveTapHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireTeamAdmin(w, r) {
		return
	}

	vars := mux.Vars(r)
	name := vars["name"]

	store, err := flowstore.NewStore()
	if err != nil {
		http.Error(w, "Failed to initialize flow store: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := store.RemoveTap(name); err != nil {
		http.Error(w, "Failed to remove tap: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": "Tap removed successfully",
	})
}

// InstallFlowHandler handles POST /api/flow-store/{tap}/{flow}/install
func InstallFlowHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireTeamAdmin(w, r) {
		return
	}

	vars := mux.Vars(r)
	tapName := vars["tap"]
	flowName := vars["flow"]

	// URL decode
	tapName = strings.ReplaceAll(tapName, "%2F", "/")
	flowName = strings.ReplaceAll(flowName, "%2F", "/")

	fs, err := flowstore.NewStore()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to initialize flow store: "+err.Error())
		return
	}

	// Platform mode: fetch from tap remote and save to team's database.
	if svc := store.FromRequest(r); svc != nil && svc.Flows != nil {
		content, err := fs.FetchFlowContent(tapName, flowName)
		if err != nil {
			respondError(w, http.StatusBadRequest, "Failed to fetch flow: "+err.Error())
			return
		}

		if err := svc.Flows.SaveFlow(r.Context(), flowName, string(content)); err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to save flow to database: "+err.Error())
			return
		}

		respondJSON(w, http.StatusOK, map[string]interface{}{
			"status":   "ok",
			"flowName": flowName,
			"message":  "Flow installed successfully",
		})
		return
	}

	// Personal mode: install to filesystem cache.
	if err := fs.InstallFlow(tapName, flowName); err != nil {
		respondError(w, http.StatusBadRequest, "Failed to install flow: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "ok",
		"flowName": flowName,
		"message":  "Flow installed successfully",
	})
}

// UninstallFlowHandler handles DELETE /api/flow-store/{tap}/{flow}
func UninstallFlowHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireTeamAdmin(w, r) {
		return
	}

	vars := mux.Vars(r)
	tapName := vars["tap"]
	flowName := vars["flow"]

	// Platform mode: remove from team's database.
	if svc := store.FromRequest(r); svc != nil && svc.Flows != nil {
		if err := svc.Flows.DeleteFlow(r.Context(), flowName); err != nil {
			respondError(w, http.StatusBadRequest, "Failed to uninstall flow: "+err.Error())
			return
		}

		respondJSON(w, http.StatusOK, map[string]interface{}{
			"status":  "ok",
			"message": "Flow uninstalled successfully",
		})
		return
	}

	// Personal mode: remove from filesystem cache.
	fs, err := flowstore.NewStore()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to initialize flow store: "+err.Error())
		return
	}

	if err := fs.UninstallFlow(tapName, flowName); err != nil {
		respondError(w, http.StatusBadRequest, "Failed to uninstall flow: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"message": "Flow uninstalled successfully",
	})
}

// UpdateFlowStoreHandler handles POST /api/flow-store/update
// Forces a refresh from remote, bypassing the cache
func UpdateFlowStoreHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireTeamAdmin(w, r) {
		return
	}

	store, err := flowstore.NewStore()
	if err != nil {
		http.Error(w, "Failed to initialize flow store: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Force refresh ignoring cache
	if err := store.ForceRefreshAllManifests(); err != nil {
		http.Error(w, "Failed to refresh manifests: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": "All stores refreshed from remote",
	})
}
