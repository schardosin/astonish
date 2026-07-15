package sandbox

import (
	"testing"

	"github.com/SAP/astonish/pkg/config"
)

// TestParseMemoryToMiB exercises the shapes the operator docs advertise
// plus a few adversarial inputs that must degrade to 0 (the "no cap"
// sentinel) rather than misinterpret the value.
func TestParseMemoryToMiB(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"   ", 0},
		{"0", 0},
		{"512", 512},
		{"512M", 512},
		{"512m", 512},
		{"512MB", 512},
		{"512Mi", 512},
		{"512MiB", 512},
		{"2G", 2048},
		{"2g", 2048},
		{"2GB", 2048},
		{"2Gi", 2048},
		{"2GiB", 2048},
		{"1T", 1024 * 1024},
		{"1048576K", 1024},
		{"1048576KB", 1024},
		{"1048576KiB", 1024},
		// Degenerate inputs: no crash, no misparse.
		{"garbage", 0},
		{"12x", 0},
		{"12.5G", 0}, // decimals unsupported, degrade to 0
		{"M", 0},     // no digits
		{"-4G", 0},   // Atoi rejects; fine by us
	}
	for _, c := range cases {
		got := parseMemoryToMiB(c.in)
		if got != c.want {
			t.Errorf("parseMemoryToMiB(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestToResourceLimits pins the straight-through conversion: CPU,
// Processes map 1:1; Memory goes through parseMemoryToMiB.
func TestToResourceLimits(t *testing.T) {
	in := config.SandboxLimits{
		Memory:    "4Gi",
		CPU:       8,
		Processes: 1024,
		Requests: config.SandboxRequests{
			CPUMillis: 200,
			MemoryMiB: 512,
		},
	}
	rl := ToResourceLimits(in)
	if rl.CPUs != 8 {
		t.Errorf("CPUs = %d, want 8", rl.CPUs)
	}
	if rl.MemoryMiB != 4096 {
		t.Errorf("MemoryMiB = %d, want 4096", rl.MemoryMiB)
	}
	if rl.PIDs != 1024 {
		t.Errorf("PIDs = %d, want 1024", rl.PIDs)
	}
	if rl.RequestCPUMillis != 200 {
		t.Errorf("RequestCPUMillis = %d, want 200", rl.RequestCPUMillis)
	}
	if rl.RequestMemoryMiB != 512 {
		t.Errorf("RequestMemoryMiB = %d, want 512", rl.RequestMemoryMiB)
	}
	// DiskMiB/TimeoutS have no config counterparts — must be zero.
	if rl.DiskMiB != 0 || rl.TimeoutS != 0 {
		t.Errorf("expected DiskMiB and TimeoutS to stay 0, got %+v", rl)
	}
}

// TestSetupFlowSandbox_Disabled verifies the no-op path: when sandbox
// is disabled (or appCfg is nil), the original tools are returned with
// a no-op Cleanup. This is the fast path flow execution relies on in
// personal mode without sandbox support.
func TestSetupFlowSandbox_Disabled(t *testing.T) {
	res, err := SetupFlowSandbox(nil, nil)
	if err != nil {
		t.Fatalf("nil config: %v", err)
	}
	if res == nil || res.Tools != nil || res.Cleanup == nil {
		t.Errorf("nil config: unexpected result %+v", res)
	}
	// Cleanup on no-op must not panic.
	res.Cleanup()

	// Explicitly-disabled sandbox: same shape.
	disabled := &config.AppConfig{
		Sandbox: config.SandboxConfig{
			Enabled: boolPtr(false),
		},
	}
	res, err = SetupFlowSandbox(disabled, nil)
	if err != nil {
		t.Fatalf("disabled sandbox: %v", err)
	}
	if res == nil || res.Cleanup == nil {
		t.Errorf("disabled sandbox: unexpected result %+v", res)
	}
	res.Cleanup()
}

// TestSetupFlowSandbox_UnknownBackend pins the error-message contract:
// unrecognised backend kinds fail loudly rather than silently falling
// back to Incus. Prevents operator typos from shipping to production.
func TestSetupFlowSandbox_UnknownBackend(t *testing.T) {
	cfg := &config.AppConfig{
		Sandbox: config.SandboxConfig{
			Enabled: boolPtr(true),
			Backend: "nonsense",
		},
	}
	_, err := SetupFlowSandbox(cfg, nil)
	if err == nil {
		t.Fatalf("SetupFlowSandbox: want error for unknown backend, got nil")
	}
}

func boolPtr(b bool) *bool { return &b }
