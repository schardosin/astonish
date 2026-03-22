package sandbox

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	incus "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
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
		if _, err := os.Stat(lower); err != nil {
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
	if _, err := os.Stat(snapshotRootfs); err == nil {
		// Template has a real snapshot — use it directly (like @base)
		return snapshotRootfs, nil
	}

	// No snapshot — this is an overlay-based custom template.
	// Stack: template-upper on top of the base it's derived from.
	tplUpperDir := OverlayUpperDir(tplContainerName)
	if _, err := os.Stat(tplUpperDir); err != nil {
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

	fmt.Println("Creating overlay shell image...")

	tarball, err := buildOverlayImageTarball()
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
func buildOverlayImageTarball() ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// metadata.yaml
	metadata := `architecture: amd64
creation_date: 1700000000
properties:
  architecture: amd64
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
`
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
func CreateOverlayContainer(client *IncusClient, containerName, templateName string, registry *TemplateRegistry) error {
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

	// Create the container from the tiny overlay image
	req := api.InstancesPost{
		Name: containerName,
		Type: api.InstanceTypeContainer,
		InstancePut: api.InstancePut{
			Config: map[string]string{
				"security.privileged": "true",
				"security.nesting":    "false",
			},
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

// MountOverlay mounts an overlayfs on a container's rootfs directory with
// pre-resolved lower layers. The lowerDir can be a single path or a
// colon-separated list of paths for stacked overlays.
func MountOverlay(poolSourcePath, containerName, lowerDir string) error {
	containerRootfs := ContainerRootfsPath(poolSourcePath, containerName)

	// Create per-container overlay dirs
	sessionDir := filepath.Join(overlayBaseDir, containerName)
	upperDir := filepath.Join(sessionDir, "upper")
	workDir := filepath.Join(sessionDir, "work")

	if err := os.MkdirAll(upperDir, 0755); err != nil {
		return fmt.Errorf("failed to create overlay upper dir: %w", err)
	}
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return fmt.Errorf("failed to create overlay work dir: %w", err)
	}

	// Mount overlayfs
	opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", lowerDir, upperDir, workDir)
	cmd := exec.Command("mount", "-t", "overlay", "overlay", "-o", opts, containerRootfs)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to mount overlayfs on %s: %w\nOutput: %s", containerRootfs, err, string(output))
	}

	return nil
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
func UnmountSessionOverlay(poolSourcePath, containerName string) error {
	containerRootfs := ContainerRootfsPath(poolSourcePath, containerName)

	// Unmount overlayfs (ignore error if not mounted)
	cmd := exec.Command("umount", containerRootfs)
	if output, err := cmd.CombinedOutput(); err != nil {
		// Not fatal — might not be overlay-mounted (old container, or already unmounted)
		log.Printf("[sandbox] Warning: failed to unmount overlay on %s: %v (%s)", containerRootfs, err, string(output))
	}

	// Remove per-session overlay dirs
	sessionDir := filepath.Join(overlayBaseDir, containerName)
	if err := os.RemoveAll(sessionDir); err != nil {
		log.Printf("[sandbox] Warning: failed to clean up overlay dir %s: %v", sessionDir, err)
	}

	return nil
}

// IsOverlayMounted checks if a container's rootfs has an overlayfs mounted on it.
func IsOverlayMounted(poolSourcePath, containerName string) bool {
	containerRootfs := ContainerRootfsPath(poolSourcePath, containerName)

	// Check /proc/mounts for an overlay mount at this path
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}

	// Each line: device mountpoint fstype options ...
	target := containerRootfs
	for _, line := range bytes.Split(data, []byte("\n")) {
		fields := bytes.Fields(line)
		if len(fields) >= 3 && string(fields[1]) == target && string(fields[2]) == "overlay" {
			return true
		}
	}

	return false
}

// MergeOverlayToRootfs copies the merged overlay view into a flat directory.
// Used when creating a template from a running session container — we need
// to materialize the overlay into a real rootfs for the template snapshot.
func MergeOverlayToRootfs(poolSourcePath, containerName, destRootfs string) error {
	containerRootfs := ContainerRootfsPath(poolSourcePath, containerName)

	if !IsOverlayMounted(poolSourcePath, containerName) {
		// Not overlay-mounted — just copy directly
		cmd := exec.Command("cp", "-a", containerRootfs+"/.", destRootfs+"/")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to copy rootfs: %w\nOutput: %s", err, string(output))
		}
		return nil
	}

	// The overlay is mounted — the merged view at containerRootfs IS what we want.
	// rsync is more efficient than cp -a for large trees.
	cmd := exec.Command("rsync", "-a", "--delete", containerRootfs+"/", destRootfs+"/")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to rsync overlay rootfs: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// CleanupAllOverlays unmounts all overlay mounts and removes the overlay base directory.
// Used during sandbox reset or cleanup.
func CleanupAllOverlays() {
	entries, err := os.ReadDir(overlayBaseDir)
	if err != nil {
		return // directory doesn't exist or can't be read
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Try to find and unmount the corresponding container rootfs
		// We don't know the pool path here, so just try to unmount by reading /proc/mounts
		data, _ := os.ReadFile("/proc/mounts")
		for _, line := range bytes.Split(data, []byte("\n")) {
			fields := bytes.Fields(line)
			if len(fields) >= 3 && string(fields[2]) == "overlay" {
				mountpoint := string(fields[1])
				// Check if this mount's workdir points to our session dir
				if bytes.Contains(line, []byte(filepath.Join(overlayBaseDir, entry.Name()))) {
					exec.Command("umount", mountpoint).Run()
				}
			}
		}
	}

	os.RemoveAll(overlayBaseDir)
}

// EnsureOverlayBaseDir creates the overlay base directory if it doesn't exist.
func EnsureOverlayBaseDir() error {
	return os.MkdirAll(overlayBaseDir, 0755)
}
