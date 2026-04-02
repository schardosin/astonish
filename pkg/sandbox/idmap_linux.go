//go:build linux

package sandbox

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
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

// applyIdmappedMount remounts an existing mount at targetPath with an idmap
// applied at the VFS level. This uses the Linux mount API (open_tree +
// mount_setattr + move_mount) to create a detached clone of the mount,
// apply a UID/GID idmap via a user namespace, and atomically replace the
// original mount.
//
// The idmap is read from the container's volatile.idmap.next config key.
// After this call, files at UID 0 on disk will appear at UID <hostid> through
// the mount (and vice versa from within the container's user namespace).
//
// This is needed because Incus's CanIdmapMount() probe can fail on overlayfs
// mounts, even though the kernel (5.19+) supports idmapped mounts on overlayfs.
// By applying the idmap ourselves, we bypass Incus's probe entirely.
func applyIdmappedMount(client *IncusClient, containerName, targetPath string) error {
	// Read the container's desired idmap
	inst, err := client.GetInstance(containerName)
	if err != nil {
		return fmt.Errorf("failed to read instance config: %w", err)
	}

	nextIdmap, ok := inst.Config["volatile.idmap.next"]
	if !ok || nextIdmap == "" || nextIdmap == "[]" {
		// No idmap configured (privileged container) — nothing to do
		return nil
	}

	// Parse the idmap entries
	var entries []idmapEntry
	if err := json.Unmarshal([]byte(nextIdmap), &entries); err != nil {
		return fmt.Errorf("failed to parse idmap: %w", err)
	}

	if len(entries) == 0 {
		return nil
	}

	// Build uid_map and gid_map strings for the user namespace
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
		return fmt.Errorf("idmap has no UID or GID entries")
	}

	// Create a child process in a new user namespace.
	// The child just sleeps; we use its user namespace for the idmap.
	cmd := exec.Command("sleep", "infinity")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUSER,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to create user namespace: %w", err)
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	childPid := cmd.Process.Pid

	// Write deny for setgroups (required before writing gid_map)
	if err := os.WriteFile(
		fmt.Sprintf("/proc/%d/setgroups", childPid),
		[]byte("deny"),
		0,
	); err != nil {
		return fmt.Errorf("failed to write setgroups deny: %w", err)
	}

	// Write uid_map
	if err := os.WriteFile(
		fmt.Sprintf("/proc/%d/uid_map", childPid),
		[]byte(strings.Join(uidLines, "\n")+"\n"),
		0,
	); err != nil {
		return fmt.Errorf("failed to write uid_map: %w", err)
	}

	// Write gid_map
	if err := os.WriteFile(
		fmt.Sprintf("/proc/%d/gid_map", childPid),
		[]byte(strings.Join(gidLines, "\n")+"\n"),
		0,
	); err != nil {
		return fmt.Errorf("failed to write gid_map: %w", err)
	}

	// Open the user namespace fd
	usernsFd, err := unix.Open(
		fmt.Sprintf("/proc/%d/ns/user", childPid),
		unix.O_RDONLY|unix.O_CLOEXEC,
		0,
	)
	if err != nil {
		return fmt.Errorf("failed to open userns fd: %w", err)
	}
	defer unix.Close(usernsFd)

	// Clone the existing mount
	treeFd, err := unix.OpenTree(-1, targetPath, unix.OPEN_TREE_CLONE|unix.OPEN_TREE_CLOEXEC)
	if err != nil {
		return fmt.Errorf("open_tree(%s) failed: %w", targetPath, err)
	}
	defer unix.Close(treeFd)

	// Apply the idmap to the cloned mount
	attr := unix.MountAttr{
		Attr_set:  unix.MOUNT_ATTR_IDMAP,
		Userns_fd: uint64(usernsFd),
	}
	if err := unix.MountSetattr(treeFd, "", unix.AT_EMPTY_PATH, &attr); err != nil {
		return fmt.Errorf("mount_setattr(MOUNT_ATTR_IDMAP) failed: %w", err)
	}

	// Unmount the original overlay
	if err := unix.Unmount(targetPath, 0); err != nil {
		return fmt.Errorf("failed to unmount original overlay at %s: %w", targetPath, err)
	}

	// Move the idmapped mount into place
	if err := unix.MoveMount(treeFd, "", -1, targetPath, unix.MOVE_MOUNT_F_EMPTY_PATH); err != nil {
		return fmt.Errorf("move_mount to %s failed: %w", targetPath, err)
	}

	slog.Info("applied idmapped mount on overlay",
		"component", "sandbox",
		"container", containerName,
		"target", targetPath,
	)

	return nil
}

// preseedIdmap copies volatile.idmap.next into volatile.last_state.idmap so
// that Incus believes the container's rootfs is already UID-shifted to the
// correct idmap. Combined with applyIdmappedMount (which actually applies the
// idmap at the VFS level), this prevents Incus from attempting its own
// shift/idmap on start.
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

// setupIdmappedOverlay applies an idmapped mount on the container's overlay
// rootfs and pre-seeds the Incus idmap state. This is the complete sequence
// needed for unprivileged overlay containers:
//
//  1. applyIdmappedMount — remounts the overlay with UID/GID translation
//  2. preseedIdmap — tells Incus the rootfs is already shifted
//
// On privileged containers, this is a no-op.
func setupIdmappedOverlay(client *IncusClient, containerName, poolSourcePath string) error {
	if IsPrivileged() {
		return nil
	}

	containerRootfs := ContainerRootfsPath(poolSourcePath, containerName)

	// Apply the idmap at the VFS level
	if err := applyIdmappedMount(client, containerName, containerRootfs); err != nil {
		return fmt.Errorf("failed to apply idmapped mount: %w", err)
	}

	// Tell Incus the rootfs is already shifted
	if err := preseedIdmap(client, containerName); err != nil {
		return fmt.Errorf("failed to pre-seed idmap: %w", err)
	}

	return nil
}

// reapplyIdmappedMount re-applies the idmap to an already-mounted overlay.
// Used when re-mounting overlays after a reboot (the mount loses its idmap).
// Unlike setupIdmappedOverlay, this does not pre-seed the idmap (it's already set).
func reapplyIdmappedMount(client *IncusClient, containerName, targetPath string) error {
	if IsPrivileged() {
		return nil
	}

	return applyIdmappedMount(client, containerName, targetPath)
}
