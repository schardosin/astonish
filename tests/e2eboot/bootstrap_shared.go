//go:build e2e

package e2eboot

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/store/entstore"
	"github.com/schardosin/astonish/pkg/store/pgutil"
)

// bootstrapShared is invoked when ASTONISH_E2E_KEEP_ALIVE=1. Instead of
// bootstrapping its own StudioServer + platform DB, it attaches to the
// running inspector instance.
//
// The returned Harness has SharedMode=true and a non-empty PerTestSuffix.
// No t.Cleanup hooks are registered for server shutdown or DB drop — the
// inspector owns those resources and they outlive the test.
//
// Tests that seed the multi-tenant world via Seed(t, h) automatically pick
// up the per-test suffix. Direct DB calls in tests should also use
// h.PerTestSuffix to avoid colliding with other tests in the shared DB.
func bootstrapShared(t *testing.T, baseDSN string) *Harness {
	t.Helper()

	state, err := ReadInspectorState()
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("[e2eboot] ASTONISH_E2E_KEEP_ALIVE=1 but inspector is not running.\n" +
				"  Start it first with:\n" +
				"      make test-e2e-inspect\n" +
				"  (or unset ASTONISH_E2E_KEEP_ALIVE to use isolated per-test bootstrap)")
		}
		t.Fatalf("[e2eboot] read inspector state: %v", err)
	}
	if state.PID == 0 {
		t.Fatalf("[e2eboot] inspector state file has no PID — file is corrupt: %s", InspectorStateFile)
	}

	// Verify the inspector HTTP endpoint is reachable. This catches stale
	// state files from a crashed inspector.
	if err := pingInspector(state.BaseURL); err != nil {
		t.Fatalf("[e2eboot] inspector at %s is unreachable: %v\n"+
			"  Stale state file? Try:  make test-e2e-inspect-stop && make test-e2e-inspect",
			state.BaseURL, err)
	}

	ctx := context.Background()

	// Open a per-test connection pool to the same platform DB the inspector
	// is using. Tests use this to seed orgs/teams/users via the existing
	// seed.go helpers. The pool is closed when the test finishes.
	platformDSN, err := pgutil.ReplaceDSNDatabase(baseDSN, config.PlatformDBName(state.Suffix))
	if err != nil {
		t.Fatalf("[e2eboot] derive platform DSN: %v", err)
	}
	_, esStore, err := entstore.NewPlatformServices(ctx, entstore.Config{
		DSN:            platformDSN,
		InstanceSuffix: state.Suffix,
	})
	if err != nil {
		t.Fatalf("[e2eboot] NewPlatformServices (shared): %v", err)
	}
	t.Cleanup(func() { esStore.Close() })

	// Login the bootstrap user to get a fresh token (its old token may be
	// expired by now). The bootstrap user is shared across all tests but
	// only used by tests that explicitly need an admin context — most tests
	// use Seed() and per-test Alice/Bob/etc. JWTs.
	token, err := loginBootstrapUser(state.BaseURL)
	if err != nil {
		t.Fatalf("[e2eboot] login bootstrap user (shared): %v", err)
	}

	// Use a package-scoped suffix in shared mode so all tests in the same
	// Go test package share the same orgs/users/data. This is Plan D: one
	// acme+globex pair per package (~3 packages total) instead of per-test.
	// First test in the package creates the world via Seed(); subsequent
	// tests detect existing data and reuse it (Seed is idempotent in
	// shared mode).
	perTestSuffix := callerPackageSuffix()
	t.Logf("[e2eboot] Attached to shared inspector at %s (suffix=%s, package=%s)",
		state.BaseURL, state.Suffix, perTestSuffix)

	return &Harness{
		BaseURL:       state.BaseURL,
		Token:         token,
		Store:         esStore,
		PlatformDSN:   platformDSN,
		Suffix:        state.Suffix,
		BaseDSN:       baseDSN,
		PerTestSuffix: perTestSuffix,
		SharedMode:    true,
	}
}

func pingInspector(baseURL string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(baseURL + "/api/health")
	if err != nil {
		// Some servers return 404 on /api/health but still respond.
		// Fall back to root.
		resp2, err2 := client.Get(baseURL + "/")
		if err2 != nil {
			return err
		}
		resp2.Body.Close()
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("inspector returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// callerPackageSuffix returns a short, deterministic suffix derived from
// the test package that called Bootstrap. All tests in the same Go test
// package share the same suffix, so they share the same orgs/users/data
// in the kept-alive inspector instance.
//
// We walk runtime.Callers up the stack until we find a frame inside
// "tests/e2e/<package>/...". The package name is then sanitized
// (lowercase, alnum only, max 16 chars) to produce a stable suffix.
//
// Examples:
//   tests/e2e/chat_auth/chat_auth_test.go      → "chatauth"
//   tests/e2e/chat_core/chat_core_test.go      → "chatcore"
//   tests/e2e/flow_assistant/flow_assistant.go → "flowassistant"
//
// Falls back to "shared" if the caller cannot be identified.
func callerPackageSuffix() string {
	pcs := make([]uintptr, 32)
	n := runtime.Callers(2, pcs)
	if n == 0 {
		return "shared"
	}
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		// Look for ".../tests/e2e/<package>/..."
		if idx := strings.Index(frame.File, "/tests/e2e/"); idx >= 0 {
			rest := frame.File[idx+len("/tests/e2e/"):]
			slash := strings.Index(rest, "/")
			if slash > 0 {
				return sanitizePackageName(rest[:slash])
			}
		}
		if !more {
			break
		}
	}
	return "shared"
}

func sanitizePackageName(name string) string {
	var b []byte
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z':
			b = append(b, byte(c))
		case c >= 'A' && c <= 'Z':
			b = append(b, byte(c+32))
		case c >= '0' && c <= '9':
			b = append(b, byte(c))
		}
		if len(b) >= 16 {
			break
		}
	}
	if len(b) == 0 {
		return "shared"
	}
	return string(b)
}
