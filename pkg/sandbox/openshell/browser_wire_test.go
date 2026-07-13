package openshell

import (
	"testing"

	"github.com/schardosin/astonish/pkg/browser"
)

func TestWireBrowserManager_NilArgs(t *testing.T) {
	t.Parallel()
	mgr := browser.NewManager(browser.DefaultConfig())
	if WireBrowserManager(nil, nil, nil, nil) {
		t.Fatal("expected false for nil args")
	}
	if WireBrowserManager(mgr, nil, nil, nil) {
		t.Fatal("expected false for nil gateway/registry")
	}
	if mgr.SandboxEnabled {
		t.Fatal("nil gateway must not enable sandbox on manager")
	}
}
