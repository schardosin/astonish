//go:build linux

package sandbox

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
)

// setupUnprivilegedOverlay mounts a plain overlay on the container's rootfs
// and pre-seeds the Incus idmap state so Incus skips its own UID shifting.
//
// The key insight: template snapshots (lower layers) are pre-shifted to the
// container's idmap UIDs during template creation (one-time cost). Session
// containers mount these pre-shifted snapshots as their overlay lower layer,
// so all files are already at the correct UIDs. Pre-seeding the idmap tells
// Incus the rootfs is already shifted, making container start instant.
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

// ShiftSnapshotUIDs recursively chowns a template snapshot's rootfs to shifted
// UIDs matching the container's idmap. This is a ONE-TIME cost per template
// snapshot creation (during sandbox init, template snapshot, or promote).
//
// After shifting, all session containers that use this snapshot as their overlay
// lower layer will see pre-shifted files, allowing instant container start
// without any per-session UID shifting.
//
// The shift uses chown --from=0:0 to only shift files at UID/GID 0 (the default
// for freshly created containers), preventing double-shifting if called again.
//
// For privileged containers, this is a no-op.
func ShiftSnapshotUIDs(client *IncusClient, templateName string) error {
	if IsPrivileged() {
		return nil
	}

	// Get the snapshot rootfs path
	poolName, err := GetPoolForProfile(client)
	if err != nil {
		return fmt.Errorf("failed to get pool name: %w", err)
	}

	poolPath, err := GetPoolSourcePath(client, poolName)
	if err != nil {
		return fmt.Errorf("failed to get pool path: %w", err)
	}

	snapRootfs := SnapshotRootfsPath(poolPath, templateName)

	// Get the idmap from the template container
	containerName := TemplateName(templateName)
	inst, err := client.GetInstance(containerName)
	if err != nil {
		return fmt.Errorf("failed to read template instance: %w", err)
	}

	nextIdmap, ok := inst.Config["volatile.idmap.next"]
	if !ok || nextIdmap == "" || nextIdmap == "[]" {
		return nil
	}

	// Parse the idmap to get the UID/GID shift values
	// Format: [{"Isuid":true,"Isgid":false,"Hostid":1000000,"Nsid":0,"Maprange":1000000000},...]
	uidShift, gidShift, err := parseIdmapShifts(nextIdmap)
	if err != nil {
		return fmt.Errorf("failed to parse idmap: %w", err)
	}

	if uidShift == 0 && gidShift == 0 {
		return nil
	}

	slog.Info("shifting template snapshot UIDs (one-time)",
		"component", "sandbox",
		"template", templateName,
		"rootfs", snapRootfs,
		"uid_shift", uidShift,
		"gid_shift", gidShift,
	)

	return chownShift(snapRootfs, uidShift, gidShift)
}

// chownShift recursively chowns a directory tree from UID/GID 0 to the
// specified shifted values. Uses --from=0:0 to skip already-shifted files.
func chownShift(rootfs string, uidShift, gidShift int64) error {
	cmd := exec.Command("chown", "-Rh", "--from=0:0",
		fmt.Sprintf("%d:%d", uidShift, gidShift),
		rootfs,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// chown -R returns exit code 1 if any individual file fails,
		// even if the vast majority succeeded. Check if it's just
		// harmless "No such file" errors.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			errMsg := stderr.String()
			hasRealError := false
			for _, line := range strings.Split(errMsg, "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				if !strings.Contains(line, "No such file or directory") {
					hasRealError = true
					break
				}
			}
			if !hasRealError {
				slog.Debug("chown shift completed with harmless warnings",
					"component", "sandbox",
					"rootfs", rootfs,
					"warning_count", strings.Count(errMsg, "\n"),
				)
				return nil
			}
		}
		return fmt.Errorf("chown shift failed: %w\nstderr: %s", err, stderr.String())
	}

	return nil
}

// parseIdmapShifts extracts the UID and GID shift values from an Incus idmap
// JSON string.
func parseIdmapShifts(idmapJSON string) (int64, int64, error) {
	type idmapEntry struct {
		Isuid    bool  `json:"Isuid"`
		Isgid    bool  `json:"Isgid"`
		Hostid   int64 `json:"Hostid"`
		Nsid     int64 `json:"Nsid"`
		Maprange int64 `json:"Maprange"`
	}

	// Use json.Decoder for parsing
	var entries []idmapEntry
	if err := json.Unmarshal([]byte(idmapJSON), &entries); err != nil {
		return 0, 0, err
	}

	var uidShift, gidShift int64
	for _, e := range entries {
		if e.Isuid && e.Nsid == 0 {
			uidShift = e.Hostid
		}
		if e.Isgid && e.Nsid == 0 {
			gidShift = e.Hostid
		}
	}

	return uidShift, gidShift, nil
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
