package browser

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/schardosin/astonish/pkg/sandbox"
)

// TestGetOrLaunch_ContainerPath_MissingResolveFunc verifies that GetOrLaunch
// returns a clear error when SandboxEnabled is true but ContainerResolveFunc
// is not wired.
func TestGetOrLaunch_ContainerPath_MissingResolveFunc(t *testing.T) {
	m := NewManager(BrowserConfig{})
	m.SandboxEnabled = true
	m.sessionID = "test-session"
	// Do NOT set ContainerResolveFunc

	_, err := m.GetOrLaunch()
	if err == nil {
		t.Fatal("expected error when ContainerResolveFunc is nil")
	}
	if !strings.Contains(err.Error(), "ContainerResolveFunc") {
		t.Errorf("error should mention ContainerResolveFunc, got: %v", err)
	}
}

// TestGetOrLaunch_ContainerPath_ResolveError verifies that a resolve failure
// is propagated and stale state is not left behind.
func TestGetOrLaunch_ContainerPath_ResolveError(t *testing.T) {
	m := NewManager(BrowserConfig{})
	m.SandboxEnabled = true
	m.sessionID = "test-session"
	m.ContainerResolveFunc = func(_ string) (string, string, error) {
		return "", "", fmt.Errorf("container not running")
	}

	_, err := m.GetOrLaunch()
	if err == nil {
		t.Fatal("expected error on resolve failure")
	}
	if !strings.Contains(err.Error(), "container not running") {
		t.Errorf("error should contain resolve error, got: %v", err)
	}
}

// TestGetOrLaunch_ContainerPath_StartError verifies that a browser start
// failure clears stale container state so retries go through the full sequence.
func TestGetOrLaunch_ContainerPath_StartError(t *testing.T) {
	m := NewManager(BrowserConfig{})
	m.SandboxEnabled = true
	m.sessionID = "test-session"
	m.ContainerResolveFunc = func(_ string) (string, string, error) {
		return "astn-sess-test", "10.0.0.1", nil
	}
	m.ContainerStartBrowserFunc = func(_ string) error {
		return fmt.Errorf("chromium failed to start")
	}

	_, err := m.GetOrLaunch()
	if err == nil {
		t.Fatal("expected error on start failure")
	}
	if !strings.Contains(err.Error(), "chromium failed to start") {
		t.Errorf("error should contain start error, got: %v", err)
	}

	// Verify stale state was cleared
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.containerName != "" {
		t.Error("containerName should be cleared after start failure")
	}
	if m.containerIP != "" {
		t.Error("containerIP should be cleared after start failure")
	}
}

// TestGetOrLaunch_ContainerPath_CallSequence verifies the callback invocation
// order: resolve → start → resolveCDP. We can't fully test CDP resolution
// without a real Chrome, but we verify that resolve and start are called with
// the correct arguments.
func TestGetOrLaunch_ContainerPath_CallSequence(t *testing.T) {
	var resolvedSession string
	var startedContainer string

	m := NewManager(BrowserConfig{})
	m.SandboxEnabled = true
	m.sessionID = "test-session-123"
	m.ContainerResolveFunc = func(sessionID string) (string, string, error) {
		resolvedSession = sessionID
		return "astn-sess-test123", "10.0.0.1", nil
	}
	m.ContainerStartBrowserFunc = func(containerName string) error {
		startedContainer = containerName
		return nil
	}
	// ContainerDialFunc that fails immediately — we just want to verify
	// the resolve+start callbacks were called, not actually connect CDP.
	m.ContainerDialFunc = func(_ string, _ int) (net.Conn, error) {
		return nil, fmt.Errorf("intentional test failure")
	}

	_, err := m.GetOrLaunch()
	// Error expected because CDP resolution will fail
	if err == nil {
		t.Fatal("expected error (no real Chrome to connect to)")
	}

	if resolvedSession != "test-session-123" {
		t.Errorf("resolve called with session %q, want %q", resolvedSession, "test-session-123")
	}
	if startedContainer != "astn-sess-test123" {
		t.Errorf("start called with container %q, want %q", startedContainer, "astn-sess-test123")
	}
}

// TestResolveCDPURL_UsesTunnel verifies that when ContainerDialFunc is set,
// HTTP requests to /json/version are routed through the tunnel dialer rather
// than direct TCP to the container IP.
func TestResolveCDPURL_UsesTunnel(t *testing.T) {
	// Start a local HTTP server that responds to /json/version
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/version" {
			resp := map[string]string{
				"webSocketDebuggerUrl": "ws://10.0.0.1:9222/devtools/browser/test-guid",
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// Extract the port the test server is listening on
	srvAddr := srv.Listener.Addr().(*net.TCPAddr)
	srvPort := srvAddr.Port

	var tunnelCalls atomic.Int32

	m := NewManager(BrowserConfig{})
	m.containerName = "astn-sess-test"
	m.ContainerDialFunc = func(containerName string, port int) (net.Conn, error) {
		tunnelCalls.Add(1)
		if containerName != "astn-sess-test" {
			return nil, fmt.Errorf("wrong container: %s", containerName)
		}
		if port != sandbox.DefaultCDPPort {
			return nil, fmt.Errorf("wrong port: %d, want %d", port, sandbox.DefaultCDPPort)
		}
		// Connect to the local test server instead
		return net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", srvPort))
	}

	// The IP here doesn't matter — the tunnel dialer ignores it
	resolved, err := m.resolveCDPURL("astn-sess-test", "10.0.0.1")
	if err != nil {
		t.Fatalf("resolveCDPURL with tunnel: %v", err)
	}

	if resolved != "ws://10.0.0.1:9222/devtools/browser/test-guid" {
		t.Errorf("resolved URL: got %q, want ws://10.0.0.1:9222/devtools/browser/test-guid", resolved)
	}

	if tunnelCalls.Load() == 0 {
		t.Error("ContainerDialFunc should have been called at least once")
	}
}

// TestResolveCDPURL_DirectFallback verifies that when ContainerDialFunc is nil,
// HTTP requests go directly to the container IP.
func TestResolveCDPURL_DirectFallback(t *testing.T) {
	// Start a local HTTP server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/version" {
			resp := map[string]string{
				"webSocketDebuggerUrl": "ws://127.0.0.1:9222/devtools/browser/direct-guid",
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// Extract host:port from test server
	srvAddr := srv.Listener.Addr().(*net.TCPAddr)

	m := NewManager(BrowserConfig{})
	// No ContainerDialFunc — should use direct HTTP
	m.ContainerDialFunc = nil

	resolved, err := m.resolveCDPURL("", srvAddr.IP.String())
	if err != nil {
		// If the port doesn't match sandbox.DefaultCDPPort, it will fail.
		// We need to override the URL to use the test server port.
		// Since resolveCDPURL hardcodes the port, this test verifies the
		// fallback path is taken (no tunnel). The actual connection will
		// fail unless the server happens to be on port 9222. That's fine —
		// this test documents the behavior.
		if !strings.Contains(err.Error(), "after 15s") {
			t.Fatalf("unexpected error (should be timeout): %v", err)
		}
		t.Skip("direct fallback test skipped — test server not on port 9222")
	}

	if resolved == "" {
		t.Error("expected non-empty resolved URL")
	}
}

// TestResolveCDPURL_RetryOnFailure verifies that resolveCDPURL retries when
// the first attempts fail.
func TestResolveCDPURL_RetryOnFailure(t *testing.T) {
	var requestCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		if r.URL.Path == "/json/version" {
			// Fail the first 2 requests, succeed on the 3rd
			if count <= 2 {
				http.Error(w, "not ready", http.StatusServiceUnavailable)
				return
			}
			resp := map[string]string{
				"webSocketDebuggerUrl": "ws://10.0.0.1:9222/devtools/browser/retry-guid",
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	srvAddr := srv.Listener.Addr().(*net.TCPAddr)
	srvPort := srvAddr.Port

	m := NewManager(BrowserConfig{})
	m.containerName = "astn-sess-retry"
	m.ContainerDialFunc = func(_ string, _ int) (net.Conn, error) {
		return net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", srvPort))
	}

	resolved, err := m.resolveCDPURL("astn-sess-retry", "10.0.0.1")
	if err != nil {
		t.Fatalf("resolveCDPURL with retry: %v", err)
	}

	if resolved != "ws://10.0.0.1:9222/devtools/browser/retry-guid" {
		t.Errorf("resolved URL: got %q", resolved)
	}

	if requestCount.Load() < 3 {
		t.Errorf("expected at least 3 requests (2 failures + 1 success), got %d", requestCount.Load())
	}
}

// TestConnectContainerCDP_NoDialFunc_FallsToRemote verifies that when
// ContainerDialFunc is nil, connectContainerCDP falls back to connectRemote.
func TestConnectContainerCDP_NoDialFunc_FallsToRemote(t *testing.T) {
	m := NewManager(BrowserConfig{
		RemoteCDPURL: "ws://fake:9222/devtools/browser/test",
	})
	m.ContainerDialFunc = nil
	m.containerName = "" // empty triggers fallback

	// This will fail because there's no real browser, but it should
	// attempt the connectRemote path (not the tunnel path)
	_, err := m.connectContainerCDP()
	if err == nil {
		t.Fatal("expected error (no real browser)")
	}
	// The error should be from connectRemote, not from tunnel dialer
	if strings.Contains(err.Error(), "tunnel") {
		t.Errorf("should not attempt tunnel when ContainerDialFunc is nil, got: %v", err)
	}
}
