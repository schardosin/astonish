//go:build e2e

package e2eboot

import (
	"testing"

	"github.com/SAP/astonish/pkg/store"
)

// initEmbedFunc initializes the local Hugot embedding function and sets it on
// the platform backend. This enables hybrid vector+keyword memory search in
// E2E tests, matching production behavior.
//
// The model directory is resolved from the real user home (~/.config/astonish/models/)
// rather than the test's temporary XDG_CONFIG_HOME, so the ~23 MB model file is
// downloaded once and cached across test runs.
//
// If initialization fails (e.g., model download fails due to no internet), a
// warning is logged and the backend continues with keyword-only search.
func initEmbedFunc(t *testing.T, backend store.PlatformBackend) {
	t.Helper()
	initEmbedFuncCore(t, backend)
}
