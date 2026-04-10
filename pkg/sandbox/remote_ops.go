package sandbox

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// activePlatform stores the detected platform for the current process.
// Set during SetupSandboxRuntime() and used by remote ops functions to
// decide whether to execute locally (Linux native) or via docker exec
// (Docker+Incus on macOS/Windows).
var activePlatform Platform = PlatformUnsupported

// SetActivePlatform sets the package-level platform used by remote ops.
// Called once during sandbox initialization.
func SetActivePlatform(p Platform) {
	activePlatform = p
}

// GetActivePlatform returns the current active platform.
func GetActivePlatform() Platform {
	return activePlatform
}

// execOnSandboxHost runs a command where the Incus daemon lives.
// On Linux native: executes locally via exec.Command.
// On Docker+Incus: executes inside the Docker container via docker exec.
// Only known sandbox commands are allowed to prevent command injection.
func execOnSandboxHost(args []string) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("execOnSandboxHost: empty command")
	}

	// Allowlist of commands that sandbox operations may execute
	allowed := map[string]bool{
		"cat": true, "cp": true, "ls": true, "mkdir": true,
		"mount": true, "rm": true, "rsync": true, "test": true, "umount": true,
	}
	if !allowed[args[0]] {
		return nil, fmt.Errorf("execOnSandboxHost: command %q not allowed", args[0])
	}

	switch activePlatform {
	case PlatformLinuxNative:
		cmd := exec.Command(args[0], args[1:]...) // #nosec G204 -- command allowlisted above
		return cmd.CombinedOutput()

	case PlatformDockerIncus:
		return ExecInDockerHost(args)

	default:
		return nil, fmt.Errorf("execOnSandboxHost: unsupported platform %s", activePlatform)
	}
}

// statOnSandboxHost checks if a path exists on the sandbox host filesystem.
// Returns nil if the path exists, an error otherwise.
func statOnSandboxHost(path string) error {
	switch activePlatform {
	case PlatformLinuxNative:
		_, err := os.Stat(path)
		return err

	case PlatformDockerIncus:
		_, err := ExecInDockerHost([]string{"test", "-e", path})
		return err

	default:
		return fmt.Errorf("statOnSandboxHost: unsupported platform %s", activePlatform)
	}
}

// validateAbsPath ensures that a path is absolute, clean, and free of
// traversal sequences, preventing path injection in sandbox filesystem operations.
func validateAbsPath(path string) error {
	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		return fmt.Errorf("path must be absolute: %s", path)
	}
	if strings.Contains(cleaned, "..") {
		return fmt.Errorf("path must not contain traversal sequences: %s", path)
	}
	return nil
}

// mkdirAllOnSandboxHost creates a directory and all parents on the sandbox host.
func mkdirAllOnSandboxHost(path string, perm os.FileMode) error {
	if err := validateAbsPath(path); err != nil {
		return fmt.Errorf("mkdirAllOnSandboxHost: %w", err)
	}
	// Path has been validated: absolute, clean, and no ".." sequences
	cleanPath := filepath.Clean(path)
	switch activePlatform {
	case PlatformLinuxNative:
		return os.MkdirAll(cleanPath, perm)

	case PlatformDockerIncus:
		_, err := ExecInDockerHost([]string{"mkdir", "-p", path})
		return err

	default:
		return fmt.Errorf("mkdirAllOnSandboxHost: unsupported platform %s", activePlatform)
	}
}

// removeAllOnSandboxHost removes a path and all children on the sandbox host.
func removeAllOnSandboxHost(path string) error {
	switch activePlatform {
	case PlatformLinuxNative:
		return os.RemoveAll(path)

	case PlatformDockerIncus:
		_, err := ExecInDockerHost([]string{"rm", "-rf", path})
		return err

	default:
		return fmt.Errorf("removeAllOnSandboxHost: unsupported platform %s", activePlatform)
	}
}

// readFileOnSandboxHost reads a file from the sandbox host filesystem.
func readFileOnSandboxHost(path string) ([]byte, error) {
	switch activePlatform {
	case PlatformLinuxNative:
		return os.ReadFile(path)

	case PlatformDockerIncus:
		return ExecInDockerHost([]string{"cat", path})

	default:
		return nil, fmt.Errorf("readFileOnSandboxHost: unsupported platform %s", activePlatform)
	}
}

// readDirOnSandboxHost lists directory entries on the sandbox host filesystem.
// Returns a list of entry names. Directories have a trailing "/".
func readDirOnSandboxHost(path string) ([]string, error) {
	switch activePlatform {
	case PlatformLinuxNative:
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name()+"/")
			} else {
				names = append(names, e.Name())
			}
		}
		return names, nil

	case PlatformDockerIncus:
		output, err := ExecInDockerHost([]string{"ls", "-1", path})
		if err != nil {
			return nil, err
		}
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		if len(lines) == 1 && lines[0] == "" {
			return nil, nil
		}
		return lines, nil

	default:
		return nil, fmt.Errorf("readDirOnSandboxHost: unsupported platform %s", activePlatform)
	}
}

// mountOverlayOnSandboxHost runs `mount -t overlay` on the sandbox host.
func mountOverlayOnSandboxHost(opts, target string) error {
	output, err := execOnSandboxHost([]string{
		"mount", "-t", "overlay", "overlay", "-o", opts, target,
	})
	if err != nil {
		return fmt.Errorf("failed to mount overlayfs on %s: %w\nOutput: %s", target, err, string(output))
	}
	return nil
}

// umountOnSandboxHost runs `umount` on the sandbox host.
func umountOnSandboxHost(target string) error {
	_, err := execOnSandboxHost([]string{"umount", target})
	return err
}

// readMountsOnSandboxHost reads /proc/mounts from the sandbox host.
func readMountsOnSandboxHost() ([]byte, error) {
	return readFileOnSandboxHost("/proc/mounts")
}

// isOverlayMountedOnSandboxHost checks if an overlay is mounted at the given
// container rootfs path on the sandbox host.
func isOverlayMountedOnSandboxHost(containerRootfs string) bool {
	data, err := readMountsOnSandboxHost()
	if err != nil {
		return false
	}

	for _, line := range bytes.Split(data, []byte("\n")) {
		fields := bytes.Fields(line)
		if len(fields) >= 3 && string(fields[1]) == containerRootfs && string(fields[2]) == "overlay" {
			return true
		}
	}

	return false
}

// rsyncOnSandboxHost runs rsync on the sandbox host filesystem.
func rsyncOnSandboxHost(src, dst string) error {
	output, err := execOnSandboxHost([]string{"rsync", "-a", "--delete", "--", src, dst})
	if err != nil {
		return fmt.Errorf("rsync failed: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// cpOnSandboxHost runs cp -a on the sandbox host filesystem.
func cpOnSandboxHost(src, dst string) error {
	output, err := execOnSandboxHost([]string{"cp", "-a", "--", src, dst})
	if err != nil {
		return fmt.Errorf("cp failed: %w\nOutput: %s", err, string(output))
	}
	return nil
}
