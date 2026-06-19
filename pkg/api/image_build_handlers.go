package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/sandbox/imagebuilder"
	k8sbackend "github.com/schardosin/astonish/pkg/sandbox/k8s"
	"github.com/schardosin/astonish/pkg/store"
)

// ---------------------------------------------------------------------------
// Image Build API — Kaniko-based sandbox image building for OpenShell
// ---------------------------------------------------------------------------

// imageBuildRequest is the request body for build endpoints.
type imageBuildRequest struct {
	Packages []string `json:"packages"`
}

// imageBuildStatusResponse is the response for build status endpoints.
type imageBuildStatusResponse struct {
	InProgress bool   `json:"in_progress"`
	Status     string `json:"status,omitempty"`    // "building", "succeeded", "failed", ""
	Image      string `json:"image,omitempty"`     // last built image
	Error      string `json:"error,omitempty"`     // last error
	JobName    string `json:"job_name,omitempty"`
	StartedAt  string `json:"started_at,omitempty"`
}

// PlatformImageBuildHandler handles POST /api/platform/admin/sandbox/base/build.
// Triggers a Kaniko build for the platform @base sandbox image.
// Response: text/event-stream with progress/done/error events.
func PlatformImageBuildHandler(w http.ResponseWriter, r *http.Request) {
	appCfg := effectiveAppConfig(r)
	if appCfg == nil || !appCfg.Sandbox.IsOpenShellBackend() {
		respondError(w, http.StatusBadRequest, "image build is only available with the OpenShell backend")
		return
	}

	osCfg := appCfg.Sandbox.OpenShell
	if !imagebuilder.IsConfigured(osCfg.Registry.URL, osCfg.Registry.SecretName) {
		respondError(w, http.StatusPreconditionFailed, "image builder not configured: registry.url and registry.secretName are required in sandbox.openshell config")
		return
	}

	var req imageBuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if len(req.Packages) == 0 {
		respondError(w, http.StatusBadRequest, "at least one package is required")
		return
	}

	db := getPlatformBackend()
	if db == nil {
		respondError(w, http.StatusServiceUnavailable, "platform database not available")
		return
	}
	tplStore := db.SandboxTemplates()
	if tplStore == nil {
		respondError(w, http.StatusServiceUnavailable, "template store not available")
		return
	}

	// Get @base template.
	base, err := tplStore.GetBySlug(r.Context(), store.SandboxTemplateScopeGlobal, "", "base")
	if err != nil || base == nil {
		respondError(w, http.StatusInternalServerError, "failed to look up @base template")
		return
	}

	// Acquire per-template build lock.
	acquired, release, err := tplStore.AcquireTemplateBuildLock(r.Context(), base.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to acquire build lock: "+err.Error())
		return
	}
	if !acquired {
		respondError(w, http.StatusConflict, "a build is already in progress for the base template")
		return
	}
	defer release()

	// Resolve base image (FROM): the original base image from config (not the
	// previously-built one — we build on top of the original).
	baseImage := osCfg.SandboxImage

	// Set up SSE streaming.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	runImageBuild(r.Context(), w, flusher, &osCfg, &appCfg.Sandbox.Kubernetes, tplStore, base, baseImage, "base", req.Packages)
}

// PlatformImageBuildStatusHandler handles GET /api/platform/admin/sandbox/base/build/status.
func PlatformImageBuildStatusHandler(w http.ResponseWriter, r *http.Request) {
	db := getPlatformBackend()
	if db == nil {
		respondError(w, http.StatusServiceUnavailable, "platform database not available")
		return
	}
	tplStore := db.SandboxTemplates()
	if tplStore == nil {
		respondError(w, http.StatusServiceUnavailable, "template store not available")
		return
	}

	base, err := tplStore.GetBySlug(r.Context(), store.SandboxTemplateScopeGlobal, "", "base")
	if err != nil || base == nil {
		respondJSON(w, http.StatusOK, imageBuildStatusResponse{})
		return
	}

	inProgress, _ := tplStore.IsTemplateBuildInProgress(r.Context(), base.ID)
	respondJSON(w, http.StatusOK, imageBuildStatusResponse{
		InProgress: inProgress,
		Status:     base.BuildStatus,
		Image:      base.LastBuiltImage,
		Error:      base.BuildError,
		JobName:    base.BuildJobName,
		StartedAt:  formatOptionalTime(base.BuildStartedAt),
	})
}

// TeamImageBuildHandler handles POST /api/team/template/build.
// Triggers a Kaniko build for the team's sandbox image.
func TeamImageBuildHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireTeamAdmin(w, r) {
		return
	}

	appCfg := effectiveAppConfig(r)
	if appCfg == nil || !appCfg.Sandbox.IsOpenShellBackend() {
		respondError(w, http.StatusBadRequest, "image build is only available with the OpenShell backend")
		return
	}

	osCfg := appCfg.Sandbox.OpenShell
	if !imagebuilder.IsConfigured(osCfg.Registry.URL, osCfg.Registry.SecretName) {
		respondError(w, http.StatusPreconditionFailed, "image builder not configured: registry.url and registry.secretName are required in sandbox.openshell config")
		return
	}

	tc := store.TenantContextFrom(r.Context())
	if tc == nil || tc.TeamSlug == "" {
		respondError(w, http.StatusBadRequest, "team context required")
		return
	}

	var req imageBuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if len(req.Packages) == 0 {
		respondError(w, http.StatusBadRequest, "at least one package is required")
		return
	}

	db := getPlatformBackend()
	if db == nil {
		respondError(w, http.StatusServiceUnavailable, "platform database not available")
		return
	}
	tplStore := db.SandboxTemplates()
	if tplStore == nil {
		respondError(w, http.StatusServiceUnavailable, "template store not available")
		return
	}

	// Get or create team template.
	templateName := "team-" + tc.TeamSlug
	tpl, err := tplStore.GetBySlug(r.Context(), store.SandboxTemplateScopeTeam, tc.TeamSlug, templateName)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to look up team template")
		return
	}
	if tpl == nil {
		// Create the template on-the-fly.
		tpl = &store.SandboxTemplate{
			Slug:    templateName,
			Scope:   store.SandboxTemplateScopeTeam,
			OwnerID: tc.TeamSlug,
			Name:    fmt.Sprintf("Team %s", tc.TeamSlug),
			Version: 1,
		}
		if err := tplStore.Create(r.Context(), tpl); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to create team template: "+err.Error())
			return
		}
	}

	// Acquire per-template build lock.
	acquired, release, err := tplStore.AcquireTemplateBuildLock(r.Context(), tpl.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to acquire build lock: "+err.Error())
		return
	}
	if !acquired {
		respondError(w, http.StatusConflict, "a build is already in progress for this team's template")
		return
	}
	defer release()

	// Resolve base image: team's current image > platform base image > config default.
	baseImage := ""
	if tpl.SandboxImage != nil && *tpl.SandboxImage != "" {
		baseImage = *tpl.SandboxImage
	}
	if baseImage == "" {
		baseImage = resolveBaseImage(r.Context())
	}
	if baseImage == "" {
		baseImage = osCfg.SandboxImage
	}

	// Set up SSE streaming.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	runImageBuild(r.Context(), w, flusher, &osCfg, &appCfg.Sandbox.Kubernetes, tplStore, tpl, baseImage, tc.TeamSlug, req.Packages)
}

// TeamImageBuildStatusHandler handles GET /api/team/template/build/status.
func TeamImageBuildStatusHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireTeamAdmin(w, r) {
		return
	}

	tc := store.TenantContextFrom(r.Context())
	if tc == nil || tc.TeamSlug == "" {
		respondError(w, http.StatusBadRequest, "team context required")
		return
	}

	db := getPlatformBackend()
	if db == nil {
		respondError(w, http.StatusServiceUnavailable, "platform database not available")
		return
	}
	tplStore := db.SandboxTemplates()
	if tplStore == nil {
		respondError(w, http.StatusServiceUnavailable, "template store not available")
		return
	}

	templateName := "team-" + tc.TeamSlug
	tpl, err := tplStore.GetBySlug(r.Context(), store.SandboxTemplateScopeTeam, tc.TeamSlug, templateName)
	if err != nil || tpl == nil {
		respondJSON(w, http.StatusOK, imageBuildStatusResponse{})
		return
	}

	inProgress, _ := tplStore.IsTemplateBuildInProgress(r.Context(), tpl.ID)
	respondJSON(w, http.StatusOK, imageBuildStatusResponse{
		InProgress: inProgress,
		Status:     tpl.BuildStatus,
		Image:      tpl.LastBuiltImage,
		Error:      tpl.BuildError,
		JobName:    tpl.BuildJobName,
		StartedAt:  formatOptionalTime(tpl.BuildStartedAt),
	})
}

// ---------------------------------------------------------------------------
// Shared build orchestration
// ---------------------------------------------------------------------------

func runImageBuild(
	ctx context.Context,
	w http.ResponseWriter,
	flusher http.Flusher,
	osCfg *config.SandboxOpenShellConfig,
	k8sCfg *config.SandboxKubernetesConfig,
	tplStore store.SandboxTemplateStore,
	tpl *store.SandboxTemplate,
	baseImage string,
	scope string,
	packages []string,
) {
	// Create the K8s client for spawning the build Job.
	k8sClient, _, err := k8sbackend.NewClientFromOptions(k8sbackend.LoadConfigOptions{
		InCluster: true,
	})
	if err != nil {
		SendErrorSSE(w, flusher, "failed to create kubernetes client: "+err.Error())
		return
	}

	namespace := osCfg.Builder.Namespace
	if namespace == "" && k8sCfg != nil {
		namespace = k8sCfg.ControlPlaneNamespace
	}
	if namespace == "" {
		namespace = "default"
	}

	builder := imagebuilder.New(imagebuilder.Config{
		Client:      k8sClient,
		Namespace:   namespace,
		RegistryURL: osCfg.Registry.URL,
		SecretName:  osCfg.Registry.SecretName,
		BuildImage:  osCfg.Builder.Image,
	})

	// Mark build as starting.
	now := time.Now()
	tpl.BuildStatus = "building"
	tpl.BuildError = ""
	tpl.BuildStartedAt = &now
	tpl.Packages = packages
	_ = tplStore.Update(ctx, tpl)

	onProgress := func(_ context.Context, msg string) {
		SendSSE(w, flusher, "progress", map[string]string{"message": msg})
	}

	// Start the build (creates ConfigMap + Job).
	result, err := builder.Build(ctx, imagebuilder.BuildSpec{
		Scope:     scope,
		BaseImage: baseImage,
		Packages:  packages,
	}, onProgress)
	if err != nil {
		tpl.BuildStatus = "failed"
		tpl.BuildError = err.Error()
		_ = tplStore.Update(ctx, tpl)
		SendErrorSSE(w, flusher, "build failed to start: "+err.Error())
		return
	}

	tpl.BuildJobName = result.JobName
	_ = tplStore.Update(ctx, tpl)

	SendSSE(w, flusher, "progress", map[string]string{"message": "Build job created, streaming logs..."})

	// Stream logs in real-time (blocks until pod completes or ctx cancelled).
	buildCtx, cancel := context.WithTimeout(ctx, imagebuilder.BuildTimeout)
	defer cancel()

	_ = builder.StreamLogs(buildCtx, result.JobName, onProgress)

	// Check final status.
	status, err := builder.GetBuildStatus(ctx, result.JobName)
	if err != nil {
		tpl.BuildStatus = "failed"
		tpl.BuildError = "failed to get build status: " + err.Error()
		_ = tplStore.Update(ctx, tpl)
		SendErrorSSE(w, flusher, tpl.BuildError)
		return
	}

	switch status.Phase {
	case imagebuilder.BuildPhaseSucceeded:
		// Auto-wire: set the built image on the template.
		imgRef := result.Image
		tpl.SandboxImage = &imgRef
		tpl.LastBuiltImage = imgRef
		tpl.BuildStatus = "succeeded"
		tpl.BuildError = ""
		if err := tplStore.Update(ctx, tpl); err != nil {
			slog.Error("failed to update template after successful build", "error", err)
		}

		SendSSE(w, flusher, "done", map[string]string{
			"image":  result.Image,
			"status": "succeeded",
		})

		slog.Info("image build succeeded",
			"scope", scope,
			"image", result.Image,
			"template", tpl.ID,
		)

	case imagebuilder.BuildPhaseFailed:
		tpl.BuildStatus = "failed"
		tpl.BuildError = status.Message
		_ = tplStore.Update(ctx, tpl)
		SendErrorSSE(w, flusher, "build failed: "+status.Message)

	default:
		tpl.BuildStatus = "failed"
		tpl.BuildError = "build ended in unexpected state: " + string(status.Phase)
		_ = tplStore.Update(ctx, tpl)
		SendErrorSSE(w, flusher, tpl.BuildError)
	}

	// Cleanup the ConfigMap (Job auto-cleans via TTL).
	builder.Cleanup(ctx, &imagebuilder.BuildResult{ConfigMapName: result.ConfigMapName})
}

func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}
