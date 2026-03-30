package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// freePort finds an available TCP port for testing.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// mockChrome starts a fake Chrome CDP HTTP server that serves realistic
// discovery endpoints. Returns the server and its host:port (e.g. "127.0.0.1:PORT").
func mockChrome(t *testing.T) *httptest.Server {
	t.Helper()

	// We need the server address in the response bodies, but the server
	// doesn't know its own address until it starts. So we start the server
	// first with a placeholder mux, then replace with the real handlers.
	// Alternatively, we compute the address lazily in each handler via r.Host.
	//
	// Use a simple approach: handlers read the server address from a variable
	// that gets set after the server starts.
	var serverAddr string

	mux := http.NewServeMux()

	mux.HandleFunc("/json/version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"Browser":"Chrome/131.0.0.0","Protocol-Version":"1.3","webSocketDebuggerUrl":"ws://%s/devtools/browser/abc-123"}`, serverAddr)
	})

	mux.HandleFunc("/json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `[{"description":"","devtoolsFrontendUrl":"","id":"page-1","title":"Example","type":"page","url":"https://example.com","webSocketDebuggerUrl":"ws://%s/devtools/page/page-1"},{"description":"","id":"browser","title":"","type":"other","url":"","webSocketDebuggerUrl":"ws://%s/devtools/browser/abc-123"}]`, serverAddr, serverAddr)
	})

	mux.HandleFunc("/json/list", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `[{"description":"","devtoolsFrontendUrl":"","id":"page-1","title":"Example","type":"page","url":"https://example.com","webSocketDebuggerUrl":"ws://%s/devtools/page/page-1"},{"description":"","id":"browser","title":"","type":"other","url":"","webSocketDebuggerUrl":"ws://%s/devtools/browser/abc-123"}]`, serverAddr, serverAddr)
	})

	// WebSocket echo handler for proxy testing. Echoes back any message received.
	wsUpgrader := websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}
	mux.HandleFunc("/devtools/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			// Echo with the path prepended so tests can verify correct routing
			reply := fmt.Sprintf("path=%s msg=%s", r.URL.Path, string(msg))
			if err := conn.WriteMessage(mt, []byte(reply)); err != nil {
				return
			}
			_ = mt
		}
	})

	srv := httptest.NewServer(mux)
	serverAddr = strings.TrimPrefix(srv.URL, "http://")
	t.Cleanup(srv.Close)
	return srv
}

// chromeHostPort extracts host:port from an httptest.Server URL.
func chromeHostPort(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	// srv.URL is like "http://127.0.0.1:PORT"
	return strings.TrimPrefix(srv.URL, "http://")
}

func TestNewHandoffServer(t *testing.T) {
	h := NewHandoffServer(nil) // nil logger should use default
	if h == nil {
		t.Fatal("expected non-nil HandoffServer")
	}
	if h.IsActive() {
		t.Error("new server should not be active")
	}
}

func TestNewHandoffServer_WithLogger(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	h := NewHandoffServer(logger)
	if h == nil {
		t.Fatal("expected non-nil HandoffServer")
	}
}

func TestCdpURLToHTTPHost(t *testing.T) {
	tests := []struct {
		name    string
		cdpURL  string
		want    string
		wantErr bool
	}{
		{"standard rod URL", "ws://127.0.0.1:44519/devtools/browser/abc-123", "127.0.0.1:44519", false},
		{"no path", "ws://localhost:9222", "localhost:9222", false},
		{"empty host", "ws:///devtools/browser/x", "", true},
		{"invalid URL", "://bad", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cdpURLToHTTPHost(tt.cdpURL)
			if (err != nil) != tt.wantErr {
				t.Errorf("cdpURLToHTTPHost(%q) error = %v, wantErr %v", tt.cdpURL, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("cdpURLToHTTPHost(%q) = %q, want %q", tt.cdpURL, got, tt.want)
			}
		})
	}
}

func TestHandoffServer_StartRequiresCDPURL(t *testing.T) {
	h := NewHandoffServer(log.New(io.Discard, "", 0))
	_, err := h.Start(HandoffOpts{Port: freePort(t)})
	if err == nil {
		t.Fatal("expected error when CDPURL is empty")
	}
	if !strings.Contains(err.Error(), "CDP URL is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHandoffServer_StartAndStop(t *testing.T) {
	chrome := mockChrome(t)
	chromeAddr := chromeHostPort(t, chrome)
	cdpURL := fmt.Sprintf("ws://%s/devtools/browser/abc-123", chromeAddr)

	h := NewHandoffServer(log.New(io.Discard, "", 0))
	port := freePort(t)

	info, err := h.Start(HandoffOpts{
		CDPURL:      cdpURL,
		Port:        port,
		BindAddress: "127.0.0.1",
		Timeout:     30 * time.Second,
		Reason:      "test handoff",
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil HandoffInfo")
		return
	}
	if info.ListenAddress == "" {
		t.Error("expected non-empty ListenAddress")
	}
	if !strings.Contains(info.ListenAddress, fmt.Sprintf(":%d", port)) {
		t.Errorf("expected port %d in address, got %s", port, info.ListenAddress)
	}

	if !h.IsActive() {
		t.Error("server should be active after Start")
	}

	// Double-start should fail
	_, err = h.Start(HandoffOpts{CDPURL: cdpURL, Port: port})
	if err == nil {
		t.Error("expected error on double Start")
	}

	// Stop
	if err := h.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if h.IsActive() {
		t.Error("server should not be active after Stop")
	}

	// Double-stop should be safe
	if err := h.Stop(); err != nil {
		t.Errorf("double Stop should be safe, got: %v", err)
	}
}

func TestHandoffServer_JSONVersionEndpoint(t *testing.T) {
	chrome := mockChrome(t)
	chromeAddr := chromeHostPort(t, chrome)
	cdpURL := fmt.Sprintf("ws://%s/devtools/browser/abc-123", chromeAddr)

	h := NewHandoffServer(log.New(io.Discard, "", 0))
	port := freePort(t)

	info, err := h.Start(HandoffOpts{
		CDPURL:      cdpURL,
		Port:        port,
		BindAddress: "127.0.0.1",
		Reason:      "test",
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer h.Stop()

	// Hit /json/version
	url := fmt.Sprintf("http://%s/json/version", info.ListenAddress)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET /json/version failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// Should contain the real browser info proxied from Chrome
	if !strings.Contains(bodyStr, "Chrome/131") {
		t.Errorf("expected real Chrome browser info in /json/version, got: %s", bodyStr)
	}

	// The webSocketDebuggerUrl should be rewritten to the proxy address, not the internal Chrome address
	if strings.Contains(bodyStr, chromeAddr) {
		t.Errorf("internal Chrome address %q should NOT appear in response: %s", chromeAddr, bodyStr)
	}
	expectedRewritten := fmt.Sprintf("ws://%s/devtools/browser/abc-123", info.ListenAddress)
	if !strings.Contains(bodyStr, expectedRewritten) {
		t.Errorf("expected rewritten WS URL %q in response, got: %s", expectedRewritten, bodyStr)
	}
}

func TestHandoffServer_JSONEndpoint(t *testing.T) {
	chrome := mockChrome(t)
	chromeAddr := chromeHostPort(t, chrome)
	cdpURL := fmt.Sprintf("ws://%s/devtools/browser/abc-123", chromeAddr)

	h := NewHandoffServer(log.New(io.Discard, "", 0))
	port := freePort(t)

	info, err := h.Start(HandoffOpts{
		CDPURL:      cdpURL,
		Port:        port,
		BindAddress: "127.0.0.1",
		Reason:      "test",
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer h.Stop()

	// Hit /json
	url := fmt.Sprintf("http://%s/json", info.ListenAddress)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET /json failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// Should be a JSON array with real targets
	var targets []map[string]any
	if err := json.Unmarshal(body, &targets); err != nil {
		t.Fatalf("expected valid JSON array, got parse error: %v (body: %s)", err, bodyStr)
	}
	if len(targets) < 1 {
		t.Fatal("expected at least one target in /json response")
		return
	}

	// Internal Chrome address should be fully rewritten
	if strings.Contains(bodyStr, chromeAddr) {
		t.Errorf("internal Chrome address %q should NOT appear in response: %s", chromeAddr, bodyStr)
	}

	// Page target should have rewritten WS URL
	expectedPageWS := fmt.Sprintf("ws://%s/devtools/page/page-1", info.ListenAddress)
	if !strings.Contains(bodyStr, expectedPageWS) {
		t.Errorf("expected rewritten page WS URL %q in response, got: %s", expectedPageWS, bodyStr)
	}
}

func TestHandoffServer_JSONListEndpoint(t *testing.T) {
	chrome := mockChrome(t)
	chromeAddr := chromeHostPort(t, chrome)
	cdpURL := fmt.Sprintf("ws://%s/devtools/browser/abc-123", chromeAddr)

	h := NewHandoffServer(log.New(io.Discard, "", 0))
	port := freePort(t)

	info, err := h.Start(HandoffOpts{
		CDPURL:      cdpURL,
		Port:        port,
		BindAddress: "127.0.0.1",
		Reason:      "test",
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer h.Stop()

	// /json/list should return the same as /json
	url := fmt.Sprintf("http://%s/json/list", info.ListenAddress)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET /json/list failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)

	var targets []map[string]any
	if err := json.Unmarshal(body, &targets); err != nil {
		t.Fatalf("expected valid JSON array from /json/list: %v", err)
	}
	if len(targets) < 1 {
		t.Error("expected at least one target in /json/list response")
	}
}

func TestHandoffServer_DoneEndpoint(t *testing.T) {
	chrome := mockChrome(t)
	chromeAddr := chromeHostPort(t, chrome)
	cdpURL := fmt.Sprintf("ws://%s/devtools/browser/abc-123", chromeAddr)

	h := NewHandoffServer(log.New(io.Discard, "", 0))
	port := freePort(t)

	info, err := h.Start(HandoffOpts{
		CDPURL:      cdpURL,
		Port:        port,
		BindAddress: "127.0.0.1",
		Reason:      "test",
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer h.Stop()

	// Hit /handoff/done via GET
	url := fmt.Sprintf("http://%s/handoff/done", info.ListenAddress)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET /handoff/done failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "OK") {
		t.Error("expected 'OK' in done response")
	}
}

func TestHandoffServer_WebSocketProxyPathForwarding(t *testing.T) {
	chrome := mockChrome(t)
	chromeAddr := chromeHostPort(t, chrome)
	cdpURL := fmt.Sprintf("ws://%s/devtools/browser/abc-123", chromeAddr)

	h := NewHandoffServer(log.New(io.Discard, "", 0))
	port := freePort(t)

	info, err := h.Start(HandoffOpts{
		CDPURL:      cdpURL,
		Port:        port,
		BindAddress: "127.0.0.1",
		Reason:      "test ws proxy",
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer h.Stop()

	// Test that a page-level WebSocket path is correctly forwarded
	pageWSURL := fmt.Sprintf("ws://%s/devtools/page/page-1", info.ListenAddress)
	conn, _, err := websocket.DefaultDialer.Dial(pageWSURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect to proxy WebSocket at %s: %v", pageWSURL, err)
	}
	defer conn.Close()

	// Send a test message
	testMsg := "hello-from-devtools"
	if err := conn.WriteMessage(websocket.TextMessage, []byte(testMsg)); err != nil {
		t.Fatalf("Failed to write message: %v", err)
	}

	// Read the echo response: our mock Chrome echoes "path=<path> msg=<msg>"
	_, reply, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read echo reply: %v", err)
	}

	replyStr := string(reply)
	// Verify the mock Chrome received the correct path (page-level, not browser-level)
	if !strings.Contains(replyStr, "path=/devtools/page/page-1") {
		t.Errorf("expected proxy to forward to /devtools/page/page-1, but mock Chrome saw: %s", replyStr)
	}
	if !strings.Contains(replyStr, testMsg) {
		t.Errorf("expected echo of test message, got: %s", replyStr)
	}
}

func TestHandoffServer_WebSocketProxyBrowserPath(t *testing.T) {
	chrome := mockChrome(t)
	chromeAddr := chromeHostPort(t, chrome)
	cdpURL := fmt.Sprintf("ws://%s/devtools/browser/abc-123", chromeAddr)

	h := NewHandoffServer(log.New(io.Discard, "", 0))
	port := freePort(t)

	info, err := h.Start(HandoffOpts{
		CDPURL:      cdpURL,
		Port:        port,
		BindAddress: "127.0.0.1",
		Reason:      "test ws proxy browser path",
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer h.Stop()

	// Test browser-level WebSocket path
	browserWSURL := fmt.Sprintf("ws://%s/devtools/browser/abc-123", info.ListenAddress)
	conn, _, err := websocket.DefaultDialer.Dial(browserWSURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte("test")); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	_, reply, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	if !strings.Contains(string(reply), "path=/devtools/browser/abc-123") {
		t.Errorf("expected browser path forwarding, got: %s", string(reply))
	}
}

func TestHandoffServer_WaitForCompletion_Signal(t *testing.T) {
	chrome := mockChrome(t)
	chromeAddr := chromeHostPort(t, chrome)
	cdpURL := fmt.Sprintf("ws://%s/devtools/browser/abc-123", chromeAddr)

	h := NewHandoffServer(log.New(io.Discard, "", 0))
	port := freePort(t)

	_, err := h.Start(HandoffOpts{
		CDPURL:      cdpURL,
		Port:        port,
		BindAddress: "127.0.0.1",
		Reason:      "test",
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer h.Stop()

	// Signal done in background after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		h.SignalDone()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.WaitForCompletion(ctx); err != nil {
		t.Fatalf("WaitForCompletion returned error: %v", err)
	}
}

func TestHandoffServer_WaitForCompletion_Timeout(t *testing.T) {
	chrome := mockChrome(t)
	chromeAddr := chromeHostPort(t, chrome)
	cdpURL := fmt.Sprintf("ws://%s/devtools/browser/abc-123", chromeAddr)

	h := NewHandoffServer(log.New(io.Discard, "", 0))
	port := freePort(t)

	_, err := h.Start(HandoffOpts{
		CDPURL:      cdpURL,
		Port:        port,
		BindAddress: "127.0.0.1",
		Reason:      "test",
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer h.Stop()

	// Very short timeout — should expire before signal
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = h.WaitForCompletion(ctx)
	if err == nil {
		t.Error("expected timeout error")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got: %v", err)
	}
}

func TestHandoffServer_SignalDoneMultipleTimes(t *testing.T) {
	chrome := mockChrome(t)
	chromeAddr := chromeHostPort(t, chrome)
	cdpURL := fmt.Sprintf("ws://%s/devtools/browser/abc-123", chromeAddr)

	h := NewHandoffServer(log.New(io.Discard, "", 0))
	port := freePort(t)

	_, err := h.Start(HandoffOpts{
		CDPURL:      cdpURL,
		Port:        port,
		BindAddress: "127.0.0.1",
		Reason:      "test",
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer h.Stop()

	// Calling SignalDone multiple times should not panic
	h.SignalDone()
	h.SignalDone()
	h.SignalDone()
}

func TestHandoffServer_DoneEndpointViaHTTP(t *testing.T) {
	chrome := mockChrome(t)
	chromeAddr := chromeHostPort(t, chrome)
	cdpURL := fmt.Sprintf("ws://%s/devtools/browser/abc-123", chromeAddr)

	h := NewHandoffServer(log.New(io.Discard, "", 0))
	port := freePort(t)

	info, err := h.Start(HandoffOpts{
		CDPURL:      cdpURL,
		Port:        port,
		BindAddress: "127.0.0.1",
		Reason:      "test",
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer h.Stop()

	// Hit the done endpoint via HTTP in background
	go func() {
		time.Sleep(50 * time.Millisecond)
		url := fmt.Sprintf("http://%s/handoff/done", info.ListenAddress)
		resp, httpErr := http.Get(url)
		if httpErr == nil {
			resp.Body.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.WaitForCompletion(ctx); err != nil {
		t.Fatalf("WaitForCompletion returned error after /handoff/done: %v", err)
	}
}

func TestHandoffServer_ListenAddressReflectsActualPort(t *testing.T) {
	chrome := mockChrome(t)
	chromeAddr := chromeHostPort(t, chrome)
	cdpURL := fmt.Sprintf("ws://%s/devtools/browser/abc-123", chromeAddr)

	h := NewHandoffServer(log.New(io.Discard, "", 0))
	port := freePort(t)

	info, err := h.Start(HandoffOpts{
		CDPURL: cdpURL,
		Port:   port,
		Reason: "defaults test",
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer h.Stop()

	if !strings.HasPrefix(info.ListenAddress, "127.0.0.1:") {
		t.Errorf("expected default bind 127.0.0.1, got %s", info.ListenAddress)
	}

	// ListenAddress should contain the actual port, not 0
	if strings.HasSuffix(info.ListenAddress, ":0") {
		t.Error("ListenAddress should contain the actual port, not :0")
	}
}

func TestHandoffOpts_Defaults(t *testing.T) {
	opts := HandoffOpts{
		CDPURL: "ws://test:1234",
	}
	if opts.Port != 0 {
		t.Error("Port zero value should be 0")
	}
	if opts.BindAddress != "" {
		t.Error("BindAddress zero value should be empty")
	}
	if opts.Timeout != 0 {
		t.Error("Timeout zero value should be 0")
	}
}

func TestHandoffServer_StopSignalsDone(t *testing.T) {
	chrome := mockChrome(t)
	chromeAddr := chromeHostPort(t, chrome)
	cdpURL := fmt.Sprintf("ws://%s/devtools/browser/abc-123", chromeAddr)

	h := NewHandoffServer(log.New(io.Discard, "", 0))
	port := freePort(t)

	_, err := h.Start(HandoffOpts{
		CDPURL: cdpURL,
		Port:   port,
		Reason: "test",
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Stop should signal doneCh
	done := make(chan struct{})
	go func() {
		ctx := context.Background()
		_ = h.WaitForCompletion(ctx)
		close(done)
	}()

	// Small delay to ensure the goroutine starts waiting
	time.Sleep(20 * time.Millisecond)

	if err := h.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	select {
	case <-done:
		// Expected — WaitForCompletion returned after Stop
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForCompletion did not return after Stop")
	}
}

func TestHandoffServer_AutoDoneAfterAllWSClose(t *testing.T) {
	chrome := mockChrome(t)
	chromeAddr := chromeHostPort(t, chrome)
	cdpURL := fmt.Sprintf("ws://%s/devtools/browser/abc-123", chromeAddr)

	h := NewHandoffServer(log.New(io.Discard, "", 0))
	h.graceDuration = 100 * time.Millisecond // short grace for test speed
	port := freePort(t)

	_, err := h.Start(HandoffOpts{
		CDPURL:      cdpURL,
		Port:        port,
		BindAddress: "127.0.0.1",
		Reason:      "test auto-done",
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer h.Stop()

	// Connect a WebSocket, then close it
	wsURL := fmt.Sprintf("ws://127.0.0.1:%d/devtools/page/page-1", port)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	conn.Close()

	// WaitForCompletion should return within grace period + some margin
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := h.WaitForCompletion(ctx); err != nil {
		t.Fatalf("expected auto-done after WS disconnect, got: %v", err)
	}
}

func TestHandoffServer_GraceCancelledOnReconnect(t *testing.T) {
	chrome := mockChrome(t)
	chromeAddr := chromeHostPort(t, chrome)
	cdpURL := fmt.Sprintf("ws://%s/devtools/browser/abc-123", chromeAddr)

	h := NewHandoffServer(log.New(io.Discard, "", 0))
	h.graceDuration = 200 * time.Millisecond
	port := freePort(t)

	_, err := h.Start(HandoffOpts{
		CDPURL:      cdpURL,
		Port:        port,
		BindAddress: "127.0.0.1",
		Reason:      "test grace cancel",
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer h.Stop()

	wsURL := fmt.Sprintf("ws://127.0.0.1:%d/devtools/page/page-1", port)

	// Connect, disconnect — this starts the grace timer
	conn1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	conn1.Close()

	// Wait briefly, then reconnect before grace expires
	time.Sleep(50 * time.Millisecond)
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to reconnect: %v", err)
	}

	// WaitForCompletion should NOT return yet (connection is still open)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err = h.WaitForCompletion(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("expected timeout (connection still open), got: %v", err)
	}

	// Now close the second connection — auto-done should fire after grace
	conn2.Close()

	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()

	if err := h.WaitForCompletion(ctx2); err != nil {
		t.Fatalf("expected auto-done after second disconnect, got: %v", err)
	}
}

func TestHandoffServer_MultipleWSConnections(t *testing.T) {
	chrome := mockChrome(t)
	chromeAddr := chromeHostPort(t, chrome)
	cdpURL := fmt.Sprintf("ws://%s/devtools/browser/abc-123", chromeAddr)

	h := NewHandoffServer(log.New(io.Discard, "", 0))
	h.graceDuration = 100 * time.Millisecond
	port := freePort(t)

	_, err := h.Start(HandoffOpts{
		CDPURL:      cdpURL,
		Port:        port,
		BindAddress: "127.0.0.1",
		Reason:      "test multi-conn",
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer h.Stop()

	// Open two connections (like DevTools does: browser + page)
	wsURL1 := fmt.Sprintf("ws://127.0.0.1:%d/devtools/browser/abc-123", port)
	wsURL2 := fmt.Sprintf("ws://127.0.0.1:%d/devtools/page/page-1", port)

	conn1, _, err := websocket.DefaultDialer.Dial(wsURL1, nil)
	if err != nil {
		t.Fatalf("Failed to connect conn1: %v", err)
	}
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL2, nil)
	if err != nil {
		t.Fatalf("Failed to connect conn2: %v", err)
	}

	// Close only one — should NOT trigger auto-done
	conn1.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err = h.WaitForCompletion(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("expected timeout (one connection still open), got: %v", err)
	}

	// Close the second — auto-done should fire after grace
	conn2.Close()

	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()

	if err := h.WaitForCompletion(ctx2); err != nil {
		t.Fatalf("expected auto-done after all connections closed, got: %v", err)
	}
}
