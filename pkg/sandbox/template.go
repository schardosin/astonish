package sandbox

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
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
	"docker.io",
}

// CoreToolInstallCommands returns the commands to install core tools in a template.
// These run inside the container after creation. Docker is included as a core
// tool because it's required for containerized MCP servers (stdio transport).
func CoreToolInstallCommands() [][]string {
	return [][]string{
		{"apt-get", "update"},
		{"apt-get", "install", "-y",
			"git", "curl", "wget", "jq", "unzip", "build-essential",
			"python3", "python3-pip", "python3-venv",
			"ca-certificates", "gnupg",
			// Docker runtime (daemon + CLI + containerd) — required for
			// containerized MCP servers and Docker-based workflows
			"docker.io",
		},
		// Remove apparmor — cannot work inside nested LXC containers
		// and blocks Docker from starting containers
		{"apt-get", "remove", "-y", "apparmor"},
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

// OptionalTool describes an optional tool that can be installed into the base template.
type OptionalTool struct {
	// ID is the unique identifier for this tool (used in BaseTemplateOptions).
	ID string
	// Name is the display name shown in the setup prompt.
	Name string
	// Description is a short explanation of what the tool does and why it's useful.
	Description string
	// URL is a link to the tool's homepage or docs.
	URL string
	// InstallCommands returns the commands to install this tool inside a container.
	InstallCommands func() [][]string
	// Recommended indicates this tool should be pre-selected / promoted during setup.
	Recommended bool
	// RequiresNesting indicates this tool needs security.nesting=true on containers
	// (e.g., Docker daemon needs to create its own namespaces and cgroups).
	RequiresNesting bool
}

// OptionalTools returns the catalog of optional tools available for installation
// into the base template. The order here is the order they are presented.
// Note: Docker is NOT optional — it's installed as a core tool because it's
// required for containerized MCP servers (stdio transport).
func OptionalTools() []OptionalTool {
	return []OptionalTool{
		{
			ID:          "opencode",
			Name:        "OpenCode",
			Description: "AI coding agent used as a delegate tool by fleet sub-agents.\nEnables autonomous code generation, editing, and analysis inside containers.",
			URL:         "https://opencode.ai",
			Recommended: true,
			InstallCommands: func() [][]string {
				return [][]string{
					{"sh", "-c", "curl -fsSL https://opencode.ai/install | bash"},
				}
			},
		},
	}
}

// BaseTemplateOptions configures which optional tools to install in the base template.
type BaseTemplateOptions struct {
	// InstallTools maps optional tool IDs to true if they should be installed.
	InstallTools map[string]bool

	// ProgressFunc, when non-nil, receives progress messages instead of
	// printing them to stdout. This allows callers (e.g. API handlers)
	// to stream progress to clients.
	ProgressFunc func(string)
}

// DefaultBaseTemplateOptions returns options with no optional tools selected.
func DefaultBaseTemplateOptions() BaseTemplateOptions {
	return BaseTemplateOptions{
		InstallTools: make(map[string]bool),
	}
}

// InitBaseTemplate creates the @base template container from a fresh image
// and installs core tools plus any selected optional tools.
// This is run during `astonish sandbox init` or `astonish setup`.
func InitBaseTemplate(client *IncusClient, registry *TemplateRegistry, opts BaseTemplateOptions) error {
	containerName := TemplateName(BaseTemplate)

	// progress routes messages to the caller's callback (e.g. SSE stream)
	// or falls back to stdout when no callback is set.
	progress := func(msg string, args ...any) {
		s := fmt.Sprintf(msg, args...)
		if opts.ProgressFunc != nil {
			opts.ProgressFunc(s)
		} else {
			fmt.Print(s)
		}
	}

	// Check if already exists
	if client.InstanceExists(containerName) {
		if client.HasSnapshot(containerName, SnapshotName) {
			// Template exists with a valid snapshot — already initialized.
			// Ensure supporting resources (overlay image, base dir) also exist.
			if err := ensureIncusEnvironment(client); err != nil {
				return fmt.Errorf("failed to set up Incus environment: %w", err)
			}
			if err := EnsureOverlayImage(client); err != nil {
				return fmt.Errorf("failed to create overlay image: %w", err)
			}
			if err := EnsureOverlayBaseDir(); err != nil {
				return fmt.Errorf("failed to create overlay base directory: %w", err)
			}
			progress("Base template already initialized (use 'astonish sandbox refresh' to re-snapshot).\n")
			return nil
		}

		// Template exists but has no snapshot — incomplete/broken state.
		// Clean up and re-create from scratch.
		progress("Found incomplete base template, cleaning up...\n")
		if err := client.StopAndDeleteInstance(containerName); err != nil {
			return fmt.Errorf("failed to clean up incomplete template: %w", err)
		}
	}

	// Ensure the Incus environment is properly configured (network, profile)
	if err := ensureIncusEnvironment(client); err != nil {
		return fmt.Errorf("failed to set up Incus environment: %w", err)
	}

	progress("Creating base template from %s...\n", DefaultBaseImage)

	// Launch from image
	if err := client.LaunchFromImage(containerName, DefaultBaseImage, nil); err != nil {
		return fmt.Errorf("failed to create base template: %w", err)
	}

	// Start the container
	progress("Starting base template...\n")
	if err := client.StartInstance(containerName); err != nil {
		// Clean up on failure
		client.DeleteInstance(containerName)
		return fmt.Errorf("failed to start base template: %w", err)
	}

	// Wait for the container to be fully ready (network, etc.)
	progress("Waiting for container to be ready...\n")
	if err := waitForReady(client, containerName, 60*time.Second); err != nil {
		client.StopAndDeleteInstance(containerName)
		return fmt.Errorf("container did not become ready: %w", err)
	}

	// Install core tools
	progress("Installing core tools...\n")
	for _, cmd := range CoreToolInstallCommands() {
		progress("  Running: %v\n", cmd)
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

	// Install optional tools selected by the user
	selectedTools := OptionalTools()
	for _, tool := range selectedTools {
		if !opts.InstallTools[tool.ID] {
			continue
		}
		progress("Installing %s...\n", tool.Name)
		for _, cmd := range tool.InstallCommands() {
			progress("  Running: %v\n", cmd)
			exitCode, err := client.ExecSimple(containerName, cmd)
			if err != nil {
				client.StopAndDeleteInstance(containerName)
				return fmt.Errorf("failed to install %s (%v): %w", tool.Name, cmd, err)
			}
			if exitCode != 0 {
				client.StopAndDeleteInstance(containerName)
				return fmt.Errorf("install %s: command %v exited with code %d", tool.Name, cmd, exitCode)
			}
		}
		progress("  %s installed.\n", tool.Name)
	}

	// Enable security.nesting — Docker is a core tool and requires nesting
	// to create its own namespaces and cgroups inside the container.
	progress("Enabling container nesting (required by Docker)...\n")
	if err := client.SetInstanceConfig(containerName, map[string]string{
		"security.nesting": "true",
	}); err != nil {
		client.StopAndDeleteInstance(containerName)
		return fmt.Errorf("failed to enable nesting: %w", err)
	}

	// Push astonish binary into the template so session containers can run
	// `astonish node` (headless tool execution server) without any additional setup.
	progress("Pushing astonish binary into template...\n")
	if err := pushAstonishBinary(client, containerName); err != nil {
		client.StopAndDeleteInstance(containerName)
		return fmt.Errorf("failed to push astonish binary: %w", err)
	}

	// Stop before snapshotting (for consistency)
	progress("Stopping template for snapshot...\n")
	if err := client.StopInstance(containerName, false); err != nil {
		client.StopAndDeleteInstance(containerName)
		return fmt.Errorf("failed to stop base template: %w", err)
	}

	// Shift rootfs UIDs for unprivileged containers (one-time cost).
	// Must happen BEFORE snapshot — btrfs snapshots are read-only.
	progress("Preparing template for unprivileged containers...\n")
	if err := ShiftTemplateRootfs(client, BaseTemplate); err != nil {
		slog.Warn("failed to shift template rootfs UIDs", "component", "sandbox", "error", err)
	}

	// Create snapshot (captures the shifted UIDs)
	progress("Creating snapshot...\n")
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
		Nesting:    true, // Docker is a core tool and requires nesting
	}
	if hash, hashErr := ComputeBinaryHash(); hashErr == nil {
		meta.BinaryHash = hash
	}

	if err := registry.Add(meta); err != nil {
		return fmt.Errorf("failed to register base template: %w", err)
	}

	// Create the tiny overlay shell image for instant session container creation.
	// This image (~670 bytes) is used instead of cloning the full template —
	// session containers get their filesystem via overlayfs.
	if err := EnsureOverlayImage(client); err != nil {
		return fmt.Errorf("failed to create overlay image: %w", err)
	}

	// Ensure the overlay base directory exists
	if err := EnsureOverlayBaseDir(); err != nil {
		return fmt.Errorf("failed to create overlay base directory: %w", err)
	}

	progress("Base template initialized successfully.\n")
	return nil
}

// CreateTemplate creates a new custom template from @base using overlayfs.
// The template container is created instantly (~260ms) from the tiny shell image
// with an overlayfs mount backed by @base's snapshot. The user can then shell in
// to customize it. When they run 'template snapshot', the overlay is materialized
// into a flat rootfs and snapshotted for use as a lower layer in sessions.
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

	// Create overlay-based container (tiny image + overlayfs mount backed by @base snapshot)
	if err := CreateOverlayContainer(client, containerName, BaseTemplate, registry, nil); err != nil {
		return fmt.Errorf("failed to create template container: %w", err)
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
// For overlay-based templates, the overlay is re-mounted if needed.
func ShellIntoTemplate(client *IncusClient, registry *TemplateRegistry, name string) error {
	containerName := TemplateName(name)

	if !client.InstanceExists(containerName) {
		return fmt.Errorf("template %q does not exist", name)
	}

	// Start if not running
	if !client.IsRunning(containerName) {
		// Re-mount overlay if this template is based on another (not @base itself)
		meta := registry.Get(name)
		if meta != nil && meta.BasedOn != "" {
			if err := ensureOverlayMounted(client, containerName, meta.BasedOn, registry); err != nil {
				slog.Warn("failed to ensure overlay for template", "component", "sandbox", "template", name, "error", err)
			}
		}

		fmt.Printf("Starting template %q...\n", name)
		if err := client.StartInstance(containerName); err != nil {
			return fmt.Errorf("failed to start template: %w", err)
		}
	}

	// Use the incus CLI for interactive shell (it handles PTY properly).
	// On Docker+Incus, chain through docker exec to reach the Incus daemon.
	var cmd *exec.Cmd
	if activePlatform == PlatformDockerIncus {
		cmd = ExecInDockerHostInteractive([]string{
			"incus", "exec", containerName, "--", "bash", "-l",
		})
	} else {
		cmd = exec.Command("incus", "exec", containerName, "--", "bash", "-l")
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Printf("Entering template %q. Type 'exit' to leave.\n", name)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("shell session ended with error: %w", err)
	}

	return nil
}

// SnapshotTemplate handles template snapshotting.
//
// For @base: stops the container, takes an Incus snapshot of its real rootfs.
// This snapshot becomes the root lower layer for all overlay chains.
//
// For custom templates (overlay-based): no Incus snapshot is needed.
// The template's state IS its overlay upper directory. Sessions from this
// template use stacked overlayfs (template-upper:@base-snapshot). This
// function just updates the registry metadata.
func SnapshotTemplate(client *IncusClient, registry *TemplateRegistry, name string) error {
	containerName := TemplateName(name)

	if !client.InstanceExists(containerName) {
		return fmt.Errorf("template %q does not exist", name)
	}

	meta := registry.Get(name)

	// Custom template (overlay-based) — no Incus snapshot needed
	if meta != nil && meta.BasedOn != "" {
		// Stop if running (for consistent state)
		if client.IsRunning(containerName) {
			fmt.Printf("Stopping template %q...\n", name)
			if err := client.StopInstance(containerName, false); err != nil {
				return fmt.Errorf("failed to stop template: %w", err)
			}
		}

		// Update metadata
		meta.SnapshotAt = time.Now()
		if hash, hashErr := ComputeBinaryHash(); hashErr == nil {
			meta.BinaryHash = hash
		}
		if err := registry.Update(meta); err != nil {
			return fmt.Errorf("failed to update template metadata: %w", err)
		}

		fmt.Printf("Template %q state saved (overlay-based, no snapshot needed).\n", name)
		return nil
	}

	// @base template — real Incus snapshot required

	// Stop if running (for consistent snapshot)
	if client.IsRunning(containerName) {
		fmt.Printf("Stopping template %q for snapshot...\n", name)
		if err := client.StopInstance(containerName, false); err != nil {
			return fmt.Errorf("failed to stop template: %w", err)
		}
	}

	// Acquire exclusive lock to prevent session containers from being created
	// while the snapshot is being replaced. Session creation holds a read lock
	// on templateSnapshotMu and uses the snapshot as the overlay lowerdir —
	// deleting it here without synchronization causes "Failed to exec /sbin/init".
	templateSnapshotMu.Lock()

	// Delete existing snapshot if present
	if client.HasSnapshot(containerName, SnapshotName) {
		fmt.Println("Removing existing snapshot...")
		if err := client.DeleteSnapshot(containerName, SnapshotName); err != nil {
			templateSnapshotMu.Unlock()
			return fmt.Errorf("failed to delete old snapshot: %w", err)
		}
	}

	// Shift rootfs UIDs for unprivileged containers (one-time cost).
	// Must happen BEFORE snapshot — btrfs snapshots are read-only.
	if err := ShiftTemplateRootfs(client, name); err != nil {
		slog.Warn("failed to shift template rootfs UIDs", "component", "sandbox", "error", err)
	}

	// Create new snapshot (captures the shifted UIDs)
	fmt.Println("Creating snapshot...")
	if err := client.CreateSnapshot(containerName, SnapshotName); err != nil {
		templateSnapshotMu.Unlock()
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	// Remount all overlay mounts that depend on the base snapshot.
	// The old snapshot directory was deleted (old inode gone) and a new one
	// created (new inode). Existing overlay mounts still reference the old
	// inode via the kernel's mount table, so they see an empty directory.
	// We must unmount+remount each one so the kernel resolves the path to
	// the new inode. This runs while templateSnapshotMu is still held to
	// prevent new overlays from being created mid-remount.
	poolName, poolErr := GetPoolForProfile(client)
	if poolErr == nil {
		poolPath, pathErr := GetPoolSourcePath(client, poolName)
		if pathErr == nil {
			snapPath := SnapshotRootfsPath(poolPath, name)
			if remountErr := RemountDependentOverlays(client, snapPath); remountErr != nil {
				slog.Warn("failed to remount dependent overlays", "component", "sandbox", "error", remountErr)
			}
		}
	}

	templateSnapshotMu.Unlock()

	// Update metadata
	if meta != nil {
		meta.SnapshotAt = time.Now()
		if hash, hashErr := ComputeBinaryHash(); hashErr == nil {
			meta.BinaryHash = hash
		}
		if err := registry.Update(meta); err != nil {
			return fmt.Errorf("failed to update template metadata: %w", err)
		}
	}

	fmt.Printf("Template %q snapshot created.\n", name)
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

	meta := registry.Get(name)

	fmt.Printf("Promoting template %q to @base...\n", name)
	fmt.Println("WARNING: This will replace the current @base template.")

	// Resolve pool paths
	poolName, err := GetPoolForProfile(client)
	if err != nil {
		return fmt.Errorf("failed to determine storage pool: %w", err)
	}
	poolPath, err := GetPoolSourcePath(client, poolName)
	if err != nil {
		return fmt.Errorf("failed to get pool path: %w", err)
	}

	// Determine if this is an overlay-based custom template or a traditional one
	isOverlayBased := meta != nil && meta.BasedOn != ""

	if isOverlayBased {
		// Overlay-based promotion: mount the stacked overlay read-only on a
		// temp dir to get the full merged view, then materialize it into @base.
		lowerLayers, err := ResolveLowerLayers(poolPath, name, registry)
		if err != nil {
			return fmt.Errorf("failed to resolve overlay layers for %q: %w", name, err)
		}

		// Create a temporary mount point for the merged view (on sandbox host)
		mergedDir := "/tmp/astonish-promote-" + name
		if err := mkdirAllOnSandboxHost(mergedDir, 0755); err != nil {
			return fmt.Errorf("failed to create temp dir for merge: %w", err)
		}
		defer removeAllOnSandboxHost(mergedDir)

		// Mount read-only overlay to get the merged view
		// For a read-only mount, we don't need upperdir/workdir
		mountOpts := fmt.Sprintf("lowerdir=%s", lowerLayers)
		if err := mountOverlayOnSandboxHost(mountOpts, mergedDir); err != nil {
			return fmt.Errorf("failed to mount merged overlay: %w", err)
		}
		defer func() {
			if err := umountOnSandboxHost(mergedDir); err != nil {
				slog.Warn("failed to unmount merged overlay", "component", "sandbox", "path", mergedDir, "error", err)
			}
		}()

		// Stop and unmount old @base overlay (if any)
		if client.IsRunning(baseName) {
			if err := client.StopInstance(baseName, false); err != nil {
				return fmt.Errorf("failed to stop old @base: %w", err)
			}
		}
		if IsOverlayMounted(poolPath, baseName) {
			if err := UnmountSessionOverlay(poolPath, baseName); err != nil {
				slog.Warn("failed to unmount old @base overlay", "component", "sandbox", "container", baseName, "error", err)
			}
		}

		// Delete old @base snapshot and container
		templateSnapshotMu.Lock()
		if client.HasSnapshot(baseName, SnapshotName) {
			if err := client.DeleteSnapshot(baseName, SnapshotName); err != nil {
				templateSnapshotMu.Unlock()
				return fmt.Errorf("failed to delete old @base snapshot: %w", err)
			}
		}
		if client.InstanceExists(baseName) {
			if err := client.StopAndDeleteInstance(baseName); err != nil {
				templateSnapshotMu.Unlock()
				return fmt.Errorf("failed to remove old @base: %w", err)
			}
		}

		// Create new @base container from tiny image
		fmt.Println("Creating new @base container...")
		arch, err := client.ServerArchitecture()
		if err != nil {
			templateSnapshotMu.Unlock()
			return fmt.Errorf("failed to detect server architecture: %w", err)
		}

		refreshCfg := containerSecurityConfig()
		refreshCfg["security.nesting"] = "false"

		req := api.InstancesPost{
			Name: baseName,
			Type: api.InstanceTypeContainer,
			InstancePut: api.InstancePut{
				Architecture: arch,
				Config:       refreshCfg,
			},
			Source: api.InstanceSource{
				Type:  "image",
				Alias: OverlayImageAlias,
			},
		}
		op, err := client.server.CreateInstance(req)
		if err != nil {
			templateSnapshotMu.Unlock()
			return fmt.Errorf("failed to create new @base container: %w", err)
		}
		if err := op.Wait(); err != nil {
			templateSnapshotMu.Unlock()
			return fmt.Errorf("failed to wait for @base container creation: %w", err)
		}

		// Copy the merged overlay view into @base's real rootfs
		baseRootfs := ContainerRootfsPath(poolPath, baseName)
		fmt.Println("Materializing template into @base rootfs (this may take a moment)...")
		if err := rsyncOnSandboxHost(mergedDir+"/", baseRootfs+"/"); err != nil {
			templateSnapshotMu.Unlock()
			return fmt.Errorf("failed to materialize into @base: %w", err)
		}

		// Shift rootfs UIDs for unprivileged containers (one-time cost)
		if err := ShiftTemplateRootfs(client, BaseTemplate); err != nil {
			slog.Warn("failed to shift template rootfs UIDs", "component", "sandbox", "error", err)
		}

		// Snapshot the new @base (captures the shifted UIDs)
		fmt.Println("Snapshotting new @base...")
		if err := client.CreateSnapshot(baseName, SnapshotName); err != nil {
			templateSnapshotMu.Unlock()
			return fmt.Errorf("failed to snapshot new @base: %w", err)
		}

		// Remount any overlays that depended on the old base snapshot
		snapPath := SnapshotRootfsPath(poolPath, BaseTemplate)
		if remountErr := RemountDependentOverlays(client, snapPath); remountErr != nil {
			slog.Warn("failed to remount dependent overlays after promote", "component", "sandbox", "error", remountErr)
		}

		templateSnapshotMu.Unlock()
	} else {
		// Traditional template with real Incus snapshot — use the old approach
		if !client.HasSnapshot(containerName, SnapshotName) {
			return fmt.Errorf("template %q has no snapshot; run 'astonish sandbox template snapshot %s' first", name, name)
		}

		templateSnapshotMu.Lock()

		// Delete the old @base
		if client.InstanceExists(baseName) {
			if err := client.StopAndDeleteInstance(baseName); err != nil {
				templateSnapshotMu.Unlock()
				return fmt.Errorf("failed to remove old @base: %w", err)
			}
		}

		// Clone from the promoted template's snapshot
		if err := client.CreateContainerFromSnapshot(baseName, name, nil); err != nil {
			templateSnapshotMu.Unlock()
			return fmt.Errorf("failed to clone promoted template to @base: %w", err)
		}

		// Shift rootfs UIDs for unprivileged containers (one-time cost)
		if err := ShiftTemplateRootfs(client, BaseTemplate); err != nil {
			slog.Warn("failed to shift template rootfs UIDs", "component", "sandbox", "error", err)
		}

		// Snapshot the new @base (captures the shifted UIDs)
		if err := client.CreateSnapshot(baseName, SnapshotName); err != nil {
			templateSnapshotMu.Unlock()
			return fmt.Errorf("failed to snapshot new @base: %w", err)
		}

		// Remount any overlays that depended on the old base snapshot
		snapPath := SnapshotRootfsPath(poolPath, BaseTemplate)
		if remountErr := RemountDependentOverlays(client, snapPath); remountErr != nil {
			slog.Warn("failed to remount dependent overlays after promote", "component", "sandbox", "error", remountErr)
		}

		templateSnapshotMu.Unlock()
	}

	// Update @base metadata
	baseMeta := registry.Get(BaseTemplate)
	if baseMeta == nil {
		baseMeta = &TemplateMeta{
			Name:      BaseTemplate,
			CreatedAt: time.Now(),
		}
	}

	baseMeta.SnapshotAt = time.Now()
	if hash, hashErr := ComputeBinaryHash(); hashErr == nil {
		baseMeta.BinaryHash = hash
	}
	if err := registry.Update(baseMeta); err != nil {
		return fmt.Errorf("failed to update base template metadata: %w", err)
	}

	fmt.Printf("@base has been replaced with the contents of %q.\n", name)
	return nil
}

// DeleteTemplate removes a template container, its overlay storage, and its metadata.
func DeleteTemplate(client *IncusClient, registry *TemplateRegistry, name string) error {
	if name == BaseTemplate {
		return fmt.Errorf("cannot delete the base template")
	}

	containerName := TemplateName(name)

	// Resolve pool path for overlay operations
	poolName, poolErr := GetPoolForProfile(client)
	var poolPath string
	if poolErr == nil {
		var pathErr error
		poolPath, pathErr = GetPoolSourcePath(client, poolName)
		if pathErr != nil {
			slog.Warn("failed to get pool source path", "component", "sandbox", "pool", poolName, "error", pathErr)
		}
	}

	// Unmount any active overlay before deleting
	if poolPath != "" && IsOverlayMounted(poolPath, containerName) {
		if err := UnmountSessionOverlay(poolPath, containerName); err != nil {
			slog.Warn("failed to unmount overlay for template", "component", "sandbox", "container", containerName, "error", err)
		}
	}

	if client.InstanceExists(containerName) {
		fmt.Printf("Destroying template container %q...\n", name)
		if err := client.StopAndDeleteInstance(containerName); err != nil {
			return fmt.Errorf("failed to destroy template container: %w", err)
		}
	}

	// Clean up overlay upper/work dirs (UnmountSessionOverlay already removes
	// session dirs, but for templates the overlay dir might still exist if
	// unmount was skipped or the template never had an active mount)
	overlayDir := OverlaySessionDir(containerName)
	if err := statOnSandboxHost(overlayDir); err == nil {
		fmt.Printf("Removing overlay storage for %q...\n", name)
		if err := removeAllOnSandboxHost(overlayDir); err != nil {
			slog.Warn("failed to remove overlay dir", "component", "sandbox", "dir", overlayDir, "error", err)
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
		// Re-mount overlay if this template is overlay-based (custom, not @base)
		meta := registry.Get(name)
		if meta != nil && meta.BasedOn != "" {
			if err := ensureOverlayMounted(client, containerName, meta.BasedOn, registry); err != nil {
				slog.Warn("failed to ensure overlay for template", "component", "sandbox", "template", name, "error", err)
			}
		}

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
				slog.Warn("could not update binary hash for template", "component", "sandbox", "template", name, "error", updateErr)
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

// CreateTemplateFromContainer creates a new template from a running session
// container. This is the wizard's "save_sandbox_template" operation.
//
// For overlay-based session containers, only the overlay upper layer (the
// session's writes/customizations) needs to be captured — not the full
// 900MB+ rootfs. The new template gets its own overlay container with the
// session's upper layer copied into it.
//
// sourceTemplate is the template that the session container was created from
// (e.g., "juicytrade" if the session used use_sandbox_template to switch).
// The new template's BasedOn field is set to this value so that the overlay
// chain correctly includes the source template's layers. If empty, defaults
// to BaseTemplate ("base").
//
// Steps:
//  1. Stop the session container
//  2. Create overlay container for the new template (from tiny image)
//  3. Copy the session's overlay upper layer to the template's overlay upper
//  4. Register the template (stacked overlay, no Incus snapshot needed)
//  5. Restart the session container
func CreateTemplateFromContainer(client *IncusClient, registry *TemplateRegistry, containerName, templateName, description, sourceTemplate string) error {
	if templateName == BaseTemplate {
		return fmt.Errorf("cannot create a template named %q (reserved)", BaseTemplate)
	}

	tplContainerName := TemplateName(templateName)
	if client.InstanceExists(tplContainerName) {
		return fmt.Errorf("template %q already exists", templateName)
	}

	// Default to @base if source template not specified
	if sourceTemplate == "" {
		sourceTemplate = BaseTemplate
	}

	// Resolve pool paths
	poolName, err := GetPoolForProfile(client)
	if err != nil {
		return fmt.Errorf("failed to determine storage pool: %w", err)
	}

	poolPath, err := GetPoolSourcePath(client, poolName)
	if err != nil {
		return fmt.Errorf("failed to get pool path: %w", err)
	}

	// Stop the session container for a consistent view.
	// We track whether we stopped it so we can restart it on ANY exit path.
	stoppedContainer := false
	if client.IsRunning(containerName) {
		slog.Info("stopping container for template creation", "component", "sandbox", "container", containerName)
		if err := client.StopInstance(containerName, false); err != nil {
			return fmt.Errorf("failed to stop container %q: %w", containerName, err)
		}
		stoppedContainer = true
	}

	// Ensure the session container is restarted on any failure or success.
	// This is critical — if we leave it stopped, the wizard session is dead.
	restartSession := func() {
		if !stoppedContainer {
			return
		}
		slog.Info("restarting session container", "component", "sandbox", "container", containerName)
		if startErr := client.StartInstance(containerName); startErr != nil {
			slog.Error("failed to restart session container", "component", "sandbox", "container", containerName, "error", startErr)
		}
	}

	// Create the template container from the tiny overlay image
	slog.Info("creating template container", "component", "sandbox", "container", tplContainerName)
	arch, err := client.ServerArchitecture()
	if err != nil {
		restartSession()
		return fmt.Errorf("failed to detect server architecture: %w", err)
	}

	tplCfg := containerSecurityConfig()
	tplCfg["security.nesting"] = "false"

	req := api.InstancesPost{
		Name: tplContainerName,
		Type: api.InstanceTypeContainer,
		InstancePut: api.InstancePut{
			Architecture: arch,
			Config:       tplCfg,
		},
		Source: api.InstanceSource{
			Type:  "image",
			Alias: OverlayImageAlias,
		},
	}

	op, err := client.server.CreateInstance(req)
	if err != nil {
		restartSession()
		return fmt.Errorf("failed to create template container: %w", err)
	}
	if err := op.Wait(); err != nil {
		restartSession()
		return fmt.Errorf("failed to wait for template container creation: %w", err)
	}

	// Copy the session's overlay upper layer to the template's overlay dir.
	// This captures just the session's customizations (typically a few MB — repos,
	// configs, installed packages), not the full 900MB+ base filesystem.
	sessionUpperDir := OverlayUpperDir(containerName)
	tplUpperDir := OverlayUpperDir(tplContainerName)

	if err := mkdirAllOnSandboxHost(filepath.Dir(tplUpperDir), 0755); err != nil {
		if delErr := client.DeleteInstance(tplContainerName); delErr != nil {
			slog.Warn("failed to delete template instance during rollback", "component", "sandbox", "container", tplContainerName, "error", delErr)
		}
		restartSession()
		return fmt.Errorf("failed to create template overlay dir: %w", err)
	}

	slog.Info("copying session overlay to template", "component", "sandbox", "template", tplContainerName)
	if err := cpOnSandboxHost(sessionUpperDir, tplUpperDir); err != nil {
		if delErr := client.DeleteInstance(tplContainerName); delErr != nil {
			slog.Warn("failed to delete template instance during rollback", "component", "sandbox", "container", tplContainerName, "error", delErr)
		}
		restartSession()
		return fmt.Errorf("failed to copy session overlay: %w", err)
	}

	// Also create the work dir for the template's overlay
	tplWorkDir := filepath.Join(overlayBaseDir, tplContainerName, "work")
	if err := mkdirAllOnSandboxHost(tplWorkDir, 0755); err != nil {
		slog.Warn("failed to create template work dir", "component", "sandbox", "error", err)
	}

	// Mount overlay on the template container so it can be started/shelled into.
	// The lower layers come from the source template that the session was based on.
	// For @base sessions, this is just the @base snapshot rootfs.
	// For custom template sessions (e.g., juicytrade), this includes the custom
	// template's upper dir stacked on top of @base — preserving all the files
	// from the parent template that the session inherited.
	lowerDir, err := ResolveLowerLayers(poolPath, sourceTemplate, registry)
	if err != nil {
		if delErr := client.DeleteInstance(tplContainerName); delErr != nil {
			slog.Warn("failed to delete template instance during rollback", "component", "sandbox", "container", tplContainerName, "error", delErr)
		}
		restartSession()
		return fmt.Errorf("failed to resolve lower layers for source template %q: %w", sourceTemplate, err)
	}
	// Mount overlay on the template container's rootfs.
	// For unprivileged containers, this mounts a plain overlay and pre-seeds
	// Incus's idmap state (template rootfs is already shifted at snapshot time).
	tplRootfs := ContainerRootfsPath(poolPath, tplContainerName)
	if err := setupUnprivilegedOverlay(client, tplContainerName, tplRootfs, lowerDir); err != nil {
		if delErr := client.DeleteInstance(tplContainerName); delErr != nil {
			slog.Warn("failed to delete template instance during rollback", "component", "sandbox", "container", tplContainerName, "error", delErr)
		}
		restartSession()
		return fmt.Errorf("failed to mount overlay on template: %w", err)
	}

	// Compute binary hash
	hash, hashErr := ComputeBinaryHash()
	if hashErr != nil {
		slog.Warn("failed to compute binary hash for template metadata", "component", "sandbox", "error", hashErr)
	}

	// Register in the template registry (overlay-based, no Incus snapshot)
	now := time.Now()
	meta := &TemplateMeta{
		Name:        templateName,
		Description: description,
		CreatedAt:   now,
		SnapshotAt:  now,
		BasedOn:     sourceTemplate,
		BinaryHash:  hash,
	}

	if err := registry.Add(meta); err != nil {
		restartSession()
		return fmt.Errorf("failed to register template: %w", err)
	}

	// Success path — restart session container
	restartSession()

	slog.Info("template created from container", "component", "sandbox", "template", templateName, "container", containerName)
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
			slog.Warn("could not compute binary hash for refresh check", "component", "sandbox", "error", err)
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

			slog.Info("template has stale binary, refreshing", "component", "sandbox", "template", meta.Name)
			if err := RefreshTemplate(client, registry, meta.Name); err != nil {
				slog.Warn("failed to refresh template", "component", "sandbox", "template", meta.Name, "error", err)
			} else {
				slog.Info("template refreshed with current binary", "component", "sandbox", "template", meta.Name)
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
			slog.Warn("failed to refresh template", "template", meta.Name, "error", err)
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

// pushAstonishBinary copies the astonish binary into a container.
// The container must be running. The binary is placed at /usr/local/bin/astonish
// with executable permissions (0755).
//
// On Linux native: pushes the running host binary directly (same arch).
// On Docker+Incus: the Linux binary is pre-baked into the Docker image at
// /usr/local/bin/astonish. We copy it from the Docker container into the
// Incus template via the Incus API. For dev builds where the baked binary
// may be stale, the developer uses `sandbox refresh` after cross-compiling.
func pushAstonishBinary(client *IncusClient, containerName string) error {
	if activePlatform == PlatformDockerIncus {
		return pushAstonishBinaryFromDocker(client, containerName)
	}
	return pushAstonishBinaryFromHost(client, containerName)
}

// pushAstonishBinaryFromHost pushes the running host binary into a container.
// Used on Linux native where host and container share the same architecture.
func pushAstonishBinaryFromHost(client *IncusClient, containerName string) error {
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

	return verifyBinaryInContainer(client, containerName)
}

// pushAstonishBinaryFromDocker copies the pre-baked Linux binary from the
// astonish-incus Docker container into an Incus template container.
// The binary in the Docker image was built for linux/amd64 during the release.
func pushAstonishBinaryFromDocker(client *IncusClient, containerName string) error {
	// The binary is at /usr/local/bin/astonish inside the Docker container.
	// We use docker exec to read it and pipe it into the Incus container via PushFile.
	fmt.Println("  Binary: from Docker image (pre-baked linux binary)")

	// Read the binary from the Docker container into memory.
	// The binary is typically ~50-80 MB so this is acceptable.
	binaryData, err := ExecInDockerHost([]string{"cat", BinaryDestPath})
	if err != nil {
		return fmt.Errorf("failed to read binary from Docker container: %w", err)
	}

	if len(binaryData) == 0 {
		return fmt.Errorf("binary in Docker container is empty (image may be corrupted)")
	}

	fmt.Printf("  Size: %.1f MB\n", float64(len(binaryData))/(1024*1024))

	// Push the binary into the Incus template container via the Incus API
	reader := bytes.NewReader(binaryData)
	if err := client.PushFile(containerName, BinaryDestPath, reader, 0755); err != nil {
		return fmt.Errorf("failed to push binary to container: %w", err)
	}

	return verifyBinaryInContainer(client, containerName)
}

// verifyBinaryInContainer checks that the pushed binary is executable inside the container.
func verifyBinaryInContainer(client *IncusClient, containerName string) error {
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
		// Inside Docker, the auto-detection of unused subnets often fails
		// because of the limited network namespace. Use a static subnet
		// that won't conflict with Docker's default ranges (172.17-31.x.x).
		ipv4Address := "auto"
		if client.platform == PlatformDockerIncus {
			ipv4Address = "10.99.0.1/24"
		}

		fmt.Printf("Creating network bridge %s...\n", networkName)
		err = server.CreateNetwork(api.NetworksPost{
			Name: networkName,
			Type: "bridge",
			NetworkPut: api.NetworkPut{
				Config: map[string]string{
					"ipv4.address": ipv4Address,
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
