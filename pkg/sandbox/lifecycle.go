package sandbox

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/schardosin/astonish/pkg/config"
)

// templateSnapshotMu protects template snapshots from being deleted while
// session containers are being created. Session creation takes a read lock
// (multiple sessions can be created concurrently); template refresh takes a
// write lock (exclusive — blocks session creation while the snapshot is being
// recreated). Without this, RefreshAllIfNeeded can delete the snapshot that
// a concurrent CreateOverlayContainer is using as its overlay lowerdir,
// causing "Failed to exec /sbin/init" errors.
var templateSnapshotMu sync.RWMutex

// EnsureSessionContainer creates or retrieves a session container.
// If the session already has a container, it ensures it's running.
// If not, it creates an overlay-based container from the specified template.
//
// Instead of cloning the full template filesystem (10-30s on dir backend),
// this creates a lightweight container from a tiny shell image (~45ms) and
// mounts an overlayfs backed by the template's lower layers (~4ms). Total: ~200ms.
// For custom templates, the overlay is stacked: template-upper:@base-snapshot.
func EnsureSessionContainer(client *IncusClient, sessRegistry *SessionRegistry, tplRegistry *TemplateRegistry, sessionID, templateName string, limits *config.SandboxLimits) (string, error) {
	// Check registry — already exists?
	if entry := sessRegistry.Get(sessionID); entry != nil {
		containerName := entry.ContainerName

		if client.IsRunning(containerName) {
			return containerName, nil
		}

		// Exists but not running — try to re-mount overlay and start it
		if client.InstanceExists(containerName) {
			// Acquire read lock to prevent racing with RemountDependentOverlays.
			// During a template refresh, RemountDependentOverlays holds the write
			// lock while it stops containers, unmounts stale overlays, remounts
			// with fresh inodes, and restarts. Without this read lock, we can
			// start the container mid-remount with a broken/stale rootfs, causing
			// the node process to crash with EOF on startup.
			templateSnapshotMu.RLock()

			// Re-mount overlay if needed (might have been lost on reboot).
			// This MUST succeed — without it the container starts with empty rootfs.
			if err := ensureOverlayMounted(client, containerName, entry.TemplateName, tplRegistry); err != nil {
				templateSnapshotMu.RUnlock()
				return "", fmt.Errorf("failed to re-mount overlay for %q: %w (try 'astonish sandbox prune' and retry)", containerName, err)
			}

			err := client.StartInstance(containerName)
			templateSnapshotMu.RUnlock()
			if err != nil {
				return "", fmt.Errorf("failed to start existing session container %q: %w", containerName, err)
			}
			return containerName, nil
		}

		// Container was registered but no longer exists — clean up and recreate
		sessRegistry.Remove(sessionID)
	}

	// Resolve template
	if templateName == "" {
		templateName = BaseTemplate
	}

	// Verify template exists (either as an Incus container or in the registry)
	tplContainerName := TemplateName(templateName)
	if !client.InstanceExists(tplContainerName) {
		return "", fmt.Errorf("template %q does not exist; run 'astonish sandbox init' first", templateName)
	}

	// For @base, verify snapshot exists (it's the root of the overlay chain)
	if templateName == BaseTemplate && !client.HasSnapshot(tplContainerName, SnapshotName) {
		return "", fmt.Errorf("template %q has no snapshot; run 'astonish sandbox init' first", templateName)
	}

	// Create overlay-based session container
	containerName := SessionContainerName(sessionID)

	// Guard against name collision (unlikely with 8-char prefix, but safe)
	if client.InstanceExists(containerName) {
		// Also clean the registry entry for whatever session owned this container
		for _, entry := range sessRegistry.List() {
			if entry.ContainerName == containerName && entry.SessionID != sessionID {
				if err := sessRegistry.Remove(entry.SessionID); err != nil {
					slog.Warn("failed to remove stale registry entry", "component", "sandbox", "session", entry.SessionID, "error", err)
				}
				break
			}
		}
		if err := destroyOverlayContainer(client, containerName); err != nil {
			return "", fmt.Errorf("failed to clean up existing container %q: %w", containerName, err)
		}
	}

	// Hold a read lock on the template snapshot while creating and starting
	// the container. This prevents RefreshTemplate from deleting the snapshot
	// (our overlay lowerdir) out from under us.
	templateSnapshotMu.RLock()
	defer templateSnapshotMu.RUnlock()

	// Create the container with overlayfs (tiny image + overlay mount)
	if err := CreateOverlayContainer(client, containerName, templateName, tplRegistry, limits); err != nil {
		return "", fmt.Errorf("failed to create session container: %w", err)
	}

	// Start
	if err := client.StartInstance(containerName); err != nil {
		// Read the LXC log for the actual forkstart error — the Incus API
		// error only says "exit status 1" without the underlying cause.
		lxcLog := readLXCLog(containerName)

		// destroyOverlayContainer unmounts the overlay, removes overlay dirs,
		// and deletes the Incus instance. If it fails (e.g., container in
		// ABORTING state), fall back to a direct delete to avoid zombies.
		if destroyErr := destroyOverlayContainer(client, containerName); destroyErr != nil {
			if delErr := client.DeleteInstance(containerName); delErr != nil {
				slog.Warn("fallback delete also failed after start failure", "component", "sandbox", "container", containerName, "error", delErr)
			}
		}
		if lxcLog != "" {
			return "", fmt.Errorf("failed to start session container: %w\n\nLXC log:\n%s", err, lxcLog)
		}
		return "", fmt.Errorf("failed to start session container: %w", err)
	}

	// Health check — verify the overlay is actually providing the rootfs.
	// Without this, a failed overlay mount results in a container that runs
	// but has an empty/wrong filesystem (just the tiny shell image).
	if err := verifyContainerHealth(client, containerName); err != nil {
		if destroyErr := destroyOverlayContainer(client, containerName); destroyErr != nil {
			if delErr := client.DeleteInstance(containerName); delErr != nil {
				slog.Warn("fallback delete also failed after health check failure", "component", "sandbox", "container", containerName, "error", delErr)
			}
		}
		return "", fmt.Errorf("session container health check failed: %w", err)
	}

	// Register
	if err := sessRegistry.Put(sessionID, containerName, templateName); err != nil {
		// Registry write failed — the container works but won't be tracked.
		// Destroy it so we don't leak an untracked container.
		if destroyErr := destroyOverlayContainer(client, containerName); destroyErr != nil {
			if delErr := client.DeleteInstance(containerName); delErr != nil {
				slog.Warn("fallback delete also failed after registry write failure", "component", "sandbox", "container", containerName, "error", delErr)
			}
		}
		return "", fmt.Errorf("failed to register session container: %w", err)
	}

	return containerName, nil
}

// DestroyForSession stops and deletes the container for a session,
// including unmounting any overlayfs.
func DestroyForSession(client *IncusClient, registry *SessionRegistry, sessionID string) error {
	entry := registry.Get(sessionID)
	if entry == nil {
		return nil // no container for this session
	}

	containerName := entry.ContainerName

	if client.InstanceExists(containerName) {
		if err := destroyOverlayContainer(client, containerName); err != nil {
			return fmt.Errorf("failed to destroy session container %q: %w", containerName, err)
		}
	}

	return registry.Remove(sessionID)
}

// destroyOverlayContainer unmounts overlay, stops, and deletes a container.
func destroyOverlayContainer(client *IncusClient, containerName string) error {
	// Stop the container first (overlay can't be unmounted while in use)
	state, _, err := client.server.GetInstanceState(containerName)
	if err == nil && state.Status == "Running" {
		if err := client.StopInstance(containerName, true); err != nil {
			return err
		}
	}

	// Unmount overlay
	poolName, err := GetPoolForProfile(client)
	if err == nil {
		poolPath, err := GetPoolSourcePath(client, poolName)
		if err == nil {
			UnmountSessionOverlay(poolPath, containerName)
		}
	}

	// Delete the container
	return client.DeleteInstance(containerName)
}

// verifyContainerHealth checks that a running container has a working rootfs
// by verifying /bin/sh exists (present in all real rootfs, absent in the tiny
// overlay shell image). This catches the case where the overlay mount failed
// silently and the container is running with an empty filesystem.
func verifyContainerHealth(client *IncusClient, containerName string) error {
	exitCode, err := client.ExecSimple(containerName, []string{"test", "-x", "/bin/sh"})
	if err != nil {
		return fmt.Errorf("cannot execute health check in %q: %w", containerName, err)
	}
	if exitCode != 0 {
		return fmt.Errorf("container %q appears to have an empty rootfs (overlay may not be mounted)", containerName)
	}
	return nil
}

// ensureOverlayMounted re-mounts the overlay if it's not currently mounted.
// This handles the case where the system was rebooted — the container exists
// in Incus's database but the overlay mount was lost.
func ensureOverlayMounted(client *IncusClient, containerName, templateName string, tplRegistry *TemplateRegistry) error {
	poolName, err := GetPoolForProfile(client)
	if err != nil {
		return err
	}

	poolPath, err := GetPoolSourcePath(client, poolName)
	if err != nil {
		return err
	}

	if IsOverlayMounted(poolPath, containerName) {
		return nil // already mounted
	}

	// Determine the correct lower layers to resolve.
	// If the container IS the template itself (e.g., ShellIntoTemplate or
	// RefreshTemplate), the lower layers come from the template's parent —
	// the template's own upper dir is the overlay upperdir, not a lowerdir.
	// Without this distinction, the same path ends up in both lowerdir and
	// upperdir, causing ELOOP ("Too many levels of symbolic links").
	resolveTemplate := templateName
	if containerName == TemplateName(templateName) && tplRegistry != nil {
		if meta := tplRegistry.Get(templateName); meta != nil && meta.BasedOn != "" {
			resolveTemplate = meta.BasedOn
		}
	}

	lowerDir, err := ResolveLowerLayers(poolPath, resolveTemplate, tplRegistry)
	if err != nil {
		return err
	}

	return MountOverlay(poolPath, containerName, lowerDir)
}

// TryDestroySessionContainer is a best-effort helper that destroys the sandbox
// container for a session. It connects to Incus, looks up the container, and
// tears it down. Errors are silently ignored — this is designed to be called
// from session deletion paths where sandbox may or may not be active.
func TryDestroySessionContainer(sessionID string) {
	platform := DetectPlatform()
	if platform == PlatformUnsupported {
		return
	}

	SetActivePlatform(platform)

	client, err := Connect(platform)
	if err != nil {
		return
	}

	registry, err := NewSessionRegistry()
	if err != nil {
		return
	}

	// Try the registry-based path first (finds container by session ID lookup)
	if entry := registry.Get(sessionID); entry != nil {
		if err := DestroyForSession(client, registry, sessionID); err != nil {
			slog.Warn("failed to destroy session container via registry", "component", "sandbox", "session", sessionID, "error", err)
		}
		return
	}

	// Registry has no entry — the entry may have been cleaned already (e.g.,
	// by LazyNodeClient.Cleanup or fleet session stop) but the container might
	// still exist if Incus was down when destruction was attempted.
	// Try to destroy by the derived container name directly.
	containerName := SessionContainerName(sessionID)
	if client.InstanceExists(containerName) {
		if err := destroyOverlayContainer(client, containerName); err != nil {
			slog.Warn("failed to destroy orphan container", "component", "sandbox", "container", containerName, "error", err)
		}
	}
}

// PruneOrphans finds and destroys containers whose sessions no longer exist.
// existingSessionIDs is the set of session IDs that are still valid.
// Returns the number of containers pruned.
func PruneOrphans(client *IncusClient, registry *SessionRegistry, existingSessionIDs map[string]bool) (int, error) {
	entries := registry.List()
	pruned := 0

	for _, entry := range entries {
		if existingSessionIDs[entry.SessionID] {
			continue // session still exists
		}

		if entry.Pinned {
			continue // manually created, exempt from cleanup
		}

		// Orphaned — destroy
		fmt.Printf("Pruning orphaned container %q (session %s)...\n", entry.ContainerName, entry.SessionID[:8])

		if client.InstanceExists(entry.ContainerName) {
			if err := destroyOverlayContainer(client, entry.ContainerName); err != nil {
				slog.Warn("failed to destroy container", "container", entry.ContainerName, "error", err)
				continue
			}
		}

		if err := registry.Remove(entry.SessionID); err != nil {
			slog.Warn("failed to remove registry entry", "error", err)
			continue
		}

		pruned++
	}

	// Also check for containers not in the registry (e.g., crashed before registration)
	sessionContainers, err := client.ListSessionContainers()
	if err != nil {
		return pruned, fmt.Errorf("failed to list session containers: %w", err)
	}

	registeredContainers := make(map[string]bool)
	for _, e := range entries {
		registeredContainers[e.ContainerName] = true
	}

	for _, inst := range sessionContainers {
		if registeredContainers[inst.Name] {
			continue // registered, handled above
		}

		// Unregistered container — check age (only prune if older than 1 hour)
		if time.Since(inst.CreatedAt) < time.Hour {
			continue // recently created, might still be registering
		}

		fmt.Printf("Pruning unregistered container %q (created %s ago)...\n", inst.Name, time.Since(inst.CreatedAt).Round(time.Minute))
		if err := destroyOverlayContainer(client, inst.Name); err != nil {
			slog.Warn("failed to destroy container", "container", inst.Name, "error", err)
			continue
		}

		pruned++
	}

	return pruned, nil
}

// PruneStaleOnStartup removes session containers left over from previous daemon
// runs whose sessions no longer exist in the session store. Containers that
// belong to live sessions are preserved — the session persists across daemon
// restarts and will reconnect to its container on the next tool call.
//
// existingSessionIDs is the set of session IDs that still exist in the session
// store. Containers belonging to these sessions are never destroyed.
//
// Returns the number of containers cleaned up.
func PruneStaleOnStartup(client *IncusClient, registry *SessionRegistry, existingSessionIDs map[string]bool) int {
	// 1. Clean registry entries pointing to non-existent containers
	pruned := registry.Reap(client)

	// 2. Build a set of container names that belong to live sessions or are pinned
	liveContainers := make(map[string]bool)
	for _, entry := range registry.List() {
		if existingSessionIDs[entry.SessionID] || entry.Pinned {
			liveContainers[entry.ContainerName] = true
		}
	}

	// 3. Destroy stopped session containers that don't belong to any live session
	sessionContainers, err := client.ListSessionContainers()
	if err != nil {
		return pruned
	}

	for _, inst := range sessionContainers {
		if inst.Status == "Running" {
			continue // leave running containers alone
		}

		if liveContainers[inst.Name] {
			continue // belongs to a live session, preserve it
		}

		// Direct name-matching fallback: the registry may have lost the
		// entry (e.g., Cleanup removed it but container destruction failed).
		// Derive the session ID from the container name and check the store.
		if matchedID := matchContainerToSession(inst.Name, existingSessionIDs); matchedID != "" {
			// Self-heal: re-register so future lookups work.
			if err := registry.Put(matchedID, inst.Name, BaseTemplate); err != nil {
				slog.Warn("failed to self-heal registry entry", "component", "sandbox", "session", matchedID, "container", inst.Name, "error", err)
			}
			continue
		}

		// Stopped + no live session + no name match → orphan, destroy it
		if err := destroyOverlayContainer(client, inst.Name); err != nil {
			continue
		}

		// Also clean registry entry if one exists
		for _, entry := range registry.List() {
			if entry.ContainerName == inst.Name {
				if err := registry.Remove(entry.SessionID); err != nil {
					slog.Warn("failed to remove registry entry for pruned container", "component", "sandbox", "container", inst.Name, "error", err)
				}
				break
			}
		}

		pruned++
	}

	return pruned
}

// matchContainerToSession tries to find a session ID in existingSessionIDs
// that matches an astn-sess-* container name by its 8-char prefix.
// Fleet containers (astn-fleet-*) use a different naming scheme that can't
// be reversed to a session ID, so they return empty.
func matchContainerToSession(containerName string, existingSessionIDs map[string]bool) string {
	if !strings.HasPrefix(containerName, SessionPrefix) {
		return ""
	}

	prefix := strings.TrimPrefix(containerName, SessionPrefix)
	if prefix == "" {
		return ""
	}

	for sessionID := range existingSessionIDs {
		if strings.HasPrefix(sessionID, prefix) {
			return sessionID
		}
	}

	return ""
}

// readLXCLog reads the LXC log file for a container after a forkstart failure.
// The log contains the actual error (e.g., missing rootfs, cgroup issue, etc.)
// that the Incus API error message doesn't include. Returns empty string on
// any read failure — this is best-effort diagnostics.
func readLXCLog(containerName string) string {
	logPath := fmt.Sprintf("/var/log/incus/%s/lxc.log", containerName)
	data, err := readFileOnSandboxHost(logPath)
	if err != nil {
		slog.Error("could not read lxc log", "component", "sandbox", "path", logPath, "error", err)
		return ""
	}
	content := strings.TrimSpace(string(data))
	// Truncate if excessively large — only the tail matters for diagnostics
	if len(content) > 4096 {
		content = "... (truncated)\n" + content[len(content)-4096:]
	}
	return content
}
