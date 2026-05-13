package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/store"
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
	Source      string   `json:"source,omitempty"` // "bundled" or "custom"
}

// ListFleetsHandler handles GET /api/fleets
func ListFleetsHandler(w http.ResponseWriter, r *http.Request) {
	// Use store abstraction if available (platform mode).
	if svc := store.FromRequest(r); svc != nil && svc.FleetTemplates != nil {
		// Start with bundled templates (always available from the binary).
		items := bundledFleetListItems()

		// Merge DB templates (custom, user-created). DB templates with the
		// same key as a bundled one override it (user forked/customized).
		dbSummaries := svc.FleetTemplates.ListFleets(r.Context())
		bundledKeys := make(map[string]bool, len(items))
		for _, item := range items {
			bundledKeys[item.Key] = true
		}
		for _, s := range dbSummaries {
			if bundledKeys[s.Key] {
				// Replace the bundled entry with the DB (custom) version.
				for i := range items {
					if items[i].Key == s.Key {
						items[i] = FleetListItem{
							Key:         s.Key,
							Name:        s.Name,
							Description: s.Description,
							AgentCount:  s.AgentCount,
							AgentNames:  s.AgentNames,
							Source:      "custom",
						}
						break
					}
				}
			} else {
				items = append(items, FleetListItem{
					Key:         s.Key,
					Name:        s.Name,
					Description: s.Description,
					AgentCount:  s.AgentCount,
					AgentNames:  s.AgentNames,
					Source:      "custom",
				})
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"fleets": items})
		return
	}

	// Fallback: direct registry access (personal mode).
	if fleetRegistryVar == nil {
		respondError(w, http.StatusServiceUnavailable, "Fleet system not initialized")
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
	json.NewEncoder(w).Encode(map[string]interface{}{"fleets": items})
}

// GetFleetHandler handles GET /api/fleets/{key}
func GetFleetHandler(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]

	if svc := store.FromRequest(r); svc != nil && svc.FleetTemplates != nil {
		// Check DB first (custom templates take priority).
		if f, ok := svc.FleetTemplates.GetFleet(r.Context(), key); ok {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"key": key, "fleet": f, "source": "custom"})
			return
		}

		// Fall back to bundled templates.
		if bundled, err := fleet.LoadBundledConfigs(); err == nil {
			if f, ok := bundled[key]; ok {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{"key": key, "fleet": f, "source": "bundled"})
				return
			}
		}

		respondError(w, http.StatusNotFound, "Fleet not found")
		return
	}

	if fleetRegistryVar == nil {
		respondError(w, http.StatusServiceUnavailable, "Fleet system not initialized")
		return
	}

	f, ok := fleetRegistryVar.GetFleet(key)
	if !ok {
		respondError(w, http.StatusNotFound, "Fleet not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"key": key, "fleet": f})
}

// SaveFleetHandler handles PUT /api/fleets/{key}
func SaveFleetHandler(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]

	var f fleet.FleetConfig
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	if err := f.Validate(); err != nil {
		respondError(w, http.StatusBadRequest, "Validation error: "+err.Error())
		return
	}

	if svc := store.FromRequest(r); svc != nil && svc.FleetTemplates != nil {
		if err := svc.FleetTemplates.Save(r.Context(), key, &f); err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to save fleet: "+err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "key": key})
		return
	}

	if fleetRegistryVar == nil {
		respondError(w, http.StatusServiceUnavailable, "Fleet system not initialized")
		return
	}

	if err := fleetRegistryVar.Save(key, &f); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to save fleet: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "key": key})
}

// DeleteFleetHandler handles DELETE /api/fleets/{key}
func DeleteFleetHandler(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]

	if svc := store.FromRequest(r); svc != nil && svc.FleetTemplates != nil {
		if err := svc.FleetTemplates.Delete(r.Context(), key); err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to delete fleet: "+err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
		return
	}

	if fleetRegistryVar == nil {
		respondError(w, http.StatusServiceUnavailable, "Fleet system not initialized")
		return
	}

	if err := fleetRegistryVar.Delete(key); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to delete fleet: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
}

// bundledFleetListItems returns list items for all bundled fleet templates.
func bundledFleetListItems() []FleetListItem {
	bundled, err := fleet.LoadBundledConfigs()
	if err != nil || len(bundled) == 0 {
		return nil
	}

	items := make([]FleetListItem, 0, len(bundled))
	for key, cfg := range bundled {
		names := make([]string, 0, len(cfg.Agents))
		for name := range cfg.Agents {
			names = append(names, name)
		}
		items = append(items, FleetListItem{
			Key:         key,
			Name:        cfg.Name,
			Description: cfg.Description,
			AgentCount:  len(cfg.Agents),
			AgentNames:  names,
			Source:      "bundled",
		})
	}
	return items
}
