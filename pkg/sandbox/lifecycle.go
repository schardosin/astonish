package sandbox

import (
	"fmt"
	"log"
	"time"
)

// EnsureSessionContainer creates or retrieves a session container.
// If the session already has a container, it ensures it's running.
// If not, it creates an overlay-based container from the specified template.
//
// Instead of cloning the full template filesystem (10-30s on dir backend),
// this creates a lightweight container from a tiny shell image (~45ms) and
// mounts an overlayfs backed by the template's lower layers (~4ms). Total: ~200ms.
// For custom templates, the overlay is stacked: template-upper:@base-snapshot.
func EnsureSessionContainer(client *IncusClient, sessRegistry *SessionRegistry, tplRegistry *TemplateRegistry, sessionID, templateName string) (string, error) {
	// Check registry — already exists?
	if entry := sessRegistry.Get(sessionID); entry != nil {
		containerName := entry.ContainerName

		if client.IsRunning(containerName) {
			return containerName, nil
		}

		// Exists but not running — try to re-mount overlay and start it
		if client.InstanceExists(containerName) {
			// Re-mount overlay if needed (might have been lost on reboot)
			if err := ensureOverlayMounted(client, containerName, entry.TemplateName, tplRegistry); err != nil {
				log.Printf("[sandbox] Warning: failed to re-mount overlay for %q: %v", containerName, err)
			}

			if err := client.StartInstance(containerName); err != nil {
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
		if err := destroyOverlayContainer(client, containerName); err != nil {
			return "", fmt.Errorf("failed to clean up existing container %q: %w", containerName, err)
		}
	}

	// Create the container with overlayfs (tiny image + overlay mount)
	if err := CreateOverlayContainer(client, containerName, templateName, tplRegistry); err != nil {
		return "", fmt.Errorf("failed to create session container: %w", err)
	}

	// Start
	if err := client.StartInstance(containerName); err != nil {
		destroyOverlayContainer(client, containerName)
		return "", fmt.Errorf("failed to start session container: %w", err)
	}

	// Register
	if err := sessRegistry.Put(sessionID, containerName, templateName); err != nil {
		// Non-fatal: container works, metadata just won't survive restart
		fmt.Printf("Warning: failed to register session container: %v\n", err)
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

	// Resolve lower layers (supports stacked overlays for custom templates)
	lowerDir, err := ResolveLowerLayers(poolPath, templateName, tplRegistry)
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

	client, err := Connect(platform)
	if err != nil {
		return
	}

	registry, err := NewSessionRegistry()
	if err != nil {
		return
	}

	_ = DestroyForSession(client, registry, sessionID)
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

		// Orphaned — destroy
		fmt.Printf("Pruning orphaned container %q (session %s)...\n", entry.ContainerName, entry.SessionID[:8])

		if client.InstanceExists(entry.ContainerName) {
			if err := destroyOverlayContainer(client, entry.ContainerName); err != nil {
				fmt.Printf("  Warning: failed to destroy %q: %v\n", entry.ContainerName, err)
				continue
			}
		}

		if err := registry.Remove(entry.SessionID); err != nil {
			fmt.Printf("  Warning: failed to remove registry entry: %v\n", err)
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
			fmt.Printf("  Warning: failed to destroy %q: %v\n", inst.Name, err)
			continue
		}

		pruned++
	}

	return pruned, nil
}
