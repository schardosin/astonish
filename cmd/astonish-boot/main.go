// Command astonish-boot is the container ENTRYPOINT for OpenShell-managed
// sandbox pods. It performs overlay composition and pivot_root before exec'ing
// the OpenShell supervisor binary.
//
// The binary is installed at /opt/astonish/bin/astonish-boot in the container
// image and referenced by ENTRYPOINT in docker/sandbox-openshell/Dockerfile.
//
// Environment variables:
//
//	ASTONISH_LAYER_CHAIN (required): comma-separated layer IDs, oldest→newest.
//	ASTONISH_SESSION_ID  (required): session identifier for upper persistence.
//	ASTONISH_LAYERS_DIR  (optional): layers PVC mount. Default: /mnt/astonish-layers.
//	ASTONISH_UPPERS_DIR  (optional): uppers PVC mount. Default: /mnt/astonish-uppers.
//	ASTONISH_OVERLAY_DIR (optional): overlay working directory. Default: /overlay.
//	ASTONISH_OVERLAY_MODE (optional): "fuse" (default), "kernel", or "auto".
//	ASTONISH_SUPERVISOR_ARGS (optional): extra args for openshell-sandbox.
//
// Execution flow:
//  1. Parse environment
//  2. Resume evicted upper (extract upper.tar.zst if present)
//  3. Pre-seed upper with first-level directories from lowest layer
//  4. Compose overlay (fuse-overlayfs or kernel mount)
//  5. Copy infrastructure binaries into overlay upper
//  6. Bind-mount kernel filesystems (/dev, /proc, /sys) + /etc/resolv.conf
//  7. Bind-mount raw upper + PVCs into overlay (for post-pivot eviction)
//  8. pivot_root into the composed overlay
//  9. Unmount + remove old root
//  10. exec openshell-sandbox
//
// See docs/architecture/openshell-sandbox-backend.md §5 for the full design.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "astonish-boot: fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// --- 1. Parse environment ---
	layerChain := requireEnv("ASTONISH_LAYER_CHAIN")
	sessionID := requireEnv("ASTONISH_SESSION_ID")
	layersDir := envOr("ASTONISH_LAYERS_DIR", "/mnt/astonish-layers")
	uppersDir := envOr("ASTONISH_UPPERS_DIR", "/mnt/astonish-uppers")
	overlayDir := envOr("ASTONISH_OVERLAY_DIR", "/overlay")
	overlayMode := envOr("ASTONISH_OVERLAY_MODE", "fuse")

	upperDir := filepath.Join(overlayDir, "upper")
	workDir := filepath.Join(overlayDir, "work")
	merged := filepath.Join(overlayDir, "merged")

	logf("starting: session=%s layers=%s overlay_mode=%s", sessionID, layerChain, overlayMode)

	// Ensure overlay directories exist.
	for _, d := range []string{upperDir, workDir, merged} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	// --- 2. Resume evicted upper (if exists) ---
	resumeTar := filepath.Join(uppersDir, sessionID, "upper.tar.zst")
	if fileExists(resumeTar) {
		logf("resuming upper from %s", resumeTar)
		if err := os.MkdirAll(upperDir, 0755); err != nil {
			return fmt.Errorf("mkdir upper for resume: %w", err)
		}
		if err := runCmd("tar", "--numeric-owner", "--xattrs", "--acls",
			"-I", "zstd", "-xf", resumeTar, "-C", upperDir); err != nil {
			return fmt.Errorf("resume tar extraction: %w", err)
		}
	}

	// --- 3. Build lowerdir string (reverse layer chain) ---
	lower, err := buildLowerDir(layerChain, layersDir)
	if err != nil {
		return err
	}
	logf("lowerdir: %s", lower)

	// --- 3b. Pre-seed upper with first-level dirs from lowest layer ---
	if err := preseedUpper(lower, upperDir); err != nil {
		logf("warning: pre-seed upper: %v", err)
		// Non-fatal; continue.
	}

	// Ensure /dev, /proc, /sys exist in upper for post-overlay bind-mounts.
	for _, d := range []string{"dev", "proc", "sys"} {
		_ = os.MkdirAll(filepath.Join(upperDir, d), 0755)
	}

	// --- 4. Compose overlay ---
	if err := mountOverlay(overlayMode, lower, upperDir, workDir, merged); err != nil {
		return fmt.Errorf("overlay mount: %w", err)
	}
	logf("overlay mounted at %s", merged)

	// --- 5. Copy infrastructure binaries into overlay upper ---
	binaries := []struct {
		src string
		dst string
	}{
		{"/opt/astonish/bin/astonish", filepath.Join(upperDir, "usr/local/bin/astonish")},
		{"/opt/openshell/bin/openshell-sandbox", filepath.Join(upperDir, "usr/local/bin/openshell-sandbox")},
	}
	for _, b := range binaries {
		if !fileExists(b.src) {
			logf("warning: binary %s not found, skipping injection", b.src)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(b.dst), 0755); err != nil {
			return fmt.Errorf("mkdir for binary %s: %w", b.dst, err)
		}
		if err := copyFile(b.src, b.dst); err != nil {
			return fmt.Errorf("copy binary %s → %s: %w", b.src, b.dst, err)
		}
		logf("injected %s", b.dst)
	}

	// --- 6. Bind-mount kernel filesystems into overlay ---
	for _, fs := range []string{"/dev", "/proc", "/sys"} {
		dst := filepath.Join(merged, fs)
		if err := os.MkdirAll(dst, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dst, err)
		}
		if err := bindMount(fs, dst); err != nil {
			logf("warning: rbind %s → %s: %v", fs, dst, err)
		}
	}

	// Bind /etc/resolv.conf for DNS resolution inside the overlay.
	resolvSrc := "/etc/resolv.conf"
	resolvDst := filepath.Join(merged, "etc/resolv.conf")
	if fileExists(resolvSrc) {
		// Ensure parent dir and target file exist.
		_ = os.MkdirAll(filepath.Dir(resolvDst), 0755)
		if !fileExists(resolvDst) {
			_ = os.WriteFile(resolvDst, nil, 0644)
		}
		if err := syscall.Mount(resolvSrc, resolvDst, "", syscall.MS_BIND, ""); err != nil {
			logf("warning: bind resolv.conf: %v", err)
		}
	}

	// --- 7. Bind-mount raw upper + PVCs into overlay for eviction/capture ---
	postPivotMounts := []struct {
		src string
		dst string
	}{
		{upperDir, filepath.Join(merged, "var/astonish/upper")},
		{uppersDir, filepath.Join(merged, "mnt/uppers")},
		{layersDir, filepath.Join(merged, "mnt/layers")},
	}
	for _, m := range postPivotMounts {
		if err := os.MkdirAll(m.dst, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", m.dst, err)
		}
		if err := bindMount(m.src, m.dst); err != nil {
			return fmt.Errorf("bind %s → %s: %w", m.src, m.dst, err)
		}
		logf("bound %s → %s", m.src, m.dst)
	}

	// --- 8. pivot_root ---
	pivotOld := filepath.Join(merged, ".pivot_old")
	if err := os.MkdirAll(pivotOld, 0755); err != nil {
		return fmt.Errorf("mkdir pivot_old: %w", err)
	}
	if err := syscall.PivotRoot(merged, pivotOld); err != nil {
		return fmt.Errorf("pivot_root(%s, %s): %w", merged, pivotOld, err)
	}
	logf("pivot_root complete")

	// After pivot_root, the old root is at /.pivot_old.
	// Change directory to / so we don't hold a reference to the old root.
	if err := os.Chdir("/"); err != nil {
		return fmt.Errorf("chdir /: %w", err)
	}

	// --- 9. Unmount + remove old root ---
	if err := syscall.Unmount("/.pivot_old", syscall.MNT_DETACH); err != nil {
		logf("warning: unmount /.pivot_old: %v (continuing)", err)
	}
	// Best-effort removal; may fail if lazy unmount hasn't fully propagated.
	_ = os.RemoveAll("/.pivot_old")
	logf("old root unmounted")

	// --- 10. exec openshell-sandbox ---
	supervisorBin := "/usr/local/bin/openshell-sandbox"
	supervisorArgs := buildSupervisorArgs()
	logf("exec: %s %v", supervisorBin, supervisorArgs)

	argv := append([]string{supervisorBin}, supervisorArgs...)
	return syscall.Exec(supervisorBin, argv, os.Environ())
}

// --- Helpers ---

func requireEnv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		fmt.Fprintf(os.Stderr, "astonish-boot: fatal: %s must be set\n", key)
		os.Exit(1)
	}
	return val
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "astonish-boot: "+format+"\n", args...)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// buildLowerDir reverses the comma-separated layer chain and builds the
// colon-separated lowerdir string. Overlayfs wants the topmost layer
// first; ASTONISH_LAYER_CHAIN is oldest-first.
func buildLowerDir(chain, layersDir string) (string, error) {
	layers := strings.Split(chain, ",")
	if len(layers) == 0 || (len(layers) == 1 && layers[0] == "") {
		return "", fmt.Errorf("empty ASTONISH_LAYER_CHAIN")
	}

	// Reverse: newest (last) becomes first lowerdir.
	reversed := make([]string, 0, len(layers))
	for i := len(layers) - 1; i >= 0; i-- {
		id := strings.TrimSpace(layers[i])
		if id == "" {
			continue
		}
		reversed = append(reversed, filepath.Join(layersDir, id, "rootfs"))
	}
	if len(reversed) == 0 {
		return "", fmt.Errorf("ASTONISH_LAYER_CHAIN contains only empty entries")
	}
	return strings.Join(reversed, ":"), nil
}

// preseedUpper creates first-level directories from the lowest layer
// (last in the colon-separated lowerdir) in the upper dir. This avoids
// cross-device rename failures in fuse-overlayfs on NFS-backed lowerdirs.
func preseedUpper(lower, upperDir string) error {
	parts := strings.Split(lower, ":")
	bottomLayer := parts[len(parts)-1]

	entries, err := os.ReadDir(bottomLayer)
	if err != nil {
		return fmt.Errorf("read bottom layer %s: %w", bottomLayer, err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Skip symlinks that resolve as directories.
		info, err := os.Lstat(filepath.Join(bottomLayer, e.Name()))
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		dst := filepath.Join(upperDir, e.Name())
		if _, err := os.Stat(dst); err == nil {
			continue // Already exists (from resume path).
		}
		if err := os.MkdirAll(dst, 0755); err != nil {
			return err
		}
	}
	return nil
}

// mountOverlay composes the overlay using the specified mode.
func mountOverlay(mode, lower, upper, work, merged string) error {
	switch mode {
	case "kernel":
		return mountOverlayKernel(lower, upper, work, merged)
	case "auto":
		if err := mountOverlayKernel(lower, upper, work, merged); err != nil {
			logf("kernel overlayfs failed (%v), falling back to fuse", err)
			return mountOverlayFuse(lower, upper, work, merged)
		}
		return nil
	default: // "fuse"
		return mountOverlayFuse(lower, upper, work, merged)
	}
}

func mountOverlayKernel(lower, upper, work, merged string) error {
	opts := fmt.Sprintf("userxattr,lowerdir=%s,upperdir=%s,workdir=%s", lower, upper, work)
	logf("trying kernel overlayfs")
	return syscall.Mount("overlay", merged, "overlay", 0, opts)
}

func mountOverlayFuse(lower, upper, work, merged string) error {
	logf("trying fuse-overlayfs")

	// Ensure /dev/fuse exists (privileged pod without device plugin).
	if !fileExists("/dev/fuse") {
		_ = runCmd("mknod", "/dev/fuse", "c", "10", "229")
		_ = os.Chmod("/dev/fuse", 0666)
	}

	opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s,squash_to_root", lower, upper, work)
	cmd := exec.Command("fuse-overlayfs", "-o", opts, merged)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("fuse-overlayfs: %w", err)
	}

	// Poll for mount readiness (fuse-overlayfs daemonizes).
	for i := 0; i < 100; i++ {
		if isMountpoint(merged) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("fuse-overlayfs mount did not appear within 5s at %s", merged)
}

// isMountpoint checks /proc/self/mountinfo for the given path.
func isMountpoint(path string) bool {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	// mountinfo format: each line has space-separated fields, field 5 is the mount point.
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[4] == path {
			return true
		}
	}
	return false
}

// bindMount performs an rbind mount with rslave propagation.
func bindMount(src, dst string) error {
	flags := uintptr(syscall.MS_BIND | syscall.MS_REC)
	if err := syscall.Mount(src, dst, "", flags, ""); err != nil {
		return err
	}
	// Make rslave so unmounts inside don't propagate back.
	slaveFlags := uintptr(syscall.MS_SLAVE | syscall.MS_REC)
	_ = syscall.Mount("", dst, "", slaveFlags, "")
	return nil
}

// copyFile copies src to dst, preserving executable permissions.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// runCmd runs a command and returns an error if it fails.
func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// buildSupervisorArgs constructs arguments for the openshell-sandbox
// supervisor. It passes through ASTONISH_SUPERVISOR_ARGS if set, otherwise
// uses sensible defaults.
func buildSupervisorArgs() []string {
	if extra := os.Getenv("ASTONISH_SUPERVISOR_ARGS"); extra != "" {
		return strings.Fields(extra)
	}
	// Default: start supervisor in daemon mode.
	return nil
}
