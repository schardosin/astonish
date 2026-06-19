package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/sandbox/openshell"
	"github.com/schardosin/astonish/pkg/store/entstore"
)

// PlatformAdminDeleteOrphanSandboxesHandler handles DELETE /api/platform/admin/sandbox/orphans.
// It lists all sandboxes from the OpenShell gateway, compares against the DB
// session registry, and deletes any untracked sandbox older than 10 minutes.
//
// Response: {"deleted_count": N, "deleted_names": ["astn-sess-abc", ...]}
func PlatformAdminDeleteOrphanSandboxesHandler(w http.ResponseWriter, r *http.Request) {
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to load app config: "+err.Error())
		return
	}
	if appCfg.Sandbox.BackendKind() != "openshell" {
		respondError(w, http.StatusBadRequest, "Orphan cleanup only applies to the OpenShell sandbox backend")
		return
	}

	backend := getPlatformBackend()
	if backend == nil {
		respondError(w, http.StatusServiceUnavailable, "Platform backend not available")
		return
	}

	entBackend, ok := backend.(*entstore.Store)
	if !ok {
		respondError(w, http.StatusInternalServerError, "Platform backend does not support AllSandboxSessionIDs")
		return
	}

	// Build gateway client
	osCfg := appCfg.Sandbox.OpenShell
	gateway, gwErr := openshell.NewGatewayClientFromConfig(openshell.GRPCClientConfig{
		Addr:           osCfg.GatewayAddr,
		TLS:            osCfg.OpenShellGatewayTLS(),
		ClientCertPath: osCfg.ClientCertPath,
		ClientKeyPath:  osCfg.ClientKeyPath,
		CACertPath:     osCfg.CACertPath,
		AuthToken:      osCfg.AuthToken,
	})
	if gwErr != nil {
		respondError(w, http.StatusServiceUnavailable, "Failed to connect to OpenShell gateway: "+gwErr.Error())
		return
	}
	defer gateway.Close()

	// Prune stale sandbox_sessions records whose chat session no longer exists.
	// Without this, AllSandboxSessionIDs returns stale IDs and the reconciler
	// would think orphan pods are still active.
	if pruned, err := entBackend.PruneStaleSandboxSessions(r.Context()); err != nil {
		slog.Warn("admin orphan cleanup: failed to prune stale sandbox sessions", "error", err)
	} else if pruned > 0 {
		slog.Info("admin orphan cleanup: pruned stale sandbox_sessions records", "count", pruned)
	}

	// Get all known session IDs from all teams
	knownIDs, idsErr := entBackend.AllSandboxSessionIDs(r.Context())
	if idsErr != nil {
		respondError(w, http.StatusInternalServerError, "Failed to query session IDs: "+idsErr.Error())
		return
	}

	// Reconcile
	deleted, err := openshell.ReconcileOrphans(r.Context(), gateway, knownIDs, 10*time.Minute)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Reconciliation failed: "+err.Error())
		return
	}

	slog.Info("admin: orphan sandbox cleanup complete",
		"deleted_count", len(deleted), "deleted_names", deleted)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"deleted_count": len(deleted),
		"deleted_names": deleted,
	})
}

// PlatformAdminListOrphanSandboxesHandler handles GET /api/platform/admin/sandbox/orphans.
// It lists all sandboxes from the OpenShell gateway that are NOT tracked in the
// DB session registry, without deleting them. Useful for previewing what the
// DELETE endpoint would clean up.
//
// Response: {"orphans": [{"name": "...", "phase": "...", "age_seconds": N}], "total_gateway": N, "total_tracked": N}
func PlatformAdminListOrphanSandboxesHandler(w http.ResponseWriter, r *http.Request) {
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to load app config: "+err.Error())
		return
	}
	if appCfg.Sandbox.BackendKind() != "openshell" {
		respondError(w, http.StatusBadRequest, "Orphan listing only applies to the OpenShell sandbox backend")
		return
	}

	backend := getPlatformBackend()
	if backend == nil {
		respondError(w, http.StatusServiceUnavailable, "Platform backend not available")
		return
	}

	entBackend, ok := backend.(*entstore.Store)
	if !ok {
		respondError(w, http.StatusInternalServerError, "Platform backend does not support AllSandboxSessionIDs")
		return
	}

	// Build gateway client
	osCfg := appCfg.Sandbox.OpenShell
	gateway, gwErr := openshell.NewGatewayClientFromConfig(openshell.GRPCClientConfig{
		Addr:           osCfg.GatewayAddr,
		TLS:            osCfg.OpenShellGatewayTLS(),
		ClientCertPath: osCfg.ClientCertPath,
		ClientKeyPath:  osCfg.ClientKeyPath,
		CACertPath:     osCfg.CACertPath,
		AuthToken:      osCfg.AuthToken,
	})
	if gwErr != nil {
		respondError(w, http.StatusServiceUnavailable, "Failed to connect to OpenShell gateway: "+gwErr.Error())
		return
	}
	defer gateway.Close()

	// List all sandboxes from the gateway
	sandboxes, listErr := gateway.ListSandboxes(r.Context(), "")
	if listErr != nil {
		respondError(w, http.StatusInternalServerError, "Failed to list sandboxes: "+listErr.Error())
		return
	}

	// Prune stale sandbox_sessions records before querying known IDs.
	if pruned, err := entBackend.PruneStaleSandboxSessions(r.Context()); err != nil {
		slog.Warn("admin orphan list: failed to prune stale sandbox sessions", "error", err)
	} else if pruned > 0 {
		slog.Info("admin orphan list: pruned stale sandbox_sessions records", "count", pruned)
	}

	// Get all known session IDs from all teams
	knownIDs, idsErr := entBackend.AllSandboxSessionIDs(r.Context())
	if idsErr != nil {
		respondError(w, http.StatusInternalServerError, "Failed to query session IDs: "+idsErr.Error())
		return
	}

	// Build the orphan list
	now := time.Now()
	type orphanInfo struct {
		Name       string `json:"name"`
		Phase      string `json:"phase"`
		AgeSeconds int    `json:"age_seconds"`
	}
	var orphans []orphanInfo
	for _, sb := range sandboxes {
		// Check by session-id label (same logic as ReconcileOrphans)
		sessionID := sb.Labels["astonish.io/session-id"]
		if sessionID != "" && knownIDs[sessionID] {
			continue
		}
		// Fallback: check by name
		if knownIDs[sb.Name] {
			continue
		}
		age := 0
		if !sb.CreatedAt.IsZero() {
			age = int(now.Sub(sb.CreatedAt).Seconds())
		}
		orphans = append(orphans, orphanInfo{
			Name:       sb.Name,
			Phase:      sb.Phase,
			AgeSeconds: age,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"orphans":       orphans,
		"orphan_count":  len(orphans),
		"total_gateway": len(sandboxes),
		"total_tracked": len(knownIDs),
	})
}
