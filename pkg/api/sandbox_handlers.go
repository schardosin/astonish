package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/sandbox"
)

// SandboxStatusResponse is the JSON response for GET /api/sandbox/status.
type SandboxStatusResponse struct {
	Platform           string `json:"platform"`
	Reason             string `json:"reason,omitempty"`
	SandboxEnabled     bool   `json:"sandboxEnabled"`
	IncusAvailable     bool   `json:"incusAvailable"`
	BaseTemplateExists bool   `json:"baseTemplateExists"`
}

// SandboxOptionalToolResponse represents one optional tool for the UI.
type SandboxOptionalToolResponse struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	URL             string `json:"url"`
	Recommended     bool   `json:"recommended"`
	RequiresNesting bool   `json:"requiresNesting"`
}

// SandboxInitRequest is the JSON body for POST /api/sandbox/init.
type SandboxInitRequest struct {
	InstallTools map[string]bool `json:"installTools"`
}

// SandboxStatusHandler handles GET /api/sandbox/status.
// Returns platform detection, sandbox config, and base template existence.
func SandboxStatusHandler(w http.ResponseWriter, r *http.Request) {
	platform, reason := sandbox.DetectPlatformReason()

	// Docker+Incus is not yet supported
	if platform == sandbox.PlatformDockerIncus {
		reason = "Docker+Incus setup is not yet implemented. Currently only Linux with native Incus is supported."
		platform = sandbox.PlatformUnsupported
	}

	// Check config
	appCfg, err := config.LoadAppConfig()
	sandboxEnabled := true
	if err == nil && appCfg != nil {
		sandboxEnabled = sandbox.IsSandboxEnabled(&appCfg.Sandbox)
	}

	// Check if base template exists
	baseTemplateExists := false
	incusAvailable := platform == sandbox.PlatformLinuxNative
	if incusAvailable {
		client, connErr := sandbox.Connect(platform)
		if connErr == nil {
			containerName := sandbox.TemplateName(sandbox.BaseTemplate)
			baseTemplateExists = client.InstanceExists(containerName)
		}
	}

	resp := SandboxStatusResponse{
		Platform:           platformString(platform),
		Reason:             reason,
		SandboxEnabled:     sandboxEnabled,
		IncusAvailable:     incusAvailable,
		BaseTemplateExists: baseTemplateExists,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// SandboxOptionalToolsHandler handles GET /api/sandbox/optional-tools.
// Returns the catalog of optional tools that can be installed in the base template.
func SandboxOptionalToolsHandler(w http.ResponseWriter, r *http.Request) {
	tools := sandbox.OptionalTools()
	resp := make([]SandboxOptionalToolResponse, 0, len(tools))
	for _, t := range tools {
		resp = append(resp, SandboxOptionalToolResponse{
			ID:              t.ID,
			Name:            t.Name,
			Description:     strings.ReplaceAll(t.Description, "\n", " "),
			URL:             t.URL,
			Recommended:     t.Recommended,
			RequiresNesting: t.RequiresNesting,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"tools": resp})
}

// SandboxInitHandler handles POST /api/sandbox/init.
// Initializes the base template with selected optional tools.
// Streams progress via SSE events: progress, done, error.
func SandboxInitHandler(w http.ResponseWriter, r *http.Request) {
	var req SandboxInitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Detect platform
	platform, reason := sandbox.DetectPlatformReason()
	if platform == sandbox.PlatformDockerIncus {
		platform = sandbox.PlatformUnsupported
	}
	if platform == sandbox.PlatformUnsupported {
		http.Error(w, fmt.Sprintf("sandbox unavailable: %s", reason), http.StatusServiceUnavailable)
		return
	}

	// Connect to Incus
	client, err := sandbox.Connect(platform)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to connect to Incus: %v", err), http.StatusInternalServerError)
		return
	}

	// Check if already exists
	containerName := sandbox.TemplateName(sandbox.BaseTemplate)
	if client.InstanceExists(containerName) {
		http.Error(w, "base template already exists", http.StatusConflict)
		return
	}

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Create registry
	registry, err := sandbox.NewTemplateRegistry()
	if err != nil {
		SendSSE(w, flusher, "error", map[string]string{"error": fmt.Sprintf("failed to create template registry: %v", err)})
		return
	}

	// Build options with SSE progress callback
	opts := sandbox.BaseTemplateOptions{
		InstallTools: req.InstallTools,
		ProgressFunc: func(msg string) {
			msg = strings.TrimRight(msg, "\n")
			if msg != "" {
				SendSSE(w, flusher, "progress", map[string]string{"message": msg})
			}
		},
	}
	if opts.InstallTools == nil {
		opts.InstallTools = make(map[string]bool)
	}

	// Run init (long-running operation — minutes)
	if err := sandbox.InitBaseTemplate(client, registry, opts); err != nil {
		SendSSE(w, flusher, "error", map[string]string{"error": err.Error()})
		return
	}

	SendSSE(w, flusher, "done", map[string]string{"status": "success"})
}

// platformString converts a sandbox.Platform to a JSON-friendly string.
func platformString(p sandbox.Platform) string {
	switch p {
	case sandbox.PlatformLinuxNative:
		return "linux_native"
	case sandbox.PlatformDockerIncus:
		return "docker_incus"
	default:
		return "unsupported"
	}
}
