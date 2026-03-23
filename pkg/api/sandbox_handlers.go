package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/sandbox"
	persistentsession "github.com/schardosin/astonish/pkg/session"
)

// --- Existing types ---

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

// --- New types for container/template management ---

// SandboxDetailResponse is the JSON response for GET /api/sandbox/details.
type SandboxDetailResponse struct {
	Platform           string `json:"platform"`
	Reason             string `json:"reason,omitempty"`
	SandboxEnabled     bool   `json:"sandboxEnabled"`
	IncusAvailable     bool   `json:"incusAvailable"`
	BaseTemplateExists bool   `json:"baseTemplateExists"`
	IncusVersion       string `json:"incus_version,omitempty"`
	StorageBackend     string `json:"storage_backend,omitempty"`
	OverlayReady       bool   `json:"overlay_ready"`
	TemplateCount      int    `json:"template_count"`
	ContainerCount     int    `json:"container_count"`
	OrphanCount        int    `json:"orphan_count"`
}

// ContainerInfo represents a session container in the list.
type ContainerInfo struct {
	Name      string `json:"name"`
	SessionID string `json:"session_id"`
	Template  string `json:"template"`
	Status    string `json:"status"`
	Created   string `json:"created"`
}

// ContainerListResponse is the JSON response for GET /api/sandbox/containers.
type ContainerListResponse struct {
	Containers []ContainerInfo `json:"containers"`
	Orphans    []string        `json:"orphans,omitempty"`
}

// TemplateInfo represents a template in the list.
type TemplateInfo struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Created      string   `json:"created"`
	LastSnapshot string   `json:"last_snapshot,omitempty"`
	FleetPlans   []string `json:"fleet_plans,omitempty"`
}

// TemplateListResponse is the JSON response for GET /api/sandbox/templates.
type TemplateListResponse struct {
	Templates []TemplateInfo `json:"templates"`
}

// TemplateDetailResponse is the JSON response for GET /api/sandbox/templates/{name}.
type TemplateDetailResponse struct {
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Created         string   `json:"created"`
	LastSnapshot    string   `json:"last_snapshot,omitempty"`
	FleetPlans      []string `json:"fleet_plans,omitempty"`
	BasedOn         string   `json:"based_on,omitempty"`
	BinaryHash      string   `json:"binary_hash,omitempty"`
	ContainerName   string   `json:"container_name"`
	ContainerStatus string   `json:"container_status"`
	SnapshotReady   bool     `json:"snapshot_ready"`
}

// CreateTemplateRequest is the JSON body for POST /api/sandbox/templates.
type CreateTemplateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// --- Existing handlers ---

// SandboxStatusHandler handles GET /api/sandbox/status.
func SandboxStatusHandler(w http.ResponseWriter, r *http.Request) {
	platform, reason := sandbox.DetectPlatformReason()

	if platform == sandbox.PlatformDockerIncus {
		reason = "Docker+Incus setup is not yet implemented. Currently only Linux with native Incus is supported."
		platform = sandbox.PlatformUnsupported
	}

	appCfg, err := config.LoadAppConfig()
	sandboxEnabled := true
	if err == nil && appCfg != nil {
		sandboxEnabled = sandbox.IsSandboxEnabled(&appCfg.Sandbox)
	}

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
func SandboxInitHandler(w http.ResponseWriter, r *http.Request) {
	var req SandboxInitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	platform, reason := sandbox.DetectPlatformReason()
	if platform == sandbox.PlatformDockerIncus {
		platform = sandbox.PlatformUnsupported
	}
	if platform == sandbox.PlatformUnsupported {
		http.Error(w, fmt.Sprintf("sandbox unavailable: %s", reason), http.StatusServiceUnavailable)
		return
	}

	client, err := sandbox.Connect(platform)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to connect to Incus: %v", err), http.StatusInternalServerError)
		return
	}

	containerName := sandbox.TemplateName(sandbox.BaseTemplate)
	if client.InstanceExists(containerName) {
		http.Error(w, "base template already exists", http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	registry, err := sandbox.NewTemplateRegistry()
	if err != nil {
		SendSSE(w, flusher, "error", map[string]string{"error": fmt.Sprintf("failed to create template registry: %v", err)})
		return
	}

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

	if err := sandbox.InitBaseTemplate(client, registry, opts); err != nil {
		SendSSE(w, flusher, "error", map[string]string{"error": err.Error()})
		return
	}

	SendSSE(w, flusher, "done", map[string]string{"status": "success"})
}

// --- New handlers: Details, Containers, Templates ---

// SandboxDetailsHandler handles GET /api/sandbox/details.
// Returns extended sandbox status including Incus version, storage, counts.
func SandboxDetailsHandler(w http.ResponseWriter, r *http.Request) {
	platform, reason := sandbox.DetectPlatformReason()
	if platform == sandbox.PlatformDockerIncus {
		reason = "Docker+Incus setup is not yet implemented. Currently only Linux with native Incus is supported."
		platform = sandbox.PlatformUnsupported
	}

	appCfg, err := config.LoadAppConfig()
	sandboxEnabled := true
	if err == nil && appCfg != nil {
		sandboxEnabled = sandbox.IsSandboxEnabled(&appCfg.Sandbox)
	}

	resp := SandboxDetailResponse{
		Platform:       platformString(platform),
		Reason:         reason,
		SandboxEnabled: sandboxEnabled,
	}

	incusAvailable := platform == sandbox.PlatformLinuxNative
	resp.IncusAvailable = incusAvailable

	if incusAvailable {
		client, connErr := sandbox.Connect(platform)
		if connErr == nil {
			containerName := sandbox.TemplateName(sandbox.BaseTemplate)
			resp.BaseTemplateExists = client.InstanceExists(containerName)

			tplRegistry, _ := sandbox.NewTemplateRegistry()
			sessRegistry, _ := sandbox.NewSessionRegistry()
			if tplRegistry != nil && sessRegistry != nil {
				status, statusErr := sandbox.Status(client, tplRegistry, sessRegistry)
				if statusErr == nil {
					resp.IncusVersion = status.IncusVersion
					resp.StorageBackend = status.StorageBackend
					resp.OverlayReady = status.OverlayReady
					resp.TemplateCount = status.TemplateCount
					resp.ContainerCount = status.SessionCount
					resp.OrphanCount = status.OrphanCount
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// SandboxContainerListHandler handles GET /api/sandbox/containers.
// Lists all session containers and identifies orphans.
func SandboxContainerListHandler(w http.ResponseWriter, r *http.Request) {
	client, err := sandboxConnect()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	sessRegistry, err := sandbox.NewSessionRegistry()
	if err != nil {
		http.Error(w, "failed to load session registry: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Auto-reap stale entries
	sessRegistry.Reap(client)

	entries := sessRegistry.List()
	containers := make([]ContainerInfo, 0, len(entries))
	for _, e := range entries {
		status := "stopped"
		if !client.InstanceExists(e.ContainerName) {
			status = "missing"
		} else if client.IsRunning(e.ContainerName) {
			status = "running"
		}
		containers = append(containers, ContainerInfo{
			Name:      e.ContainerName,
			SessionID: e.SessionID,
			Template:  e.TemplateName,
			Status:    status,
			Created:   e.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}

	// Find orphans (containers in Incus not in registry)
	registeredNames := make(map[string]bool)
	for _, e := range entries {
		registeredNames[e.ContainerName] = true
	}
	incusContainers, _ := client.ListSessionContainers()
	var orphans []string
	for _, inst := range incusContainers {
		if !registeredNames[inst.Name] {
			orphans = append(orphans, inst.Name)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ContainerListResponse{
		Containers: containers,
		Orphans:    orphans,
	})
}

// SandboxContainerDeleteHandler handles DELETE /api/sandbox/containers/{id}.
// Destroys a session container by session ID or container name.
func SandboxContainerDeleteHandler(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if id == "" {
		http.Error(w, "missing container/session id", http.StatusBadRequest)
		return
	}

	client, err := sandboxConnect()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	sessRegistry, err := sandbox.NewSessionRegistry()
	if err != nil {
		http.Error(w, "failed to load session registry: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Resolve to session ID (accepts session ID, container name, or prefix)
	sessionID, found := sessRegistry.ResolveSessionID(id)
	if !found {
		http.Error(w, "container not found: "+id, http.StatusNotFound)
		return
	}

	if err := sandbox.DestroyForSession(client, sessRegistry, sessionID); err != nil {
		http.Error(w, "failed to destroy container: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}

// SandboxPruneHandler handles POST /api/sandbox/prune.
// Prunes orphaned containers whose sessions no longer exist.
func SandboxPruneHandler(w http.ResponseWriter, r *http.Request) {
	client, err := sandboxConnect()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	sessRegistry, err := sandbox.NewSessionRegistry()
	if err != nil {
		http.Error(w, "failed to load session registry: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Load existing session IDs from the session store
	existingSessionIDs := make(map[string]bool)
	appCfg, cfgErr := config.LoadAppConfig()
	if cfgErr == nil {
		if sessDir, dirErr := config.GetSessionsDir(&appCfg.Sessions); dirErr == nil {
			if store, fsErr := persistentsession.NewFileStore(sessDir); fsErr == nil {
				if indexData, loadErr := store.Index().Load(); loadErr == nil {
					for id := range indexData.Sessions {
						existingSessionIDs[id] = true
					}
				}
			}
		}
	}

	pruned, err := sandbox.PruneOrphans(client, sessRegistry, existingSessionIDs)
	if err != nil {
		http.Error(w, "prune failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"pruned": pruned})
}

// SandboxTemplateListHandler handles GET /api/sandbox/templates.
// Lists all registered templates.
func SandboxTemplateListHandler(w http.ResponseWriter, r *http.Request) {
	tplRegistry, err := sandbox.NewTemplateRegistry()
	if err != nil {
		http.Error(w, "failed to load template registry: "+err.Error(), http.StatusInternalServerError)
		return
	}

	entries := tplRegistry.List()
	templates := make([]TemplateInfo, 0, len(entries))
	for _, t := range entries {
		info := TemplateInfo{
			Name:        t.Name,
			Description: t.Description,
			Created:     t.CreatedAt.Format("2006-01-02 15:04:05"),
			FleetPlans:  t.FleetPlans,
		}
		if !t.SnapshotAt.IsZero() {
			info.LastSnapshot = t.SnapshotAt.Format("2006-01-02 15:04:05")
		}
		templates = append(templates, info)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TemplateListResponse{Templates: templates})
}

// SandboxTemplateInfoHandler handles GET /api/sandbox/templates/{name}.
// Returns detailed information about a single template.
func SandboxTemplateInfoHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if name == "" {
		http.Error(w, "missing template name", http.StatusBadRequest)
		return
	}

	tplRegistry, err := sandbox.NewTemplateRegistry()
	if err != nil {
		http.Error(w, "failed to load template registry: "+err.Error(), http.StatusInternalServerError)
		return
	}

	meta := tplRegistry.Get(name)
	if meta == nil {
		http.Error(w, "template not found: "+name, http.StatusNotFound)
		return
	}

	resp := TemplateDetailResponse{
		Name:        meta.Name,
		Description: meta.Description,
		Created:     meta.CreatedAt.Format("2006-01-02 15:04:05"),
		FleetPlans:  meta.FleetPlans,
		BasedOn:     meta.BasedOn,
	}
	if !meta.SnapshotAt.IsZero() {
		resp.LastSnapshot = meta.SnapshotAt.Format("2006-01-02 15:04:05")
	}
	if meta.BinaryHash != "" && len(meta.BinaryHash) > 16 {
		resp.BinaryHash = meta.BinaryHash[:16] + "..."
	} else {
		resp.BinaryHash = meta.BinaryHash
	}

	// Get live container state from Incus
	containerName := sandbox.TemplateName(name)
	resp.ContainerName = containerName
	resp.ContainerStatus = "missing"

	client, connErr := sandboxConnect()
	if connErr == nil {
		if client.InstanceExists(containerName) {
			inst, instErr := client.GetInstance(containerName)
			if instErr == nil {
				resp.ContainerStatus = inst.Status
			} else {
				resp.ContainerStatus = "unknown"
			}
			resp.SnapshotReady = client.HasSnapshot(containerName, "snap0")
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// SandboxTemplateCreateHandler handles POST /api/sandbox/templates.
// Creates a new template from @base.
func SandboxTemplateCreateHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "template name is required", http.StatusBadRequest)
		return
	}
	if req.Name == "base" {
		http.Error(w, "cannot create a template named 'base'", http.StatusBadRequest)
		return
	}

	client, err := sandboxConnect()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	tplRegistry, err := sandbox.NewTemplateRegistry()
	if err != nil {
		http.Error(w, "failed to load template registry: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := sandbox.CreateTemplate(client, tplRegistry, req.Name, req.Description); err != nil {
		http.Error(w, "failed to create template: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok", "name": req.Name})
}

// SandboxTemplateDeleteHandler handles DELETE /api/sandbox/templates/{name}.
// Deletes a template (cannot delete @base).
func SandboxTemplateDeleteHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if name == "" {
		http.Error(w, "missing template name", http.StatusBadRequest)
		return
	}
	if name == "base" {
		http.Error(w, "cannot delete the base template", http.StatusBadRequest)
		return
	}

	client, err := sandboxConnect()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	tplRegistry, err := sandbox.NewTemplateRegistry()
	if err != nil {
		http.Error(w, "failed to load template registry: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := sandbox.DeleteTemplate(client, tplRegistry, name); err != nil {
		http.Error(w, "failed to delete template: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}

// SandboxTemplateSnapshotHandler handles POST /api/sandbox/templates/{name}/snapshot.
// Snapshots a template, freezing its state for cloning into session containers.
func SandboxTemplateSnapshotHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if name == "" {
		http.Error(w, "missing template name", http.StatusBadRequest)
		return
	}

	client, err := sandboxConnect()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	tplRegistry, err := sandbox.NewTemplateRegistry()
	if err != nil {
		http.Error(w, "failed to load template registry: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := sandbox.SnapshotTemplate(client, tplRegistry, name); err != nil {
		http.Error(w, "failed to snapshot template: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}

// SandboxTemplatePromoteHandler handles POST /api/sandbox/templates/{name}/promote.
// Promotes a template to replace @base.
func SandboxTemplatePromoteHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if name == "" {
		http.Error(w, "missing template name", http.StatusBadRequest)
		return
	}
	if name == "base" {
		http.Error(w, "cannot promote @base to itself", http.StatusBadRequest)
		return
	}

	client, err := sandboxConnect()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	tplRegistry, err := sandbox.NewTemplateRegistry()
	if err != nil {
		http.Error(w, "failed to load template registry: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := sandbox.PromoteTemplate(client, tplRegistry, name); err != nil {
		http.Error(w, "failed to promote template: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}

// SandboxRefreshHandler handles POST /api/sandbox/refresh.
// Refreshes all templates with the current astonish binary.
func SandboxRefreshHandler(w http.ResponseWriter, r *http.Request) {
	client, err := sandboxConnect()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	tplRegistry, err := sandbox.NewTemplateRegistry()
	if err != nil {
		http.Error(w, "failed to load template registry: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := sandbox.RefreshAll(client, tplRegistry); err != nil {
		http.Error(w, "failed to refresh templates: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}

// --- Helpers ---

// sandboxConnect detects the platform and connects to Incus.
func sandboxConnect() (*sandbox.IncusClient, error) {
	platform, reason := sandbox.DetectPlatformReason()
	if platform == sandbox.PlatformDockerIncus {
		platform = sandbox.PlatformUnsupported
	}
	if platform == sandbox.PlatformUnsupported {
		return nil, fmt.Errorf("sandbox unavailable: %s", reason)
	}
	client, err := sandbox.Connect(platform)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Incus: %w", err)
	}
	return client, nil
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
