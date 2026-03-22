package sandbox

import (
	"fmt"
	"time"
)

// EnsureSessionContainer creates or retrieves a session container.
// If the session already has a container, it ensures it's running.
// If not, it clones from the specified template (default: @base).
func EnsureSessionContainer(client *IncusClient, registry *SessionRegistry, sessionID, templateName string) (string, error) {
	// Check registry — already exists?
	if entry := registry.Get(sessionID); entry != nil {
		containerName := entry.ContainerName

		if client.IsRunning(containerName) {
			return containerName, nil
		}

		// Exists but not running — try to start it
		if client.InstanceExists(containerName) {
			if err := client.StartInstance(containerName); err != nil {
				return "", fmt.Errorf("failed to start existing session container %q: %w", containerName, err)
			}
			return containerName, nil
		}

		// Container was registered but no longer exists — clean up and recreate
		registry.Remove(sessionID)
	}

	// Resolve template
	if templateName == "" {
		templateName = BaseTemplate
	}

	// Verify template has a snapshot
	tplContainerName := TemplateName(templateName)
	if !client.InstanceExists(tplContainerName) {
		return "", fmt.Errorf("template %q does not exist; run 'astonish sandbox init' first", templateName)
	}

	if !client.HasSnapshot(tplContainerName, SnapshotName) {
		return "", fmt.Errorf("template %q has no snapshot; run 'astonish sandbox template snapshot %s' first", templateName, templateName)
	}

	// Clone from template snapshot
	containerName := SessionContainerName(sessionID)

	// Guard against name collision (unlikely with 8-char prefix, but safe)
	if client.InstanceExists(containerName) {
		// Probably orphaned from a previous session with same ID prefix.
		// Destroy it and recreate.
		if err := client.StopAndDeleteInstance(containerName); err != nil {
			return "", fmt.Errorf("failed to clean up existing container %q: %w", containerName, err)
		}
	}

	if err := client.CreateContainerFromSnapshot(containerName, templateName, nil); err != nil {
		return "", fmt.Errorf("failed to create session container: %w", err)
	}

	// Start
	if err := client.StartInstance(containerName); err != nil {
		client.DeleteInstance(containerName)
		return "", fmt.Errorf("failed to start session container: %w", err)
	}

	// Register
	if err := registry.Put(sessionID, containerName, templateName); err != nil {
		// Non-fatal: container works, metadata just won't survive restart
		fmt.Printf("Warning: failed to register session container: %v\n", err)
	}

	return containerName, nil
}

// DestroyForSession stops and deletes the container for a session.
func DestroyForSession(client *IncusClient, registry *SessionRegistry, sessionID string) error {
	entry := registry.Get(sessionID)
	if entry == nil {
		return nil // no container for this session
	}

	containerName := entry.ContainerName

	if client.InstanceExists(containerName) {
		if err := client.StopAndDeleteInstance(containerName); err != nil {
			return fmt.Errorf("failed to destroy session container %q: %w", containerName, err)
		}
	}

	return registry.Remove(sessionID)
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
			if err := client.StopAndDeleteInstance(entry.ContainerName); err != nil {
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
		if err := client.StopAndDeleteInstance(inst.Name); err != nil {
			fmt.Printf("  Warning: failed to destroy %q: %v\n", inst.Name, err)
			continue
		}

		pruned++
	}

	return pruned, nil
}
