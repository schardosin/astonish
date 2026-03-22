package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/lxc/incus/v6/shared/api"
)

// DefaultBaseImage is the default image used for the @base template.
const DefaultBaseImage = "ubuntu/24.04"

// CoreTools are the tools installed in the @base template during setup.
var CoreTools = []string{
	"git",
	"curl",
	"wget",
	"jq",
	"unzip",
	"build-essential",
}

// CoreToolInstallCommands returns the commands to install core tools in a template.
// These run inside the container after creation.
func CoreToolInstallCommands() [][]string {
	return [][]string{
		{"apt-get", "update"},
		{"apt-get", "install", "-y",
			"git", "curl", "wget", "jq", "unzip", "build-essential",
			"python3", "python3-pip", "python3-venv",
			"ca-certificates", "gnupg",
		},
		// Install Node.js via NodeSource
		{"sh", "-c", "curl -fsSL https://deb.nodesource.com/setup_22.x | bash -"},
		{"apt-get", "install", "-y", "nodejs"},
		// Install uv (Python package manager) — provides uvx
		{"sh", "-c", "curl -LsSf https://astral.sh/uv/install.sh | sh"},
		// Clean up apt cache
		{"apt-get", "clean"},
		{"rm", "-rf", "/var/lib/apt/lists/*"},
	}
}

// InitBaseTemplate creates the @base template container from a fresh image
// and installs core tools. This is run during `astonish sandbox init`.
func InitBaseTemplate(client *IncusClient, registry *TemplateRegistry) error {
	containerName := TemplateName(BaseTemplate)

	// Check if already exists
	if client.InstanceExists(containerName) {
		return fmt.Errorf("base template %q already exists; use 'astonish sandbox refresh' to re-snapshot", containerName)
	}

	// Ensure the Incus environment is properly configured (network, profile)
	if err := ensureIncusEnvironment(client); err != nil {
		return fmt.Errorf("failed to set up Incus environment: %w", err)
	}

	fmt.Printf("Creating base template from %s...\n", DefaultBaseImage)

	// Launch from image
	if err := client.LaunchFromImage(containerName, DefaultBaseImage, nil); err != nil {
		return fmt.Errorf("failed to create base template: %w", err)
	}

	// Start the container
	fmt.Println("Starting base template...")
	if err := client.StartInstance(containerName); err != nil {
		// Clean up on failure
		client.DeleteInstance(containerName)
		return fmt.Errorf("failed to start base template: %w", err)
	}

	// Wait for the container to be fully ready (network, etc.)
	fmt.Println("Waiting for container to be ready...")
	if err := waitForReady(client, containerName, 60*time.Second); err != nil {
		client.StopAndDeleteInstance(containerName)
		return fmt.Errorf("container did not become ready: %w", err)
	}

	// Install core tools
	fmt.Println("Installing core tools...")
	for _, cmd := range CoreToolInstallCommands() {
		fmt.Printf("  Running: %v\n", cmd)
		exitCode, err := client.ExecSimple(containerName, cmd)
		if err != nil {
			client.StopAndDeleteInstance(containerName)
			return fmt.Errorf("failed to run %v: %w", cmd, err)
		}
		if exitCode != 0 {
			client.StopAndDeleteInstance(containerName)
			return fmt.Errorf("command %v exited with code %d", cmd, exitCode)
		}
	}

	// Push astonish binary into the template so session containers can run
	// `astonish node` (headless tool execution server) without any additional setup.
	fmt.Println("Pushing astonish binary into template...")
	if err := pushAstonishBinary(client, containerName); err != nil {
		client.StopAndDeleteInstance(containerName)
		return fmt.Errorf("failed to push astonish binary: %w", err)
	}

	// Stop before snapshotting (for consistency)
	fmt.Println("Stopping template for snapshot...")
	if err := client.StopInstance(containerName, false); err != nil {
		client.StopAndDeleteInstance(containerName)
		return fmt.Errorf("failed to stop base template: %w", err)
	}

	// Create snapshot
	fmt.Println("Creating snapshot...")
	if err := client.CreateSnapshot(containerName, SnapshotName); err != nil {
		client.DeleteInstance(containerName)
		return fmt.Errorf("failed to snapshot base template: %w", err)
	}

	// Register in metadata
	now := time.Now()
	meta := &TemplateMeta{
		Name:       BaseTemplate,
		CreatedAt:  now,
		SnapshotAt: now,
	}
	if hash, hashErr := ComputeBinaryHash(); hashErr == nil {
		meta.BinaryHash = hash
	}

	if err := registry.Add(meta); err != nil {
		return fmt.Errorf("failed to register base template: %w", err)
	}

	fmt.Println("Base template initialized successfully.")
	return nil
}

// CreateTemplate creates a new custom template by cloning from @base.
func CreateTemplate(client *IncusClient, registry *TemplateRegistry, name, description string) error {
	if name == BaseTemplate {
		return fmt.Errorf("cannot create a template named %q (reserved)", BaseTemplate)
	}

	containerName := TemplateName(name)

	// Check if already exists
	if client.InstanceExists(containerName) {
		return fmt.Errorf("template %q already exists", name)
	}

	// Verify @base exists and has a snapshot
	baseName := TemplateName(BaseTemplate)
	if !client.InstanceExists(baseName) {
		return fmt.Errorf("base template does not exist; run 'astonish sandbox init' first")
	}

	if !client.HasSnapshot(baseName, SnapshotName) {
		return fmt.Errorf("base template has no snapshot; run 'astonish sandbox refresh' first")
	}

	fmt.Printf("Creating template %q from @base...\n", name)

	// Clone from @base snapshot
	if err := client.CreateContainerFromSnapshot(containerName, BaseTemplate, nil); err != nil {
		return fmt.Errorf("failed to clone from @base: %w", err)
	}

	// Register in metadata
	meta := &TemplateMeta{
		Name:        name,
		Description: description,
		CreatedAt:   time.Now(),
		BasedOn:     BaseTemplate,
	}

	if err := registry.Add(meta); err != nil {
		return fmt.Errorf("failed to register template: %w", err)
	}

	fmt.Printf("Template %q created. Use 'astonish sandbox template shell %s' to customize it.\n", name, name)
	return nil
}

// ShellIntoTemplate starts an interactive shell in a template container.
// The template must exist. It will be started if stopped.
func ShellIntoTemplate(client *IncusClient, name string) error {
	containerName := TemplateName(name)

	if !client.InstanceExists(containerName) {
		return fmt.Errorf("template %q does not exist", name)
	}

	// Start if not running
	if !client.IsRunning(containerName) {
		fmt.Printf("Starting template %q...\n", name)
		if err := client.StartInstance(containerName); err != nil {
			return fmt.Errorf("failed to start template: %w", err)
		}
	}

	// Use the incus CLI for interactive shell (it handles PTY properly)
	cmd := exec.Command("incus", "exec", containerName, "--", "bash", "-l")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Printf("Entering template %q. Type 'exit' to leave.\n", name)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("shell session ended with error: %w", err)
	}

	return nil
}

// SnapshotTemplate stops the template (if running) and creates/refreshes its snapshot.
func SnapshotTemplate(client *IncusClient, registry *TemplateRegistry, name string) error {
	containerName := TemplateName(name)

	if !client.InstanceExists(containerName) {
		return fmt.Errorf("template %q does not exist", name)
	}

	// Stop if running (for consistent snapshot)
	if client.IsRunning(containerName) {
		fmt.Printf("Stopping template %q for snapshot...\n", name)
		if err := client.StopInstance(containerName, false); err != nil {
			return fmt.Errorf("failed to stop template: %w", err)
		}
	}

	// Delete existing snapshot if present
	if client.HasSnapshot(containerName, SnapshotName) {
		fmt.Println("Removing existing snapshot...")
		if err := client.DeleteSnapshot(containerName, SnapshotName); err != nil {
			return fmt.Errorf("failed to delete old snapshot: %w", err)
		}
	}

	// Create new snapshot
	fmt.Println("Creating snapshot...")
	if err := client.CreateSnapshot(containerName, SnapshotName); err != nil {
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	// Update metadata
	meta := registry.Get(name)
	if meta != nil {
		meta.SnapshotAt = time.Now()
		if err := registry.Update(meta); err != nil {
			return fmt.Errorf("failed to update template metadata: %w", err)
		}
	}

	fmt.Printf("Template %q snapshot created. It is now ready for cloning.\n", name)
	return nil
}

// PromoteTemplate replaces @base with the contents of the specified template.
// This makes the template's state the new default for all future sessions.
func PromoteTemplate(client *IncusClient, registry *TemplateRegistry, name string) error {
	if name == BaseTemplate {
		return fmt.Errorf("cannot promote @base to itself")
	}

	containerName := TemplateName(name)
	baseName := TemplateName(BaseTemplate)

	if !client.InstanceExists(containerName) {
		return fmt.Errorf("template %q does not exist", name)
	}

	if !client.HasSnapshot(containerName, SnapshotName) {
		return fmt.Errorf("template %q has no snapshot; run 'astonish sandbox template snapshot %s' first", name, name)
	}

	fmt.Printf("Promoting template %q to @base...\n", name)
	fmt.Println("WARNING: This will replace the current @base template.")

	// Delete the old @base
	if client.InstanceExists(baseName) {
		if err := client.StopAndDeleteInstance(baseName); err != nil {
			return fmt.Errorf("failed to remove old @base: %w", err)
		}
	}

	// Clone from the promoted template's snapshot
	if err := client.CreateContainerFromSnapshot(baseName, name, nil); err != nil {
		return fmt.Errorf("failed to clone promoted template to @base: %w", err)
	}

	// Snapshot the new @base
	if err := client.CreateSnapshot(baseName, SnapshotName); err != nil {
		return fmt.Errorf("failed to snapshot new @base: %w", err)
	}

	// Update metadata
	baseMeta := registry.Get(BaseTemplate)
	if baseMeta == nil {
		baseMeta = &TemplateMeta{
			Name:      BaseTemplate,
			CreatedAt: time.Now(),
		}
	}

	baseMeta.SnapshotAt = time.Now()
	if err := registry.Update(baseMeta); err != nil {
		return fmt.Errorf("failed to update base template metadata: %w", err)
	}

	fmt.Printf("@base has been replaced with the contents of %q.\n", name)
	return nil
}

// DeleteTemplate removes a template container and its metadata.
func DeleteTemplate(client *IncusClient, registry *TemplateRegistry, name string) error {
	if name == BaseTemplate {
		return fmt.Errorf("cannot delete the base template")
	}

	containerName := TemplateName(name)

	if client.InstanceExists(containerName) {
		fmt.Printf("Destroying template container %q...\n", name)
		if err := client.StopAndDeleteInstance(containerName); err != nil {
			return fmt.Errorf("failed to destroy template container: %w", err)
		}
	}

	if err := registry.Remove(name); err != nil {
		return fmt.Errorf("failed to remove template metadata: %w", err)
	}

	fmt.Printf("Template %q deleted.\n", name)
	return nil
}

// RefreshBase re-snapshots the @base template.
// Use this after manually customizing the base template.
func RefreshBase(client *IncusClient, registry *TemplateRegistry) error {
	return RefreshTemplate(client, registry, BaseTemplate)
}

// RefreshTemplate pushes the current astonish binary into a template and
// re-snapshots it. This is the generalized version of the old RefreshBase.
// Works for any template (@base or custom project templates).
func RefreshTemplate(client *IncusClient, registry *TemplateRegistry, name string) error {
	containerName := TemplateName(name)

	if !client.InstanceExists(containerName) {
		return fmt.Errorf("template %q does not exist; run 'astonish sandbox init' first", name)
	}

	// Start the template so we can push the updated binary
	if !client.IsRunning(containerName) {
		fmt.Printf("Starting template %q...\n", name)
		if err := client.StartInstance(containerName); err != nil {
			return fmt.Errorf("failed to start template %q: %w", name, err)
		}
		if err := waitForReady(client, containerName, 30*time.Second); err != nil {
			return fmt.Errorf("template %q not ready: %w", name, err)
		}
	}

	// Push the current astonish binary into the template
	fmt.Printf("Pushing astonish binary into template %q...\n", name)
	if err := pushAstonishBinary(client, containerName); err != nil {
		return fmt.Errorf("failed to push astonish binary: %w", err)
	}

	// Compute the binary hash for tracking
	hash, hashErr := ComputeBinaryHash()

	// Re-snapshot (this stops the container first)
	if err := SnapshotTemplate(client, registry, name); err != nil {
		return err
	}

	// Update the BinaryHash in the registry
	if hashErr == nil && hash != "" {
		meta := registry.Get(name)
		if meta != nil {
			meta.BinaryHash = hash
			if updateErr := registry.Update(meta); updateErr != nil {
				log.Printf("[sandbox] Warning: could not update BinaryHash for template %q: %v", name, updateErr)
			}
		}
	}

	return nil
}

// ComputeBinaryHash returns the SHA-256 hex digest of the running astonish binary.
// This is used to detect when the binary has been rebuilt and templates need
// refreshing (the in-container binary is stale).
func ComputeBinaryHash() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to determine binary path: %w", err)
	}

	realPath, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		realPath = exePath
	}

	f, err := os.Open(realPath)
	if err != nil {
		return "", fmt.Errorf("failed to open binary %s: %w", realPath, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("failed to hash binary: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// CreateTemplateFromContainer snapshots a running session container and creates
// a new template from it. This is the wizard's "save_sandbox_template" operation:
//
//  1. Stop the node process (caller must do this before calling)
//  2. Create a temporary snapshot on the session container
//  3. Copy the snapshot as a new template container
//  4. Snapshot the new template container (for cloning)
//  5. Register in the template registry
//  6. Clean up the temporary snapshot
//
// The container must be running (but with node stopped). The template name must
// not already exist.
func CreateTemplateFromContainer(client *IncusClient, registry *TemplateRegistry, containerName, templateName, description string) error {
	if templateName == BaseTemplate {
		return fmt.Errorf("cannot create a template named %q (reserved)", BaseTemplate)
	}

	tplContainerName := TemplateName(templateName)
	if client.InstanceExists(tplContainerName) {
		return fmt.Errorf("template %q already exists", templateName)
	}

	// Temporary snapshot name (unique to avoid conflicts)
	tmpSnapName := fmt.Sprintf("tpl-save-%d", time.Now().Unix())

	// Stop the container for a consistent snapshot
	if client.IsRunning(containerName) {
		log.Printf("[sandbox] Stopping container %q for template snapshot...", containerName)
		if err := client.StopInstance(containerName, false); err != nil {
			return fmt.Errorf("failed to stop container %q: %w", containerName, err)
		}
	}

	// Create temporary snapshot on the session container
	log.Printf("[sandbox] Creating temporary snapshot %q on %q...", tmpSnapName, containerName)
	if err := client.CreateSnapshot(containerName, tmpSnapName); err != nil {
		return fmt.Errorf("failed to create snapshot on %q: %w", containerName, err)
	}

	// Ensure cleanup of temporary snapshot
	defer func() {
		if client.HasSnapshot(containerName, tmpSnapName) {
			_ = client.DeleteSnapshot(containerName, tmpSnapName)
		}
	}()

	// Copy the snapshot as a new template container
	log.Printf("[sandbox] Copying snapshot to template %q...", tplContainerName)
	if err := client.CopyFromAnySnapshot(tplContainerName, containerName, tmpSnapName, nil); err != nil {
		return fmt.Errorf("failed to copy snapshot to template: %w", err)
	}

	// Snapshot the new template (so it can be used for cloning via CreateContainerFromSnapshot)
	log.Printf("[sandbox] Creating clone-ready snapshot on template %q...", tplContainerName)
	if err := client.CreateSnapshot(tplContainerName, SnapshotName); err != nil {
		// Clean up template container on failure
		_ = client.DeleteInstance(tplContainerName)
		return fmt.Errorf("failed to snapshot template %q: %w", templateName, err)
	}

	// Compute binary hash
	hash, _ := ComputeBinaryHash()

	// Register in the template registry
	now := time.Now()
	meta := &TemplateMeta{
		Name:        templateName,
		Description: description,
		CreatedAt:   now,
		SnapshotAt:  now,
		BasedOn:     BaseTemplate,
		BinaryHash:  hash,
	}

	if err := registry.Add(meta); err != nil {
		return fmt.Errorf("failed to register template: %w", err)
	}

	// Restart the session container so the session can continue
	log.Printf("[sandbox] Restarting session container %q...", containerName)
	if err := client.StartInstance(containerName); err != nil {
		log.Printf("[sandbox] Warning: failed to restart session container %q: %v", containerName, err)
	}

	log.Printf("[sandbox] Template %q created from container %q", templateName, containerName)
	return nil
}

// refreshAllOnce ensures RefreshAllIfNeeded runs at most once per process.
var refreshAllOnce sync.Once

// RefreshAllIfNeeded is an async, process-wide singleton that checks all
// templates and refreshes those with stale binaries. It must be called as
// `go RefreshAllIfNeeded(...)` or from a goroutine — it must NOT block
// the daemon startup path (this was the cause of the 502 bug).
func RefreshAllIfNeeded(client *IncusClient, registry *TemplateRegistry) {
	refreshAllOnce.Do(func() {
		currentHash, err := ComputeBinaryHash()
		if err != nil {
			log.Printf("[sandbox] Warning: could not compute binary hash for refresh check: %v", err)
			return
		}

		templates := registry.List()
		for _, meta := range templates {
			if meta.BinaryHash == currentHash {
				continue // binary matches, no refresh needed
			}

			containerName := TemplateName(meta.Name)
			if !client.InstanceExists(containerName) {
				continue // template container missing, skip
			}

			if !client.HasSnapshot(containerName, SnapshotName) {
				continue // no snapshot to refresh
			}

			log.Printf("[sandbox] Template %q has stale binary (hash mismatch), refreshing...", meta.Name)
			if err := RefreshTemplate(client, registry, meta.Name); err != nil {
				log.Printf("[sandbox] Warning: failed to refresh template %q: %v", meta.Name, err)
			} else {
				log.Printf("[sandbox] Template %q refreshed with current binary", meta.Name)
			}
		}
	})
}

// RefreshAll refreshes all templates unconditionally. Used by `astonish sandbox refresh --force`.
func RefreshAll(client *IncusClient, registry *TemplateRegistry) error {
	templates := registry.List()
	if len(templates) == 0 {
		return fmt.Errorf("no templates to refresh")
	}

	for _, meta := range templates {
		containerName := TemplateName(meta.Name)
		if !client.InstanceExists(containerName) {
			fmt.Printf("Skipping %q (container missing)\n", meta.Name)
			continue
		}

		fmt.Printf("Refreshing template %q...\n", meta.Name)
		if err := RefreshTemplate(client, registry, meta.Name); err != nil {
			fmt.Printf("  Warning: failed to refresh %q: %v\n", meta.Name, err)
		} else {
			fmt.Printf("  Done.\n")
		}
	}

	return nil
}

// waitForReady waits for a container to be ready (network available).
func waitForReady(client *IncusClient, containerName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		// Try running a simple command
		exitCode, err := client.ExecSimple(containerName, []string{"true"})
		if err == nil && exitCode == 0 {
			return nil
		}

		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for container %q to be ready", containerName)
}

// BinaryDestPath is where the astonish binary is placed inside templates/containers.
const BinaryDestPath = "/usr/local/bin/astonish"

// pushAstonishBinary copies the running astonish binary into a container.
// The container must be running. The binary is placed at /usr/local/bin/astonish
// with executable permissions (0755).
func pushAstonishBinary(client *IncusClient, containerName string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine astonish binary path: %w", err)
	}

	// Resolve symlinks to get the real binary
	realPath, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		realPath = exePath
	}

	f, err := os.Open(realPath)
	if err != nil {
		return fmt.Errorf("failed to open binary %s: %w", realPath, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat binary: %w", err)
	}

	fmt.Printf("  Binary: %s (%.1f MB)\n", realPath, float64(info.Size())/(1024*1024))

	if err := client.PushFile(containerName, BinaryDestPath, f, 0755); err != nil {
		return fmt.Errorf("failed to push binary to container: %w", err)
	}

	// Verify it's accessible
	exitCode, err := client.ExecSimple(containerName, []string{BinaryDestPath, "--version"})
	if err != nil {
		return fmt.Errorf("binary verification failed: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("binary verification exited with code %d", exitCode)
	}

	return nil
}

// ensureIncusEnvironment checks and fixes the Incus environment for Astonish.
// It ensures:
// 1. A storage pool exists
// 2. A network bridge exists (incusbr0)
// 3. The default profile has a root disk device and network device
func ensureIncusEnvironment(client *IncusClient) error {
	server := client.Server()

	// 1. Check storage pool
	pools, err := server.GetStoragePools()
	if err != nil {
		return fmt.Errorf("failed to list storage pools: %w", err)
	}
	if len(pools) == 0 {
		return fmt.Errorf("no storage pool configured; run 'incus admin init' first")
	}
	poolName := pools[0].Name

	// 2. Ensure network bridge exists
	networkName := "incusbr0"
	_, _, err = server.GetNetwork(networkName)
	if err != nil {
		fmt.Printf("Creating network bridge %s...\n", networkName)
		err = server.CreateNetwork(api.NetworksPost{
			Name: networkName,
			Type: "bridge",
			NetworkPut: api.NetworkPut{
				Config: map[string]string{
					"ipv4.address": "auto",
					"ipv4.nat":     "true",
					"ipv6.address": "none",
				},
			},
		})
		if err != nil {
			return fmt.Errorf("failed to create network bridge: %w", err)
		}
	}

	// 3. Ensure default profile has root disk device
	profile, _, err := server.GetProfile("default")
	if err != nil {
		return fmt.Errorf("failed to get default profile: %w", err)
	}

	needsUpdate := false

	if profile.Devices == nil {
		profile.Devices = map[string]map[string]string{}
	}

	// Ensure root disk
	if _, ok := profile.Devices["root"]; !ok {
		fmt.Println("Adding root disk device to default profile...")
		profile.Devices["root"] = map[string]string{
			"type": "disk",
			"path": "/",
			"pool": poolName,
		}
		needsUpdate = true
	}

	// Ensure network device
	if _, ok := profile.Devices["eth0"]; !ok {
		fmt.Println("Adding network device to default profile...")
		profile.Devices["eth0"] = map[string]string{
			"type":    "nic",
			"network": networkName,
			"name":    "eth0",
		}
		needsUpdate = true
	}

	if needsUpdate {
		err = server.UpdateProfile("default", profile.ProfilePut, "")
		if err != nil {
			return fmt.Errorf("failed to update default profile: %w", err)
		}
	}

	return nil
}
