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

// idmapEntry matches the JSON format Incus uses for volatile.idmap.next.
type idmapEntry struct {
	Isuid    bool  `json:"Isuid"`
	Isgid    bool  `json:"Isgid"`
	Hostid   int64 `json:"Hostid"`
	Nsid     int64 `json:"Nsid"`
	Maprange int64 `json:"Maprange"`
}

// chownShiftOverlay recursively chowns the overlay's merged rootfs to shifted
// UIDs/GIDs matching the container's idmap. This is the key operation for
// unprivileged containers on kernels < 6.12 where overlayfs doesn't support
// FS_ALLOW_IDMAP (idmapped mounts can't be applied to the overlay itself).
//
// How it works:
//   - We mount a plain overlay (all files at UID/GID 0)
//   - Then chown every file to the shifted UID/GID (e.g., 0 → 1000000)
//   - This triggers copy-up for files from the lower layer: the chowned copy
//     lands in the upper layer while the lower (template snapshot) stays clean
//   - After chown, we pre-seed volatile.last_state.idmap to tell Incus the
//     rootfs is already shifted, so it skips its own (impossible) idmap attempt
//
// The cost is proportional to the rootfs size (~40k files for Ubuntu = 5-15s).
// This happens once per session/template container creation.
func chownShiftOverlay(containerRootfs string, entries []idmapEntry) error {
	if len(entries) == 0 {
		return nil
	}

	// Find the UID and GID host IDs from the idmap.
	// Typically: Isuid entry maps nsid 0 → hostid 1000000
	//            Isgid entry maps nsid 0 → hostid 1000000
	var uidShift, gidShift int64
	for _, e := range entries {
		if e.Isuid && e.Nsid == 0 {
			uidShift = e.Hostid
		}
		if e.Isgid && e.Nsid == 0 {
			gidShift = e.Hostid
		}
	}

	if uidShift == 0 && gidShift == 0 {
		return nil // no shift needed (identity map)
	}

	slog.Info("shifting overlay rootfs UIDs/GIDs",
		"component", "sandbox",
		"rootfs", containerRootfs,
		"uid_shift", uidShift,
		"gid_shift", gidShift,
	)

	// Recursive chown to shift all files from UID/GID 0 to the shifted values.
	// Since all files in the plain overlay start at UID/GID 0 (from the
	// template snapshot), the shifted UID = 0 + uidShift = uidShift.
	//
	// --from=0:0 ensures we only shift files that are currently at 0:0,
	// preventing double-shifting if this function is called again.
	// -Rh: recursive, operate on symlinks themselves (not targets).
	//
	// Stderr is suppressed because chown may emit harmless "No such file or
	// directory" warnings on overlayfs whiteout entries or very long paths
	// from the lower layer (e.g., Node.js OpenSSL headers). These don't
	// affect functionality — the important files are shifted correctly.
	cmd := exec.Command("chown", "-Rh", "--from=0:0",
		fmt.Sprintf("%d:%d", uidShift, gidShift),
		containerRootfs,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// chown -R returns exit code 1 if any individual file fails,
		// even if the vast majority succeeded. Check if it's just
		// harmless "No such file" errors from whiteouts/long paths.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			errMsg := stderr.String()
			// If all errors are "No such file or directory", treat as success
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
					"rootfs", containerRootfs,
					"warning_count", strings.Count(errMsg, "\n"),
				)
				return nil
			}
		}
		return fmt.Errorf("chown shift failed: %w\nstderr: %s", err, stderr.String())
	}

	return nil
}

// preseedIdmap copies volatile.idmap.next into volatile.last_state.idmap so
// that Incus believes the container's rootfs is already UID-shifted to the
// correct idmap. This prevents Incus from attempting its own idmap/shift on
// container start (which would fail on overlayfs with EINVAL on kernels < 6.12).
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

// setupUnprivilegedOverlay is the complete sequence for mounting an unprivileged
// overlay container's rootfs:
//
//  1. Mount a plain overlay (all files at UID 0)
//  2. Recursive chown to shift UIDs/GIDs to match the container's idmap
//  3. Pre-seed Incus idmap state so it skips its own shift
//
// On privileged containers, this falls back to a plain overlay mount (no shift).
func setupUnprivilegedOverlay(client *IncusClient, containerName, containerRootfs, lowerDir string) error {
	if IsPrivileged() {
		return mountPlainOverlay(containerName, containerRootfs, lowerDir)
	}

	// Mount plain overlay first (UID 0 everywhere)
	if err := mountPlainOverlay(containerName, containerRootfs, lowerDir); err != nil {
		return err
	}

	// Read the container's desired idmap
	inst, err := client.GetInstance(containerName)
	if err != nil {
		return fmt.Errorf("failed to read instance config: %w", err)
	}

	nextIdmap, ok := inst.Config["volatile.idmap.next"]
	if !ok || nextIdmap == "" || nextIdmap == "[]" {
		// No idmap — container is effectively privileged, nothing to shift
		return nil
	}

	var entries []idmapEntry
	if err := json.Unmarshal([]byte(nextIdmap), &entries); err != nil {
		return fmt.Errorf("failed to parse idmap: %w", err)
	}

	if len(entries) == 0 {
		return nil
	}

	// Chown the overlay rootfs to shifted UIDs
	if err := chownShiftOverlay(containerRootfs, entries); err != nil {
		return fmt.Errorf("failed to shift overlay UIDs: %w", err)
	}

	// Tell Incus the rootfs is already shifted
	if err := preseedIdmap(client, containerName); err != nil {
		return fmt.Errorf("failed to pre-seed idmap: %w", err)
	}

	return nil
}

// mountPlainOverlay creates upper/work dirs and mounts a plain (non-idmapped)
// overlay. Used for privileged containers or as the first step before chown shift.
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
// a remount. Only shifts files still at UID 0 (new from the fresh lower layer);
// already-shifted files in the upper layer are untouched thanks to --from=0:0.
// Called from RemountDependentOverlays in overlay.go.
func reshiftOverlayUIDs(client *IncusClient, containerName, containerRootfs string) error {
	inst, err := client.GetInstance(containerName)
	if err != nil {
		return err
	}

	nextIdmap, ok := inst.Config["volatile.idmap.next"]
	if !ok || nextIdmap == "" || nextIdmap == "[]" {
		return nil
	}

	var entries []idmapEntry
	if err := json.Unmarshal([]byte(nextIdmap), &entries); err != nil {
		return err
	}

	return chownShiftOverlay(containerRootfs, entries)
}
