package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/schardosin/astonish/pkg/sandbox"
	incus "github.com/schardosin/astonish/pkg/sandbox/incus"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/pgstore"
)

// --- Team Template API ---
//
// These endpoints manage per-team sandbox template containers.
// The template name is always "team-<slug>" and is derived server-side from
// the authenticated team context — never from user input.
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
// Returns whether the team template container exists and its state.
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
	containerName := incus.TemplateName(templateName)

	resp := TeamTemplateStatusResponse{
		TemplateName: templateName,
	}

	client, err := sandboxConnect()
	if err != nil {
		// Sandbox unavailable — template cannot exist
		respondJSON(w, http.StatusOK, resp)
		return
	}

	resp.Exists = client.InstanceExists(containerName)
	if resp.Exists {
		resp.Running = client.IsRunning(containerName)
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
// Creates the team template container from @base and starts it.
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
	containerName := incus.TemplateName(templateName)

	client, err := sandboxConnect()
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	registry, err := sandbox.NewTemplateRegistry()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load template registry: "+err.Error())
		return
	}

	// If already exists, just ensure it's running
	if client.InstanceExists(containerName) {
		if !client.IsRunning(containerName) {
			if err := client.StartInstance(containerName); err != nil {
				respondError(w, http.StatusInternalServerError, "failed to start template: "+err.Error())
				return
			}
		}
		respondJSON(w, http.StatusOK, map[string]any{"status": "ok", "templateName": templateName, "created": false})
		return
	}

	// Create the template from @base
	if err := sandbox.CreateTemplate(client, registry, templateName, "Team template for "+tc.TeamSlug); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create template: "+err.Error())
		return
	}

	// Start the container for immediate terminal access
	if !client.IsRunning(containerName) {
		if err := client.StartInstance(containerName); err != nil {
			slog.Warn("team template created but failed to start", "component", "sandbox-terminal", "template", templateName, "error", err)
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{"status": "ok", "templateName": templateName, "created": true})
}

// TeamTemplateSaveHandler handles POST /api/team/template/save.
// Saves the current template state and sets it as the team's default for fleet sessions.
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
	containerName := incus.TemplateName(templateName)

	client, err := sandboxConnect()
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	if !client.InstanceExists(containerName) {
		respondError(w, http.StatusNotFound, "Team template does not exist — create it first")
		return
	}

	registry, err := sandbox.NewTemplateRegistry()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load template registry: "+err.Error())
		return
	}

	// Snapshot the template (for overlay-based templates this just updates metadata)
	if err := sandbox.SnapshotTemplate(client, registry, templateName); err != nil {
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
// Destroys the current team template and recreates it fresh from @base.
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
	containerName := incus.TemplateName(templateName)

	client, err := sandboxConnect()
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	registry, err := sandbox.NewTemplateRegistry()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load template registry: "+err.Error())
		return
	}

	// Delete existing template if it exists
	if client.InstanceExists(containerName) {
		if err := sandbox.DeleteTemplate(client, registry, templateName); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to delete template: "+err.Error())
			return
		}
	}

	// Recreate from @base
	if err := sandbox.CreateTemplate(client, registry, templateName, "Team template for "+tc.TeamSlug); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to recreate template: "+err.Error())
		return
	}

	// Start the container
	if !client.IsRunning(containerName) {
		if err := client.StartInstance(containerName); err != nil {
			slog.Warn("team template recreated but failed to start", "component", "sandbox-terminal", "template", templateName, "error", err)
		}
	}

	// Clear saved template from team settings (it's a fresh container now)
	svc := store.FromRequest(r)
	if svc != nil && svc.Settings != nil {
		settings, err := svc.Settings.Get(r.Context())
		if err == nil && settings != nil && settings.TemplateName == templateName {
			settings.TemplateName = ""
			if saveErr := svc.Settings.Save(r.Context(), settings); saveErr != nil {
				slog.Warn("failed to clear template from team settings", "component", "sandbox-terminal", "error", saveErr)
			}
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{"status": "ok", "templateName": templateName, "restored": true})
}

// TeamTemplateDeleteHandler handles DELETE /api/team/template.
// Destroys the team template container and clears the team settings.
func TeamTemplateDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireTeamAdmin(w, r) {
		return
	}

	tc := pgstore.TenantContextFrom(r.Context())
	if tc == nil || tc.TeamSlug == "" {
		respondError(w, http.StatusBadRequest, "Team context required")
		return
	}

	templateName := "team-" + tc.TeamSlug
	containerName := incus.TemplateName(templateName)

	client, err := sandboxConnect()
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	registry, err := sandbox.NewTemplateRegistry()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load template registry: "+err.Error())
		return
	}

	if client.InstanceExists(containerName) {
		if err := sandbox.DeleteTemplate(client, registry, templateName); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to delete template: "+err.Error())
			return
		}
	}

	// Clear from team settings
	svc := store.FromRequest(r)
	if svc != nil && svc.Settings != nil {
		settings, err := svc.Settings.Get(r.Context())
		if err == nil && settings != nil && settings.TemplateName != "" {
			settings.TemplateName = ""
			if saveErr := svc.Settings.Save(r.Context(), settings); saveErr != nil {
				slog.Warn("failed to clear template from team settings", "component", "sandbox-terminal", "error", saveErr)
			}
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{"status": "ok", "deleted": true})
}

// TeamTemplateStartHandler handles POST /api/team/template/start.
// Starts the team template container (for terminal access after save/stop).
func TeamTemplateStartHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireTeamAdmin(w, r) {
		return
	}

	tc := pgstore.TenantContextFrom(r.Context())
	if tc == nil || tc.TeamSlug == "" {
		respondError(w, http.StatusBadRequest, "Team context required")
		return
	}

	templateName := "team-" + tc.TeamSlug
	containerName := incus.TemplateName(templateName)

	client, err := sandboxConnect()
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	if !client.InstanceExists(containerName) {
		respondError(w, http.StatusNotFound, "Team template does not exist")
		return
	}

	if client.IsRunning(containerName) {
		respondJSON(w, http.StatusOK, map[string]any{"status": "ok", "alreadyRunning": true})
		return
	}

	// Ensure overlay is mounted before starting
	registry, err := sandbox.NewTemplateRegistry()
	if err == nil {
		meta := registry.Get(templateName)
		if meta != nil && meta.BasedOn != "" {
			// Overlay templates need their overlay mounted
			sandbox.EnsureOverlayMounted(client, containerName, meta.BasedOn, registry)
		}
	}

	if err := client.StartInstance(containerName); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to start template: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"status": "ok", "started": true})
}

// TeamTemplatePackagesHandler handles POST /api/team/template/packages.
// Installs packages in the team template container (non-interactive).
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

	templateName := "team-" + tc.TeamSlug
	containerName := incus.TemplateName(templateName)

	client, err := sandboxConnect()
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	if !client.InstanceExists(containerName) || !client.IsRunning(containerName) {
		respondError(w, http.StatusConflict, "Team template is not running")
		return
	}

	// Execute apt-get install non-interactively
	cmd := append([]string{"apt-get", "install", "-y"}, req.Packages...)
	proc, err := incus.ExecInteractive(client, containerName, cmd, incus.ExecOpts{})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to exec install: "+err.Error())
		return
	}
	defer proc.Close()

	exitCode, _ := proc.Wait()
	if exitCode != 0 {
		respondError(w, http.StatusInternalServerError, "package install failed with exit code")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"status": "ok", "installed": req.Packages})
}
