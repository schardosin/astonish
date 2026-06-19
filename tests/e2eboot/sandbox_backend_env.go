// sandbox_backend_env.go — shared utility for determining sandbox backend.
// Intentionally NOT build-tagged so it's available to both e2e tests and
// the inspector binary (platform_core.go uses it for config generation).
package e2eboot

import "os"

// SandboxBackendName returns the active sandbox backend name from the
// ASTONISH_E2E_SANDBOX_BACKEND environment variable. Defaults to "k8s".
func SandboxBackendName() string {
	if b := os.Getenv("ASTONISH_E2E_SANDBOX_BACKEND"); b != "" {
		return b
	}
	return "k8s"
}
