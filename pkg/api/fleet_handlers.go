package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

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
		// Store already returns bundled (source=bundled) + non-colliding custom rows.
		summaries := svc.FleetTemplates.ListFleets(r.Context())
		items := make([]FleetListItem, 0, len(summaries))
		for _, s := range summaries {
			source := s.Source
			if source == "" {
				if fleet.IsBundledKey(s.Key) {
					source = "bundled"
				} else {
					source = "custom"
				}
			}
			items = append(items, FleetListItem{
				Key:         s.Key,
				Name:        s.Name,
				Description: s.Description,
				AgentCount:  s.AgentCount,
				AgentNames:  s.AgentNames,
				Source:      source,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"fleets": items})
		return
	}

	// Fallback: direct registry access (legacy personal mode).
	if fleetRegistryVar == nil {
		respondError(w, http.StatusServiceUnavailable, "Fleet system not initialized")
		return
	}

	summaries := fleetRegistryVar.ListFleets()
	items := make([]FleetListItem, len(summaries))
	for i, s := range summaries {
		source := "custom"
		if fleet.IsBundledKey(s.Key) {
			source = "bundled"
		}
		items[i] = FleetListItem{
			Key:         s.Key,
			Name:        s.Name,
			Description: s.Description,
			AgentCount:  s.AgentCount,
			AgentNames:  s.AgentNames,
			Source:      source,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"fleets": items})
}

// GetFleetHandler handles GET /api/fleets/{key}
func GetFleetHandler(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]

	if svc := store.FromRequest(r); svc != nil && svc.FleetTemplates != nil {
		if f, ok := svc.FleetTemplates.GetFleet(r.Context(), key); ok {
			source := "custom"
			if fleet.IsBundledKey(key) {
				source = "bundled"
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"key": key, "fleet": f, "source": source})
			return
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

	source := "custom"
	if fleet.IsBundledKey(key) {
		source = "bundled"
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"key": key, "fleet": f, "source": source})
}

// SaveFleetHandler handles PUT /api/fleets/{key}
func SaveFleetHandler(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]

	if fleet.IsBundledKey(key) {
		respondError(w, http.StatusConflict, "Bundled fleet templates are immutable; clone to a new key to customize")
		return
	}

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
			if errors.Is(err, store.ErrBundledTemplateImmutable) {
				respondError(w, http.StatusConflict, err.Error())
				return
			}
			respondError(w, http.StatusInternalServerError, "Failed to save fleet: "+err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "key": key, "source": "custom"})
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

	if fleet.IsBundledKey(key) {
		respondError(w, http.StatusConflict, "Bundled fleet templates cannot be deleted")
		return
	}

	if svc := store.FromRequest(r); svc != nil && svc.FleetTemplates != nil {
		if err := svc.FleetTemplates.Delete(r.Context(), key); err != nil {
			if errors.Is(err, store.ErrBundledTemplateImmutable) {
				respondError(w, http.StatusConflict, err.Error())
				return
			}
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

// CloneFleetRequest is the body for POST /api/fleets/{key}/clone.
type CloneFleetRequest struct {
	NewKey string `json:"new_key"`
	Name   string `json:"name,omitempty"`
}

// CloneFleetHandler handles POST /api/fleets/{key}/clone.
// Copies a bundled or custom template into a new custom DB key.
func CloneFleetHandler(w http.ResponseWriter, r *http.Request) {
	fromKey := mux.Vars(r)["key"]

	var req CloneFleetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	newKey := strings.TrimSpace(req.NewKey)
	if newKey == "" {
		respondError(w, http.StatusBadRequest, "new_key is required")
		return
	}
	if fleet.IsBundledKey(newKey) {
		respondError(w, http.StatusConflict, "Cannot clone onto a bundled template key; choose a different new_key")
		return
	}

	if svc := store.FromRequest(r); svc != nil && svc.FleetTemplates != nil {
		src, ok := svc.FleetTemplates.GetFleet(r.Context(), fromKey)
		if !ok {
			respondError(w, http.StatusNotFound, "Source fleet not found")
			return
		}
		// Reject if a custom template already occupies newKey.
		if existing, exists := svc.FleetTemplates.GetFleet(r.Context(), newKey); exists {
			_ = existing
			respondError(w, http.StatusConflict, "A fleet template with key "+newKey+" already exists")
			return
		}

		cfg, err := normalizeFleetConfig(src)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to read source fleet: "+err.Error())
			return
		}
		if strings.TrimSpace(req.Name) != "" {
			cfg.Name = strings.TrimSpace(req.Name)
		} else if cfg.Name != "" {
			cfg.Name = cfg.Name + " Copy"
		} else {
			cfg.Name = newKey
		}
		if err := cfg.Validate(); err != nil {
			respondError(w, http.StatusBadRequest, "Validation error: "+err.Error())
			return
		}
		if err := svc.FleetTemplates.Save(r.Context(), newKey, cfg); err != nil {
			if errors.Is(err, store.ErrBundledTemplateImmutable) {
				respondError(w, http.StatusConflict, err.Error())
				return
			}
			respondError(w, http.StatusInternalServerError, "Failed to save cloned fleet: "+err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "key": newKey, "source": "custom"})
		return
	}

	respondError(w, http.StatusServiceUnavailable, "Fleet system not initialized")
}

func normalizeFleetConfig(src any) (*fleet.FleetConfig, error) {
	switch v := src.(type) {
	case *fleet.FleetConfig:
		// Deep-ish copy via JSON so the clone is independent of the cached bundled pointer.
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		var cfg fleet.FleetConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, err
		}
		return &cfg, nil
	case fleet.FleetConfig:
		cfg := v
		return &cfg, nil
	case map[string]any:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		var cfg fleet.FleetConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, err
		}
		return &cfg, nil
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		var cfg fleet.FleetConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, err
		}
		return &cfg, nil
	}
}
