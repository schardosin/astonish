package sandbox

import (
	"strings"
	"testing"

	"github.com/schardosin/astonish/pkg/config"
)

// TestBackendFromAppConfig_NilAppConfig pins the nil-input error path.
func TestBackendFromAppConfig_NilAppConfig(t *testing.T) {
	_, _, err := BackendFromAppConfig(nil)
	if err == nil {
		t.Fatal("expected error for nil AppConfig")
	}
	if !strings.Contains(err.Error(), "nil app config") {
		t.Errorf("error = %v, want nil-app-config wording", err)
	}
}

// TestBackendFromAppConfig_K8sBranchTakesKubernetesSubConfig is covered
// from the k8s package's test suite (it blank-imports its own init), not
// here — pkg/sandbox deliberately does not link pkg/sandbox/k8s. See
// pkg/sandbox/k8s/backend_test.go:TestBackendFromAppConfig_K8s.

// TestBackendFromAppConfig_DefaultsToIncus ensures backward-compatibility:
// an empty Backend field must route through the incus path. We can't go
// all the way because SetupSandboxRuntime may not find a local Incus
// daemon in CI — we just assert we reach that branch (failing with an
// incus-flavoured error is the success signal here).
func TestBackendFromAppConfig_DefaultsToIncus(t *testing.T) {
	appCfg := &config.AppConfig{}
	// Backend unset → "incus" per BackendKind().
	_, _, err := BackendFromAppConfig(appCfg)
	if err == nil {
		// If Incus happens to be available, that's fine too.
		return
	}
	if !strings.Contains(err.Error(), "incus") {
		t.Errorf("expected incus path error, got: %v", err)
	}
}

// TestBackendFromAppConfig_UnknownKind surfaces a clear error for typos.
func TestBackendFromAppConfig_UnknownKind(t *testing.T) {
	appCfg := &config.AppConfig{}
	appCfg.Sandbox.Backend = "docker" // not supported

	_, _, err := BackendFromAppConfig(appCfg)
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
	if !strings.Contains(err.Error(), "docker") {
		t.Errorf("error should name the unknown kind; got: %v", err)
	}
}
