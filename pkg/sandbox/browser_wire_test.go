package sandbox

import (
	"testing"

	"github.com/schardosin/astonish/pkg/browser"
)

func TestWireIncusBrowserManager_NilArgs(t *testing.T) {
	t.Parallel()
	mgr := browser.NewManager(browser.DefaultConfig())
	if WireIncusBrowserManager(nil, nil, nil) {
		t.Fatal("expected false for nil mgr/client")
	}
	if WireIncusBrowserManager(mgr, nil, nil) {
		t.Fatal("expected false for nil client")
	}
	if mgr.SandboxEnabled {
		t.Fatal("nil client must not enable sandbox on manager")
	}
}
