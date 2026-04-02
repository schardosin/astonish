//go:build linux

package sandbox

import (
	"fmt"
	"log/slog"
	"path/filepath"
)

// setupUnprivilegedOverlay mounts a plain overlay on the container's rootfs.
//
// For unprivileged containers, Incus handles the UID/GID shifting automatically
// during container start via handleIdmappedStorage(). On filesystems that support
// idmapped mounts (btrfs, ext4), Incus uses kernel-level idmapped mounts. On
// overlayfs (which doesn't support FS_ALLOW_IDMAP on kernels < 6.12), Incus
// falls back to a recursive chown (ShiftPath) to shift file ownership to match
// the container's user namespace mapping (e.g., UID 0 → 1000000).
//
// We do NOT pre-seed volatile.last_state.idmap — we let Incus detect the
// mismatch between diskIdmap (empty) and nextIdmap (shifted), perform the
// shift itself, and record the result. This ensures Incus's template engine,
// AppArmor setup, and other pre-start operations all see consistent UIDs.
//
// On privileged containers, no shifting is needed — this is a plain mount.
func setupUnprivilegedOverlay(_ *IncusClient, containerName, containerRootfs, lowerDir string) error {
	return mountPlainOverlay(containerName, containerRootfs, lowerDir)
}

// mountPlainOverlay creates upper/work dirs and mounts a plain overlay.
// Used for both privileged containers and as the base for unprivileged
// containers (where Incus handles UID shifting on container start).
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

// reshiftOverlayUIDs re-applies the UID/GID shift on an overlay rootfs after
// a remount (e.g., after a template snapshot is recreated). Only shifts files
// still at UID 0 (new from the fresh lower layer); already-shifted files in
// the upper layer are untouched thanks to --from=0:0.
//
// This is needed because RemountDependentOverlays creates a fresh overlay mount
// with a new lower layer, but the container's volatile.last_state.idmap still
// says the rootfs is shifted. The fresh lower files are at UID 0 (unshifted)
// and need to be shifted to match.
//
// We call Incus's own shift mechanism by resetting volatile.last_state.idmap
// to empty, which makes Incus re-shift on the next container start. For
// containers that are restarted immediately after remount (the normal case
// in RemountDependentOverlays), this happens automatically.
func reshiftOverlayUIDs(client *IncusClient, containerName, _ string) error {
	if IsPrivileged() {
		return nil
	}

	// Reset the disk idmap to empty so Incus detects a mismatch on next start
	// and performs its own shift (recursive chown) of the rootfs.
	inst, err := client.GetInstance(containerName)
	if err != nil {
		return err
	}

	nextIdmap, ok := inst.Config["volatile.idmap.next"]
	if !ok || nextIdmap == "" || nextIdmap == "[]" {
		return nil
	}

	slog.Info("resetting disk idmap for re-shift on next start",
		"component", "sandbox",
		"container", containerName,
	)

	return client.SetInstanceConfig(containerName, map[string]string{
		"volatile.last_state.idmap": "[]",
	})
}
