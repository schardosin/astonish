package sandbox

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"log"
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

// GetPoolSourcePath returns the filesystem path of a storage pool's root.
// For a dir-backend pool, this is the "source" config key. For CoW backends
// (btrfs, ZFS) it falls back to the default Incus path.
func GetPoolSourcePath(client *IncusClient, poolName string) (string, error) {
	pool, _, err := client.server.GetStoragePool(poolName)
	if err != nil {
		return "", fmt.Errorf("failed to get storage pool %q: %w", poolName, err)
	}

	if src, ok := pool.Config["source"]; ok && src != "" {
		return src, nil
	}

	// Default Incus path
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
	containerConfig := map[string]string{
		"security.privileged": "true",
		"security.nesting":    fmt.Sprintf("%t", nesting),
	}
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

	// Mount overlayfs on the container's rootfs
	if err := MountOverlay(poolPath, containerName, lowerDir); err != nil {
		// Clean up the container on failure
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
// and removes the per-session overlay directories.
// On Docker+Incus, these operations run inside the Docker container.
func UnmountSessionOverlay(poolSourcePath, containerName string) error {
	containerRootfs := ContainerRootfsPath(poolSourcePath, containerName)

	// Unmount overlayfs (ignore error if not mounted)
	if err := umountOnSandboxHost(containerRootfs); err != nil {
		// Not fatal — might not be overlay-mounted (old container, or already unmounted)
		log.Printf("[sandbox] Warning: failed to unmount overlay on %s: %v", containerRootfs, err)
	}

	// Remove per-session overlay dirs
	sessionDir := filepath.Join(overlayBaseDir, containerName)
	if err := removeAllOnSandboxHost(sessionDir); err != nil {
		log.Printf("[sandbox] Warning: failed to clean up overlay dir %s: %v", sessionDir, err)
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
		data, _ := readMountsOnSandboxHost()
		for _, line := range bytes.Split(data, []byte("\n")) {
			fields := bytes.Fields(line)
			if len(fields) >= 3 && string(fields[2]) == "overlay" {
				mountpoint := string(fields[1])
				// Check if this mount's workdir points to our session dir
				if bytes.Contains(line, []byte(filepath.Join(overlayBaseDir, name))) {
					_ = umountOnSandboxHost(mountpoint)
				}
			}
		}
	}

	_ = removeAllOnSandboxHost(overlayBaseDir)
}

// RemountDependentOverlays finds all overlay mounts whose lowerdir includes
// the given path and remounts them. This is critical after a base snapshot is
// recreated (delete + create): the old directory inode is gone, so existing
// overlay mounts that referenced it as a lowerdir become stale (empty rootfs).
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
	}

	var affected []overlayMount

	for _, line := range bytes.Split(data, []byte("\n")) {
		fields := bytes.Fields(line)
		if len(fields) < 4 {
			continue
		}
		if string(fields[2]) != "overlay" {
			continue
		}

		mountpoint := string(fields[1])
		opts := string(fields[3])

		// Check if this overlay's lowerdir includes the snapshot path.
		// The options field looks like:
		//   rw,relatime,lowerdir=/path/a:/path/b,upperdir=/path/c,workdir=/path/d,...
		if !strings.Contains(opts, snapshotPath) {
			continue
		}

		// Extract the container name from the mountpoint path.
		// Pattern: .../containers/<containerName>/rootfs
		containerName := containerNameFromRootfs(mountpoint)

		affected = append(affected, overlayMount{
			mountpoint:    mountpoint,
			containerName: containerName,
			opts:          opts,
		})
	}

	if len(affected) == 0 {
		return nil
	}

	log.Printf("[sandbox] Remounting %d overlay(s) after snapshot recreation at %s", len(affected), snapshotPath)

	// Phase 1: stop running containers so we can unmount their rootfs
	for i := range affected {
		m := &affected[i]
		if m.containerName != "" && client.IsRunning(m.containerName) {
			log.Printf("[sandbox] Stopping %s for overlay remount...", m.containerName)
			if err := client.StopInstance(m.containerName, true); err != nil {
				log.Printf("[sandbox] Warning: failed to stop %s: %v", m.containerName, err)
				continue
			}
			m.wasRunning = true
		}
	}

	// Phase 2: unmount + remount each overlay
	for i := range affected {
		m := &affected[i]
		lowerDir := extractMountOpt(m.opts, "lowerdir")
		upperDir := extractMountOpt(m.opts, "upperdir")
		workDir := extractMountOpt(m.opts, "workdir")

		if lowerDir == "" || upperDir == "" || workDir == "" {
			log.Printf("[sandbox] Warning: could not parse overlay opts for %s, skipping remount", m.mountpoint)
			continue
		}

		// Unmount the stale overlay
		if err := umountOnSandboxHost(m.mountpoint); err != nil {
			log.Printf("[sandbox] Warning: failed to unmount stale overlay at %s: %v", m.mountpoint, err)
			continue
		}

		// Overlayfs requires a clean workdir after remount — recreate it
		if err := removeAllOnSandboxHost(workDir); err != nil {
			log.Printf("[sandbox] Warning: failed to clean workdir %s: %v", workDir, err)
		}
		if err := mkdirAllOnSandboxHost(workDir, 0755); err != nil {
			log.Printf("[sandbox] Warning: failed to recreate workdir %s: %v", workDir, err)
			continue
		}

		// Remount with the same options (paths are the same strings,
		// but the kernel will resolve them to the new inodes)
		newOpts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", lowerDir, upperDir, workDir)
		if err := mountOverlayOnSandboxHost(newOpts, m.mountpoint); err != nil {
			log.Printf("[sandbox] ERROR: failed to remount overlay at %s: %v", m.mountpoint, err)
			continue
		}

		log.Printf("[sandbox] Remounted overlay at %s", m.mountpoint)
	}

	// Phase 3: restart containers that were running before
	for i := range affected {
		m := &affected[i]
		if m.wasRunning && m.containerName != "" {
			log.Printf("[sandbox] Restarting %s after overlay remount...", m.containerName)
			if err := client.StartInstance(m.containerName); err != nil {
				log.Printf("[sandbox] Warning: failed to restart %s: %v", m.containerName, err)
			}
		}
	}

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
	return mkdirAllOnSandboxHost(overlayBaseDir, 0755)
}
