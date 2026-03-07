package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/fleet"
)

// FleetPlanListItem is a single item in the fleet plans list response.
type FleetPlanListItem struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	CreatedFrom string   `json:"created_from,omitempty"`
	ChannelType string   `json:"channel_type"`
	AgentCount  int      `json:"agent_count"`
	AgentNames  []string `json:"agent_names"`
	Activated   bool     `json:"activated"`
}

// ListFleetPlansHandler handles GET /api/fleet-plans.
func ListFleetPlansHandler(w http.ResponseWriter, r *http.Request) {
	if fleetPlanRegistryVar == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"plans": []FleetPlanListItem{},
		})
		return
	}

	summaries := fleetPlanRegistryVar.ListPlans()
	items := make([]FleetPlanListItem, len(summaries))
	for i, s := range summaries {
		activated := false
		if plan, ok := fleetPlanRegistryVar.GetPlan(s.Key); ok {
			activated = plan.IsActivated()
		}
		items[i] = FleetPlanListItem{
			Key:         s.Key,
			Name:        s.Name,
			Description: s.Description,
			CreatedFrom: s.CreatedFrom,
			ChannelType: s.ChannelType,
			AgentCount:  s.AgentCount,
			AgentNames:  s.AgentNames,
			Activated:   activated,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"plans": items,
	})
}

// GetFleetPlanHandler handles GET /api/fleet-plans/{key}.
func GetFleetPlanHandler(w http.ResponseWriter, r *http.Request) {
	if fleetPlanRegistryVar == nil {
		http.Error(w, "Fleet plan system not initialized", http.StatusServiceUnavailable)
		return
	}

	key := mux.Vars(r)["key"]
	plan, ok := fleetPlanRegistryVar.GetPlan(key)
	if !ok {
		http.Error(w, "Fleet plan not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"key":  key,
		"plan": plan,
	})
}

// SaveFleetPlanHandler handles PUT /api/fleet-plans/{key}.
func SaveFleetPlanHandler(w http.ResponseWriter, r *http.Request) {
	if fleetPlanRegistryVar == nil {
		http.Error(w, "Fleet plan system not initialized", http.StatusServiceUnavailable)
		return
	}

	key := mux.Vars(r)["key"]

	var plan fleet.FleetPlan
	if err := json.NewDecoder(r.Body).Decode(&plan); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	plan.Key = key
	if err := fleetPlanRegistryVar.Save(&plan); err != nil {
		http.Error(w, "Failed to save fleet plan: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "saved",
		"key":    key,
	})
}

// DeleteFleetPlanHandler handles DELETE /api/fleet-plans/{key}.
func DeleteFleetPlanHandler(w http.ResponseWriter, r *http.Request) {
	if fleetPlanRegistryVar == nil {
		http.Error(w, "Fleet plan system not initialized", http.StatusServiceUnavailable)
		return
	}

	key := mux.Vars(r)["key"]
	if err := fleetPlanRegistryVar.Delete(key); err != nil {
		http.Error(w, "Failed to delete fleet plan: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "deleted",
		"key":    key,
	})
}
