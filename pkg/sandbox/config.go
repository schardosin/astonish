package sandbox

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/sandbox/incus"
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
// IsPrivileged. Follows the same pattern as activePlatform (now in
// pkg/sandbox/incus).
var sandboxCfg *config.SandboxConfig

// sandboxConfigProvider adapts the package-level sandboxCfg to the
// incus.ConfigProvider interface, avoiding a pkg/sandbox/incus → pkg/sandbox
// import cycle.
type sandboxConfigProvider struct{}

// IsPrivileged reports whether containers should run in privileged mode
// according to the active sandbox config.
func (sandboxConfigProvider) IsPrivileged() bool {
	if sandboxCfg != nil && sandboxCfg.Privileged != nil {
		return *sandboxCfg.Privileged
	}
	return false
}

// Register the ConfigProvider as early as possible so pkg/sandbox/incus sees
// the current sandboxCfg whether or not SetSandboxConfig has been called.
// Both pkg/sandbox and pkg/sandbox/incus are linked together at init time;
// this guarantees a consistent view from first use.
func init() {
	incus.SetConfigProvider(sandboxConfigProvider{})
}

// SetSandboxConfig stores the sandbox configuration at package level for
// use by container creation functions. The incus.ConfigProvider registration
// happens once in init(); this function only updates the underlying config.
func SetSandboxConfig(c *config.SandboxConfig) {
	sandboxCfg = c
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
	return sandboxConfigProvider{}.IsPrivileged()
}

// containerSecurityConfig returns the security-related config keys for a
// container. The canonical implementation now lives in pkg/sandbox/incus;
// this local wrapper preserves the historical unexported name for staying
// files and tests.
func containerSecurityConfig() map[string]string {
	return incus.ContainerSecurityConfig()
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
