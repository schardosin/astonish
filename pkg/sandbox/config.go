package sandbox

import (
	"fmt"

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
			OrphanCheckHours: 6,
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

// ValidateSandboxConfig checks the sandbox configuration for errors.
func ValidateSandboxConfig(c *config.SandboxConfig) error {
	if c.Network != "" && c.Network != "bridged" && c.Network != "none" {
		return fmt.Errorf("sandbox.network must be 'bridged' or 'none', got %q", c.Network)
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
	return nil
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
