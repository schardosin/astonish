package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/schardosin/astonish/pkg/sandbox"
	"github.com/schardosin/astonish/pkg/store"
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

	tc := store.TenantContextFrom(r.Context())
	if tc == nil || tc.TeamSlug == "" {
		respondError(w, http.StatusBadRequest, "Team context required")
		return
	}

	templateName := "team-" + tc.TeamSlug
	sessionID := teamTemplateSessionID(tc.TeamSlug)

	resp := TeamTemplateStatusResponse{
		TemplateName: templateName,
	}

	backend, cleanup, err := sandboxBackendForTeamTemplate(r)
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
// Creates the team template editor session and starts it.
// If a previously-saved template exists, the editor resumes on top of
// that layer chain. Otherwise, it starts from @base.
func TeamTemplateCreateHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireTeamAdmin(w, r) {
		return
	}

	tc := store.TenantContextFrom(r.Context())
	if tc == nil || tc.TeamSlug == "" {
		respondError(w, http.StatusBadRequest, "Team context required")
		return
	}

	templateName := "team-" + tc.TeamSlug
	sessionID := teamTemplateSessionID(tc.TeamSlug)

	backend, cleanup, err := sandboxBackendForTeamTemplate(r)
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

	// Always create the editor from the configured @base chain. When the
	// admin has run Configure Base Sandbox, the resulting delta layer is
	// included so the editor sees the same tools as chat sessions. If no
	// config exists yet, resolveBaseLayerChain returns nil and the K8s
	// backend defaults to plain ["@base"].
	baseChain := resolveBaseLayerChain(r.Context())
	_, err = backend.CreateSession(r.Context(), sandbox.SessionSpec{
		SessionID:  sessionID,
		Type:       sandbox.SessionTypeChat,
		TemplateID: sandbox.BaseTemplateID,
		TeamSlug:   tc.TeamSlug,
		LayerChain: baseChain,
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
//
// Architecture (K8s):
//  1. Runs the capture pipeline → produces a content-addressed layer on the PVC.
//  2. Persists a sandbox_layers row (idempotent, content-addressed).
//  3. Upserts a sandbox_templates row (scope=team, slug=team-<slug>).
//  4. Increments the new layer's ref_count, decrements the old one.
//  5. Updates team_settings.template_name = "team-<slug>" so chat sessions
//     can resolve the name → layer chain at launch time.
func TeamTemplateSaveHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireTeamAdmin(w, r) {
		return
	}

	tc := store.TenantContextFrom(r.Context())
	if tc == nil || tc.TeamSlug == "" {
		respondError(w, http.StatusBadRequest, "Team context required")
		return
	}

	templateSlug := "team-" + tc.TeamSlug
	sessionID := teamTemplateSessionID(tc.TeamSlug)

	backend, cleanup, err := sandboxBackendForTeamTemplate(r)
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
	artifact, err := backend.SaveSessionAsTemplate(r.Context(), sessionID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save template: "+err.Error())
		return
	}

	// Persist the layer and template DAG rows in the platform DB.
	// This MUST succeed; without the DB rows, chat sessions cannot resolve
	// the team's layer chain and the save is effectively a no-op.
	platformBackend := getPlatformBackend()
	if platformBackend == nil {
		respondError(w, http.StatusServiceUnavailable, "platform DB not available — cannot persist template")
		return
	}
	if err := persistTeamTemplateArtifact(r.Context(), platformBackend, tc.TeamSlug, templateSlug, artifact); err != nil {
		slog.Error("failed to persist template artifact to platform DB",
			"team", tc.TeamSlug,
			"layer", artifact.LayerID,
			"parentLayer", artifact.ParentLayer,
			"error", err)
		respondError(w, http.StatusInternalServerError, "failed to persist template: "+err.Error())
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
		settings.TemplateName = templateSlug
		if err := svc.Settings.Save(r.Context(), settings); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to save team settings: "+err.Error())
			return
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{"status": "ok", "templateName": templateSlug})
}

// persistTeamTemplateArtifact writes the captured layer and template rows
// to the platform DB. Idempotent: re-capturing the same content is safe.
//
// Steps:
//  1. PutLayer (ON CONFLICT DO NOTHING for content dedup).
//  2. GetBySlug to see if the template already exists.
//  3. If exists: decrement old layer ref, update top_layer_id, increment new.
//  4. If not: Create template row, increment ref.
func persistTeamTemplateArtifact(ctx context.Context, backend store.PlatformBackend, teamSlug, templateSlug string, artifact *sandbox.TemplateArtifact) error {
	layers := backend.SandboxLayers()
	templates := backend.SandboxTemplates()
	if layers == nil || templates == nil {
		return fmt.Errorf("platform store not available")
	}

	// 1. Ensure the layer row exists.
	layer := &store.SandboxLayer{
		LayerID:    artifact.LayerID,
		ParentLayer: ptrIfNonEmpty(artifact.ParentLayer),
		CephFSPath: "/mnt/astonish-layers/" + artifact.LayerID,
		SizeBytes:  artifact.SizeBytes,
	}
	if err := layers.PutLayer(ctx, layer); err != nil {
		return fmt.Errorf("put layer: %w", err)
	}

	// 2. Check if the template already exists.
	existing, err := templates.GetBySlug(ctx, store.SandboxTemplateScopeTeam, teamSlug, templateSlug)
	if err != nil {
		return fmt.Errorf("get template by slug: %w", err)
	}

	baseTemplateID := baseTemplateUUID // well-known UUID from migration 005

	if existing != nil {
		// 3a. Template exists — swap top_layer_id.
		oldLayerID := existing.TopLayerID
		existing.TopLayerID = &artifact.LayerID
		existing.Version++
		if err := templates.Update(ctx, existing); err != nil {
			return fmt.Errorf("update template: %w", err)
		}
		// Ref-count: +1 new, -1 old.
		if err := layers.IncrementRefCount(ctx, artifact.LayerID); err != nil {
			return fmt.Errorf("increment ref on new layer: %w", err)
		}
		if oldLayerID != nil && *oldLayerID != "" && *oldLayerID != "@base" {
			if err := layers.DecrementRefCount(ctx, *oldLayerID); err != nil {
				slog.Warn("failed to decrement ref on old layer",
					"layer", *oldLayerID, "error", err)
			}
		}
	} else {
		// 3b. New template — create with parent = @base.
		tpl := &store.SandboxTemplate{
			ID:               uuid.New().String(),
			Slug:             templateSlug,
			Scope:            store.SandboxTemplateScopeTeam,
			OwnerID:          teamSlug,
			Name:             "Team " + teamSlug + " default",
			ParentTemplateID: &baseTemplateID,
			TopLayerID:       &artifact.LayerID,
			Version:          1,
		}
		if err := templates.Create(ctx, tpl); err != nil {
			return fmt.Errorf("create template: %w", err)
		}
		if err := layers.IncrementRefCount(ctx, artifact.LayerID); err != nil {
			return fmt.Errorf("increment ref on new layer: %w", err)
		}
	}

	return nil
}

// baseTemplateUUID is the well-known UUID for the global @base template,
// matching the seed migration (005_seed_base_template.sql).
const baseTemplateUUID = "a0000000-0000-4000-8000-000000000001"

// ptrIfNonEmpty returns a pointer to s if non-empty, nil otherwise.
func ptrIfNonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// deleteTeamTemplateState removes the sandbox_templates DB row and decrements
// the ref_count on the associated layer. This is the DB-side complement to
// DestroySession (which removes the pod). Both Delete and Restore handlers
// call this to ensure a subsequent Create starts fresh from @base.
//
// Best-effort: errors are logged but do not propagate. The GC reconciler can
// heal a partially-cleaned state (orphan layers with stale ref_counts).
// Returns the layer ID whose ref_count was decremented (empty string if none).
// Callers use this to decide whether to reclaim bytes from disk.
func deleteTeamTemplateState(ctx context.Context, teamSlug string) string {
	backend := getPlatformBackend()
	if backend == nil {
		return ""
	}
	templates := backend.SandboxTemplates()
	layers := backend.SandboxLayers()
	if templates == nil || layers == nil {
		return ""
	}
	return deleteTeamTemplateStateWith(ctx, teamSlug, templates, layers)
}

// deleteTeamTemplateStateWith is the testable core of deleteTeamTemplateState.
// It accepts explicit store interfaces so callers (and tests) don't depend on
// the package-level platformPGStoreInstance global.
// Returns the layer ID that was decremented (empty if nothing to do).
func deleteTeamTemplateStateWith(ctx context.Context, teamSlug string, templates store.SandboxTemplateStore, layers store.LayerStore) string {
	templateSlug := "team-" + teamSlug
	tpl, err := templates.GetBySlug(ctx, store.SandboxTemplateScopeTeam, teamSlug, templateSlug)
	if err != nil || tpl == nil {
		// Nothing to clean up — template never saved or already deleted.
		return ""
	}

	// Capture layer ID before deleting the row (FK is ON DELETE RESTRICT on
	// the *layer* side, so deleting the template row is safe).
	oldLayerID := tpl.TopLayerID

	if err := templates.Delete(ctx, tpl.ID); err != nil {
		slog.Warn("deleteTeamTemplateState: failed to delete template row",
			"component", "team-template", "templateID", tpl.ID, "error", err)
		return "" // Don't decrement ref if the row still exists.
	}

	// Decrement the layer's ref_count.
	if oldLayerID != nil && *oldLayerID != "" && *oldLayerID != sandbox.BaseTemplateID {
		if err := layers.DecrementRefCount(ctx, *oldLayerID); err != nil {
			slog.Warn("deleteTeamTemplateState: failed to decrement layer ref_count",
				"component", "team-template", "layer", *oldLayerID, "error", err)
		}
		return *oldLayerID
	}
	return ""
}

// reclaimLayerBytes checks whether a layer's ref_count has reached 0 and,
// if so, removes the layer bytes from the backend storage (PVC) and deletes
// the layer row from the platform DB. Best-effort: errors are logged.
//
// layerID may be empty (e.g. when deleteTeamTemplateState found nothing to do),
// in which case this is a no-op.
func reclaimLayerBytes(ctx context.Context, backend sandbox.Backend, layerID string) {
	if layerID == "" || layerID == sandbox.BaseTemplateID {
		return
	}
	platformBackend := getPlatformBackend()
	if platformBackend == nil {
		return
	}
	layers := platformBackend.SandboxLayers()
	if layers == nil {
		return
	}

	// Re-read the layer to check ref_count.
	layer, err := layers.GetLayer(ctx, layerID)
	if err != nil || layer == nil {
		// Layer row already gone or error reading — nothing to do.
		return
	}
	if layer.RefCount > 0 {
		// Another template still references this layer (content-addressed
		// dedup). Leave bytes in place.
		return
	}

	// ref_count == 0: reclaim bytes from disk via the backend.
	if err := backend.DeleteTemplate(ctx, layerID, true); err != nil {
		slog.Warn("reclaimLayerBytes: failed to delete layer bytes from disk",
			"component", "team-template", "layer", layerID, "error", err)
		// Don't delete the DB row if bytes couldn't be reclaimed — leaves
		// a trail for manual cleanup.
		return
	}

	// Remove the layer row from the DB.
	if err := layers.DeleteLayer(ctx, layerID); err != nil {
		slog.Warn("reclaimLayerBytes: failed to delete layer row",
			"component", "team-template", "layer", layerID, "error", err)
	}
}

// TeamTemplateRestoreHandler handles POST /api/team/template/restore.
// Destroys the current editor session, removes the saved template from the
// platform DB, reclaims layer bytes from disk if unreferenced, and recreates
// the editor from @base.
func TeamTemplateRestoreHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireTeamAdmin(w, r) {
		return
	}

	tc := store.TenantContextFrom(r.Context())
	if tc == nil || tc.TeamSlug == "" {
		respondError(w, http.StatusBadRequest, "Team context required")
		return
	}

	templateName := "team-" + tc.TeamSlug
	sessionID := teamTemplateSessionID(tc.TeamSlug)

	backend, cleanup, err := sandboxBackendForTeamTemplate(r)
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

	// Remove the saved template row + decrement layer ref_count so the
	// next Create (or Save) starts from a clean @base slate.
	layerID := deleteTeamTemplateState(r.Context(), tc.TeamSlug)

	// If the layer's ref_count reached 0, reclaim bytes from disk and
	// remove the layer row from the DB.
	reclaimLayerBytes(r.Context(), backend, layerID)

	// Recreate the editor from the configured @base chain (same logic as
	// TeamTemplateCreateHandler). When admin has configured @base, the
	// chain includes the delta layer so the editor sees installed tools.
	baseChain := resolveBaseLayerChain(r.Context())
	_, err = backend.CreateSession(r.Context(), sandbox.SessionSpec{
		SessionID:  sessionID,
		Type:       sandbox.SessionTypeChat,
		TemplateID: sandbox.BaseTemplateID,
		TeamSlug:   tc.TeamSlug,
		LayerChain: baseChain,
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
// Destroys the team template editor session, removes the saved template from
// the platform DB (row + layer ref_count decrement), reclaims layer bytes from
// disk if unreferenced, and clears team settings.
// After this, a subsequent Create will start fresh from @base.
func TeamTemplateDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireTeamAdmin(w, r) {
		return
	}

	tc := store.TenantContextFrom(r.Context())
	if tc == nil || tc.TeamSlug == "" {
		respondError(w, http.StatusBadRequest, "Team context required")
		return
	}

	sessionID := teamTemplateSessionID(tc.TeamSlug)

	backend, cleanup, err := sandboxBackendForTeamTemplate(r)
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if cleanup != nil {
		defer cleanup()
	}

	// Destroy the editor session (pod).
	if err := backend.DestroySession(r.Context(), sessionID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete template editor: "+err.Error())
		return
	}

	// Remove the saved template row + decrement layer ref_count so a
	// subsequent Create starts fresh from @base.
	layerID := deleteTeamTemplateState(r.Context(), tc.TeamSlug)

	// If the layer's ref_count reached 0, reclaim bytes from disk and
	// remove the layer row from the DB.
	reclaimLayerBytes(r.Context(), backend, layerID)

	// Clear from team settings so fleet sessions revert to @base.
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

	tc := store.TenantContextFrom(r.Context())
	if tc == nil || tc.TeamSlug == "" {
		respondError(w, http.StatusBadRequest, "Team context required")
		return
	}

	sessionID := teamTemplateSessionID(tc.TeamSlug)

	backend, cleanup, err := sandboxBackendForTeamTemplate(r)
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

	tc := store.TenantContextFrom(r.Context())
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

	backend, cleanup, err := sandboxBackendForTeamTemplate(r)
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
