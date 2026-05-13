package incus

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// activePlatform stores the detected platform for the current process.
// Set during sandbox initialization via SetActivePlatform() and used by the
// remote-ops helpers to decide whether to execute locally (Linux native) or
// via docker exec (Docker+Incus on macOS/Windows).
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

// ExecOnSandboxHost runs a command where the Incus daemon lives.
// On Linux native: executes locally via exec.Command.
// On Docker+Incus: executes inside the Docker container via docker exec.
// Only known sandbox commands are allowed to prevent command injection.
func ExecOnSandboxHost(args []string) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("ExecOnSandboxHost: empty command")
	}

	// Resolve command name to a safe constant via allowlist.
	// By assigning the constant string from the switch to safeCmd,
	// the value passed to exec.Command is never the user-provided args[0].
	var safeCmd string
	switch args[0] {
	case "cat":
		safeCmd = "cat"
	case "cp":
		safeCmd = "cp"
	case "ls":
		safeCmd = "ls"
	case "mkdir":
		safeCmd = "mkdir"
	case "mount":
		safeCmd = "mount"
	case "rm":
		safeCmd = "rm"
	case "rsync":
		safeCmd = "rsync"
	case "sh":
		safeCmd = "sh"
	case "test":
		safeCmd = "test"
	case "umount":
		safeCmd = "umount"
	default:
		return nil, fmt.Errorf("ExecOnSandboxHost: command %q not allowed", args[0])
	}

	switch activePlatform {
	case PlatformLinuxNative:
		cmd := exec.Command(safeCmd, args[1:]...)
		return cmd.CombinedOutput()

	case PlatformDockerIncus:
		return ExecInDockerHost(append([]string{safeCmd}, args[1:]...))

	default:
		return nil, fmt.Errorf("ExecOnSandboxHost: unsupported platform %s", activePlatform)
	}
}

// StatOnSandboxHost checks if a path exists on the sandbox host filesystem.
// Returns nil if the path exists, an error otherwise.
func StatOnSandboxHost(path string) error {
	switch activePlatform {
	case PlatformLinuxNative:
		_, err := os.Stat(path)
		return err

	case PlatformDockerIncus:
		_, err := ExecInDockerHost([]string{"test", "-e", path})
		return err

	default:
		return fmt.Errorf("StatOnSandboxHost: unsupported platform %s", activePlatform)
	}
}

// MkdirAllOnSandboxHost creates a directory and all parents on the sandbox host.
func MkdirAllOnSandboxHost(path string, perm os.FileMode) error {
	// Inline path validation so CodeQL can trace the sanitization.
	cleanPath := filepath.Clean(path)
	if !filepath.IsAbs(cleanPath) {
		return fmt.Errorf("MkdirAllOnSandboxHost: path must be absolute: %s", path)
	}
	if strings.Contains(cleanPath, "..") {
		return fmt.Errorf("MkdirAllOnSandboxHost: path must not contain traversal sequences: %s", path)
	}

	switch activePlatform {
	case PlatformLinuxNative:
		return os.MkdirAll(cleanPath, perm)

	case PlatformDockerIncus:
		_, err := ExecInDockerHost([]string{"mkdir", "-p", "--", cleanPath})
		return err

	default:
		return fmt.Errorf("MkdirAllOnSandboxHost: unsupported platform %s", activePlatform)
	}
}

// RemoveAllOnSandboxHost removes a path and all children on the sandbox host.
func RemoveAllOnSandboxHost(path string) error {
	switch activePlatform {
	case PlatformLinuxNative:
		return os.RemoveAll(path)

	case PlatformDockerIncus:
		_, err := ExecInDockerHost([]string{"rm", "-rf", path})
		return err

	default:
		return fmt.Errorf("RemoveAllOnSandboxHost: unsupported platform %s", activePlatform)
	}
}

// ReadFileOnSandboxHost reads a file from the sandbox host filesystem.
func ReadFileOnSandboxHost(path string) ([]byte, error) {
	switch activePlatform {
	case PlatformLinuxNative:
		return os.ReadFile(path)

	case PlatformDockerIncus:
		return ExecInDockerHost([]string{"cat", path})

	default:
		return nil, fmt.Errorf("ReadFileOnSandboxHost: unsupported platform %s", activePlatform)
	}
}

// ReadDirOnSandboxHost lists directory entries on the sandbox host filesystem.
// Returns a list of entry names. Directories have a trailing "/".
func ReadDirOnSandboxHost(path string) ([]string, error) {
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
		return nil, fmt.Errorf("ReadDirOnSandboxHost: unsupported platform %s", activePlatform)
	}
}

// MountOverlayOnSandboxHost runs `mount -t overlay` on the sandbox host.
func MountOverlayOnSandboxHost(opts, target string) error {
	output, err := ExecOnSandboxHost([]string{
		"mount", "-t", "overlay", "overlay", "-o", opts, target,
	})
	if err != nil {
		return fmt.Errorf("failed to mount overlayfs on %s: %w\nOutput: %s", target, err, string(output))
	}
	return nil
}

// UmountOnSandboxHost runs `umount` on the sandbox host.
func UmountOnSandboxHost(target string) error {
	_, err := ExecOnSandboxHost([]string{"umount", target})
	return err
}

// ReadMountsOnSandboxHost reads /proc/mounts from the sandbox host.
func ReadMountsOnSandboxHost() ([]byte, error) {
	return ReadFileOnSandboxHost("/proc/mounts")
}

// IsOverlayMountedOnSandboxHost checks if an overlay is mounted at the given
// container rootfs path on the sandbox host.
func IsOverlayMountedOnSandboxHost(containerRootfs string) bool {
	data, err := ReadMountsOnSandboxHost()
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

// RsyncOnSandboxHost runs rsync on the sandbox host filesystem.
func RsyncOnSandboxHost(src, dst string) error {
	output, err := ExecOnSandboxHost([]string{"rsync", "-a", "--delete", "--", src, dst})
	if err != nil {
		return fmt.Errorf("rsync failed: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// CpOnSandboxHost runs cp -a on the sandbox host filesystem.
func CpOnSandboxHost(src, dst string) error {
	output, err := ExecOnSandboxHost([]string{"cp", "-a", "--", src, dst})
	if err != nil {
		return fmt.Errorf("cp failed: %w\nOutput: %s", err, string(output))
	}
	return nil
}
