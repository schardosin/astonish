package tools

import (
	"fmt"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/browser"
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

func TestSandboxBrowserErrorTip_LaunchExit143_NoRestartStudioTip(t *testing.T) {
	mgr := browser.NewManager(browser.BrowserConfig{})
	mgr.SandboxEnabled = true
	mgr.EnsureSessionID("aa4a9422-75d1-4073-b238-b0e9f4e94462")
	// Container cleared after failed StartChromiumInContainer — previously caused
	// a false "CDP not bound / restart Studio" tip on top of exit 143.
	err := fmt.Errorf(
		`failed to get browser page: failed to start browser in container "astn-sess-aa4a9422-75d1-4073-b238-b0e9f4e94462": ` +
			`browser launch exited with code 143 (interrupted by SIGTERM)`,
	)
	tip := sandboxBrowserErrorTip(mgr, err, "")
	if tip != "" && strings.Contains(tip, "restart Studio") {
		t.Fatalf("launch-143 must not get restart-Studio tip: %q", tip)
	}
	if tip != "" && strings.Contains(tip, "CDP is not bound") {
		t.Fatalf("launch-143 must not get CDP-unbound tip: %q", tip)
	}
	if tip == "" || !strings.Contains(tip, "SIGTERM") {
		t.Fatalf("launch-143 tip = %q, want SIGTERM guidance", tip)
	}
}

func TestSandboxBrowserErrorTip_LaunchFailedConcrete_NoTip(t *testing.T) {
	mgr := browser.NewManager(browser.BrowserConfig{})
	mgr.SandboxEnabled = true
	mgr.EnsureSessionID("sess-x")
	err := fmt.Errorf(`failed to start browser in container "astn-sess-x": browser launch exited with code 1: CloakBrowser process died on startup`)
	tip := sandboxBrowserErrorTip(mgr, err, "")
	if tip != "" {
		t.Fatalf("concrete launch failure should not get extra tip: %q", tip)
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
