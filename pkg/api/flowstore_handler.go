package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/flowstore"
)

// FlowStoreListResponse is the response for GET /api/flow-store
type FlowStoreListResponse struct {
	Taps  []TapInfo `json:"taps"`
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
// Returns all taps and flows
func ListFlowStoreHandler(w http.ResponseWriter, r *http.Request) {
	store, err := flowstore.NewStore()
	if err != nil {
		http.Error(w, "Failed to initialize flow store: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Update manifests
	_ = store.UpdateAllManifests()

	// Get taps
	var taps []TapInfo
	for _, tap := range store.GetAllTaps() {
		taps = append(taps, TapInfo{
			Name:       tap.Name,
			URL:        tap.URL,
			IsOfficial: tap.Name == flowstore.OfficialStoreName,
		})
	}

	// Get flows
	var flows []FlowInfo
	for _, f := range store.ListAllFlows() {
		fullName := f.Name
		if f.TapName != flowstore.OfficialStoreName {
			fullName = f.TapName + "/" + f.Name
		}
		flows = append(flows, FlowInfo{
			Name:        f.Name,
			Description: f.Description,
			Tags:        f.Tags,
			TapName:     f.TapName,
			Installed:   f.Installed,
			FullName:    fullName,
		})
	}

	response := FlowStoreListResponse{
		Taps:  taps,
		Flows: flows,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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
	var req AddTapRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}

	store, err := flowstore.NewStore()
	if err != nil {
		http.Error(w, "Failed to initialize flow store: "+err.Error(), http.StatusInternalServerError)
		return
	}

	tapName, err := store.AddTap(req.URL, req.Alias)
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
	vars := mux.Vars(r)
	tapName := vars["tap"]
	flowName := vars["flow"]

	// URL decode
	tapName = strings.ReplaceAll(tapName, "%2F", "/")
	flowName = strings.ReplaceAll(flowName, "%2F", "/")

	store, err := flowstore.NewStore()
	if err != nil {
		http.Error(w, "Failed to initialize flow store: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := store.InstallFlow(tapName, flowName); err != nil {
		http.Error(w, "Failed to install flow: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "ok",
		"flowName": flowName,
		"message":  "Flow installed successfully",
	})
}

// UninstallFlowHandler handles DELETE /api/flow-store/{tap}/{flow}
func UninstallFlowHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	tapName := vars["tap"]
	flowName := vars["flow"]

	store, err := flowstore.NewStore()
	if err != nil {
		http.Error(w, "Failed to initialize flow store: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := store.UninstallFlow(tapName, flowName); err != nil {
		http.Error(w, "Failed to uninstall flow: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": "Flow uninstalled successfully",
	})
}

// UpdateFlowStoreHandler handles POST /api/flow-store/update
// Forces a refresh from remote, bypassing the cache
func UpdateFlowStoreHandler(w http.ResponseWriter, r *http.Request) {
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
