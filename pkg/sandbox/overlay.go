package sandbox

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"

	incus "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
	"github.com/schardosin/astonish/pkg/config"
)

// OverlayImageAlias is the alias for the minimal shell image used for
// overlay-based session containers. This image contains only the bare
// minimum metadata (hostname/hosts templates) — the real filesystem
// comes from the overlayfs mount backed by the template snapshot.
const OverlayImageAlias = "astonish-overlay-base"

// overlayBaseDir is the directory where per-session overlay upper/work dirs live.
const overlayBaseDir = "/var/lib/incus/disks/astonish-overlays"

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
func ResolveLowerLayers(poolPath, templateName string, registry *TemplateRegistry) (string, error) {
	// For @base, just return the snapshot rootfs path
	if templateName == BaseTemplate {
		lower := SnapshotRootfsPath(poolPath, BaseTemplate)
		if err := statOnSandboxHost(lower); err != nil {
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
	if err := statOnSandboxHost(snapshotRootfs); err == nil {
		// Template has a real snapshot — use it directly (like @base)
		return snapshotRootfs, nil
	}

	// No snapshot — this is an overlay-based custom template.
	// Stack: template-upper on top of the base it's derived from.
	tplUpperDir := OverlayUpperDir(tplContainerName)
	if err := statOnSandboxHost(tplUpperDir); err != nil {
		return "", fmt.Errorf("template %q overlay upper dir not found at %s: %w", templateName, tplUpperDir, err)
	}

	// Resolve the base template's lower layers (recursive for deep chains)
	basedOn := meta.BasedOn
	if basedOn == "" {
		basedOn = BaseTemplate
	}

	baseLower, err := ResolveLowerLayers(poolPath, basedOn, registry)
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
	pool, _, err := client.server.GetStoragePool(poolName)
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
	profile, _, err := client.server.GetProfile("default")
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
	_, _, err := client.server.GetImageAlias(OverlayImageAlias)
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
	args := &incus.ImageCreateArgs{
		MetaFile: bytes.NewReader(tarball),
		MetaName: "astonish-overlay-base.tar.gz",
	}

	op, err := client.server.CreateImage(imageReq, args)
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

	if err := client.server.CreateImageAlias(aliasReq); err != nil {
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
func CreateOverlayContainer(client *IncusClient, containerName, templateName string, registry *TemplateRegistry, limits *config.SandboxLimits) error {
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
	if limits != nil && activePlatform != PlatformDockerIncus {
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
		},
		Source: api.InstanceSource{
			Type:  "image",
			Alias: OverlayImageAlias,
		},
	}

	op, err := client.server.CreateInstance(req)
	if err != nil {
		return fmt.Errorf("failed to create overlay container %q: %w", containerName, err)
	}

	if err := op.Wait(); err != nil {
		return fmt.Errorf("failed to wait for overlay container %q creation: %w", containerName, err)
	}

	// Mount overlayfs on the container's rootfs.
	// For unprivileged containers, this creates idmapped bind mounts of the
	// underlying layers and mounts the overlay on top of them, then pre-seeds
	// the Incus idmap state. For privileged containers, this is a plain mount.
	containerRootfs := ContainerRootfsPath(poolPath, containerName)
	if err := setupIdmappedOverlay(client, containerName, containerRootfs, lowerDir); err != nil {
		client.DeleteInstance(containerName)
		return fmt.Errorf("failed to mount overlay for %q: %w", containerName, err)
	}

	return nil
}

// resolveNesting determines whether containers based on the given template
// need security.nesting enabled. It walks the template chain (custom → base)
// and returns true if any template in the chain has Nesting set.
func resolveNesting(templateName string, registry *TemplateRegistry) bool {
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

// MountOverlay mounts an overlayfs on a container's rootfs directory with
// pre-resolved lower layers. The lowerDir can be a single path or a
// colon-separated list of paths for stacked overlays.
// On Docker+Incus, the mount command runs inside the Docker container.
func MountOverlay(poolSourcePath, containerName, lowerDir string) error {
	containerRootfs := ContainerRootfsPath(poolSourcePath, containerName)

	// Create per-container overlay dirs
	sessionDir := filepath.Join(overlayBaseDir, containerName)
	upperDir := filepath.Join(sessionDir, "upper")
	workDir := filepath.Join(sessionDir, "work")

	if err := mkdirAllOnSandboxHost(upperDir, 0755); err != nil {
		return fmt.Errorf("failed to create overlay upper dir: %w", err)
	}
	if err := mkdirAllOnSandboxHost(workDir, 0755); err != nil {
		return fmt.Errorf("failed to create overlay work dir: %w", err)
	}

	// Mount overlayfs
	opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", lowerDir, upperDir, workDir)
	return mountOverlayOnSandboxHost(opts, containerRootfs)
}

// MountSessionOverlay mounts an overlayfs on a container's rootfs directory.
// This is a convenience wrapper that resolves a single template snapshot as the
// lower layer. For stacked overlays (custom templates), use MountOverlay with
// ResolveLowerLayers instead.
func MountSessionOverlay(poolSourcePath, containerName, templateName string) error {
	lowerDir := SnapshotRootfsPath(poolSourcePath, templateName)
	return MountOverlay(poolSourcePath, containerName, lowerDir)
}

// UnmountSessionOverlay unmounts the overlayfs from a container's rootfs
// and removes the per-session overlay directories (including idmapped bind
// mounts for unprivileged containers).
// On Docker+Incus, these operations run inside the Docker container.
func UnmountSessionOverlay(poolSourcePath, containerName string) error {
	containerRootfs := ContainerRootfsPath(poolSourcePath, containerName)

	// Unmount overlayfs (ignore error if not mounted)
	if err := umountOnSandboxHost(containerRootfs); err != nil {
		// Not fatal — might not be overlay-mounted (old container, or already unmounted)
		slog.Warn("failed to unmount overlay", "component", "sandbox", "mountpoint", containerRootfs, "error", err)
	}

	// Tear down idmapped bind mounts (no-op if privileged or none exist)
	teardownIdmappedLayers(containerName)

	// Remove per-session overlay dirs
	sessionDir := filepath.Join(overlayBaseDir, containerName)
	if err := removeAllOnSandboxHost(sessionDir); err != nil {
		slog.Warn("failed to clean up overlay dir", "component", "sandbox", "dir", sessionDir, "error", err)
	}

	// Remove idmap sibling dir (e.g., <containerName>-idmap/)
	idmapDir := idmapMountsDir(containerName)
	if err := removeAllOnSandboxHost(idmapDir); err != nil {
		slog.Warn("failed to clean up idmap dir", "component", "sandbox", "dir", idmapDir, "error", err)
	}

	return nil
}

// IsOverlayMounted checks if a container's rootfs has an overlayfs mounted on it.
// On Docker+Incus, reads /proc/mounts from inside the Docker container.
func IsOverlayMounted(poolSourcePath, containerName string) bool {
	containerRootfs := ContainerRootfsPath(poolSourcePath, containerName)
	return isOverlayMountedOnSandboxHost(containerRootfs)
}

// MergeOverlayToRootfs copies the merged overlay view into a flat directory.
// Used when creating a template from a running session container — we need
// to materialize the overlay into a real rootfs for the template snapshot.
// On Docker+Incus, these operations run inside the Docker container.
func MergeOverlayToRootfs(poolSourcePath, containerName, destRootfs string) error {
	containerRootfs := ContainerRootfsPath(poolSourcePath, containerName)

	if !IsOverlayMounted(poolSourcePath, containerName) {
		// Not overlay-mounted — just copy directly
		if err := cpOnSandboxHost(containerRootfs+"/.", destRootfs+"/"); err != nil {
			return fmt.Errorf("failed to copy rootfs: %w", err)
		}
		return nil
	}

	// The overlay is mounted — the merged view at containerRootfs IS what we want.
	// rsync is more efficient than cp -a for large trees.
	if err := rsyncOnSandboxHost(containerRootfs+"/", destRootfs+"/"); err != nil {
		return fmt.Errorf("failed to rsync overlay rootfs: %w", err)
	}

	return nil
}

// CleanupAllOverlays unmounts all overlay mounts and removes the overlay base directory.
// Used during sandbox reset or cleanup.
// On Docker+Incus, these operations run inside the Docker container.
func CleanupAllOverlays() {
	entries, err := readDirOnSandboxHost(overlayBaseDir)
	if err != nil {
		return // directory doesn't exist or can't be read
	}

	for _, entryName := range entries {
		// readDirOnSandboxHost may include trailing "/" for dirs
		name := filepath.Base(entryName)
		if name == "" || name == "." {
			continue
		}

		// Try to find and unmount the corresponding container rootfs
		// We don't know the pool path here, so just try to unmount by reading /proc/mounts
		data, readErr := readMountsOnSandboxHost()
		if readErr != nil {
			slog.Warn("failed to read mounts during overlay cleanup", "component", "sandbox", "error", readErr)
		}
		for _, line := range bytes.Split(data, []byte("\n")) {
			fields := bytes.Fields(line)
			if len(fields) >= 3 && string(fields[2]) == "overlay" {
				mountpoint := string(fields[1])
				// Check if this mount's workdir points to our session dir
				if bytes.Contains(line, []byte(filepath.Join(overlayBaseDir, name))) {
					if err := umountOnSandboxHost(mountpoint); err != nil {
						slog.Warn("failed to unmount overlay during cleanup", "component", "sandbox", "mountpoint", mountpoint, "error", err)
					}
				}
			}
		}
	}

	if err := removeAllOnSandboxHost(overlayBaseDir); err != nil {
		slog.Warn("failed to remove overlay base directory", "component", "sandbox", "path", overlayBaseDir, "error", err)
	}
}

// RemountDependentOverlays finds all overlay mounts whose lowerdir includes
// the given path and remounts them. This is critical after a base snapshot is
// recreated (delete + create): the old directory inode is gone, so existing
// overlay mounts that referenced it as a lowerdir become stale (empty rootfs).
//
// For unprivileged containers with idmapped underlying mounts, this also
// detects stale idmapped bind mounts that reference the snapshot path and
// recreates the full mount stack (idmapped layers + overlay on top).
//
// The function parses /proc/mounts, identifies affected overlays, stops any
// running containers that use the mount (umount fails on busy mounts),
// unmounts, recreates the work directory (overlayfs requires a clean workdir
// after remount), remounts with the same options, and restarts stopped containers.
//
// This MUST be called while templateSnapshotMu is held (write lock) to prevent
// new overlay mounts from being created between the snapshot swap and remount.
func RemountDependentOverlays(client *IncusClient, snapshotPath string) error {
	data, err := readMountsOnSandboxHost()
	if err != nil {
		return fmt.Errorf("failed to read mounts: %w", err)
	}

	type overlayMount struct {
		mountpoint    string
		containerName string
		opts          string
		wasRunning    bool
		hasIdmap      bool // true if this container uses idmapped underlying mounts
	}

	// Track affected containers by name to avoid duplicates.
	// A container may appear twice: once for the overlay, once for idmapped bind mounts.
	affectedMap := make(map[string]*overlayMount)

	for _, line := range bytes.Split(data, []byte("\n")) {
		fields := bytes.Fields(line)
		if len(fields) < 4 {
			continue
		}

		mountpoint := string(fields[1])
		fstype := string(fields[2])
		opts := string(fields[3])

		if !strings.Contains(opts, snapshotPath) && !strings.Contains(mountpoint, snapshotPath) {
			continue
		}

		if fstype == "overlay" {
			// Direct overlay mount referencing the snapshot in lowerdir (privileged)
			containerName := containerNameFromRootfs(mountpoint)
			if containerName == "" {
				continue
			}
			if existing, ok := affectedMap[containerName]; ok {
				existing.opts = opts
			} else {
				affectedMap[containerName] = &overlayMount{
					mountpoint:    mountpoint,
					containerName: containerName,
					opts:          opts,
				}
			}
		} else if strings.Contains(mountpoint, "-idmap/lower-") {
			// Idmapped bind mount of a lower layer (unprivileged).
			// Path pattern: .../astonish-overlays/<containerName>-idmap/lower-N
			// Extract container name from the path (strip -idmap suffix).
			parts := strings.Split(mountpoint, "/")
			for i, p := range parts {
				if p == "astonish-overlays" && i+1 < len(parts) {
					dirName := parts[i+1]
					containerName := strings.TrimSuffix(dirName, "-idmap")
					if containerName == "" || containerName == dirName {
						continue // not an idmap dir
					}
					if existing, ok := affectedMap[containerName]; ok {
						existing.hasIdmap = true
					} else {
						// We found the idmap mount but not the overlay yet.
						affectedMap[containerName] = &overlayMount{
							containerName: containerName,
							hasIdmap:      true,
						}
					}
					break
				}
			}
		}
	}

	if len(affectedMap) == 0 {
		return nil
	}

	// Convert map to slice for ordered processing
	var affected []*overlayMount
	for _, m := range affectedMap {
		affected = append(affected, m)
	}

	slog.Info("remounting overlays after snapshot recreation", "component", "sandbox", "count", len(affected), "snapshot_path", snapshotPath)

	// Phase 1: stop running containers so we can unmount their rootfs
	for _, m := range affected {
		if m.containerName != "" && client.IsRunning(m.containerName) {
			slog.Info("stopping container for overlay remount", "component", "sandbox", "container", m.containerName)
			if err := client.StopInstance(m.containerName, true); err != nil {
				slog.Warn("failed to stop container for overlay remount", "component", "sandbox", "container", m.containerName, "error", err)
				continue
			}
			m.wasRunning = true
		}
	}

	// Phase 2: unmount + remount each overlay
	for _, m := range affected {
		// Unmount the overlay first (must be done before idmap layers)
		if m.mountpoint != "" {
			if err := umountOnSandboxHost(m.mountpoint); err != nil {
				slog.Warn("failed to unmount stale overlay", "component", "sandbox", "mountpoint", m.mountpoint, "error", err)
				continue
			}
		}

		// Tear down idmapped bind mounts if present
		if m.hasIdmap {
			teardownIdmappedLayers(m.containerName)
		}

		// Parse original mount options to get the paths for remount
		lowerDir := extractMountOpt(m.opts, "lowerdir")
		upperDir := extractMountOpt(m.opts, "upperdir")
		workDir := extractMountOpt(m.opts, "workdir")

		if lowerDir == "" || upperDir == "" || workDir == "" {
			slog.Warn("could not parse overlay opts, skipping remount", "component", "sandbox", "mountpoint", m.mountpoint)
			continue
		}

		// For idmapped mounts, the lowerdir/upperdir/workdir from /proc/mounts
		// point to the idmap directories, not the originals. We need to resolve
		// the original paths. The original lower paths are what the idmapped bind
		// mounts were cloned from — we can reconstruct them by stripping the
		// idmap directory prefix and looking at the original overlay structure.
		originalLowerDir := lowerDir
		originalWorkDir := workDir
		if m.hasIdmap {
			// Resolve idmapped paths back to originals.
			// Idmap lower paths: .../idmap/lower-N -> original lower paths
			// We stored the original lowerdir in the same order, so we can
			// rebuild from the overlay directory structure.
			//
			// For now, we use the snapshot path directly since it was just
			// recreated at the same location. The caller passes snapshotPath
			// which is the new snapshot's rootfs path.
			//
			// For stacked templates (multiple lowers), the first lower is usually
			// the template upper, and the last is the base snapshot. We need to
			// replace only the parts that reference the snapshot.
			idmapDir := idmapMountsDir(m.containerName)
			lowerParts := strings.Split(lowerDir, ":")
			var resolvedLowers []string
			for _, lp := range lowerParts {
				if strings.HasPrefix(lp, idmapDir) {
					// This is an idmapped path — resolve it back.
					// The original path structure mirrors the idmap mount source.
					// Since we don't have a reverse mapping, use the snapshotPath
					// for the layer that contained it.
					resolvedLowers = append(resolvedLowers, lp)
				} else {
					resolvedLowers = append(resolvedLowers, lp)
				}
			}
			originalLowerDir = strings.Join(resolvedLowers, ":")

			// For the workdir, resolve back to the original.
			// Idmap workdir: .../idmap/upper-work/work -> .../work
			sessionDir := filepath.Join(overlayBaseDir, m.containerName)
			if strings.HasPrefix(workDir, idmapDir) {
				originalWorkDir = filepath.Join(sessionDir, "work")
			}
		}

		// Overlayfs requires a clean workdir after remount — recreate it
		if err := removeAllOnSandboxHost(originalWorkDir); err != nil {
			slog.Warn("failed to clean workdir", "component", "sandbox", "workdir", originalWorkDir, "error", err)
		}
		if err := mkdirAllOnSandboxHost(originalWorkDir, 0755); err != nil {
			slog.Warn("failed to recreate workdir", "component", "sandbox", "workdir", originalWorkDir, "error", err)
			continue
		}

		if m.hasIdmap && m.containerName != "" {
			// For unprivileged containers, resolve the actual lower layers
			// from the original paths and use setupIdmappedOverlay to recreate
			// the full mount stack (idmapped layers + overlay).
			//
			// Since the snapshot was just recreated at the same path, the
			// original lower paths are still valid — we just need to get them
			// without the idmap prefix. We use resolveOriginalLowers to find them.
			origLowers := resolveOriginalLowers(data, m.containerName, snapshotPath)
			if origLowers == "" {
				// Fallback: use snapshotPath as the sole lower
				origLowers = snapshotPath
			}

			if err := mountIdmappedOverlay(client, m.containerName, m.mountpoint, origLowers); err != nil {
				slog.Error("failed to remount idmapped overlay",
					"component", "sandbox", "container", m.containerName, "error", err)
			} else {
				slog.Info("remounted idmapped overlay", "component", "sandbox", "container", m.containerName)
			}
		} else {
			// Privileged container — plain overlay remount
			newOpts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", originalLowerDir, upperDir, originalWorkDir)
			if err := mountOverlayOnSandboxHost(newOpts, m.mountpoint); err != nil {
				slog.Error("failed to remount overlay", "component", "sandbox", "mountpoint", m.mountpoint, "error", err)
				continue
			}
			slog.Info("remounted overlay", "component", "sandbox", "mountpoint", m.mountpoint)
		}
	}

	// Phase 3: restart containers that were running before
	for _, m := range affected {
		if m.wasRunning && m.containerName != "" {
			slog.Info("restarting container after overlay remount", "component", "sandbox", "container", m.containerName)
			if err := client.StartInstance(m.containerName); err != nil {
				slog.Warn("failed to restart container after overlay remount", "component", "sandbox", "container", m.containerName, "error", err)
			}
		}
	}

	return nil
}

// resolveOriginalLowers attempts to find the original (non-idmapped) lower
// layer paths for a container by examining its idmapped bind mount entries
// in /proc/mounts. Each idmap/lower-N mount has a source device/path that
// reveals the original lower directory.
//
// Falls back to snapshotPath if resolution fails.
func resolveOriginalLowers(mountData []byte, containerName, snapshotPath string) string {
	idmapDir := idmapMountsDir(containerName)
	prefix := filepath.Join(idmapDir, "lower-")

	// Collect lower mount sources in order (lower-0, lower-1, ...)
	type lowerEntry struct {
		index int
		// We can't easily get the source path from /proc/mounts for bind mounts
		// (they show the device, not the bind source). Instead, we know that the
		// snapshot was recreated at snapshotPath, so for the layer that references
		// the snapshot, we use snapshotPath directly.
		//
		// For stacked templates, additional lower layers (template upper dirs)
		// are NOT affected by the snapshot recreation — they're on a different
		// filesystem. So we only need to identify how many lowers there were
		// and which one was the snapshot.
		mountpoint string
	}

	var lowers []lowerEntry
	for _, line := range bytes.Split(mountData, []byte("\n")) {
		fields := bytes.Fields(line)
		if len(fields) < 2 {
			continue
		}
		mp := string(fields[1])
		if strings.HasPrefix(mp, prefix) {
			suffix := strings.TrimPrefix(mp, prefix)
			idx := 0
			for _, c := range suffix {
				if c >= '0' && c <= '9' {
					idx = idx*10 + int(c-'0')
				}
			}
			lowers = append(lowers, lowerEntry{index: idx, mountpoint: mp})
		}
	}

	if len(lowers) == 0 {
		return snapshotPath
	}

	// For @base template containers, there's typically one lower (the snapshot).
	// For custom templates, there are multiple lowers:
	//   lower-0 = template upper dir (not affected by snapshot recreation)
	//   lower-1 = base snapshot rootfs (affected)
	//
	// Since we can't determine the template upper dir from /proc/mounts,
	// and since RemountDependentOverlays is called because the snapshot changed,
	// the safest approach is: if there's only 1 lower, return snapshotPath.
	// If there are multiple lowers, we need the template upper dirs too.
	//
	// For now, return just the snapshot path. Custom template overlays
	// with stacked layers will need to be resolved via a separate mechanism.
	// TODO: For stacked template support, pass the template registry to
	// RemountDependentOverlays and use ResolveLowerLayers.
	return snapshotPath
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
	return mkdirAllOnSandboxHost(overlayBaseDir, 0755)
}
