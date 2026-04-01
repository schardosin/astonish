package sandbox

import (
	"testing"

	"github.com/schardosin/astonish/pkg/config"
)

func TestIsSandboxEnabled(t *testing.T) {
	t.Run("nil means true", func(t *testing.T) {
		c := &config.SandboxConfig{}
		if !IsSandboxEnabled(c) {
			t.Error("expected true when Enabled is nil")
		}
	})

	t.Run("explicit true", func(t *testing.T) {
		enabled := true
		c := &config.SandboxConfig{Enabled: &enabled}
		if !IsSandboxEnabled(c) {
			t.Error("expected true")
		}
	})

	t.Run("explicit false", func(t *testing.T) {
		enabled := false
		c := &config.SandboxConfig{Enabled: &enabled}
		if IsSandboxEnabled(c) {
			t.Error("expected false")
		}
	})
}

func TestValidateSandboxConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.SandboxConfig
		wantErr bool
	}{
		{
			name:    "zero value is valid",
			cfg:     config.SandboxConfig{},
			wantErr: false,
		},
		{
			name:    "valid bridged network",
			cfg:     config.SandboxConfig{Network: "bridged"},
			wantErr: false,
		},
		{
			name:    "valid none network",
			cfg:     config.SandboxConfig{Network: "none"},
			wantErr: false,
		},
		{
			name:    "invalid network",
			cfg:     config.SandboxConfig{Network: "host"},
			wantErr: true,
		},
		{
			name:    "valid memory 2GB",
			cfg:     config.SandboxConfig{Limits: config.SandboxLimits{Memory: "2GB"}},
			wantErr: false,
		},
		{
			name:    "valid memory 512MB",
			cfg:     config.SandboxConfig{Limits: config.SandboxLimits{Memory: "512MB"}},
			wantErr: false,
		},
		{
			name:    "valid memory 1024kB",
			cfg:     config.SandboxConfig{Limits: config.SandboxLimits{Memory: "1024kB"}},
			wantErr: false,
		},
		{
			name:    "invalid memory string",
			cfg:     config.SandboxConfig{Limits: config.SandboxLimits{Memory: "abc"}},
			wantErr: true,
		},
		{
			name:    "invalid memory no unit",
			cfg:     config.SandboxConfig{Limits: config.SandboxLimits{Memory: "2048"}},
			wantErr: true,
		},
		{
			name:    "invalid memory negative",
			cfg:     config.SandboxConfig{Limits: config.SandboxLimits{Memory: "-1GB"}},
			wantErr: true,
		},
		{
			name:    "empty memory is valid (means use default)",
			cfg:     config.SandboxConfig{Limits: config.SandboxLimits{Memory: ""}},
			wantErr: false,
		},
		{
			name:    "negative CPU",
			cfg:     config.SandboxConfig{Limits: config.SandboxLimits{CPU: -1}},
			wantErr: true,
		},
		{
			name:    "zero CPU is valid (means use default)",
			cfg:     config.SandboxConfig{Limits: config.SandboxLimits{CPU: 0}},
			wantErr: false,
		},
		{
			name:    "negative processes",
			cfg:     config.SandboxConfig{Limits: config.SandboxLimits{Processes: -1}},
			wantErr: true,
		},
		{
			name:    "negative orphan check hours",
			cfg:     config.SandboxConfig{Prune: config.SandboxPruneConfig{OrphanCheckHours: -1}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSandboxConfig(&tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSandboxConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEffectiveLimits(t *testing.T) {
	t.Run("all zeros get defaults", func(t *testing.T) {
		c := &config.SandboxConfig{}
		l := EffectiveLimits(c)
		if l.Memory != "2GB" {
			t.Errorf("Memory = %q, want 2GB", l.Memory)
		}
		if l.CPU != 2 {
			t.Errorf("CPU = %d, want 2", l.CPU)
		}
		if l.Processes != 500 {
			t.Errorf("Processes = %d, want 500", l.Processes)
		}
	})

	t.Run("explicit values preserved", func(t *testing.T) {
		c := &config.SandboxConfig{
			Limits: config.SandboxLimits{
				Memory:    "4GB",
				CPU:       8,
				Processes: 1000,
			},
		}
		l := EffectiveLimits(c)
		if l.Memory != "4GB" {
			t.Errorf("Memory = %q, want 4GB", l.Memory)
		}
		if l.CPU != 8 {
			t.Errorf("CPU = %d, want 8", l.CPU)
		}
		if l.Processes != 1000 {
			t.Errorf("Processes = %d, want 1000", l.Processes)
		}
	})

	t.Run("partial override", func(t *testing.T) {
		c := &config.SandboxConfig{
			Limits: config.SandboxLimits{
				Memory: "1GB",
			},
		}
		l := EffectiveLimits(c)
		if l.Memory != "1GB" {
			t.Errorf("Memory = %q, want 1GB", l.Memory)
		}
		if l.CPU != 2 {
			t.Errorf("CPU = %d, want 2 (default)", l.CPU)
		}
		if l.Processes != 500 {
			t.Errorf("Processes = %d, want 500 (default)", l.Processes)
		}
	})
}

func TestEffectiveNetwork(t *testing.T) {
	t.Run("empty returns bridged", func(t *testing.T) {
		c := &config.SandboxConfig{}
		if got := EffectiveNetwork(c); got != "bridged" {
			t.Errorf("got %q, want bridged", got)
		}
	})

	t.Run("explicit none", func(t *testing.T) {
		c := &config.SandboxConfig{Network: "none"}
		if got := EffectiveNetwork(c); got != "none" {
			t.Errorf("got %q, want none", got)
		}
	})
}

func TestDefaultSandboxConfig(t *testing.T) {
	d := DefaultSandboxConfig()
	if d.Enabled == nil || !*d.Enabled {
		t.Error("expected Enabled=true")
	}
	if d.Limits.Memory != "2GB" {
		t.Errorf("Memory = %q, want 2GB", d.Limits.Memory)
	}
	if d.Limits.CPU != 2 {
		t.Errorf("CPU = %d, want 2", d.Limits.CPU)
	}
	if d.Limits.Processes != 500 {
		t.Errorf("Processes = %d, want 500", d.Limits.Processes)
	}
	if d.Network != "bridged" {
		t.Errorf("Network = %q, want bridged", d.Network)
	}
	if d.Prune.OrphanCheckHours != 6 {
		t.Errorf("OrphanCheckHours = %d, want 6", d.Prune.OrphanCheckHours)
	}
}

func TestIsPrivileged(t *testing.T) {
	// Save and restore package-level state
	origPlatform := activePlatform
	origCfg := sandboxCfg
	t.Cleanup(func() {
		activePlatform = origPlatform
		sandboxCfg = origCfg
	})

	t.Run("nil config on Linux native defaults to unprivileged", func(t *testing.T) {
		activePlatform = PlatformLinuxNative
		sandboxCfg = nil
		if IsPrivileged() {
			t.Error("expected false (unprivileged) on Linux native with nil config")
		}
	})

	t.Run("nil config on Docker defaults to privileged", func(t *testing.T) {
		activePlatform = PlatformDockerIncus
		sandboxCfg = nil
		if !IsPrivileged() {
			t.Error("expected true (privileged) on Docker+Incus with nil config")
		}
	})

	t.Run("nil config on unsupported defaults to privileged", func(t *testing.T) {
		activePlatform = PlatformUnsupported
		sandboxCfg = nil
		if !IsPrivileged() {
			t.Error("expected true (privileged) on unsupported platform")
		}
	})

	t.Run("explicit true overrides Linux native default", func(t *testing.T) {
		activePlatform = PlatformLinuxNative
		priv := true
		sandboxCfg = &config.SandboxConfig{Privileged: &priv}
		if !IsPrivileged() {
			t.Error("expected true when explicitly set to privileged on Linux native")
		}
	})

	t.Run("explicit false overrides Docker default", func(t *testing.T) {
		activePlatform = PlatformDockerIncus
		priv := false
		sandboxCfg = &config.SandboxConfig{Privileged: &priv}
		if IsPrivileged() {
			t.Error("expected false when explicitly set to unprivileged on Docker+Incus")
		}
	})

	t.Run("nil Privileged field uses platform default", func(t *testing.T) {
		activePlatform = PlatformLinuxNative
		sandboxCfg = &config.SandboxConfig{} // Privileged is nil
		if IsPrivileged() {
			t.Error("expected false on Linux native with nil Privileged field")
		}
	})
}

func TestContainerSecurityConfig(t *testing.T) {
	origPlatform := activePlatform
	origCfg := sandboxCfg
	t.Cleanup(func() {
		activePlatform = origPlatform
		sandboxCfg = origCfg
	})

	t.Run("privileged mode returns privileged config", func(t *testing.T) {
		activePlatform = PlatformDockerIncus
		sandboxCfg = nil
		cfg := containerSecurityConfig()
		if cfg["security.privileged"] != "true" {
			t.Errorf("expected security.privileged=true, got %q", cfg["security.privileged"])
		}
		if _, ok := cfg["security.syscalls.intercept.mknod"]; ok {
			t.Error("privileged mode should not set syscall intercepts")
		}
	})

	t.Run("unprivileged mode returns syscall intercepts", func(t *testing.T) {
		activePlatform = PlatformLinuxNative
		sandboxCfg = nil
		cfg := containerSecurityConfig()
		if cfg["security.privileged"] != "false" {
			t.Errorf("expected security.privileged=false, got %q", cfg["security.privileged"])
		}
		if cfg["security.syscalls.intercept.mknod"] != "true" {
			t.Error("expected security.syscalls.intercept.mknod=true")
		}
		if cfg["security.syscalls.intercept.setxattr"] != "true" {
			t.Error("expected security.syscalls.intercept.setxattr=true")
		}
	})
}
