package incus

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"

	incusclient "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/sandbox/tmplmeta"
)

// OverlayImageAlias is the alias for the minimal shell image used for
// overlay-based session containers. This image contains only the bare
// minimum metadata (hostname/hosts templates) — the real filesystem
// comes from the overlayfs mount backed by the template snapshot.
const OverlayImageAlias = "astonish-overlay-base"

// OverlayBaseDir is the directory where per-session overlay upper/work dirs live.
const OverlayBaseDir = "/var/lib/incus/disks/astonish-overlays"

// overlayBaseDir is retained as an unexported alias for readability in-package.
const overlayBaseDir = OverlayBaseDir

// OverlaySessionDir returns the per-session overlay storage directory.
func OverlaySessionDir(sessionID string) string {
	return filepath.Join(overlayBaseDir, sessionID)
}

// OverlayUpperDir returns the overlay upper directory path for a container.
// This is where all writes from the container go. For custom templates,
// this directory IS the template's state — it becomes a lower layer for sessions.
func OverlayUpperDir(containerName string) string {
	return filepath.Join(overlayBaseDir, containerName, "upper")
}

// ResolveLowerLayers returns the colon-separated lowerdir string for mounting
// an overlay on top of a template. Overlayfs supports multiple lower layers
// (colon-separated, leftmost = highest priority).
//
//   - @base template: single lower layer = @base snapshot rootfs
//   - Custom template (BasedOn=@base): stacked = template-upper:@base-snapshot-rootfs
//     (template customizations on top, @base OS underneath)
func ResolveLowerLayers(poolPath, templateName string, registry *tmplmeta.TemplateRegistry) (string, error) {
	return resolveLowerLayersInner(poolPath, templateName, registry, 0)
}

// maxTemplateChainDepth prevents infinite recursion from circular BasedOn chains.
const maxTemplateChainDepth = 10

func resolveLowerLayersInner(poolPath, templateName string, registry *tmplmeta.TemplateRegistry, depth int) (string, error) {
	if depth > maxTemplateChainDepth {
		return "", fmt.Errorf("template chain exceeds maximum depth (%d) — possible circular BasedOn reference", maxTemplateChainDepth)
	}

	// For @base, just return the snapshot rootfs path
	if templateName == BaseTemplate {
		lower := SnapshotRootfsPath(poolPath, BaseTemplate)
		if err := StatOnSandboxHost(lower); err != nil {
			return "", fmt.Errorf("@base snapshot rootfs not found at %s: %w", lower, err)
		}
		return lower, nil
	}

	// For custom templates, look up what they're based on
	if registry == nil {
		return "", fmt.Errorf("template registry is nil; cannot resolve custom template %q", templateName)
	}
	meta := registry.Get(templateName)
	if meta == nil {
		return "", fmt.Errorf("template %q not found in registry", templateName)
	}

	// Check if this custom template has a real Incus snapshot (materialized)
	tplContainerName := TemplateName(templateName)
	snapshotRootfs := SnapshotRootfsPath(poolPath, templateName)
	if err := StatOnSandboxHost(snapshotRootfs); err == nil {
		// Template has a real snapshot — use it directly (like @base)
		return snapshotRootfs, nil
	}

	// No snapshot — this is an overlay-based custom template.
	// Stack: template-upper on top of the base it's derived from.
	tplUpperDir := OverlayUpperDir(tplContainerName)
	if err := StatOnSandboxHost(tplUpperDir); err != nil {
		return "", fmt.Errorf("template %q overlay upper dir not found at %s: %w", templateName, tplUpperDir, err)
	}

	// Resolve the base template's lower layers (recursive for deep chains)
	basedOn := meta.BasedOn
	if basedOn == "" {
		basedOn = BaseTemplate
	}

	baseLower, err := resolveLowerLayersInner(poolPath, basedOn, registry, depth+1)
	if err != nil {
		return "", fmt.Errorf("failed to resolve base %q for template %q: %w", basedOn, templateName, err)
	}

	// Stack: template upper (highest priority) : base layers
	return tplUpperDir + ":" + baseLower, nil
}

// GetPoolSourcePath returns the filesystem path of a storage pool's root
// where container rootfs directories actually reside.
//
// For "dir" backend pools the source config value is the directory itself.
// For CoW backends (btrfs, zfs) the source is typically a loop-device image
// file (e.g. /var/lib/incus/disks/default.img) which is NOT a directory —
// the actual mount point is /var/lib/incus/storage-pools/<poolName>.
func GetPoolSourcePath(client *IncusClient, poolName string) (string, error) {
	pool, _, err := client.Server().GetStoragePool(poolName)
	if err != nil {
		return "", fmt.Errorf("failed to get storage pool %q: %w", poolName, err)
	}

	// For dir-backend pools, the source config key IS the directory.
	if pool.Driver == "dir" {
		if src, ok := pool.Config["source"]; ok && src != "" {
			return src, nil
		}
	}

	// For all other backends (btrfs, zfs, lvm, ceph, etc.) the source is
	// a block device or image file, not the directory tree. Incus mounts
	// the pool's filesystem at the canonical path below.
	return filepath.Join("/var/lib/incus/storage-pools", poolName), nil
}

// GetPoolForProfile returns the storage pool name from the default profile's root disk.
func GetPoolForProfile(client *IncusClient) (string, error) {
	profile, _, err := client.Server().GetProfile("default")
	if err != nil {
		return "", fmt.Errorf("failed to get default profile: %w", err)
	}

	if root, ok := profile.Devices["root"]; ok {
		if pool, ok := root["pool"]; ok {
			return pool, nil
		}
	}

	return "", fmt.Errorf("default profile has no root disk device with a pool")
}

// SnapshotRootfsPath returns the filesystem path to a template snapshot's rootfs.
func SnapshotRootfsPath(poolSourcePath, templateName string) string {
	return filepath.Join(poolSourcePath, "containers-snapshots", TemplateName(templateName), SnapshotName, "rootfs")
}

// ContainerRootfsPath returns the filesystem path to a container's rootfs.
func ContainerRootfsPath(poolSourcePath, containerName string) string {
	return filepath.Join(poolSourcePath, "containers", containerName, "rootfs")
}

// EnsureOverlayImage creates and imports the tiny shell image into Incus
// if it doesn't already exist. This image is ~670 bytes and contains only
// the metadata Incus needs to create a container (hostname/hosts templates).
// The real filesystem comes from overlayfs.
func EnsureOverlayImage(client *IncusClient) error {
	// Check if already imported
	_, _, err := client.Server().GetImageAlias(OverlayImageAlias)
	if err == nil {
		return nil // already exists
	}

	// Get the server's architecture so the image metadata matches
	arch, err := client.ServerArchitecture()
	if err != nil {
		return fmt.Errorf("failed to detect server architecture for overlay image: %w", err)
	}

	fmt.Println("Creating overlay shell image...")

	tarball, err := buildOverlayImageTarball(arch)
	if err != nil {
		return fmt.Errorf("failed to build overlay image: %w", err)
	}

	// Import the image using the unified tarball (meta + rootfs in one file)
	imageReq := api.ImagesPost{}
	args := &incusclient.ImageCreateArgs{
		MetaFile: bytes.NewReader(tarball),
		MetaName: "astonish-overlay-base.tar.gz",
	}

	op, err := client.Server().CreateImage(imageReq, args)
	if err != nil {
		return fmt.Errorf("failed to import overlay image: %w", err)
	}

	if err := op.Wait(); err != nil {
		return fmt.Errorf("failed to wait for overlay image import: %w", err)
	}

	// Get the fingerprint from the operation
	opAPI := op.Get()
	fingerprint := ""
	if opAPI.Metadata != nil {
		if fp, ok := opAPI.Metadata["fingerprint"]; ok {
			if s, ok := fp.(string); ok {
				fingerprint = s
			}
		}
	}

	if fingerprint == "" {
		return fmt.Errorf("overlay image import succeeded but no fingerprint returned")
	}

	// Create the alias
	aliasReq := api.ImageAliasesPost{
		ImageAliasesEntry: api.ImageAliasesEntry{
			Name: OverlayImageAlias,
			ImageAliasesEntryPut: api.ImageAliasesEntryPut{
				Description: "Astonish overlay base (minimal shell for overlay containers)",
				Target:      fingerprint,
			},
		},
	}

	if err := client.Server().CreateImageAlias(aliasReq); err != nil {
		return fmt.Errorf("failed to create image alias %q: %w", OverlayImageAlias, err)
	}

	fmt.Println("Overlay shell image ready.")
	return nil
}

// buildOverlayImageTarball creates a minimal Incus image tarball in memory.
// This is a unified image (metadata.yaml + rootfs/ in a single tarball).
// The architecture parameter should match the Incus server's architecture
// (e.g., "x86_64", "aarch64") to avoid mismatches on cross-arch setups.
func buildOverlayImageTarball(architecture string) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// metadata.yaml
	metadata := fmt.Sprintf(`architecture: %s
creation_date: 1700000000
properties:
  architecture: %s
  description: Astonish overlay base (minimal)
  os: ubuntu
  release: noble
templates:
  /etc/hostname:
    when: [create, copy]
    template: hostname.tpl
  /etc/hosts:
    when: [create, copy]
    template: hosts.tpl
`, architecture, architecture)
	if err := addTarFile(tw, "metadata.yaml", []byte(metadata), 0644); err != nil {
		return nil, err
	}

	// Template files
	if err := addTarFile(tw, "templates/hostname.tpl", []byte("{{ container.name }}\n"), 0644); err != nil {
		return nil, err
	}

	hostsTpl := `127.0.0.1   localhost
127.0.1.1   {{ container.name }}
::1     localhost ip6-localhost ip6-loopback
ff02::1 ip6-allnodes
ff02::2 ip6-allrouters
`
	if err := addTarFile(tw, "templates/hosts.tpl", []byte(hostsTpl), 0644); err != nil {
		return nil, err
	}

	// Minimal rootfs directories that Incus expects
	dirs := []string{
		"rootfs/",
		"rootfs/dev/",
		"rootfs/proc/",
		"rootfs/sys/",
		"rootfs/tmp/",
		"rootfs/etc/",
		"rootfs/bin/",
		"rootfs/usr/",
		"rootfs/usr/bin/",
		"rootfs/usr/local/",
		"rootfs/usr/local/bin/",
	}
	for _, d := range dirs {
		if err := addTarDir(tw, d, 0755); err != nil {
			return nil, err
		}
	}

	// Minimal /etc files
	if err := addTarFile(tw, "rootfs/etc/hostname", []byte("astonish\n"), 0644); err != nil {
		return nil, err
	}
	if err := addTarFile(tw, "rootfs/etc/hosts", []byte("127.0.0.1 localhost\n"), 0644); err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// addTarFile adds a regular file to a tar archive.
func addTarFile(tw *tar.Writer, name string, data []byte, mode int64) error {
	hdr := &tar.Header{
		Name: name,
		Mode: mode,
		Size: int64(len(data)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// addTarDir adds a directory entry to a tar archive.
func addTarDir(tw *tar.Writer, name string, mode int64) error {
	hdr := &tar.Header{
		Typeflag: tar.TypeDir,
		Name:     name,
		Mode:     mode,
	}
	return tw.WriteHeader(hdr)
}

// CreateOverlayContainer creates a lightweight Incus container from the
// tiny shell image and mounts an overlayfs on its rootfs backed by the
// template's lower layers. This replaces CreateContainerFromSnapshot.
//
// Steps:
//  1. Create container from the overlay shell image (metadata only, ~45ms)
//  2. Mount overlayfs: lower=resolved template layers, upper=per-container dir
//  3. The container's rootfs now shows the full template filesystem
//
// The container is NOT started — caller should start it after this returns.
// Pass registry=nil when the template is @base (no registry lookup needed).
func CreateOverlayContainer(client *IncusClient, containerName, templateName string, registry *tmplmeta.TemplateRegistry, limits *config.SandboxLimits) error {
	return CreateOverlayContainerWithProfiles(client, containerName, templateName, registry, limits, nil)
}

// CreateOverlayContainerWithProfiles creates an overlay container with optional
// Incus profile overrides. If profiles is nil or empty, the default profile is
// used. In platform mode, pass the org profile to attach the container to the
// org's isolated bridge network.
func CreateOverlayContainerWithProfiles(client *IncusClient, containerName, templateName string, registry *tmplmeta.TemplateRegistry, limits *config.SandboxLimits, profiles []string) error {
	// Resolve the storage pool and paths
	poolName, err := GetPoolForProfile(client)
	if err != nil {
		return fmt.Errorf("failed to determine storage pool: %w", err)
	}

	poolPath, err := GetPoolSourcePath(client, poolName)
	if err != nil {
		return fmt.Errorf("failed to get pool source path: %w", err)
	}

	// Resolve the lower layers for this template
	lowerDir, err := ResolveLowerLayers(poolPath, templateName, registry)
	if err != nil {
		return fmt.Errorf("failed to resolve lower layers for template %q: %w", templateName, err)
	}

	// Determine if nesting is required by walking the template chain.
	// Custom templates inherit nesting from their base.
	nesting := resolveNesting(templateName, registry)

	// Get the server's architecture to set explicitly on the container.
	// On Docker+Incus (macOS ARM64 host with amd64 container), Incus
	// may default to the wrong architecture if not specified.
	arch, err := client.ServerArchitecture()
	if err != nil {
		return fmt.Errorf("failed to detect server architecture: %w", err)
	}

	// Create the container from the tiny overlay image
	containerConfig := containerSecurityConfig()
	containerConfig["security.nesting"] = fmt.Sprintf("%t", nesting)
	// Apply resource limits if provided (session containers only, not templates).
	// Skip on Docker+Incus — cgroup controller delegation inside Docker Desktop's
	// VM is unreliable. Setting limits.memory/cpu/processes requires cgroup
	// controllers that may not be delegatable, causing forkstart to fail with
	// "Device or resource busy" when LXC tries to enable them.
	if limits != nil && GetActivePlatform() != PlatformDockerIncus {
		if limits.Memory != "" {
			containerConfig["limits.memory"] = limits.Memory
		}
		if limits.CPU > 0 {
			containerConfig["limits.cpu"] = strconv.Itoa(limits.CPU)
		}
		if limits.Processes > 0 {
			containerConfig["limits.processes"] = strconv.Itoa(limits.Processes)
		}
	}

	req := api.InstancesPost{
		Name: containerName,
		Type: api.InstanceTypeContainer,
		InstancePut: api.InstancePut{
			Architecture: arch,
			Config:       containerConfig,
			Profiles:     profiles,
		},
		Source: api.InstanceSource{
			Type:  "image",
			Alias: OverlayImageAlias,
		},
	}

	op, err := client.Server().CreateInstance(req)
	if err != nil {
		return fmt.Errorf("failed to create overlay container %q: %w", containerName, err)
	}

	if err := op.Wait(); err != nil {
		return fmt.Errorf("failed to wait for overlay container %q creation: %w", containerName, err)
	}

	// Mount overlayfs on the container's rootfs.
	// For unprivileged containers, this mounts a plain overlay and pre-seeds
	// Incus's idmap state (template rootfs is already shifted at snapshot time).
	// For privileged containers, this is a plain mount (no shift needed).
	containerRootfs := ContainerRootfsPath(poolPath, containerName)
	if err := setupUnprivilegedOverlay(client, containerName, containerRootfs, lowerDir); err != nil {
		client.DeleteInstance(containerName)
		return fmt.Errorf("failed to mount overlay for %q: %w", containerName, err)
	}

	return nil
}

// resolveNesting determines whether containers based on the given template
// need security.nesting enabled. It walks the template chain (custom → base)
// and returns true if any template in the chain has Nesting set.
func resolveNesting(templateName string, registry *tmplmeta.TemplateRegistry) bool {
	if registry == nil {
		return false
	}

	// Check the template itself
	meta := registry.Get(templateName)
	if meta != nil && meta.Nesting {
		return true
	}

	// Check the parent (base) template if this is a custom template
	if meta != nil && meta.BasedOn != "" {
		parent := registry.Get(meta.BasedOn)
		if parent != nil && parent.Nesting {
			return true
		}
	}

	// For "base" template or if no meta found, check the base directly
	if templateName != BaseTemplate {
		base := registry.Get(BaseTemplate)
		if base != nil && base.Nesting {
			return true
		}
	}

	return false
}

// UnmountSessionOverlay unmounts the overlayfs from a container's rootfs
// and removes the per-session overlay directories.
// On Docker+Incus, these operations run inside the Docker container.
func UnmountSessionOverlay(poolSourcePath, containerName string) error {
	containerRootfs := ContainerRootfsPath(poolSourcePath, containerName)

	// Unmount overlayfs (ignore error if not mounted)
	if err := UmountOnSandboxHost(containerRootfs); err != nil {
		// Not fatal — might not be overlay-mounted (old container, or already unmounted)
		slog.Warn("failed to unmount overlay", "component", "sandbox", "mountpoint", containerRootfs, "error", err)
	}

	// Remove per-session overlay dirs
	sessionDir := filepath.Join(overlayBaseDir, containerName)
	if err := RemoveAllOnSandboxHost(sessionDir); err != nil {
		slog.Warn("failed to clean up overlay dir", "component", "sandbox", "dir", sessionDir, "error", err)
	}

	return nil
}

// IsOverlayMounted checks if a container's rootfs has an overlayfs mounted on it.
// On Docker+Incus, reads /proc/mounts from inside the Docker container.
func IsOverlayMounted(poolSourcePath, containerName string) bool {
	containerRootfs := ContainerRootfsPath(poolSourcePath, containerName)
	return IsOverlayMountedOnSandboxHost(containerRootfs)
}

// MergeOverlayToRootfs copies the merged overlay view into a flat directory.
// Used when creating a template from a running session container — we need
// to materialize the overlay into a real rootfs for the template snapshot.
// On Docker+Incus, these operations run inside the Docker container.
func MergeOverlayToRootfs(poolSourcePath, containerName, destRootfs string) error {
	containerRootfs := ContainerRootfsPath(poolSourcePath, containerName)

	if !IsOverlayMounted(poolSourcePath, containerName) {
		// Not overlay-mounted — just copy directly
		if err := CpOnSandboxHost(containerRootfs+"/.", destRootfs+"/"); err != nil {
			return fmt.Errorf("failed to copy rootfs: %w", err)
		}
		return nil
	}

	// The overlay is mounted — the merged view at containerRootfs IS what we want.
	// rsync is more efficient than cp -a for large trees.
	if err := RsyncOnSandboxHost(containerRootfs+"/", destRootfs+"/"); err != nil {
		return fmt.Errorf("failed to rsync overlay rootfs: %w", err)
	}

	return nil
}

// CleanupAllOverlays unmounts all overlay mounts and removes the overlay base directory.
// Used during sandbox reset or cleanup.
// On Docker+Incus, these operations run inside the Docker container.
func CleanupAllOverlays() {
	entries, err := ReadDirOnSandboxHost(overlayBaseDir)
	if err != nil {
		return // directory doesn't exist or can't be read
	}

	for _, entryName := range entries {
		// ReadDirOnSandboxHost may include trailing "/" for dirs
		name := filepath.Base(entryName)
		if name == "" || name == "." {
			continue
		}

		// Try to find and unmount the corresponding container rootfs
		// We don't know the pool path here, so just try to unmount by reading /proc/mounts
		data, readErr := ReadMountsOnSandboxHost()
		if readErr != nil {
			slog.Warn("failed to read mounts during overlay cleanup", "component", "sandbox", "error", readErr)
		}
		for _, line := range bytes.Split(data, []byte("\n")) {
			fields := bytes.Fields(line)
			if len(fields) >= 3 && string(fields[2]) == "overlay" {
				mountpoint := string(fields[1])
				// Check if this mount's workdir points to our session dir
				if bytes.Contains(line, []byte(filepath.Join(overlayBaseDir, name))) {
					if err := UmountOnSandboxHost(mountpoint); err != nil {
						slog.Warn("failed to unmount overlay during cleanup", "component", "sandbox", "mountpoint", mountpoint, "error", err)
					}
				}
			}
		}
	}

	if err := RemoveAllOnSandboxHost(overlayBaseDir); err != nil {
		slog.Warn("failed to remove overlay base directory", "component", "sandbox", "path", overlayBaseDir, "error", err)
	}
}

// DependentOverlay describes an overlay mount that depends on a template
// snapshot and may need to be stopped/remounted when the snapshot is recreated.
type DependentOverlay struct {
	Mountpoint    string
	ContainerName string
	Opts          string
	WasRunning    bool
}

// FindDependentOverlays enumerates all overlay mounts whose lowerdir includes
// the given snapshotPath. Returns the list of affected mounts for subsequent
// stop/remount/restart operations.
func FindDependentOverlays(snapshotPath string) ([]*DependentOverlay, error) {
	data, err := ReadMountsOnSandboxHost()
	if err != nil {
		return nil, fmt.Errorf("failed to read mounts: %w", err)
	}

	var affected []*DependentOverlay

	for _, line := range bytes.Split(data, []byte("\n")) {
		fields := bytes.Fields(line)
		if len(fields) < 4 {
			continue
		}

		mountpoint := string(fields[1])
		fstype := string(fields[2])
		opts := string(fields[3])

		if fstype != "overlay" {
			continue
		}

		if !strings.Contains(opts, snapshotPath) {
			continue
		}

		containerName := containerNameFromRootfs(mountpoint)
		if containerName == "" {
			continue
		}

		affected = append(affected, &DependentOverlay{
			Mountpoint:    mountpoint,
			ContainerName: containerName,
			Opts:          opts,
		})
	}

	return affected, nil
}

// StopDependentContainers force-stops all running containers in the dependent
// overlay list. This MUST be called BEFORE deleting a snapshot to prevent
// containers from seeing a stale overlay (empty lowerdir) during the window
// between snapshot deletion and remount.
func StopDependentContainers(client *IncusClient, overlays []*DependentOverlay) {
	for _, m := range overlays {
		if m.ContainerName != "" && client.IsRunning(m.ContainerName) {
			slog.Info("stopping container for overlay remount", "component", "sandbox", "container", m.ContainerName)
			if err := client.StopInstance(m.ContainerName, true); err != nil {
				slog.Warn("failed to stop container for overlay remount", "component", "sandbox", "container", m.ContainerName, "error", err)
				continue
			}
			m.WasRunning = true
		}
	}
}

// RemountOverlays unmounts stale overlays and remounts them with fresh inode
// references. This MUST be called AFTER the new snapshot is created so the
// kernel resolves the lowerdir path to the new inode.
//
// For unprivileged containers, the remounted overlay also needs the Incus
// idmap state re-seeded so the container can start without a slow shift pass.
func RemountOverlays(client *IncusClient, overlays []*DependentOverlay) {
	for _, m := range overlays {
		if err := UmountOnSandboxHost(m.Mountpoint); err != nil {
			slog.Warn("failed to unmount stale overlay", "component", "sandbox", "mountpoint", m.Mountpoint, "error", err)
			continue
		}

		// Parse original mount options to get the paths for remount
		lowerDir := extractMountOpt(m.Opts, "lowerdir")
		upperDir := extractMountOpt(m.Opts, "upperdir")
		workDir := extractMountOpt(m.Opts, "workdir")

		if lowerDir == "" || upperDir == "" || workDir == "" {
			slog.Warn("could not parse overlay opts, skipping remount", "component", "sandbox", "mountpoint", m.Mountpoint)
			continue
		}

		// Overlayfs requires a clean workdir after remount — recreate it
		if err := RemoveAllOnSandboxHost(workDir); err != nil {
			slog.Warn("failed to clean workdir", "component", "sandbox", "workdir", workDir, "error", err)
		}
		if err := MkdirAllOnSandboxHost(workDir, 0755); err != nil {
			slog.Warn("failed to recreate workdir", "component", "sandbox", "workdir", workDir, "error", err)
			continue
		}

		// Remount overlay with same options
		newOpts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", lowerDir, upperDir, workDir)
		if err := MountOverlayOnSandboxHost(newOpts, m.Mountpoint); err != nil {
			slog.Error("failed to remount overlay", "component", "sandbox", "mountpoint", m.Mountpoint, "error", err)
			continue
		}

		// For unprivileged containers, re-seed the idmap state so the
		// container can start without a slow shift pass.
		if !IsPrivileged() {
			if err := reshiftOverlayUIDs(client, m.ContainerName); err != nil {
				slog.Warn("failed to re-shift UIDs after remount", "component", "sandbox",
					"container", m.ContainerName, "error", err)
			}
		}

		slog.Info("remounted overlay", "component", "sandbox", "container", m.ContainerName)
	}
}

// RestartDependentContainers restarts containers that were previously stopped
// for overlay remounting. Only containers marked WasRunning are restarted.
func RestartDependentContainers(client *IncusClient, overlays []*DependentOverlay) {
	for _, m := range overlays {
		if m.WasRunning && m.ContainerName != "" {
			slog.Info("restarting container after overlay remount", "component", "sandbox", "container", m.ContainerName)
			if err := client.StartInstance(m.ContainerName); err != nil {
				slog.Warn("failed to restart container after overlay remount", "component", "sandbox", "container", m.ContainerName, "error", err)
			}
		}
	}
}

// RemountDependentOverlays finds all overlay mounts whose lowerdir includes
// the given path and remounts them. This is the legacy all-in-one function
// that combines Find + Stop + Remount + Restart.
//
// DEPRECATED: New code should use the composable phase functions directly
// (FindDependentOverlays, StopDependentContainers, RemountOverlays,
// RestartDependentContainers) to ensure containers are stopped BEFORE
// snapshot deletion, eliminating the stale-overlay race window.
//
// This MUST be called while templateSnapshotMu is held (write lock) to prevent
// new overlay mounts from being created between the snapshot swap and remount.
func RemountDependentOverlays(client *IncusClient, snapshotPath string) error {
	overlays, err := FindDependentOverlays(snapshotPath)
	if err != nil {
		return err
	}

	if len(overlays) == 0 {
		return nil
	}

	slog.Info("remounting overlays after snapshot recreation", "component", "sandbox", "count", len(overlays), "snapshot_path", snapshotPath)

	StopDependentContainers(client, overlays)
	RemountOverlays(client, overlays)
	RestartDependentContainers(client, overlays)

	return nil
}

// containerNameFromRootfs extracts the Incus container name from a rootfs
// mountpoint path like ".../containers/<name>/rootfs".
func containerNameFromRootfs(mountpoint string) string {
	// Split the path and find the segment after "containers"
	parts := strings.Split(mountpoint, "/")
	for i, p := range parts {
		if p == "containers" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// extractMountOpt extracts a named option value from a comma-separated
// mount options string. For example, extractMountOpt("rw,lowerdir=/a:/b,upperdir=/c", "lowerdir")
// returns "/a:/b".
func extractMountOpt(opts, key string) string {
	prefix := key + "="
	for _, part := range strings.Split(opts, ",") {
		if strings.HasPrefix(part, prefix) {
			return part[len(prefix):]
		}
	}
	return ""
}

// EnsureOverlayBaseDir creates the overlay base directory if it doesn't exist.
// On Docker+Incus, this creates the directory inside the Docker container.
func EnsureOverlayBaseDir() error {
	return MkdirAllOnSandboxHost(overlayBaseDir, 0755)
}
