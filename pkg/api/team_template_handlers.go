package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/schardosin/astonish/pkg/sandbox"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/pgstore"
)

// --- Team Template API ---
//
// These endpoints manage per-team sandbox template editor sessions.
// The session ID is always "team-template-<slug>" and is derived server-side
// from the authenticated team context — never from user input.
//
// The implementation is backend-agnostic: it delegates to sandbox.Backend
// (CreateSession, ExecInteractive, SaveSessionAsTemplate, DestroySession)
// which transparently handles both Incus and K8s deployments.
//
// All endpoints require team admin permission.

// TeamTemplateStatusResponse is the response for GET /api/team/template/status.
type TeamTemplateStatusResponse struct {
	Exists       bool   `json:"exists"`
	Running      bool   `json:"running"`
	TemplateName string `json:"templateName"`
	Saved        bool   `json:"saved"` // true if TeamSettings.TemplateName is set
}

// TeamTemplateStatusHandler handles GET /api/team/template/status.
// Returns whether the team template editor session exists and its state.
func TeamTemplateStatusHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireTeamAdmin(w, r) {
		return
	}

	tc := pgstore.TenantContextFrom(r.Context())
	if tc == nil || tc.TeamSlug == "" {
		respondError(w, http.StatusBadRequest, "Team context required")
		return
	}

	templateName := "team-" + tc.TeamSlug
	sessionID := teamTemplateSessionID(tc.TeamSlug)

	resp := TeamTemplateStatusResponse{
		TemplateName: templateName,
	}

	backend, cleanup, err := sandboxBackendForRequest(r)
	if err != nil {
		// Sandbox unavailable — template cannot exist
		respondJSON(w, http.StatusOK, resp)
		return
	}
	if cleanup != nil {
		defer cleanup()
	}

	state, err := backend.SessionState(r.Context(), sessionID)
	if err != nil {
		// Error querying state — report as not existing
		respondJSON(w, http.StatusOK, resp)
		return
	}

	switch state {
	case sandbox.SessionStateRunning:
		resp.Exists = true
		resp.Running = true
	case sandbox.SessionStateStopped, sandbox.SessionStateCreating, sandbox.SessionStateResuming:
		resp.Exists = true
		resp.Running = false
	default:
		// SessionStateGone or any unknown state
		resp.Exists = false
	}

	// Check if team settings has this template saved
	svc := store.FromRequest(r)
	if svc != nil && svc.Settings != nil {
		settings, err := svc.Settings.Get(r.Context())
		if err == nil && settings != nil {
			resp.Saved = settings.TemplateName == templateName
		}
	}

	respondJSON(w, http.StatusOK, resp)
}

// TeamTemplateCreateHandler handles POST /api/team/template/create.
// Creates the team template editor session from @base and starts it.
func TeamTemplateCreateHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireTeamAdmin(w, r) {
		return
	}

	tc := pgstore.TenantContextFrom(r.Context())
	if tc == nil || tc.TeamSlug == "" {
		respondError(w, http.StatusBadRequest, "Team context required")
		return
	}

	templateName := "team-" + tc.TeamSlug
	sessionID := teamTemplateSessionID(tc.TeamSlug)

	backend, cleanup, err := sandboxBackendForRequest(r)
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if cleanup != nil {
		defer cleanup()
	}

	// Check if editor session already exists
	state, _ := backend.SessionState(r.Context(), sessionID)
	if state == sandbox.SessionStateRunning {
		respondJSON(w, http.StatusOK, map[string]any{"status": "ok", "templateName": templateName, "created": false})
		return
	}
	if state == sandbox.SessionStateStopped {
		// Resume the stopped session
		if err := backend.StartSession(r.Context(), sessionID); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to start template editor: "+err.Error())
			return
		}
		respondJSON(w, http.StatusOK, map[string]any{"status": "ok", "templateName": templateName, "created": false})
		return
	}

	// Create fresh editor session from @base
	_, err = backend.CreateSession(r.Context(), sandbox.SessionSpec{
		SessionID:  sessionID,
		Type:       sandbox.SessionTypeChat, // editor sessions use chat type
		TemplateID: sandbox.BaseTemplateID,
		TeamSlug:   tc.TeamSlug,
		Labels: map[string]string{
			"astonish.io/purpose": "team-template-editor",
			"astonish.io/team":    tc.TeamSlug,
		},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create template editor: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"status": "ok", "templateName": templateName, "created": true})
}

// TeamTemplateSaveHandler handles POST /api/team/template/save.
// Captures the editor session's upper layer as a template and sets it as
// the team's default for fleet sessions.
func TeamTemplateSaveHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireTeamAdmin(w, r) {
		return
	}

	tc := pgstore.TenantContextFrom(r.Context())
	if tc == nil || tc.TeamSlug == "" {
		respondError(w, http.StatusBadRequest, "Team context required")
		return
	}

	templateName := "team-" + tc.TeamSlug
	sessionID := teamTemplateSessionID(tc.TeamSlug)

	backend, cleanup, err := sandboxBackendForRequest(r)
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if cleanup != nil {
		defer cleanup()
	}

	// Verify the editor session exists and is running
	state, _ := backend.SessionState(r.Context(), sessionID)
	if state == sandbox.SessionStateGone {
		respondError(w, http.StatusNotFound, "Team template editor does not exist — create it first")
		return
	}
	if state != sandbox.SessionStateRunning {
		respondError(w, http.StatusConflict, "Team template editor is not running")
		return
	}

	// Capture the upper layer as a template
	_, err = backend.SaveSessionAsTemplate(r.Context(), sessionID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save template: "+err.Error())
		return
	}

	// Update team settings to use this template
	svc := store.FromRequest(r)
	if svc != nil && svc.Settings != nil {
		settings, err := svc.Settings.Get(r.Context())
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to read team settings: "+err.Error())
			return
		}
		if settings == nil {
			settings = &store.TeamSettings{}
		}
		settings.TemplateName = templateName
		if err := svc.Settings.Save(r.Context(), settings); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to save team settings: "+err.Error())
			return
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{"status": "ok", "templateName": templateName})
}

// TeamTemplateRestoreHandler handles POST /api/team/template/restore.
// Destroys the current editor session and recreates it fresh from @base.
func TeamTemplateRestoreHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireTeamAdmin(w, r) {
		return
	}

	tc := pgstore.TenantContextFrom(r.Context())
	if tc == nil || tc.TeamSlug == "" {
		respondError(w, http.StatusBadRequest, "Team context required")
		return
	}

	templateName := "team-" + tc.TeamSlug
	sessionID := teamTemplateSessionID(tc.TeamSlug)

	backend, cleanup, err := sandboxBackendForRequest(r)
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if cleanup != nil {
		defer cleanup()
	}

	// Destroy existing editor session if it exists
	if err := backend.DestroySession(r.Context(), sessionID); err != nil {
		slog.Warn("failed to destroy old template editor session", "component", "team-template", "error", err)
	}

	// Recreate from @base
	_, err = backend.CreateSession(r.Context(), sandbox.SessionSpec{
		SessionID:  sessionID,
		Type:       sandbox.SessionTypeChat,
		TemplateID: sandbox.BaseTemplateID,
		TeamSlug:   tc.TeamSlug,
		Labels: map[string]string{
			"astonish.io/purpose": "team-template-editor",
			"astonish.io/team":    tc.TeamSlug,
		},
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to recreate template editor: "+err.Error())
		return
	}

	// Clear saved template from team settings (it's a fresh session now)
	svc := store.FromRequest(r)
	if svc != nil && svc.Settings != nil {
		settings, err := svc.Settings.Get(r.Context())
		if err == nil && settings != nil && settings.TemplateName == templateName {
			settings.TemplateName = ""
			if saveErr := svc.Settings.Save(r.Context(), settings); saveErr != nil {
				slog.Warn("failed to clear template from team settings", "component", "team-template", "error", saveErr)
			}
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{"status": "ok", "templateName": templateName, "restored": true})
}

// TeamTemplateDeleteHandler handles DELETE /api/team/template.
// Destroys the team template editor session and clears the team settings.
func TeamTemplateDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireTeamAdmin(w, r) {
		return
	}

	tc := pgstore.TenantContextFrom(r.Context())
	if tc == nil || tc.TeamSlug == "" {
		respondError(w, http.StatusBadRequest, "Team context required")
		return
	}

	sessionID := teamTemplateSessionID(tc.TeamSlug)

	backend, cleanup, err := sandboxBackendForRequest(r)
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if cleanup != nil {
		defer cleanup()
	}

	// Destroy the editor session
	if err := backend.DestroySession(r.Context(), sessionID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete template editor: "+err.Error())
		return
	}

	// Clear from team settings
	svc := store.FromRequest(r)
	if svc != nil && svc.Settings != nil {
		settings, err := svc.Settings.Get(r.Context())
		if err == nil && settings != nil && settings.TemplateName != "" {
			settings.TemplateName = ""
			if saveErr := svc.Settings.Save(r.Context(), settings); saveErr != nil {
				slog.Warn("failed to clear template from team settings", "component", "team-template", "error", saveErr)
			}
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{"status": "ok", "deleted": true})
}

// TeamTemplateStartHandler handles POST /api/team/template/start.
// Starts the team template editor session (for terminal access after stop).
func TeamTemplateStartHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireTeamAdmin(w, r) {
		return
	}

	tc := pgstore.TenantContextFrom(r.Context())
	if tc == nil || tc.TeamSlug == "" {
		respondError(w, http.StatusBadRequest, "Team context required")
		return
	}

	sessionID := teamTemplateSessionID(tc.TeamSlug)

	backend, cleanup, err := sandboxBackendForRequest(r)
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if cleanup != nil {
		defer cleanup()
	}

	state, _ := backend.SessionState(r.Context(), sessionID)
	if state == sandbox.SessionStateGone {
		respondError(w, http.StatusNotFound, "Team template editor does not exist")
		return
	}
	if state == sandbox.SessionStateRunning {
		respondJSON(w, http.StatusOK, map[string]any{"status": "ok", "alreadyRunning": true})
		return
	}

	if err := backend.StartSession(r.Context(), sessionID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to start template editor: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"status": "ok", "started": true})
}

// TeamTemplatePackagesHandler handles POST /api/team/template/packages.
// Installs packages in the team template editor session (non-interactive).
type TeamTemplatePackagesRequest struct {
	Packages []string `json:"packages"`
}

func TeamTemplatePackagesHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireTeamAdmin(w, r) {
		return
	}

	tc := pgstore.TenantContextFrom(r.Context())
	if tc == nil || tc.TeamSlug == "" {
		respondError(w, http.StatusBadRequest, "Team context required")
		return
	}

	var req TeamTemplatePackagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if len(req.Packages) == 0 {
		respondError(w, http.StatusBadRequest, "packages list is required")
		return
	}

	sessionID := teamTemplateSessionID(tc.TeamSlug)

	backend, cleanup, err := sandboxBackendForRequest(r)
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if cleanup != nil {
		defer cleanup()
	}

	// Verify editor session is running
	state, _ := backend.SessionState(r.Context(), sessionID)
	if state != sandbox.SessionStateRunning {
		respondError(w, http.StatusConflict, "Team template editor is not running")
		return
	}

	// Execute apt-get install non-interactively
	cmd := append([]string{"apt-get", "install", "-y"}, req.Packages...)
	result, err := backend.Exec(r.Context(), sessionID, sandbox.ExecSpec{
		Command: cmd,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to exec install: "+err.Error())
		return
	}
	if result.ExitCode != 0 {
		respondError(w, http.StatusInternalServerError, "package install failed with exit code "+itoa(result.ExitCode))
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"status": "ok", "installed": req.Packages})
}

// teamTemplateExecInteractive starts a PTY session in the team template
// editor. Used by the WebSocket terminal handler.
func teamTemplateExecInteractive(ctx context.Context, backend sandbox.Backend, teamSlug string, opts sandbox.PTYSpec) (sandbox.ExecStream, error) {
	sessionID := teamTemplateSessionID(teamSlug)
	return backend.ExecInteractive(ctx, sessionID, opts)
}

// itoa is a quick int-to-string helper to avoid importing strconv for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
