package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
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
	// DockerfileBody is the user-authored Dockerfile content (everything after
	// FROM). Supports full Dockerfile syntax: RUN, ENV, WORKDIR, ARG, COPY, etc.
	// FROM, ENTRYPOINT, CMD, and EXPOSE are rejected.
	DockerfileBody string `json:"dockerfile_body"`

	// Packages is deprecated — kept for backward compatibility. If set and
	// DockerfileBody is empty, packages are auto-migrated to a Dockerfile body.
	Packages []string `json:"packages,omitempty"`
}

// dockerfileBodyRequest is used for save-without-build endpoints.
type dockerfileBodyRequest struct {
	DockerfileBody string `json:"dockerfile_body"`
}

// dockerfileBodyResponse is the response for Dockerfile body GET endpoints.
type dockerfileBodyResponse struct {
	DockerfileBody string `json:"dockerfile_body"`
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

	// Resolve dockerfile body: prefer DockerfileBody, fall back to migrating
	// legacy Packages field.
	dockerfileBody := req.DockerfileBody
	if dockerfileBody == "" && len(req.Packages) > 0 {
		dockerfileBody = imagebuilder.MigratePackagesToDockerfile(req.Packages)
	}
	if strings.TrimSpace(dockerfileBody) == "" {
		respondError(w, http.StatusBadRequest, "dockerfile_body is required (or packages for backward compatibility)")
		return
	}
	if err := validateDockerfileBody(dockerfileBody); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
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

	runImageBuild(r.Context(), w, flusher, &osCfg, &appCfg.Sandbox.Kubernetes, tplStore, base, baseImage, "base", dockerfileBody, "")
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

	// Resolve dockerfile body: prefer DockerfileBody, fall back to migrating
	// legacy Packages field.
	dockerfileBody := req.DockerfileBody
	if dockerfileBody == "" && len(req.Packages) > 0 {
		dockerfileBody = imagebuilder.MigratePackagesToDockerfile(req.Packages)
	}
	if strings.TrimSpace(dockerfileBody) == "" {
		respondError(w, http.StatusBadRequest, "dockerfile_body is required (or packages for backward compatibility)")
		return
	}
	if err := validateDockerfileBody(dockerfileBody); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
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

	// Always build from the ORIGINAL base image (Layer 1).
	// Never use a previously-built custom image as base.
	baseImage := osCfg.SandboxImage

	// Resolve platform Dockerfile body (Layer 2) to merge with team body.
	platformBody := resolvePlatformDockerfileBody(r.Context())

	// Set up SSE streaming.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	runImageBuild(r.Context(), w, flusher, &osCfg, &appCfg.Sandbox.Kubernetes, tplStore, tpl, baseImage, tc.TeamSlug, platformBody, dockerfileBody)
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
	platformBody string,
	teamBody string,
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
	// Store the team body (or platform body for base builds) in the template record.
	now := time.Now()
	tpl.BuildStatus = "building"
	tpl.BuildError = ""
	tpl.BuildStartedAt = &now
	if scope == "base" {
		tpl.DockerfileBody = &platformBody
	} else {
		tpl.DockerfileBody = &teamBody
	}
	_ = tplStore.Update(ctx, tpl)

	onProgress := func(_ context.Context, msg string) {
		SendSSE(w, flusher, "progress", map[string]string{"message": msg})
	}

	// Start the build (creates ConfigMap + Job).
	result, err := builder.Build(ctx, imagebuilder.BuildSpec{
		Scope:        scope,
		BaseImage:    baseImage,
		PlatformBody: platformBody,
		TeamBody:     teamBody,
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

	// After StreamLogs returns (pod exited or context cancelled), poll for a
	// terminal Job condition. The K8s Job controller typically reconciles within
	// seconds, but if StreamLogs was interrupted by the build timeout while the
	// pod is still running, we continue polling for up to 5 more minutes.
	var status *imagebuilder.BuildStatus
	pollDeadline := time.Now().Add(5 * time.Minute)
	for attempt := 0; ; attempt++ {
		status, err = builder.GetBuildStatus(ctx, result.JobName)
		if err != nil {
			break
		}
		if status.Phase == imagebuilder.BuildPhaseSucceeded || status.Phase == imagebuilder.BuildPhaseFailed {
			break
		}
		if time.Now().After(pollDeadline) {
			break
		}
		// Short backoff: 1s for first 10, then 5s thereafter.
		if attempt < 10 {
			time.Sleep(1 * time.Second)
		} else {
			time.Sleep(5 * time.Second)
		}
		// Keep the client informed that we're still waiting.
		if attempt > 0 && attempt%10 == 0 {
			SendSSE(w, flusher, "progress", map[string]string{"message": "Waiting for build to complete..."})
		}
	}
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

		// For team-scoped builds, wire team settings so chat sessions use this image.
		if tpl.Scope == store.SandboxTemplateScopeTeam && tpl.OwnerID != "" {
			if svc := store.FromContext(ctx); svc != nil && svc.Settings != nil {
				if settings, sErr := svc.Settings.Get(ctx); sErr == nil {
					if settings.TemplateName != tpl.Slug {
						settings.TemplateName = tpl.Slug
						_ = svc.Settings.Save(ctx, settings)
					}
				}
			}
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

// ---------------------------------------------------------------------------
// Dockerfile body validation
// ---------------------------------------------------------------------------

// forbiddenInstructionRe matches Dockerfile instructions that are controlled
// by the platform and must not appear in user-authored bodies.
var forbiddenInstructionRe = regexp.MustCompile(`(?mi)^\s*(FROM|ENTRYPOINT|CMD|EXPOSE)\s`)

// validateDockerfileBody checks that the body doesn't contain forbidden
// instructions and respects size limits.
func validateDockerfileBody(body string) error {
	if len(body) > 65536 {
		return fmt.Errorf("dockerfile body too large (max 64KB)")
	}
	matches := forbiddenInstructionRe.FindAllString(body, 1)
	if len(matches) > 0 {
		instruction := strings.TrimSpace(matches[0])
		return fmt.Errorf("forbidden instruction %q: FROM, ENTRYPOINT, CMD, and EXPOSE are controlled by the platform", instruction)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Dockerfile body CRUD endpoints
// ---------------------------------------------------------------------------

// TeamDockerfileGetHandler handles GET /api/team/template/dockerfile.
// Returns the saved Dockerfile body for the team's template.
func TeamDockerfileGetHandler(w http.ResponseWriter, r *http.Request) {
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

	templateName := "team-" + tc.TeamSlug
	tpl, err := tplStore.GetBySlug(r.Context(), store.SandboxTemplateScopeTeam, tc.TeamSlug, templateName)
	if err != nil || tpl == nil {
		// No template yet — return empty body.
		respondJSON(w, http.StatusOK, dockerfileBodyResponse{})
		return
	}

	body := ""
	if tpl.DockerfileBody != nil {
		body = *tpl.DockerfileBody
	} else if len(tpl.Packages) > 0 {
		// Auto-migrate legacy packages to Dockerfile body for display.
		body = imagebuilder.MigratePackagesToDockerfile(tpl.Packages)
	}

	respondJSON(w, http.StatusOK, dockerfileBodyResponse{DockerfileBody: body})
}

// TeamDockerfileSaveHandler handles PUT /api/team/template/dockerfile.
// Saves the Dockerfile body without triggering a build.
func TeamDockerfileSaveHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireTeamAdmin(w, r) {
		return
	}

	tc := store.TenantContextFrom(r.Context())
	if tc == nil || tc.TeamSlug == "" {
		respondError(w, http.StatusBadRequest, "team context required")
		return
	}

	var req dockerfileBodyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if err := validateDockerfileBody(req.DockerfileBody); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	db := getPlatformBackend()
	if db == nil {
		respondError(w, http.StatusServiceUnavailable, "platform database not available")
		return
	}
	tplStore := db.SandboxTemplates()

	templateName := "team-" + tc.TeamSlug
	tpl, err := tplStore.GetBySlug(r.Context(), store.SandboxTemplateScopeTeam, tc.TeamSlug, templateName)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to look up team template")
		return
	}
	if tpl == nil {
		// Create the template on-the-fly.
		tpl = &store.SandboxTemplate{
			Slug:           templateName,
			Scope:          store.SandboxTemplateScopeTeam,
			OwnerID:        tc.TeamSlug,
			Name:           fmt.Sprintf("Team %s", tc.TeamSlug),
			DockerfileBody: &req.DockerfileBody,
			Version:        1,
		}
		if err := tplStore.Create(r.Context(), tpl); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to create team template: "+err.Error())
			return
		}
	} else {
		tpl.DockerfileBody = &req.DockerfileBody
		if err := tplStore.Update(r.Context(), tpl); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to save dockerfile: "+err.Error())
			return
		}
	}

	// Wire team settings so chat sessions resolve this template's image.
	if svc := store.FromContext(r.Context()); svc != nil && svc.Settings != nil {
		if settings, sErr := svc.Settings.Get(r.Context()); sErr == nil {
			if settings.TemplateName != templateName {
				settings.TemplateName = templateName
				_ = svc.Settings.Save(r.Context(), settings)
			}
		}
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// PlatformDockerfileGetHandler handles GET /api/platform/admin/sandbox/base/dockerfile.
func PlatformDockerfileGetHandler(w http.ResponseWriter, r *http.Request) {
	db := getPlatformBackend()
	if db == nil {
		respondError(w, http.StatusServiceUnavailable, "platform database not available")
		return
	}
	tplStore := db.SandboxTemplates()

	base, err := tplStore.GetBySlug(r.Context(), store.SandboxTemplateScopeGlobal, "", "base")
	if err != nil || base == nil {
		respondJSON(w, http.StatusOK, dockerfileBodyResponse{})
		return
	}

	body := ""
	if base.DockerfileBody != nil {
		body = *base.DockerfileBody
	} else if len(base.Packages) > 0 {
		body = imagebuilder.MigratePackagesToDockerfile(base.Packages)
	}

	respondJSON(w, http.StatusOK, dockerfileBodyResponse{DockerfileBody: body})
}

// PlatformDockerfileSaveHandler handles PUT /api/platform/admin/sandbox/base/dockerfile.
func PlatformDockerfileSaveHandler(w http.ResponseWriter, r *http.Request) {
	var req dockerfileBodyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if err := validateDockerfileBody(req.DockerfileBody); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	db := getPlatformBackend()
	if db == nil {
		respondError(w, http.StatusServiceUnavailable, "platform database not available")
		return
	}
	tplStore := db.SandboxTemplates()

	base, err := tplStore.GetBySlug(r.Context(), store.SandboxTemplateScopeGlobal, "", "base")
	if err != nil || base == nil {
		respondError(w, http.StatusInternalServerError, "failed to look up @base template")
		return
	}

	base.DockerfileBody = &req.DockerfileBody
	if err := tplStore.Update(r.Context(), base); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save dockerfile: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}
