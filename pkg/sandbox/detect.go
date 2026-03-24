package sandbox

import (
	"fmt"
	"os/exec"
	"runtime"
)

// Platform represents the detected host platform for sandbox operation.
type Platform int

const (
	// PlatformLinuxNative indicates Linux with Incus available directly.
	PlatformLinuxNative Platform = iota
	// PlatformDockerIncus indicates macOS/Windows where Incus runs inside Docker.
	PlatformDockerIncus
	// PlatformUnsupported indicates no container runtime is available.
	PlatformUnsupported
)

// String returns a human-readable platform name.
func (p Platform) String() string {
	switch p {
	case PlatformLinuxNative:
		return "Linux (native Incus)"
	case PlatformDockerIncus:
		return "Docker + Incus"
	case PlatformUnsupported:
		return "Unsupported (no container runtime)"
	default:
		return "Unknown"
	}
}

// DetectPlatform determines the host platform and available container runtime.
func DetectPlatform() Platform {
	p, _ := DetectPlatformReason()
	return p
}

// DetectPlatformReason determines the host platform and returns a human-readable
// reason if the platform is unsupported. The reason distinguishes between
// "not installed" and "installed but daemon not running" so callers can show
// actionable guidance.
func DetectPlatformReason() (Platform, string) {
	if runtime.GOOS == "linux" {
		platform, reason := incusCheck()
		if platform == PlatformLinuxNative {
			return platform, ""
		}
		return PlatformUnsupported, reason
	}

	// macOS / Windows: check for Docker
	platform, reason := dockerCheck()
	if platform == PlatformDockerIncus {
		return platform, ""
	}
	return PlatformUnsupported, reason
}

// incusCheck probes for Incus on Linux.
// Returns PlatformLinuxNative if the daemon is reachable, or PlatformUnsupported
// with a reason explaining what's wrong.
func incusCheck() (Platform, string) {
	path, err := exec.LookPath("incus")
	if err != nil || path == "" {
		return PlatformUnsupported, "Incus is not installed.\nInstall with: apt install incus && incus admin init"
	}

	// Binary exists — check if the daemon is running
	cmd := exec.Command("incus", "info")
	if output, err := cmd.CombinedOutput(); err != nil {
		return PlatformUnsupported, fmt.Sprintf(
			"Incus is installed but the daemon is not running.\n"+
				"Start it with: sudo systemctl start incus\n"+
				"Detail: %s", firstLine(output))
	}

	return PlatformLinuxNative, ""
}

// dockerCheck probes for Docker on macOS/Windows.
func dockerCheck() (Platform, string) {
	path, err := exec.LookPath("docker")
	if err != nil || path == "" {
		return PlatformUnsupported, "Docker is not installed.\nInstall Docker Desktop or any Docker-compatible runtime."
	}

	cmd := exec.Command("docker", "info")
	if output, err := cmd.CombinedOutput(); err != nil {
		return PlatformUnsupported, fmt.Sprintf(
			"Docker is installed but the daemon is not running.\n"+
				"Start Docker Desktop and try again.\n"+
				"Detail: %s", firstLine(output))
	}

	return PlatformDockerIncus, ""
}

// firstLine returns the first line (up to 200 chars) of output for error messages.
func firstLine(output []byte) string {
	s := string(output)
	for i, c := range s {
		if c == '\n' {
			s = s[:i]
			break
		}
	}
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}
