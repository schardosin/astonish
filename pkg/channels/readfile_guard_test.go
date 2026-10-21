package channels

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestChannelManagerReadFileRespectsReadFileFunc verifies the sandbox-aware
// indirection in readFile. The platform-mode wiring in pkg/daemon/run.go sets a
// ReadFileFunc that fails closed when the sandbox is disabled, so document
// delivery never falls through to a plain host os.ReadFile on the shared daemon.
// This test proves readFile honors that closure (and does not read the host),
// and that the nil case still falls back to os.ReadFile for trusted local use.
func TestChannelManagerReadFileRespectsReadFileFunc(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "other-tenant-secret.txt")
	if err := os.WriteFile(secret, []byte("CANARY-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("platform-refuse-closure-fails-closed", func(t *testing.T) {
		m := &ChannelManager{}
		// Mirror the platform-mode/sandbox-disabled wiring: refuse host reads.
		m.SetReadFileFunc(func(sessionID, path string) ([]byte, error) {
			return nil, os.ErrPermission
		})
		data, err := m.readFile("session-1", secret)
		if err == nil {
			t.Fatalf("expected refusal, got %d bytes (host read leaked)", len(data))
		}
		if strings.Contains(string(data), "CANARY") {
			t.Fatal("refuse closure still returned host file contents")
		}
	})

	t.Run("nil-func-falls-back-to-host-read", func(t *testing.T) {
		m := &ChannelManager{} // ReadFileFunc nil => trusted local host read
		data, err := m.readFile("session-1", secret)
		if err != nil {
			t.Fatalf("trusted local host read failed: %v", err)
		}
		if string(data) != "CANARY-SECRET" {
			t.Fatalf("unexpected host read content: %q", data)
		}
	})
}
