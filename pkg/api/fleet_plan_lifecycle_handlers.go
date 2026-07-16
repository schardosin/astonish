package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/SAP/astonish/pkg/fleet"
	"github.com/SAP/astonish/pkg/store"
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
		respondError(w, http.StatusServiceUnavailable, "Plan activation system not initialized (requires daemon mode with scheduler)")
		return
	}

	key := mux.Vars(r)["key"]
	if err := planActivatorVar.Activate(r.Context(), key); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
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
		respondError(w, http.StatusServiceUnavailable, "Plan activation system not initialized (requires daemon mode with scheduler)")
		return
	}

	key := mux.Vars(r)["key"]
	if err := planActivatorVar.Deactivate(r.Context(), key); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
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
		respondError(w, http.StatusServiceUnavailable, "Plan activation system not initialized (requires daemon mode with scheduler)")
		return
	}

	key := mux.Vars(r)["key"]
	status, err := planActivatorVar.Status(key)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	// Include issues needing attention in the status response so the Fleet UI
	// can display them with a "Retry" button for manual intervention.
	issuesNeedingAttention := planActivatorVar.GetIssuesNeedingAttention(key)

	// Include issues that are still being retried so the UI can warn immediately.
	issuesRetrying := planActivatorVar.GetIssuesRetrying(key)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"activated":                status.Activated,
		"scheduler_job_id":         status.SchedulerJobID,
		"activated_at":             status.ActivatedAt,
		"last_poll_at":             status.LastPollAt,
		"last_poll_status":         status.LastPollStatus,
		"last_poll_error":          status.LastPollError,
		"sessions_started":         status.SessionsStarted,
		"last_start_error":         status.LastStartError,
		"last_start_error_at":      status.LastStartErrorAt,
		"issues_needing_attention": issuesNeedingAttention,
		"issues_retrying":          issuesRetrying,
		// Legacy field for backward compatibility with existing UI code
		"failed_issues": issuesNeedingAttention,
	})
}

// PatchFleetPlanAgentHandler handles PATCH /api/fleet-plans/{key}/agents/{agent_key}.
func PatchFleetPlanAgentHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]
	agentKey := vars["agent_key"]
	if key == "" || agentKey == "" {
		respondError(w, http.StatusBadRequest, "plan key and agent_key are required")
		return
	}

	var planStore store.FleetPlanStore
	if svc := store.FromRequest(r); svc != nil && svc.FleetPlans != nil {
		planStore = svc.FleetPlans
	}
	if planStore == nil {
		respondError(w, http.StatusServiceUnavailable, "Fleet plan store not available")
		return
	}

	planAny, found := planStore.GetPlan(r.Context(), key)
	if !found {
		respondError(w, http.StatusNotFound, fmt.Sprintf("Fleet plan %q not found", key))
		return
	}
	plan, ok := planAny.(*fleet.FleetPlan)
	if !ok {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Fleet plan %q has unexpected type", key))
		return
	}
	agentCfg, ok := plan.Agents[agentKey]
	if !ok {
		respondError(w, http.StatusNotFound, fmt.Sprintf("Agent %q not found", agentKey))
		return
	}

	if err := json.NewDecoder(r.Body).Decode(&agentCfg); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	plan.Agents[agentKey] = agentCfg
	if err := plan.Validate(); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := planStore.Save(r.Context(), plan); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":    "ok",
		"plan_key":  key,
		"agent_key": agentKey,
		"agent":     agentCfg,
	})
}

// RetryFleetIssueHandler handles POST /api/fleet-plans/{key}/retry/{issueNumber}.
// It resets the retry count for a failed issue and triggers immediate recovery
// using the existing JSONL transcript, resuming the fleet session from where it
// left off.
func RetryFleetIssueHandler(w http.ResponseWriter, r *http.Request) {
	if planActivatorVar == nil {
		respondError(w, http.StatusServiceUnavailable, "Plan activation system not initialized")
		return
	}

	key := mux.Vars(r)["key"]
	issueNumStr := mux.Vars(r)["issueNumber"]
	issueNum, parseErr := strconv.Atoi(issueNumStr)
	if parseErr != nil {
		respondError(w, http.StatusBadRequest, "Invalid issue number")
		return
	}

	// Get the plan — try store from request first (platform mode), then global registry
	var plan *fleet.FleetPlan
	if svc := store.FromRequest(r); svc != nil && svc.FleetPlans != nil {
		planAny, found := svc.FleetPlans.GetPlan(r.Context(), key)
		if !found {
			respondError(w, http.StatusNotFound, fmt.Sprintf("Fleet plan %q not found", key))
			return
		}
		var ok bool
		plan, ok = planAny.(*fleet.FleetPlan)
		if !ok {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("Fleet plan %q has unexpected type", key))
			return
		}
	} else {
		if fleetPlanRegistryVar == nil {
			respondError(w, http.StatusServiceUnavailable, "Fleet plan registry not available")
			return
		}
		var ok bool
		plan, ok = fleetPlanRegistryVar.GetPlan(key)
		if !ok {
			respondError(w, http.StatusNotFound, fmt.Sprintf("Fleet plan %q not found", key))
			return
		}
	}

	// Reset the retry count so the issue can be picked up again
	monitor, err := planActivatorVar.RetryFailedIssue(key, issueNum)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Find the session ID from the monitor state
	issueState := monitor.GetIssueState(issueNum)
	if issueState == nil || issueState.SessionID == "" {
		respondError(w, http.StatusInternalServerError, "Could not find session ID for the issue")
		return
	}
	sessionID := issueState.SessionID

	// Use the same recovery path as daemon-restart recovery
	repo := fleet.GetConfigString(plan.Channel.Config, "repo")
	recoverCfg := fleet.RecoverFleetConfig{
		Plan:        plan,
		SessionID:   sessionID,
		IssueNumber: issueNum,
		IssueTitle:  monitor.GetIssueTitle(issueNum),
		Repo:        repo,
		GHToken:     planActivatorVar.ResolveGHTokenForPlan(plan),
		UserID:      plan.CreatedBy, // run under plan creator's identity
		CompletionFunc: func(sessionErr error) {
			if sessionErr != nil {
				monitor.IncrementRetryCount(issueNum, sessionErr.Error())
			} else {
				monitor.ClearRetryOnSuccess(issueNum)
			}
		},
		BallChangeFunc: func(_ string, commentID int64) {
			monitor.UpdateCursor(issueNum, commentID)
		},
	}

	// Resolve the session store for platform mode. Fleet sessions started by the
	// daemon are persisted in the team Sessions store (not PersonalSessions),
	// so we use that for recovery too.
	var retrySessionStore store.SessionStore
	if svc := store.FromRequest(r); svc != nil && svc.Sessions != nil {
		retrySessionStore = svc.Sessions
	}

	// Build tenant-scoped stores for the recovered fleet session.
	var retryFleetStores *FleetStores
	if svc := store.FromRequest(r); svc != nil {
		retryFleetStores = FleetStoresFromServices(svc)
	}

	if recoverErr := RecoverFleetSession(context.Background(), recoverCfg, retrySessionStore, retryFleetStores); recoverErr != nil {
		// Recovery failed; increment retry count
		monitor.IncrementRetryCount(issueNum, fmt.Sprintf("retry recovery failed: %v", recoverErr))
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Recovery failed: %v", recoverErr))
		return
	}

	slog.Info("issue recovery started", "component", "fleet-retry", "issue", issueNum, "plan", key, "session_id", sessionID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "recovering",
		"session_id": sessionID,
		"issue":      issueNum,
	})
}
