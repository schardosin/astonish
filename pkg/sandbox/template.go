package sandbox

import (
	"fmt"
	"os"
	"os/exec"
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
	return SnapshotTemplate(client, registry, BaseTemplate)
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
