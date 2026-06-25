package openshell

import (
	"testing"

	"github.com/schardosin/astonish/pkg/config"
)

func TestDefaultSandboxPolicy_IncludesPTYDevices(t *testing.T) {
	cfg := config.SandboxOpenShellConfig{
		NetworkPolicy: config.NetworkPolicyConfig{
			Presets: []string{"default"},
		},
	}
	policy := defaultSandboxPolicy(cfg)

	// Verify /dev/ptmx and /dev/pts are in read-write paths.
	rwSet := make(map[string]bool)
	for _, p := range policy.Filesystem.ReadWrite {
		rwSet[p] = true
	}

	if !rwSet["/dev/ptmx"] {
		t.Error("expected /dev/ptmx in read-write paths")
	}
	if !rwSet["/dev/pts"] {
		t.Error("expected /dev/pts in read-write paths")
	}

	// Verify standard workspace paths are still present.
	for _, expected := range []string{"/sandbox", "/tmp", "/var/tmp", "/home", "/run"} {
		if !rwSet[expected] {
			t.Errorf("expected %s in read-write paths", expected)
		}
	}
}

func TestDefaultSandboxPolicy_IncludesDeviceNodes(t *testing.T) {
	cfg := config.SandboxOpenShellConfig{}
	policy := defaultSandboxPolicy(cfg)

	roSet := make(map[string]bool)
	for _, p := range policy.Filesystem.ReadOnly {
		roSet[p] = true
	}

	if !roSet["/dev/null"] {
		t.Error("expected /dev/null in read-only paths")
	}
	if !roSet["/dev/urandom"] {
		t.Error("expected /dev/urandom in read-only paths")
	}
}

func TestDefaultSandboxPolicy_LandlockDefaultBestEffort(t *testing.T) {
	cfg := config.SandboxOpenShellConfig{}
	policy := defaultSandboxPolicy(cfg)

	if policy.Landlock == nil {
		t.Fatal("expected landlock spec, got nil")
	}
	if policy.Landlock.Compatibility != "best_effort" {
		t.Errorf("landlock compatibility = %q, want %q", policy.Landlock.Compatibility, "best_effort")
	}
}

func TestDefaultSandboxPolicy_LandlockConfigurable(t *testing.T) {
	cfg := config.SandboxOpenShellConfig{
		LandlockCompatibility: "hard_requirement",
	}
	policy := defaultSandboxPolicy(cfg)

	if policy.Landlock.Compatibility != "hard_requirement" {
		t.Errorf("landlock compatibility = %q, want %q", policy.Landlock.Compatibility, "hard_requirement")
	}
}

func TestDefaultSandboxPolicy_ExtraFilesystemPaths(t *testing.T) {
	cfg := config.SandboxOpenShellConfig{
		FilesystemPolicy: config.FilesystemPolicyConfig{
			ExtraReadOnly:  []string{"/data/models"},
			ExtraReadWrite: []string{"/mnt/scratch", "/dev/fuse"},
		},
	}
	policy := defaultSandboxPolicy(cfg)

	roSet := make(map[string]bool)
	for _, p := range policy.Filesystem.ReadOnly {
		roSet[p] = true
	}
	if !roSet["/data/models"] {
		t.Error("expected /data/models in read-only paths (from extra)")
	}

	rwSet := make(map[string]bool)
	for _, p := range policy.Filesystem.ReadWrite {
		rwSet[p] = true
	}
	if !rwSet["/mnt/scratch"] {
		t.Error("expected /mnt/scratch in read-write paths (from extra)")
	}
	if !rwSet["/dev/fuse"] {
		t.Error("expected /dev/fuse in read-write paths (from extra)")
	}

	// Verify defaults are still present.
	if !rwSet["/dev/ptmx"] {
		t.Error("expected /dev/ptmx still present with extra paths")
	}
}

func TestDefaultSandboxPolicy_NetworkPoliciesPopulated(t *testing.T) {
	cfg := config.SandboxOpenShellConfig{
		NetworkPolicy: config.NetworkPolicyConfig{
			Presets: []string{"default"},
		},
	}
	policy := defaultSandboxPolicy(cfg)

	if policy.NetworkPolicies == nil {
		t.Fatal("expected non-nil network policies")
	}
	egress, ok := policy.NetworkPolicies["egress"]
	if !ok {
		t.Fatal("expected 'egress' network policy")
	}
	if egress.Name != "astonish-egress" {
		t.Errorf("egress name = %q, want %q", egress.Name, "astonish-egress")
	}
	if len(egress.Endpoints) == 0 {
		t.Error("expected non-empty egress endpoints")
	}
}
