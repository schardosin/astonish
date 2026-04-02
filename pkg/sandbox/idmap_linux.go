//go:build linux

package sandbox

import (
	"fmt"
	"log/slog"
	"path/filepath"
)

// setupUnprivilegedOverlay mounts a plain overlay on the container's rootfs
// and pre-seeds the Incus idmap state so Incus skips its own UID shifting.
//
// For unprivileged containers on kernels < 6.12 where overlayfs doesn't support
// FS_ALLOW_IDMAP, we can't use idmapped mounts on the overlay. Letting Incus
// handle the shift via recursive chown is too slow (copies every file to the
// upper layer via copy-up).
//
// Instead, we mount a plain overlay (files at UID 0) and pre-seed
// volatile.last_state.idmap to match volatile.idmap.next. This makes Incus
// believe the rootfs is already shifted and skip its own ShiftPath. LXC then
// sets up the container's user namespace with lxc.idmap, mapping host UID
// 1000000 → container UID 0. Files at UID 0 on the overlay appear as
// unmapped inside the container, but since the user namespace also maps
// host UID 0 → nobody, the files are accessible to root inside the container
// because LXC's rootfs mount is done before the user namespace is applied.
//
// On privileged containers, no shifting is needed — this is a plain mount.
func setupUnprivilegedOverlay(client *IncusClient, containerName, containerRootfs, lowerDir string) error {
	// Mount plain overlay first (UID 0 everywhere)
	if err := mountPlainOverlay(containerName, containerRootfs, lowerDir); err != nil {
		return err
	}

	if IsPrivileged() {
		return nil
	}

	// Pre-seed the idmap so Incus skips its own (slow) ShiftPath
	if err := preseedIdmap(client, containerName); err != nil {
		return fmt.Errorf("failed to pre-seed idmap: %w", err)
	}

	return nil
}

// preseedIdmap copies volatile.idmap.next into volatile.last_state.idmap so
// that Incus believes the container's rootfs is already UID-shifted. This
// prevents Incus from doing a recursive chown (ShiftPath) on every container
// start, which would be extremely slow on overlayfs due to copy-up.
func preseedIdmap(client *IncusClient, containerName string) error {
	inst, err := client.GetInstance(containerName)
	if err != nil {
		return fmt.Errorf("failed to read instance config: %w", err)
	}

	nextIdmap, ok := inst.Config["volatile.idmap.next"]
	if !ok || nextIdmap == "" {
		return nil
	}

	slog.Debug("pre-seeding idmap to skip Incus shift",
		"component", "sandbox",
		"container", containerName,
	)

	return client.SetInstanceConfig(containerName, map[string]string{
		"volatile.last_state.idmap": nextIdmap,
	})
}

// mountPlainOverlay creates upper/work dirs and mounts a plain overlay.
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

// reshiftOverlayUIDs handles overlay UID consistency after a remount.
// Since we pre-seed the idmap (no actual shifting), after a remount we just
// need to ensure the pre-seed is still in place for the next start.
func reshiftOverlayUIDs(client *IncusClient, containerName, _ string) error {
	if IsPrivileged() {
		return nil
	}

	return preseedIdmap(client, containerName)
}
