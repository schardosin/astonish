package sandbox

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/schardosin/astonish/pkg/config"
)

// DefaultSandboxConfig returns sensible defaults for the sandbox system.
func DefaultSandboxConfig() config.SandboxConfig {
	enabled := true
	return config.SandboxConfig{
		Enabled: &enabled,
		Limits: config.SandboxLimits{
			Memory:    "2GB",
			CPU:       2,
			Processes: 500,
		},
		Network: "bridged",
		Prune: config.SandboxPruneConfig{
			OrphanCheckHours:   6,
			IdleTimeoutMinutes: 10,
		},
	}
}

// IsSandboxEnabled returns whether sandbox is enabled. Defaults to true when nil.
func IsSandboxEnabled(c *config.SandboxConfig) bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// validMemoryPattern matches Incus memory limit strings like "512MB", "2GB", "1024kB".
var validMemoryPattern = regexp.MustCompile(`(?i)^\d+\s*(kB|MB|GB|TB|EB|PB)$`)

// ValidateSandboxConfig checks the sandbox configuration for errors.
func ValidateSandboxConfig(c *config.SandboxConfig) error {
	if c.Network != "" && c.Network != "bridged" && c.Network != "none" {
		return fmt.Errorf("sandbox.network must be 'bridged' or 'none', got %q", c.Network)
	}
	if c.Limits.Memory != "" && !validMemoryPattern.MatchString(strings.TrimSpace(c.Limits.Memory)) {
		return fmt.Errorf("sandbox.limits.memory must be a valid size (e.g. '2GB', '512MB'), got %q", c.Limits.Memory)
	}
	if c.Limits.CPU < 0 {
		return fmt.Errorf("sandbox.limits.cpu must be >= 0, got %d", c.Limits.CPU)
	}
	if c.Limits.Processes < 0 {
		return fmt.Errorf("sandbox.limits.processes must be >= 0, got %d", c.Limits.Processes)
	}
	if c.Prune.OrphanCheckHours < 0 {
		return fmt.Errorf("sandbox.prune.orphan_check_hours must be >= 0, got %d", c.Prune.OrphanCheckHours)
	}
	if c.Prune.IdleTimeoutMinutes < 0 {
		return fmt.Errorf("sandbox.prune.idle_timeout_minutes must be >= 0, got %d", c.Prune.IdleTimeoutMinutes)
	}
	return nil
}

// sandboxCfg stores the active sandbox configuration at package level.
// Set via SetSandboxConfig during sandbox initialization. Used by
// IsPrivileged and container creation functions. Follows the same pattern
// as activePlatform in remote_ops.go.
var sandboxCfg *config.SandboxConfig

// SetSandboxConfig stores the sandbox configuration at package level for
// use by container creation functions (defaultContainerConfig, etc.).
func SetSandboxConfig(c *config.SandboxConfig) {
	sandboxCfg = c
}

// GetSandboxConfig returns the stored sandbox configuration.
// Returns nil if not yet initialized.
func GetSandboxConfig() *config.SandboxConfig {
	return sandboxCfg
}

// IsPrivileged determines whether containers should run in privileged mode.
// Resolution order:
//  1. Explicit user config (sandbox.privileged: true/false) — always honored
//  2. Default: false (unprivileged) on all platforms.
//
// Unprivileged containers use user namespaces to map container root (UID 0) to
// an unprivileged host UID (e.g., 100000+), preventing container-to-host escapes.
// The overlay system pre-seeds the Incus idmap state so that UID shifting is
// skipped at container start time (the template snapshot already has shifted UIDs).
//
// Users can set sandbox.privileged: true for environments that require it
// (e.g., nested LXC on Proxmox where /proc can't mount in double-nested
// user namespaces).
func IsPrivileged() bool {
	// Explicit user override takes priority
	if sandboxCfg != nil && sandboxCfg.Privileged != nil {
		return *sandboxCfg.Privileged
	}
	return false
}

// containerSecurityConfig returns the security-related config keys for a container
// based on the current privilege mode and platform.
//
// On native Linux (unprivileged), containers get full hardening:
//   - Syscall intercepts for mknod/setxattr (needed for Docker images)
//   - Default syscall deny list (blocks dangerous syscalls like kexec, module loading)
//   - Compat syscall deny (blocks 32-bit syscall attacks on x86_64)
//   - Guest API disabled (removes /dev/incus from container)
//
// On Docker+Incus (macOS/Windows), syscall hardening is skipped because:
//   - The Docker Desktop VM is the security boundary, not LXC
//   - Seccomp intercepts may not work in nested/emulated environments
//     (e.g., deny_compat fails on aarch64 with "Unsupported architecture")
//   - Containers are still unprivileged (user namespaces active) unless
//     the user explicitly sets sandbox.privileged: true
//
// Note: security.idmap.isolated is intentionally NOT set. All containers must
// share the same idmap range so that overlay lower layers (shared template
// snapshots with pre-shifted UIDs) have correct ownership for all containers.
func containerSecurityConfig() map[string]string {
	if IsPrivileged() {
		return map[string]string{
			"security.privileged": "true",
		}
	}

	// On Docker+Incus, skip syscall hardening — the Docker VM provides
	// isolation and seccomp features may not work in nested environments.
	if activePlatform == PlatformDockerIncus {
		return map[string]string{
			"security.privileged": "false",
		}
	}

	// Native Linux: full hardening
	return map[string]string{
		"security.privileged":                  "false",
		"security.syscalls.intercept.mknod":    "true",
		"security.syscalls.intercept.setxattr": "true",
		"security.syscalls.deny_default":       "true",
		"security.syscalls.deny_compat":        "true",
		"security.guestapi":                    "false",
	}
}

// EffectiveLimits returns the limits with defaults filled in for any zero values.
func EffectiveLimits(c *config.SandboxConfig) config.SandboxLimits {
	defaults := DefaultSandboxConfig().Limits
	l := c.Limits
	if l.Memory == "" {
		l.Memory = defaults.Memory
	}
	if l.CPU == 0 {
		l.CPU = defaults.CPU
	}
	if l.Processes == 0 {
		l.Processes = defaults.Processes
	}
	return l
}

// EffectiveNetwork returns the network mode with default filled in.
func EffectiveNetwork(c *config.SandboxConfig) string {
	if c.Network == "" {
		return "bridged"
	}
	return c.Network
}

// EffectiveIdleTimeout returns the idle timeout duration for sandbox containers.
// Uses the configured value if > 0, otherwise falls back to the default (10 min).
func EffectiveIdleTimeout(c *config.SandboxConfig) time.Duration {
	if c.Prune.IdleTimeoutMinutes > 0 {
		return time.Duration(c.Prune.IdleTimeoutMinutes) * time.Minute
	}
	defaults := DefaultSandboxConfig()
	return time.Duration(defaults.Prune.IdleTimeoutMinutes) * time.Minute
}
