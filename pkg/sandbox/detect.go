package sandbox

import (
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
	if runtime.GOOS == "linux" {
		if incusAvailable() {
			return PlatformLinuxNative
		}
		return PlatformUnsupported
	}

	// macOS / Windows: check for Docker
	if dockerAvailable() {
		return PlatformDockerIncus
	}

	return PlatformUnsupported
}

// incusAvailable checks whether the Incus daemon is reachable.
// It first checks for the incus binary, then tries to contact the daemon.
func incusAvailable() bool {
	path, err := exec.LookPath("incus")
	if err != nil || path == "" {
		return false
	}
	// Verify the daemon is reachable
	cmd := exec.Command("incus", "info")
	return cmd.Run() == nil
}

// dockerAvailable checks whether a Docker-compatible runtime is available.
// It checks for the docker CLI and verifies the daemon is reachable.
// This does NOT check for any specific product (Docker Desktop, OrbStack, etc.).
func dockerAvailable() bool {
	path, err := exec.LookPath("docker")
	if err != nil || path == "" {
		return false
	}
	cmd := exec.Command("docker", "info")
	return cmd.Run() == nil
}
