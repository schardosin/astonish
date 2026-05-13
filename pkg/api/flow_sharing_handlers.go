package api

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
)

// --------------------------------------------------------------------------
// Request / response types
// --------------------------------------------------------------------------

// FlowPublishRequest is the payload for publishing a personal flow to a team.
type FlowPublishRequest struct {
	Name string `json:"name"`
}

// FlowForkRequest is the payload for forking a team flow to personal.
type FlowForkRequest struct {
	Name string `json:"name"`
}

// --------------------------------------------------------------------------
// Publish personal flow to team
// --------------------------------------------------------------------------

// FlowPublishToTeamHandler copies a personal flow to the team schema.
//
//	POST /api/agents/{name}/publish
//
// Platform mode only. The flow YAML is copied as-is (copy semantics —
// the personal copy remains). The team gets an independent copy that can
// be edited separately.
func FlowPublishToTeamHandler(w http.ResponseWriter, r *http.Request) {
	// Team admins can publish flows to the team.
	if !RequireTeamAdmin(w, r) {
		return
	}

	svc := RequirePlatformServices(w, r)
	if svc == nil {
		return
	}

	name := mux.Vars(r)["name"]
	if name == "" {
		respondError(w, http.StatusBadRequest, "flow name is required")
		return
	}

	if svc.PersonalFlows == nil {
		respondError(w, http.StatusServiceUnavailable, "personal flow store not available")
		return
	}
	if svc.Flows == nil {
		respondError(w, http.StatusServiceUnavailable, "team flow store not available")
		return
	}

	// Read from personal store
	yamlContent, err := svc.PersonalFlows.GetFlow(r.Context(), name)
	if err != nil {
		respondError(w, http.StatusNotFound, fmt.Sprintf("personal flow not found: %v", err))
		return
	}

	// Save to team store (copy semantics — personal copy remains)
	if err := svc.Flows.SaveFlow(r.Context(), name, yamlContent); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to publish flow to team: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"published": true,
		"name":      name,
		"scope":     "team",
		"message":   fmt.Sprintf("Flow '%s' published to team", name),
	})
}

// --------------------------------------------------------------------------
// Fork team flow to personal
// --------------------------------------------------------------------------

// FlowForkToPersonalHandler copies a team flow to the user's personal schema.
//
//	POST /api/agents/{name}/fork
//
// Platform mode only. Creates a personal copy that the user can modify
// independently without affecting the team version.
func FlowForkToPersonalHandler(w http.ResponseWriter, r *http.Request) {
	svc := RequirePlatformServices(w, r)
	if svc == nil {
		return
	}

	name := mux.Vars(r)["name"]
	if name == "" {
		respondError(w, http.StatusBadRequest, "flow name is required")
		return
	}

	if svc.Flows == nil {
		respondError(w, http.StatusServiceUnavailable, "team flow store not available")
		return
	}
	if svc.PersonalFlows == nil {
		respondError(w, http.StatusServiceUnavailable, "personal flow store not available")
		return
	}

	// Read from team store
	yamlContent, err := svc.Flows.GetFlow(r.Context(), name)
	if err != nil {
		respondError(w, http.StatusNotFound, fmt.Sprintf("team flow not found: %v", err))
		return
	}

	// Save to personal store (copy semantics — team copy remains)
	if err := svc.PersonalFlows.SaveFlow(r.Context(), name, yamlContent); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to fork flow to personal: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"forked":  true,
		"name":    name,
		"scope":   "personal",
		"message": fmt.Sprintf("Flow '%s' forked from team to personal", name),
	})
}
