//go:build e2e

package e2eboot

import (
	"os"
	"testing"
	"time"
)

// SandboxTestHelper provides a backend-agnostic interface for e2e tests
// that need to interact directly with sandbox infrastructure. Most tests
// interact with sandboxes only through the SSE/API layer and don't need
// this. Only sandbox_layerchain and the bootstrap sweep/cleanup code use it.
type SandboxTestHelper interface {
	// WaitForReady blocks until the sandbox session is ready for commands.
	WaitForReady(t *testing.T, sessionID string, timeout time.Duration)

	// Cleanup destroys a single sandbox session (pod/container).
	Cleanup(t *testing.T, sessionID string)

	// SweepAll destroys all sandboxes in the test namespace.
	// Used for pre-test and post-test cleanup.
	SweepAll(t *testing.T)

	// AssertCommandPresent asserts a binary is available in the sandbox rootfs.
	AssertCommandPresent(t *testing.T, sessionID string, cmd string, msg string)

	// AssertCommandAbsent asserts a binary is NOT available in the sandbox rootfs.
	AssertCommandAbsent(t *testing.T, sessionID string, cmd string, msg string)

	// DeriveResourceID converts a session ID to the backend-specific resource
	// identifier (pod name for K8s, sandbox name for OpenShell).
	DeriveResourceID(sessionID string) string

	// GetMetadata retrieves backend-specific metadata by key.
	// For K8s: pod annotations. For OpenShell: returns "" (no annotations).
	GetMetadata(t *testing.T, sessionID string, key string) string

	// Backend returns the backend identifier ("k8s" or "openshell").
	Backend() string
}

// activeSandboxHelper is the globally-registered helper. Set during init
// based on ASTONISH_E2E_SANDBOX_BACKEND.
var activeSandboxHelper SandboxTestHelper

// GetSandboxHelper returns the active SandboxTestHelper for the current
// e2e run. Defaults to K8s if ASTONISH_E2E_SANDBOX_BACKEND is not set.
func GetSandboxHelper() SandboxTestHelper {
	if activeSandboxHelper == nil {
		// Lazy init based on env. Default to K8s.
		backend := os.Getenv("ASTONISH_E2E_SANDBOX_BACKEND")
		switch backend {
		case "openshell":
			activeSandboxHelper = newOpenShellHelper()
		default:
			activeSandboxHelper = &k8sSandboxHelper{}
		}
	}
	return activeSandboxHelper
}
