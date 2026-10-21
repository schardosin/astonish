package api

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGuardedHostReadSessionFile_DeniesInPlatformMode proves the chat
// schedule_job test-run path (and the daemon scheduler) deny a model-supplied
// host-file read in multi-tenant platform mode — including the SQLite backend,
// which the original IsPostgres()-only guard missed. A job prompt that names a
// host path such as /proc/self/environ must not have its contents returned.
func TestGuardedHostReadSessionFile_DeniesInPlatformMode(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "secret.env")
	if err := os.WriteFile(secret, []byte("API_KEY=canary-should-not-leak"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	read := guardedHostReadSessionFile(true)
	data, err := read("", secret)
	if err == nil {
		t.Fatalf("platform-mode host read must be denied, got %d bytes", len(data))
	}
	if len(data) != 0 {
		t.Fatalf("denied read must return no bytes, got %q", string(data))
	}
}

// TestGuardedHostReadSessionFile_AllowsTrustedLocal confirms the trusted
// single-tenant (file backend / local Code) path still reads report bodies
// from the host, preserving the legitimate adaptive-report fallback.
func TestGuardedHostReadSessionFile_AllowsTrustedLocal(t *testing.T) {
	report := filepath.Join(t.TempDir(), "report.md")
	if err := os.WriteFile(report, []byte("# Report\n\nbody"), 0o600); err != nil {
		t.Fatalf("write report: %v", err)
	}

	read := guardedHostReadSessionFile(false)
	data, err := read("", report)
	if err != nil {
		t.Fatalf("trusted-local host read must succeed, got err %v", err)
	}
	if string(data) != "# Report\n\nbody" {
		t.Fatalf("got %q", string(data))
	}
}
