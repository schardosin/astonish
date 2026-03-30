package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"sort"
	"time"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/credentials"
	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/sandbox"
	persistentsession "github.com/schardosin/astonish/pkg/session"
	"google.golang.org/adk/session"
)

// StudioSessionsHandler handles GET /api/studio/sessions.
func StudioSessionsHandler(w http.ResponseWriter, r *http.Request) {
	cm := GetChatManager()
	if err := cm.ensureReady(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fs := cm.fileStore()
	if fs == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]StudioSessionResponse{})
		return
	}

	metas, err := fs.ListSessionMetas(studioChatAppName, studioChatUserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Sort by updated_at descending (most recent first)
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].UpdatedAt.After(metas[j].UpdatedAt)
	})

	sessions := make([]StudioSessionResponse, 0, len(metas))
	for _, m := range metas {
		sessions = append(sessions, StudioSessionResponse{
			ID:           m.ID,
			Title:        m.Title,
			CreatedAt:    m.CreatedAt.Format(time.RFC3339),
			UpdatedAt:    m.UpdatedAt.Format(time.RFC3339),
			MessageCount: m.MessageCount,
			FleetKey:     m.FleetKey,
			FleetName:    m.FleetName,
			IssueNumber:  m.IssueNumber,
			Repo:         m.Repo,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions)
}

// StudioSessionHandler handles GET /api/studio/sessions/{id}.
func StudioSessionHandler(w http.ResponseWriter, r *http.Request) {
	cm := GetChatManager()
	if err := cm.ensureReady(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sessionID := mux.Vars(r)["id"]
	fs := cm.fileStore()
	if fs == nil {
		http.Error(w, "File store not available", http.StatusInternalServerError)
		return
	}

	// Get session metadata
	meta, err := fs.GetSessionMeta(sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}

	// Fleet sessions: read transcript and return fleet-style messages
	if meta.FleetKey != "" {
		transcriptPath := filepath.Join(fs.BaseDir(), studioChatAppName, studioChatUserID, sessionID+".jsonl")
		transcript := persistentsession.NewTranscript(transcriptPath)
		events, readErr := transcript.ReadEvents()

		var fleetMessages []FleetMessageSummary
		if readErr == nil && len(events) > 0 {
			fleetMessages = fleetEventsToMessages(events)
		}

		resp := StudioSessionDetailResponse{
			StudioSessionResponse: StudioSessionResponse{
				ID:           meta.ID,
				Title:        meta.Title,
				CreatedAt:    meta.CreatedAt.Format(time.RFC3339),
				UpdatedAt:    meta.UpdatedAt.Format(time.RFC3339),
				MessageCount: meta.MessageCount,
				FleetKey:     meta.FleetKey,
				FleetName:    meta.FleetName,
				IssueNumber:  meta.IssueNumber,
				Repo:         meta.Repo,
			},
			Messages:      []StudioMessage{},
			FleetMessages: fleetMessages,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	// Get session transcript
	getResp, err := fs.Get(r.Context(), &session.GetRequest{
		AppName:   studioChatAppName,
		UserID:    studioChatUserID,
		SessionID: sessionID,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load session: %v", err), http.StatusInternalServerError)
		return
	}

	// Transform ADK events into simplified messages
	var redactor *credentials.Redactor
	if cm.components != nil && cm.components.ChatAgent != nil {
		redactor = cm.components.ChatAgent.Redactor
	}
	messages := eventsToMessages(getResp.Session.Events(), redactor)

	resp := StudioSessionDetailResponse{
		StudioSessionResponse: StudioSessionResponse{
			ID:           meta.ID,
			Title:        meta.Title,
			CreatedAt:    meta.CreatedAt.Format(time.RFC3339),
			UpdatedAt:    meta.UpdatedAt.Format(time.RFC3339),
			MessageCount: meta.MessageCount,
		},
		Messages: messages,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// StudioDeleteSessionHandler handles DELETE /api/studio/sessions/{id}.
func StudioDeleteSessionHandler(w http.ResponseWriter, r *http.Request) {
	cm := GetChatManager()
	if err := cm.ensureReady(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sessionID := mux.Vars(r)["id"]

	// If this is an active fleet session, stop it and clean up sandbox
	registry := getFleetSessionRegistry()
	if fs := registry.Get(sessionID); fs != nil {
		fs.Stop()
		fs.Cleanup() // destroy sandbox container + clean session registry
		registry.Unregister(sessionID)
	}

	// Clean up per-session workspace directory if one was recorded.
	// Read metadata before deleting the session (deletion removes metadata).
	if fileStore := getFleetFileStore(); fileStore != nil {
		if meta, metaErr := fileStore.GetSessionMeta(sessionID); metaErr == nil && meta.WorkspaceDir != "" {
			if cleanErr := fleet.CleanupSessionWorkspace(meta.WorkspaceDir); cleanErr != nil {
				slog.Warn("could not clean up workspace", "component", "fleet", "workspace", meta.WorkspaceDir, "error", cleanErr)
			}
		}
	}

	sessionService := cm.components.SessionService

	err := sessionService.Delete(r.Context(), &session.DeleteRequest{
		AppName:   studioChatAppName,
		UserID:    studioChatUserID,
		SessionID: sessionID,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete session: %v", err), http.StatusInternalServerError)
		return
	}

	// Best-effort: destroy sandbox container if one exists for this session
	sandbox.TryDestroySessionContainer(sessionID)

	w.WriteHeader(http.StatusNoContent)
}

// StudioStopHandler handles POST /api/studio/sessions/{id}/stop.
func StudioStopHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["id"]
	cm := GetChatManager()
	cm.cancelStream(sessionID)
	w.WriteHeader(http.StatusNoContent)
}
