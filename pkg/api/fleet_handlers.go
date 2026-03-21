package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/fleet"
)

// Package-level registries, set by the launcher during initialization.
var (
	fleetRegistryVar     *fleet.Registry
	fleetPlanRegistryVar *fleet.PlanRegistry
)

// SetFleetRegistry sets the fleet registry for API handlers.
func SetFleetRegistry(reg *fleet.Registry) {
	fleetRegistryVar = reg
}

// SetFleetPlanRegistry sets the fleet plan registry for API handlers.
func SetFleetPlanRegistry(reg *fleet.PlanRegistry) {
	fleetPlanRegistryVar = reg
}

// GetFleetRegistry returns the fleet registry (for use by other API packages).
func GetFleetRegistry() *fleet.Registry {
	return fleetRegistryVar
}

// GetFleetPlanRegistry returns the fleet plan registry (for use by other packages).
func GetFleetPlanRegistry() *fleet.PlanRegistry {
	return fleetPlanRegistryVar
}

// --- Fleet Handlers ---

// FleetListItem represents a fleet in listing responses.
type FleetListItem struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	AgentCount  int      `json:"agent_count"`
	AgentNames  []string `json:"agent_names"`
}

// ListFleetsHandler handles GET /api/fleets
func ListFleetsHandler(w http.ResponseWriter, r *http.Request) {
	if fleetRegistryVar == nil {
		http.Error(w, "Fleet system not initialized", http.StatusServiceUnavailable)
		return
	}

	summaries := fleetRegistryVar.ListFleets()
	items := make([]FleetListItem, len(summaries))
	for i, s := range summaries {
		items[i] = FleetListItem{
			Key:         s.Key,
			Name:        s.Name,
			Description: s.Description,
			AgentCount:  s.AgentCount,
			AgentNames:  s.AgentNames,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"fleets": items,
	})
}

// GetFleetHandler handles GET /api/fleets/{key}
func GetFleetHandler(w http.ResponseWriter, r *http.Request) {
	if fleetRegistryVar == nil {
		http.Error(w, "Fleet system not initialized", http.StatusServiceUnavailable)
		return
	}

	key := mux.Vars(r)["key"]
	f, ok := fleetRegistryVar.GetFleet(key)
	if !ok {
		http.Error(w, "Fleet not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"key":   key,
		"fleet": f,
	})
}

// SaveFleetHandler handles PUT /api/fleets/{key}
func SaveFleetHandler(w http.ResponseWriter, r *http.Request) {
	if fleetRegistryVar == nil {
		http.Error(w, "Fleet system not initialized", http.StatusServiceUnavailable)
		return
	}

	key := mux.Vars(r)["key"]

	var f fleet.FleetConfig
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := f.Validate(); err != nil {
		http.Error(w, "Validation error: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := fleetRegistryVar.Save(key, &f); err != nil {
		http.Error(w, "Failed to save fleet: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"key":    key,
	})
}

// DeleteFleetHandler handles DELETE /api/fleets/{key}
func DeleteFleetHandler(w http.ResponseWriter, r *http.Request) {
	if fleetRegistryVar == nil {
		http.Error(w, "Fleet system not initialized", http.StatusServiceUnavailable)
		return
	}

	key := mux.Vars(r)["key"]

	if err := fleetRegistryVar.Delete(key); err != nil {
		http.Error(w, "Failed to delete fleet: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}
