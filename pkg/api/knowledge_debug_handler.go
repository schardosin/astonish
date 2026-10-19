package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"github.com/SAP/astonish/pkg/store"
	"google.golang.org/adk/session"
)

type knowledgeCaptureStatus string

const (
	knowledgeCaptured           knowledgeCaptureStatus = "captured"
	knowledgeCaptureDisabled    knowledgeCaptureStatus = "disabled"
	knowledgeCaptureUnavailable knowledgeCaptureStatus = "unavailable"
)

// resolveInvocationKnowledgeDebug reads only the target invocation's audit trail.
// Legacy empty-ID records may be attributed only within an uninterrupted system
// preparation interval immediately before that invocation, never across a user
// message or another invocation. Stored maps are not modified.
func resolveInvocationKnowledgeDebug(events []*session.Event, invocationID string) (map[string]any, map[string]any) {
	if invocationID == "" {
		return nil, nil
	}
	resolve := func(key string, knowledge bool) map[string]any {
		var exact, pending, legacy map[string]any
		for _, evt := range events {
			if evt == nil {
				continue
			}
			if evt.Author == "user" || (evt.Content != nil && evt.Content.Role == "user") {
				pending = nil
			}
			if evt.InvocationID != "" {
				if evt.InvocationID == invocationID && pending != nil {
					legacy = pending
				}
				pending = nil
			}
			if evt.Author != "system" && evt.InvocationID == "" {
				pending = nil
			}
			data, ok := evt.Actions.StateDelta[key].(map[string]any)
			if !ok {
				continue
			}
			if version, versioned := data["version"]; versioned {
				payloadID, _ := data["invocation_id"].(string)
				if (version != 1 && version != float64(1)) || evt.InvocationID != invocationID || payloadID != invocationID {
					continue
				}
				status, _ := data["capture_status"].(string)
				if knowledgeCaptureStatus(status) != knowledgeCaptured && knowledgeCaptureStatus(status) != knowledgeCaptureDisabled {
					continue
				}
				if _, ok := data["dispatched"].(bool); !ok {
					continue
				}
				exact = data
			} else if evt.InvocationID == invocationID {
				exact = data
			} else if evt.InvocationID == "" && evt.Author == "system" && evt.Content == nil {
				pending = data
			}
		}
		if exact == nil {
			exact = legacy
		}
		if exact == nil {
			return nil
		}
		out := make(map[string]any, len(exact)+1)
		for k, v := range exact {
			out[k] = v
		}
		if knowledge && out["version"] == nil {
			out["capture_status"] = string(knowledgeCaptureUnavailable)
			delete(out, "snapshot")
		}
		return out
	}
	return resolve("_knowledge_injection", true), resolve("_tool_injection", false)
}

// StudioKnowledgeDebugHandler returns knowledge and tool injection debug data
// for a specific invocation within a session.
//
// GET /api/studio/sessions/{id}/knowledge-debug?invocationId=X
//
// Both debug endpoints use the same invocation resolver and persisted records.
func StudioKnowledgeDebugHandler(w http.ResponseWriter, r *http.Request) {
	if !IsPlatformAdmin(GetPlatformUser(r)) {
		respondError(w, http.StatusForbidden, "platform superadmin access required")
		return
	}
	sessionID := mux.Vars(r)["id"]
	invocationID := strings.TrimSpace(r.URL.Query().Get("invocationId"))
	if invocationID == "" {
		respondError(w, http.StatusBadRequest, "invocationId is required")
		return
	}
	svc := store.FromRequest(r)
	if svc == nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}
	sessions := resolveSessionStore(svc, sessionID)
	if sessions == nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	userID := effectiveUserID(r)
	events, err := sessions.ReadTranscriptEvents(r.Context(), studioChatAppName, userID, sessionID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to read session events")
		return
	}

	knowledgeData, toolData := resolveInvocationKnowledgeDebug(events, invocationID)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"sessionId":    sessionID,
		"invocationId": invocationID,
		"knowledge":    knowledgeData,
		"tools":        toolData,
	})
}

// PlatformAdminKnowledgeDebugHandler handles
// GET /api/platform/admin/sessions/{id}/knowledge-debug.
// It mirrors the studio handler but uses the platform admin session resolution
// path (org + scope + user/team), consistent with PlatformAdminCacheDiagnosticsHandler.
func PlatformAdminKnowledgeDebugHandler(w http.ResponseWriter, r *http.Request) {
	_, backend := platformAdminGuard(w, r)
	if backend == nil {
		return
	}

	orgSlug := r.URL.Query().Get("org")
	scope := r.URL.Query().Get("scope")
	userID := r.URL.Query().Get("user")
	teamSlug := r.URL.Query().Get("team")
	invocationID := strings.TrimSpace(r.URL.Query().Get("invocationId"))

	if orgSlug == "" || (scope != "personal" && scope != "team") {
		respondError(w, http.StatusBadRequest, "org and scope=personal|team are required")
		return
	}
	if scope == "personal" && userID == "" {
		respondError(w, http.StatusBadRequest, "user is required for personal scope")
		return
	}
	if scope == "team" && teamSlug == "" {
		respondError(w, http.StatusBadRequest, "team is required for team scope")
		return
	}
	if invocationID == "" {
		respondError(w, http.StatusBadRequest, "invocationId is required")
		return
	}

	org, err := backend.Organizations().GetBySlug(r.Context(), orgSlug)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to resolve organization")
		return
	}
	if org == nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}
	orgStore, err := backend.ForOrg(orgSlug)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to resolve organization store")
		return
	}

	var sessions store.SessionStore
	if scope == "personal" {
		user, err := backend.Users().GetByID(r.Context(), userID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to resolve user")
			return
		}
		if user == nil {
			respondError(w, http.StatusNotFound, "session not found")
			return
		}
		memberRole, err := backend.Organizations().GetMemberRole(r.Context(), userID, org.ID)
		if err != nil || memberRole == "" {
			respondError(w, http.StatusNotFound, "session not found")
			return
		}
		sessions = orgStore.ForUser(userID).Sessions()
	} else {
		team, err := orgStore.Teams().GetTeamBySlug(r.Context(), teamSlug)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to resolve team")
			return
		}
		if team == nil {
			respondError(w, http.StatusNotFound, "session not found")
			return
		}
		sessions = orgStore.ForTeam(teamSlug).Sessions()
	}
	if sessions == nil {
		respondError(w, http.StatusInternalServerError, "session store not available")
		return
	}

	sessionID := mux.Vars(r)["id"]
	meta, err := sessions.GetSessionMeta(r.Context(), sessionID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to resolve session")
		return
	}
	if meta == nil || (scope == "personal" && meta.UserID != userID) {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	events, err := sessions.ReadTranscriptEvents(r.Context(), studioChatAppName, userID, sessionID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to read session events")
		return
	}

	knowledgeData, toolData := resolveInvocationKnowledgeDebug(events, invocationID)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"sessionId":    sessionID,
		"invocationId": invocationID,
		"knowledge":    knowledgeData,
		"tools":        toolData,
	})
}
