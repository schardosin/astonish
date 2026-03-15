package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

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

	// Include failed issues in the status response so the Fleet UI can
	// display them with a "Continue" button for manual intervention.
	failedIssues := planActivatorVar.GetFailedIssues(key)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"activated":        status.Activated,
		"scheduler_job_id": status.SchedulerJobID,
		"activated_at":     status.ActivatedAt,
		"last_poll_at":     status.LastPollAt,
		"last_poll_status": status.LastPollStatus,
		"last_poll_error":  status.LastPollError,
		"sessions_started": status.SessionsStarted,
		"failed_issues":    failedIssues,
	})
}

// RetryFleetIssueHandler handles POST /api/fleet-plans/{key}/retry/{issueNumber}.
// It resets a failed issue back to in_progress and triggers recovery using the
// existing JSONL transcript, resuming the fleet session from where it left off.
func RetryFleetIssueHandler(w http.ResponseWriter, r *http.Request) {
	if planActivatorVar == nil {
		http.Error(w, "Plan activation system not initialized", http.StatusServiceUnavailable)
		return
	}

	key := mux.Vars(r)["key"]
	issueNumStr := mux.Vars(r)["issueNumber"]
	issueNum, parseErr := strconv.Atoi(issueNumStr)
	if parseErr != nil {
		http.Error(w, "Invalid issue number", http.StatusBadRequest)
		return
	}

	// Get the plan
	if fleetPlanRegistryVar == nil {
		http.Error(w, "Fleet plan registry not available", http.StatusServiceUnavailable)
		return
	}
	plan, ok := fleetPlanRegistryVar.GetPlan(key)
	if !ok {
		http.Error(w, fmt.Sprintf("Fleet plan %q not found", key), http.StatusNotFound)
		return
	}

	// Reset the issue from "failed" to "in_progress"
	monitor, err := planActivatorVar.RetryFailedIssue(key, issueNum)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Find the session ID from the monitor state
	agentBall := monitor.GetAgentBallIssues()
	var sessionID string
	for _, ab := range agentBall {
		if ab.IssueNumber == issueNum {
			sessionID = ab.SessionID
			break
		}
	}
	if sessionID == "" {
		http.Error(w, "Could not find session ID for the issue", http.StatusInternalServerError)
		return
	}

	// Use the same recovery path as daemon-restart recovery
	repo := fleet.GetConfigString(plan.Channel.Config, "repo")
	recoverCfg := fleet.RecoverFleetConfig{
		Plan:        plan,
		SessionID:   sessionID,
		IssueNumber: issueNum,
		IssueTitle:  monitor.GetIssueTitle(issueNum),
		Repo:        repo,
		GHToken:     planActivatorVar.ResolveGHTokenForPlan(plan),
		CompletionFunc: func(sessionErr error) {
			if sessionErr != nil {
				monitor.MarkFailed(issueNum, sessionErr.Error())
			} else {
				monitor.MarkCustomer(issueNum, 0)
			}
		},
		BallChangeFunc: func(ball string, commentID int64) {
			switch ball {
			case "customer":
				monitor.MarkCustomer(issueNum, commentID)
			case "agents":
				monitor.UpdateLastCommentID(issueNum, commentID)
			}
		},
	}

	if recoverErr := RecoverFleetSession(context.Background(), recoverCfg); recoverErr != nil {
		// Recovery failed; mark back as failed
		monitor.MarkFailed(issueNum, fmt.Sprintf("retry recovery failed: %v", recoverErr))
		http.Error(w, fmt.Sprintf("Recovery failed: %v", recoverErr), http.StatusInternalServerError)
		return
	}

	log.Printf("[fleet-retry] Issue #%d (plan %q) recovery started, session %s", issueNum, key, sessionID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "recovering",
		"session_id": sessionID,
		"issue":      issueNum,
	})
}
