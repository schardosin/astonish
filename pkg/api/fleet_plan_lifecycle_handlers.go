package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/fleet"
)

// planActivatorVar holds the PlanActivator instance, set by the daemon.
var planActivatorVar *fleet.PlanActivator

// SetPlanActivator registers the plan activator for API handlers.
func SetPlanActivator(pa *fleet.PlanActivator) {
	planActivatorVar = pa
}

// GetPlanActivator returns the plan activator (for use by other packages).
func GetPlanActivator() *fleet.PlanActivator {
	return planActivatorVar
}

// ActivateFleetPlanHandler handles POST /api/fleet-plans/{key}/activate.
func ActivateFleetPlanHandler(w http.ResponseWriter, r *http.Request) {
	if planActivatorVar == nil {
		http.Error(w, "Plan activation system not initialized (requires daemon mode with scheduler)", http.StatusServiceUnavailable)
		return
	}

	key := mux.Vars(r)["key"]
	if err := planActivatorVar.Activate(r.Context(), key); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "activated",
		"key":    key,
	})
}

// DeactivateFleetPlanHandler handles POST /api/fleet-plans/{key}/deactivate.
func DeactivateFleetPlanHandler(w http.ResponseWriter, r *http.Request) {
	if planActivatorVar == nil {
		http.Error(w, "Plan activation system not initialized (requires daemon mode with scheduler)", http.StatusServiceUnavailable)
		return
	}

	key := mux.Vars(r)["key"]
	if err := planActivatorVar.Deactivate(r.Context(), key); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "deactivated",
		"key":    key,
	})
}

// FleetPlanStatusHandler handles GET /api/fleet-plans/{key}/status.
func FleetPlanStatusHandler(w http.ResponseWriter, r *http.Request) {
	if planActivatorVar == nil {
		http.Error(w, "Plan activation system not initialized (requires daemon mode with scheduler)", http.StatusServiceUnavailable)
		return
	}

	key := mux.Vars(r)["key"]
	status, err := planActivatorVar.Status(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}
