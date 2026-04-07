package sandbox

import (
	"encoding/json"
	"fmt"
	"log/slog"
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

// ShiftTemplateRootfs recursively chowns a template container's rootfs to
// shifted UIDs matching the container's idmap. This MUST be called BEFORE
// CreateSnapshot, while the container's rootfs is still writable (btrfs
// snapshots are read-only).
//
// This is a ONE-TIME cost per template creation (sandbox init, template
// snapshot, promote). The snapshot captures the shifted state, so all session
// containers that use it as their overlay lower layer see pre-shifted files,
// enabling instant container start without per-session UID shifting.
//
// The shift handles ALL UIDs in the rootfs (root, system users, and regular
// users like the browser user). It enumerates unique UID:GID pairs below the
// shift base and runs a targeted chown for each, preserving per-user ownership
// distinctions (e.g., UID 0→1000000, UID 1001→1001001).
//
// Double-shifting is prevented by checking volatile.last_state.idmap before
// shifting — if it already matches the expected idmap, the rootfs was shifted
// in a previous run and we skip it.
//
// For privileged containers, this is a no-op.
func ShiftTemplateRootfs(client *IncusClient, templateName string) error {
	if IsPrivileged() {
		return nil
	}

	// Get the container rootfs path (writable, unlike the snapshot)
	poolName, err := GetPoolForProfile(client)
	if err != nil {
		return fmt.Errorf("failed to get pool name: %w", err)
	}

	poolPath, err := GetPoolSourcePath(client, poolName)
	if err != nil {
		return fmt.Errorf("failed to get pool path: %w", err)
	}

	containerName := TemplateName(templateName)
	containerRootfs := ContainerRootfsPath(poolPath, containerName)

	// Get the idmap from the template container
	inst, err := client.GetInstance(containerName)
	if err != nil {
		return fmt.Errorf("failed to read template instance: %w", err)
	}

	nextIdmap, ok := inst.Config["volatile.idmap.next"]
	if !ok || nextIdmap == "" || nextIdmap == "[]" {
		return nil
	}

	// Guard against double-shifting: if volatile.last_state.idmap already
	// matches the expected idmap, the rootfs was shifted in a previous run.
	if lastIdmap, hasLast := inst.Config["volatile.last_state.idmap"]; hasLast && lastIdmap == nextIdmap {
		slog.Debug("rootfs already shifted (idmap matches), skipping",
			"component", "sandbox",
			"template", templateName,
		)
		return nil
	}

	// Parse the idmap to get the UID/GID shift values
	uidShift, gidShift, err := parseIdmapShifts(nextIdmap)
	if err != nil {
		return fmt.Errorf("failed to parse idmap: %w", err)
	}

	if uidShift == 0 && gidShift == 0 {
		return nil
	}

	slog.Info("shifting template rootfs UIDs (one-time)",
		"component", "sandbox",
		"template", templateName,
		"rootfs", containerRootfs,
		"uid_shift", uidShift,
		"gid_shift", gidShift,
	)

	if err := chownShift(containerRootfs, uidShift, gidShift); err != nil {
		return err
	}

	// Also pre-seed the idmap on the template container so Incus knows the
	// rootfs is already shifted. This prevents Incus from trying to shift
	// again if the template container is ever started directly.
	return preseedIdmap(client, containerName)
}

// chownShift recursively shifts all UIDs and GIDs in a directory tree by the
// specified offset. It handles ALL users (root, system users, regular users)
// by enumerating unique UID:GID pairs below the shift base and running a
// targeted `chown --from=U:G (U+shift):(G+shift)` for each pair.
//
// This is more correct than the previous --from=0:0 approach which only
// shifted root-owned files, leaving non-root users (e.g., the browser user
// at UID 1001) unshifted and unmapped inside the container.
//
// Dispatches through execOnSandboxHost so it works on both native Linux
// and Docker+Incus (where the chown runs inside the Docker container).
func chownShift(rootfs string, uidShift, gidShift int64) error {
	// Enumerate all unique UID:GID pairs in the rootfs that are below
	// the shift base (i.e., not yet shifted). Then run a targeted
	// `chown -Rh --from=U:G (U+uidShift):(G+gidShift)` for each pair.
	// Using --from ensures only files matching the exact original UID:GID
	// are touched, preserving per-user ownership distinctions.
	//
	// The shell script:
	// 1. find -printf '%U:%G\n' — prints numeric uid:gid for every file
	// 2. sort -u — deduplicates (typically ~20-30 unique pairs)
	// 3. For each pair where uid < shift_base, run chown --from
	//
	// This is efficient: the find pass is a single directory walk, and
	// each subsequent chown --from pass only stats+chowns matching files.
	script := fmt.Sprintf(`
set -e
ROOTFS=%q
UID_SHIFT=%d
GID_SHIFT=%d

# Collect unique uid:gid pairs below the shift base
pairs=$(find "$ROOTFS" -printf '%%U:%%G\n' 2>/dev/null | sort -un)

for pair in $pairs; do
    u="${pair%%:*}"
    g="${pair#*:}"
    # Only shift UIDs that haven't been shifted yet (below the shift base)
    if [ "$u" -lt "$UID_SHIFT" ] && [ "$g" -lt "$GID_SHIFT" ]; then
        new_u=$((u + UID_SHIFT))
        new_g=$((g + GID_SHIFT))
        chown -Rh --from="$u:$g" "$new_u:$new_g" "$ROOTFS" 2>/dev/null || true
    fi
done
`, rootfs, uidShift, gidShift)

	output, err := execOnSandboxHost([]string{"sh", "-c", script})
	if err != nil {
		errMsg := string(output)
		// Filter harmless "No such file" errors (race with transient files)
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
		return fmt.Errorf("chown shift failed: %w\nstderr: %s", err, errMsg)
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
func reshiftOverlayUIDs(client *IncusClient, containerName string) error {
	if IsPrivileged() {
		return nil
	}

	return preseedIdmap(client, containerName)
}
