package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/SAP/astonish/pkg/sandbox"
	"github.com/SAP/astonish/pkg/sandbox/baseconfig"
	"github.com/SAP/astonish/pkg/store"
)

// PlatformBaseConfigGetHandler returns the current @base template configuration.
// GET /api/platform/admin/sandbox/base
func PlatformBaseConfigGetHandler(w http.ResponseWriter, r *http.Request) {
	// OpenShell backend: the interactive package build is not available.
	// Instead, return the current custom image info + the backend kind so
	// the frontend can show the image-setting UI.
	appCfg := effectiveAppConfig(r)
	if appCfg != nil && appCfg.Sandbox.Backend == "openshell" {
		currentImage := resolveBaseImage(r.Context())
		defaultImage := ""
		if appCfg.Sandbox.OpenShell.SandboxImage != "" {
			defaultImage = appCfg.Sandbox.OpenShell.SandboxImage
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"backend":          "openshell",
			"build_supported":  false,
			"sandbox_image":    currentImage,
			"default_image":    defaultImage,
			"message":          "Package installation via the interactive editor is not available with the OpenShell backend. Set a custom container image with your packages pre-installed.",
		})
		return
	}

	backend := getPlatformBackend()
	if backend == nil {
		respondError(w, http.StatusServiceUnavailable, "platform database not available")
		return
	}

	tplStore := backend.SandboxTemplates()
	if tplStore == nil {
		respondError(w, http.StatusServiceUnavailable, "template store not available")
		return
	}

	info, err := tplStore.GetBaseConfig(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get base config: %v", err))
		return
	}
	if info == nil {
		respondError(w, http.StatusNotFound, "@base template not found in database")
		return
	}

	type response struct {
		LayerID      string                 `json:"layer_id"`
		SizeBytes    int64                  `json:"size_bytes"`
		Config       *baseconfig.BaseConfig `json:"config"`
		ConfiguredBy string                 `json:"configured_by,omitempty"`
		ConfiguredAt *time.Time             `json:"configured_at,omitempty"`
		UpdatedAt    time.Time              `json:"updated_at"`
	}

	resp := response{
		LayerID:      info.LayerID,
		SizeBytes:    info.SizeBytes,
		ConfiguredBy: info.ConfiguredBy,
		ConfiguredAt: info.ConfiguredAt,
		UpdatedAt:    info.UpdatedAt,
	}

	if info.ConfigJSON != nil {
		var cfg baseconfig.BaseConfig
		if err := json.Unmarshal(info.ConfigJSON, &cfg); err == nil {
			resp.Config = &cfg
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// PlatformBaseConfigStatusHandler returns whether a build is in progress.
// GET /api/platform/admin/sandbox/base/status
func PlatformBaseConfigStatusHandler(w http.ResponseWriter, r *http.Request) {
	backend := getPlatformBackend()
	if backend == nil {
		respondError(w, http.StatusServiceUnavailable, "platform database not available")
		return
	}

	tplStore := backend.SandboxTemplates()
	if tplStore == nil {
		respondError(w, http.StatusServiceUnavailable, "template store not available")
		return
	}

	inProgress, err := tplStore.IsBuildInProgress(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to check build status: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"in_progress": inProgress})
}

// PlatformBaseConfigBuildHandler triggers a base template build.
// POST /api/platform/admin/sandbox/base/configure
//
// Request body: BaseConfig JSON.
// Response: text/event-stream with progress/done/error events.
func PlatformBaseConfigBuildHandler(w http.ResponseWriter, r *http.Request) {
	db := getPlatformBackend()
	if db == nil {
		respondError(w, http.StatusServiceUnavailable, "platform database not available")
		return
	}

	// Parse the BaseConfig from request body.
	var cfg baseconfig.BaseConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	// Get the sandbox backend early so we can detect its native architecture
	// for architecture-specific package selection (e.g. KasmVNC .deb) when
	// the caller did not explicitly provide one. This is critical on
	// Apple Silicon macOS with Docker+Incus, where containers are arm64.
	sbBackendForArch, cleanupForArch, err := sandboxBackendForRequest(r)
	if cleanupForArch != nil {
		defer cleanupForArch()
	}
	if err == nil && cfg.Architecture == "" {
		if arch := sbBackendForArch.ServerArchitecture(); arch != "" {
			cfg.Architecture = arch
		}
	}
	if cfg.Architecture == "" {
		cfg.Architecture = "amd64" // final fallback
	}

	// Determine target distro from sandbox backend kind.
	// Incus containers use Ubuntu Noble; K8s uses Debian Bookworm.
	if cfg.Distro == "" {
		appCfg := effectiveAppConfig(r)
		if appCfg != nil && !appCfg.Sandbox.IsK8sBackend() {
			cfg.Distro = string(sandbox.DistroUbuntuNoble)
		}
		// If K8s or unknown, leave empty — Render() defaults to Bookworm.
	}

	// Validate.
	if err := cfg.Validate(); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Render steps.
	steps, err := cfg.Render()
	if err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("failed to render config: %v", err))
		return
	}

	// Acquire build lock (single build at a time).
	tplStore := db.SandboxTemplates()
	if tplStore == nil {
		respondError(w, http.StatusServiceUnavailable, "template store not available")
		return
	}

	acquired, release, err := tplStore.AcquireBuildLock(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to acquire build lock: %v", err))
		return
	}
	if !acquired {
		respondError(w, http.StatusConflict, "another base configuration build is already in progress")
		return
	}
	defer release()

	// Set up SSE streaming.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Get the sandbox backend.
	sbBackend, cleanup, err := sandboxBackendForRequest(r)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		SendSSE(w, flusher, "error", map[string]string{"error": fmt.Sprintf("sandbox backend unavailable: %v", err)})
		return
	}

	// OpenShell backend does not support BuildTemplate — base sandbox
	// customization is done via the container image (Dockerfile).
	appCfgCheck := effectiveAppConfig(r)
	if appCfgCheck != nil && appCfgCheck.Sandbox.Backend == "openshell" {
		SendSSE(w, flusher, "error", map[string]string{
			"error": "Base sandbox customization is not available with the OpenShell backend. " +
				"Customize the sandbox by modifying docker/sandbox-openshell/Dockerfile and rebuilding the image.",
		})
		return
	}

	// Send initial progress.
	SendSSE(w, flusher, "progress", map[string]string{
		"message": fmt.Sprintf("Starting base configuration build (%d steps)...", len(steps)),
	})

	// Build the template.
	templateID := fmt.Sprintf("@base-config-%d", time.Now().UnixMilli())

	for i, step := range steps {
		// Truncate for display.
		display := step
		if len(display) > 80 {
			display = display[:77] + "..."
		}
		SendSSE(w, flusher, "progress", map[string]string{
			"message": fmt.Sprintf("[%d/%d] %s", i+1, len(steps), display),
			"step":    fmt.Sprintf("%d", i+1),
			"total":   fmt.Sprintf("%d", len(steps)),
		})
	}

	SendSSE(w, flusher, "progress", map[string]string{
		"message": "Executing build steps in sandbox pod (this may take several minutes)...",
	})

	artifact, err := sbBackend.BuildTemplate(r.Context(), sandbox.TemplateBuildSpec{
		TemplateID:   templateID,
		ParentLayers: []string{sandbox.BaseTemplateID},
		Steps:        steps,
	})
	if err != nil {
		SendSSE(w, flusher, "error", map[string]string{"error": fmt.Sprintf("build failed: %v", err)})
		return
	}

	SendSSE(w, flusher, "progress", map[string]string{
		"message": fmt.Sprintf("Build complete. Layer: %s (%d bytes). Persisting...", artifact.LayerID, artifact.SizeBytes),
	})

	// Persist: register the layer and update @base.
	layers := db.SandboxLayers()

	// Get old top_layer_id for ref_count management.
	oldLayerID, err := tplStore.GetBaseTopLayerID(r.Context())
	if err != nil {
		slog.Error("failed to get old @base layer", "error", err)
	}

	// Register the new layer (PutLayer is idempotent on duplicate layer_id).
	newLayer := &store.SandboxLayer{
		LayerID:    artifact.LayerID,
		CephFSPath: artifact.CephFSPath,
		SizeBytes:  artifact.SizeBytes,
	}
	if err := layers.PutLayer(r.Context(), newLayer); err != nil {
		// Ignore "already exists" — content-addressed dedup.
		if !strings.Contains(err.Error(), "duplicate") && !strings.Contains(err.Error(), "already exists") {
			SendSSE(w, flusher, "error", map[string]string{"error": fmt.Sprintf("failed to register layer: %v", err)})
			return
		}
	}

	// Increment ref_count for the new layer (template reference).
	if err := layers.IncrementRefCount(r.Context(), artifact.LayerID); err != nil {
		slog.Error("failed to increment ref_count on new layer", "layer", artifact.LayerID, "error", err)
	}

	// Serialize config to JSON for persistence.
	configJSON, err := cfg.ToJSON()
	if err != nil {
		SendSSE(w, flusher, "error", map[string]string{"error": fmt.Sprintf("failed to serialize config: %v", err)})
		return
	}

	// Update @base template row.
	// TODO: pass actual user ID when auth context is available
	if err := tplStore.SetBaseConfig(r.Context(), artifact.LayerID, configJSON, ""); err != nil {
		SendSSE(w, flusher, "error", map[string]string{"error": fmt.Sprintf("failed to update @base: %v", err)})
		return
	}

	// Decrement old layer ref_count.
	if oldLayerID != "" && oldLayerID != "@base" && oldLayerID != artifact.LayerID {
		if err := layers.DecrementRefCount(r.Context(), oldLayerID); err != nil {
			slog.Error("failed to decrement old layer ref_count", "layer", oldLayerID, "error", err)
		}
	}

	SendSSE(w, flusher, "done", map[string]any{
		"layer_id":   artifact.LayerID,
		"size_bytes": artifact.SizeBytes,
		"status":     "success",
	})
}

// PlatformBaseConfigOptionalToolsHandler returns the available optional tools catalog.
// GET /api/platform/admin/sandbox/base/tools
func PlatformBaseConfigOptionalToolsHandler(w http.ResponseWriter, r *http.Request) {
	catalog := sandbox.OptionalTools()

	type toolInfo struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		URL         string `json:"url"`
		Recommended bool   `json:"recommended"`
	}

	tools := make([]toolInfo, 0, len(catalog))
	for _, t := range catalog {
		tools = append(tools, toolInfo{
			ID:          t.ID,
			Name:        t.Name,
			Description: t.Description,
			URL:         t.URL,
			Recommended: t.Recommended,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tools)
}

// PlatformBaseImageHandler handles POST /api/platform/admin/sandbox/base/image.
// Sets or clears the custom sandbox image on the @base template. This is the
// platform-wide image override used by the OpenShell backend.
//
// Request body: {"image": "ghcr.io/sap/custom-sandbox:v2"} or {"image": ""} to clear.
func PlatformBaseImageHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Image string `json:"image"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	db := getPlatformBackend()
	if db == nil {
		respondError(w, http.StatusServiceUnavailable, "platform database not available")
		return
	}
	templates := db.SandboxTemplates()
	if templates == nil {
		respondError(w, http.StatusServiceUnavailable, "template store not available")
		return
	}

	// Look up @base.
	base, err := templates.GetBySlug(r.Context(), store.SandboxTemplateScopeGlobal, "", "base")
	if err != nil || base == nil {
		respondError(w, http.StatusInternalServerError, "failed to look up @base template")
		return
	}

	// Update.
	if req.Image != "" {
		base.SandboxImage = &req.Image
	} else {
		base.SandboxImage = nil
	}
	if err := templates.Update(r.Context(), base); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update @base: "+err.Error())
		return
	}

	slog.Info("platform base sandbox image set", "image", req.Image)
	respondJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"image":  req.Image,
	})
}
