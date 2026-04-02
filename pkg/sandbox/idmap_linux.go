//go:build linux

package sandbox

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// idmapEntry matches the JSON format Incus uses for volatile.idmap.next.
type idmapEntry struct {
	Isuid    bool  `json:"Isuid"`
	Isgid    bool  `json:"Isgid"`
	Hostid   int64 `json:"Hostid"`
	Nsid     int64 `json:"Nsid"`
	Maprange int64 `json:"Maprange"`
}

// idmapMountsDir returns the directory where idmapped bind mounts are placed
// for a container. This is a SIBLING of the session dir (not inside it) to
// avoid the recursive mount problem where cloning the session dir would
// capture the idmap/ directory inside it.
//
// Layout:
//
//	overlayBaseDir/
//	  <containerName>/          <- session dir (upper/, work/)
//	  <containerName>-idmap/    <- idmap mounts dir (lower-0/, upper-work/)
func idmapMountsDir(containerName string) string {
	return filepath.Join(overlayBaseDir, containerName+"-idmap")
}

// createUserNamespace creates a child process in a new user namespace with
// the given UID/GID idmap written to it. Returns the open userns fd and a
// cleanup function that kills the child process and closes the fd.
//
// The caller MUST call cleanup when done.
func createUserNamespace(entries []idmapEntry) (usernsFd int, cleanup func(), err error) {
	// Build uid_map and gid_map strings
	var uidLines, gidLines []string
	for _, e := range entries {
		line := fmt.Sprintf("%d %d %d", e.Nsid, e.Hostid, e.Maprange)
		if e.Isuid {
			uidLines = append(uidLines, line)
		}
		if e.Isgid {
			gidLines = append(gidLines, line)
		}
	}

	if len(uidLines) == 0 || len(gidLines) == 0 {
		return -1, nil, fmt.Errorf("idmap has no UID or GID entries")
	}

	// Create a child process in a new user namespace.
	// The child just sleeps; we use its user namespace for the idmap.
	cmd := exec.Command("sleep", "infinity")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUSER,
	}
	if err := cmd.Start(); err != nil {
		return -1, nil, fmt.Errorf("failed to create user namespace: %w", err)
	}

	killChild := func() {
		cmd.Process.Kill()
		cmd.Wait()
	}

	childPid := cmd.Process.Pid

	// Write deny for setgroups (required before writing gid_map)
	if err := os.WriteFile(
		fmt.Sprintf("/proc/%d/setgroups", childPid),
		[]byte("deny"), 0,
	); err != nil {
		killChild()
		return -1, nil, fmt.Errorf("failed to write setgroups deny: %w", err)
	}

	// Write uid_map
	if err := os.WriteFile(
		fmt.Sprintf("/proc/%d/uid_map", childPid),
		[]byte(strings.Join(uidLines, "\n")+"\n"), 0,
	); err != nil {
		killChild()
		return -1, nil, fmt.Errorf("failed to write uid_map: %w", err)
	}

	// Write gid_map
	if err := os.WriteFile(
		fmt.Sprintf("/proc/%d/gid_map", childPid),
		[]byte(strings.Join(gidLines, "\n")+"\n"), 0,
	); err != nil {
		killChild()
		return -1, nil, fmt.Errorf("failed to write gid_map: %w", err)
	}

	// Open the user namespace fd
	fd, err := unix.Open(
		fmt.Sprintf("/proc/%d/ns/user", childPid),
		unix.O_RDONLY|unix.O_CLOEXEC, 0,
	)
	if err != nil {
		killChild()
		return -1, nil, fmt.Errorf("failed to open userns fd: %w", err)
	}

	return fd, func() {
		unix.Close(fd)
		killChild()
	}, nil
}

// createIdmappedBindMount creates an idmapped bind mount of sourcePath at
// targetPath. It uses the Linux mount API: open_tree(OPEN_TREE_CLONE) to
// clone the mount, mount_setattr(MOUNT_ATTR_IDMAP) to apply the idmap via
// the provided user namespace fd, and move_mount() to place it at targetPath.
//
// targetPath must exist as a directory before calling this function.
func createIdmappedBindMount(sourcePath, targetPath string, usernsFd int) error {
	// Clone the source mount at the given path.
	// AT_RECURSIVE is needed so that submounts (if any) are included.
	treeFd, err := unix.OpenTree(-1, sourcePath,
		unix.OPEN_TREE_CLONE|unix.OPEN_TREE_CLOEXEC|unix.AT_RECURSIVE)
	if err != nil {
		return fmt.Errorf("open_tree(%s) failed: %w", sourcePath, err)
	}
	defer unix.Close(treeFd)

	// Apply the idmap
	attr := unix.MountAttr{
		Attr_set:  unix.MOUNT_ATTR_IDMAP,
		Userns_fd: uint64(usernsFd),
	}
	if err := unix.MountSetattr(treeFd, "", unix.AT_EMPTY_PATH, &attr); err != nil {
		return fmt.Errorf("mount_setattr(MOUNT_ATTR_IDMAP) on %s failed: %w", sourcePath, err)
	}

	// Move the idmapped mount into place
	if err := unix.MoveMount(treeFd, "", -1, targetPath, unix.MOVE_MOUNT_F_EMPTY_PATH); err != nil {
		return fmt.Errorf("move_mount to %s failed: %w", targetPath, err)
	}

	return nil
}

// mountIdmappedOverlay creates idmapped bind mounts for the overlay's lower
// layers and upper/work directory, then mounts overlayfs on top of them.
//
// This is needed on kernels < 6.12 where overlayfs itself doesn't support
// FS_ALLOW_IDMAP. By idmapping the underlying filesystem mounts (btrfs for
// lower layers, ext4 for upper/work), the overlay sees files with shifted
// UIDs/GIDs without needing kernel support for idmapping the overlay directly.
//
// Layout:
//
//	overlayBaseDir/<containerName>/           <- session dir
//	  upper/                                  <- overlay upper layer
//	  work/                                   <- overlay work dir
//	overlayBaseDir/<containerName>-idmap/     <- idmap mounts (sibling dir)
//	  lower-0/                                <- idmapped bind mount of 1st lower layer
//	  lower-N/                                <- idmapped bind mount of Nth lower layer
//	  upper-work/                             <- idmapped bind mount of the session dir
func mountIdmappedOverlay(client *IncusClient, containerName, containerRootfs, lowerDir string) error {
	// Read the container's desired idmap
	inst, err := client.GetInstance(containerName)
	if err != nil {
		return fmt.Errorf("failed to read instance config: %w", err)
	}

	nextIdmap, ok := inst.Config["volatile.idmap.next"]
	if !ok || nextIdmap == "" || nextIdmap == "[]" {
		// No idmap — should not happen for unprivileged, but fall back to plain mount
		return mountPlainOverlay(containerName, containerRootfs, lowerDir)
	}

	var entries []idmapEntry
	if err := json.Unmarshal([]byte(nextIdmap), &entries); err != nil {
		return fmt.Errorf("failed to parse idmap: %w", err)
	}

	if len(entries) == 0 {
		return mountPlainOverlay(containerName, containerRootfs, lowerDir)
	}

	sessionDir := filepath.Join(overlayBaseDir, containerName)
	idmapDir := idmapMountsDir(containerName)

	// Clean up any stale idmapped mounts from a previous failed attempt.
	// If we don't do this, open_tree on already-idmapped mounts returns EPERM
	// because you can't idmap a mount that's already idmapped.
	unmountIdmappedLayers(containerName)

	// Create upper/ and work/ directories in the session dir BEFORE creating
	// the idmapped clone. open_tree(OPEN_TREE_CLONE) captures a snapshot of
	// the mount's directory tree at clone time, so these must exist to appear
	// inside the idmapped bind mount at <idmapDir>/upper-work/upper and work.
	upperDir := filepath.Join(sessionDir, "upper")
	workDir := filepath.Join(sessionDir, "work")
	if err := mkdirAllOnSandboxHost(upperDir, 0755); err != nil {
		return fmt.Errorf("failed to create overlay upper dir: %w", err)
	}
	if err := mkdirAllOnSandboxHost(workDir, 0755); err != nil {
		return fmt.Errorf("failed to create overlay work dir: %w", err)
	}

	// Create the idmap mounts directory (sibling of session dir, NOT inside it).
	// This avoids the recursive mount problem where cloning the session dir
	// would capture idmap/ inside it, creating circular mount references.
	if err := mkdirAllOnSandboxHost(idmapDir, 0755); err != nil {
		return fmt.Errorf("failed to create idmap mounts dir: %w", err)
	}

	// Create user namespace with the idmap
	usernsFd, cleanup, err := createUserNamespace(entries)
	if err != nil {
		return fmt.Errorf("failed to create user namespace for idmap: %w", err)
	}
	defer cleanup()

	// Track created mounts for cleanup on error
	var createdMounts []string
	cleanupMounts := func() {
		for i := len(createdMounts) - 1; i >= 0; i-- {
			unix.Unmount(createdMounts[i], unix.MNT_DETACH)
		}
	}

	// Idmap each lower layer
	lowerPaths := strings.Split(lowerDir, ":")
	var idmappedLowers []string

	for i, lowerPath := range lowerPaths {
		mountTarget := filepath.Join(idmapDir, fmt.Sprintf("lower-%d", i))
		if err := mkdirAllOnSandboxHost(mountTarget, 0755); err != nil {
			cleanupMounts()
			return fmt.Errorf("failed to create lower idmap dir %d: %w", i, err)
		}

		if err := createIdmappedBindMount(lowerPath, mountTarget, usernsFd); err != nil {
			cleanupMounts()
			return fmt.Errorf("failed to create idmapped bind mount for lower layer %d (%s): %w", i, lowerPath, err)
		}
		createdMounts = append(createdMounts, mountTarget)
		idmappedLowers = append(idmappedLowers, mountTarget)
	}

	// Idmap the upper/work parent directory.
	// The session dir contains ONLY upper/ and work/ (no idmap/ subdirectory
	// since we moved it to a sibling). Overlayfs requires upperdir and workdir
	// on the same mount, so we clone their common parent.
	upperWorkTarget := filepath.Join(idmapDir, "upper-work")
	if err := mkdirAllOnSandboxHost(upperWorkTarget, 0755); err != nil {
		cleanupMounts()
		return fmt.Errorf("failed to create upper-work idmap dir: %w", err)
	}

	if err := createIdmappedBindMount(sessionDir, upperWorkTarget, usernsFd); err != nil {
		cleanupMounts()
		return fmt.Errorf("failed to create idmapped bind mount for upper/work: %w", err)
	}
	createdMounts = append(createdMounts, upperWorkTarget)

	// Mount overlayfs using the idmapped paths
	idmappedLowerDir := strings.Join(idmappedLowers, ":")
	idmappedUpperDir := filepath.Join(upperWorkTarget, "upper")
	idmappedWorkDir := filepath.Join(upperWorkTarget, "work")

	opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s",
		idmappedLowerDir, idmappedUpperDir, idmappedWorkDir)

	if err := mountOverlayOnSandboxHost(opts, containerRootfs); err != nil {
		cleanupMounts()
		return fmt.Errorf("failed to mount overlay with idmapped layers: %w", err)
	}

	slog.Info("mounted overlay with idmapped underlying layers",
		"component", "sandbox",
		"container", containerName,
		"lower_count", len(lowerPaths),
	)

	return nil
}

// mountPlainOverlay creates upper/work dirs and mounts a plain (non-idmapped)
// overlay. Used as fallback when no idmap is configured.
func mountPlainOverlay(containerName, containerRootfs, lowerDir string) error {
	upperDir := filepath.Join(overlayBaseDir, containerName, "upper")
	workDir := filepath.Join(overlayBaseDir, containerName, "work")
	if err := mkdirAllOnSandboxHost(upperDir, 0755); err != nil {
		return fmt.Errorf("failed to create overlay upper dir: %w", err)
	}
	if err := mkdirAllOnSandboxHost(workDir, 0755); err != nil {
		return fmt.Errorf("failed to create overlay work dir: %w", err)
	}
	opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s",
		lowerDir, upperDir, workDir)
	return mountOverlayOnSandboxHost(opts, containerRootfs)
}

// unmountIdmappedLayers unmounts any idmapped bind mounts in the container's
// idmap directory. Uses MNT_DETACH for robustness — if a mount is busy or
// has children, lazy unmount ensures it's cleaned up eventually.
func unmountIdmappedLayers(containerName string) {
	idmapDir := idmapMountsDir(containerName)

	// Unmount upper-work first (it depends on session dir, not vice versa),
	// then lowers in reverse order.
	upperWork := filepath.Join(idmapDir, "upper-work")
	if err := unix.Unmount(upperWork, unix.MNT_DETACH); err != nil {
		// Not fatal — may not exist if privileged or already unmounted
		slog.Debug("idmap unmount upper-work", "component", "sandbox",
			"container", containerName, "error", err)
	}

	// Unmount lower-N mounts (scan for lower-* entries)
	entries, err := os.ReadDir(idmapDir)
	if err != nil {
		return // idmap dir doesn't exist — nothing to clean up
	}

	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if strings.HasPrefix(e.Name(), "lower-") {
			target := filepath.Join(idmapDir, e.Name())
			if err := unix.Unmount(target, unix.MNT_DETACH); err != nil {
				slog.Debug("idmap unmount lower", "component", "sandbox",
					"container", containerName, "mount", e.Name(), "error", err)
			}
		}
	}
}

// preseedIdmap copies volatile.idmap.next into volatile.last_state.idmap so
// that Incus believes the container's rootfs is already UID-shifted to the
// correct idmap. Combined with the idmapped underlying mounts, this prevents
// Incus from attempting its own shift/idmap on container start.
func preseedIdmap(client *IncusClient, containerName string) error {
	inst, err := client.GetInstance(containerName)
	if err != nil {
		return fmt.Errorf("failed to read instance config: %w", err)
	}

	nextIdmap, ok := inst.Config["volatile.idmap.next"]
	if !ok || nextIdmap == "" {
		return nil
	}

	return client.SetInstanceConfig(containerName, map[string]string{
		"volatile.last_state.idmap": nextIdmap,
	})
}

// setupIdmappedOverlay is the complete sequence for mounting an unprivileged
// overlay container's rootfs:
//
//  1. Create idmapped bind mounts of lower layers and upper/work
//  2. Mount overlayfs on top of the idmapped mounts
//  3. Pre-seed Incus idmap state so it skips its own shift
//
// On privileged containers, this falls back to a plain overlay mount.
func setupIdmappedOverlay(client *IncusClient, containerName, containerRootfs, lowerDir string) error {
	if IsPrivileged() {
		return mountPlainOverlay(containerName, containerRootfs, lowerDir)
	}

	// Mount overlay on top of idmapped bind mounts
	if err := mountIdmappedOverlay(client, containerName, containerRootfs, lowerDir); err != nil {
		return err
	}

	// Tell Incus the rootfs is already shifted
	if err := preseedIdmap(client, containerName); err != nil {
		return fmt.Errorf("failed to pre-seed idmap: %w", err)
	}

	return nil
}

// teardownIdmappedLayers cleans up idmapped bind mounts for a container.
// No-op on privileged containers or when no idmap mounts exist.
func teardownIdmappedLayers(containerName string) {
	if IsPrivileged() {
		return
	}
	unmountIdmappedLayers(containerName)
}
