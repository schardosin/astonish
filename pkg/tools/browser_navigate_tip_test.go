package tools

import (
	"fmt"
	"strings"
	"testing"

	"github.com/schardosin/astonish/pkg/browser"
)

func TestSandboxBrowserErrorTip_HostChromeLoopback(t *testing.T) {
	mgr := browser.NewManager(browser.BrowserConfig{})
	mgr.SandboxEnabled = false
	mgr.EnsureSessionID("sess-host")

	tip := sandboxBrowserErrorTip(mgr, fmt.Errorf("net::ERR_CONNECTION_REFUSED"), "http://127.0.0.1:8000/")
	if tip == "" || !strings.Contains(tip, "SandboxEnabled=false") {
		t.Fatalf("host tip = %q", tip)
	}
	if strings.Contains(tip, "in-container Chromium") {
		t.Fatalf("must not claim in-container when SandboxEnabled=false: %q", tip)
	}
}

func TestSandboxBrowserErrorTip_UnboundCDP(t *testing.T) {
	mgr := browser.NewManager(browser.BrowserConfig{})
	mgr.SandboxEnabled = true
	mgr.EnsureSessionID("sess-unbound")

	tip := sandboxBrowserErrorTip(mgr, fmt.Errorf("failed to get browser page"), "")
	if tip == "" || !strings.Contains(tip, "CDP is not bound") {
		t.Fatalf("unbound tip = %q", tip)
	}
}

func TestSandboxBrowserErrorTip_WrongSessionContainer(t *testing.T) {
	mgr := browser.NewManager(browser.BrowserConfig{})
	mgr.SandboxEnabled = true
	mgr.EnsureSessionID("aec5c0cb-af29-41a1-9d4b-a18b3e17fb03")
	mgr.SetContainerForTest("astn-sess-2033fdea-fb63-46a2-bb3b-8b219400b38d", "10.99.0.86")

	tip := sandboxBrowserErrorTip(mgr, fmt.Errorf("net::ERR_CONNECTION_REFUSED"), "http://127.0.0.1:8000/")
	if tip == "" || !strings.Contains(tip, "stuck on a previous session") {
		t.Fatalf("wrong-session tip = %q", tip)
	}
	if strings.Contains(tip, "in-container Chromium could not reach") {
		t.Fatalf("wrong-session must not use app-dead tip: %q", tip)
	}
}

func TestSandboxBrowserErrorTip_InContainerLoopback(t *testing.T) {
	mgr := browser.NewManager(browser.BrowserConfig{})
	mgr.SandboxEnabled = true
	sess := "aec5c0cb-af29-41a1-9d4b-a18b3e17fb03"
	mgr.EnsureSessionID(sess)
	mgr.SetContainerForTest("astn-sess-"+sess, "10.99.0.182")

	tip := sandboxBrowserErrorTip(mgr, fmt.Errorf("net::ERR_CONNECTION_REFUSED"), "http://127.0.0.1:8000/")
	if tip == "" || !strings.Contains(tip, "in-container Chromium could not reach") {
		t.Fatalf("in-container tip = %q", tip)
	}
	if !strings.Contains(tip, "session=") || !strings.Contains(tip, "container=") {
		t.Fatalf("in-container tip missing diagnostics: %q", tip)
	}
}

func TestSandboxBrowserErrorTip_NonRefusedNoLoopbackTip(t *testing.T) {
	mgr := browser.NewManager(browser.BrowserConfig{})
	mgr.SandboxEnabled = true
	sess := "aec5c0cb-af29-41a1-9d4b-a18b3e17fb03"
	mgr.EnsureSessionID(sess)
	mgr.SetContainerForTest("astn-sess-"+sess, "10.99.0.182")

	tip := sandboxBrowserErrorTip(mgr, fmt.Errorf("timeout"), "http://127.0.0.1:8000/")
	if tip != "" {
		t.Fatalf("non-refused navigate should not get loopback tip: %q", tip)
	}
}

func TestContainerMatchesSession(t *testing.T) {
	if !containerMatchesSession("astn-sess-abc-123", "abc-123") {
		t.Fatal("expected match")
	}
	if containerMatchesSession("astn-sess-other", "abc-123") {
		t.Fatal("expected mismatch")
	}
}
