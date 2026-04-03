package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
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

	// Binary exists — check if the daemon is reachable
	cmd := exec.Command("incus", "info")
	if output, err := cmd.CombinedOutput(); err != nil {
		detail := firstLine(output)
		// Distinguish permission errors from daemon-not-running.
		// Incus may report "Permission denied" (OS-level) or its own
		// "You don't have the needed permissions" message.
		lowDetail := strings.ToLower(detail)
		if strings.Contains(lowDetail, "permission denied") || strings.Contains(lowDetail, "don't have the needed permissions") {
			return PlatformUnsupported, fmt.Sprintf(
				"Incus is installed but the socket is not accessible (permission denied).\n"+
					"Either run as root or add your user to the 'incus' group:\n"+
					"  sudo usermod -aG incus $USER && newgrp incus\n"+
					"Detail: %s", detail)
		}
		return PlatformUnsupported, fmt.Sprintf(
			"Incus is installed but the daemon is not reachable.\n"+
				"Start it with: sudo systemctl start incus\n"+
				"Detail: %s", detail)
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

// IsInsideLXC detects whether the current process is running inside an LXC
// container (e.g., on Proxmox or other LXC hosts). This matters because
// unprivileged Incus containers cannot run inside nested LXC environments —
// mounting /proc in a double-nested user namespace is not permitted.
//
// Detection uses two methods:
//  1. Check /proc/1/environ for "container=lxc" (set by LXC/Incus on the
//     init process of every container)
//  2. Fallback: run systemd-detect-virt and check for "lxc"
//
// Returns false on non-Linux platforms (Docker+Incus on macOS handles this
// differently via the Docker VM boundary).
func IsInsideLXC() bool {
	if runtime.GOOS != "linux" {
		return false
	}

	// Primary: check /proc/1/environ for container=lxc
	// This is the most reliable method — LXC/Incus always sets this.
	data, err := os.ReadFile("/proc/1/environ")
	if err == nil {
		// environ is null-byte separated
		for _, entry := range strings.Split(string(data), "\x00") {
			if entry == "container=lxc" {
				return true
			}
		}
	}

	// Fallback: systemd-detect-virt
	output, err := exec.Command("systemd-detect-virt").Output()
	if err == nil {
		if strings.TrimSpace(string(output)) == "lxc" {
			return true
		}
	}

	return false
}
