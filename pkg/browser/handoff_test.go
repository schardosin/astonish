package browser

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
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
	h := NewHandoffServer(log.New(io.Discard, "", 0))
	port := freePort(t)

	info, err := h.Start(HandoffOpts{
		CDPURL:      "ws://127.0.0.1:12345/devtools/browser/fake-id",
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
	_, err = h.Start(HandoffOpts{CDPURL: "ws://127.0.0.1:12345/devtools/browser/fake-id", Port: port})
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
	h := NewHandoffServer(log.New(io.Discard, "", 0))
	port := freePort(t)

	info, err := h.Start(HandoffOpts{
		CDPURL:      "ws://127.0.0.1:9999/devtools/browser/test-id",
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

	if !strings.Contains(bodyStr, "Astonish Browser Handoff") {
		t.Error("expected 'Astonish Browser Handoff' in /json/version response")
	}
	if !strings.Contains(bodyStr, "ws://127.0.0.1:9999/devtools/browser/test-id") {
		t.Error("expected CDP URL in /json/version response")
	}
}

func TestHandoffServer_JSONEndpoint(t *testing.T) {
	h := NewHandoffServer(log.New(io.Discard, "", 0))
	port := freePort(t)

	info, err := h.Start(HandoffOpts{
		CDPURL:      "ws://127.0.0.1:9999/devtools/browser/test-id",
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

	if !strings.Contains(bodyStr, "Astonish Browser") {
		t.Error("expected target entry in /json response")
	}
}

func TestHandoffServer_DoneEndpoint(t *testing.T) {
	h := NewHandoffServer(log.New(io.Discard, "", 0))
	port := freePort(t)

	info, err := h.Start(HandoffOpts{
		CDPURL:      "ws://127.0.0.1:9999/devtools/browser/test-id",
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

func TestHandoffServer_WaitForCompletion_Signal(t *testing.T) {
	h := NewHandoffServer(log.New(io.Discard, "", 0))
	port := freePort(t)

	_, err := h.Start(HandoffOpts{
		CDPURL:      "ws://127.0.0.1:9999/devtools/browser/test-id",
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
	h := NewHandoffServer(log.New(io.Discard, "", 0))
	port := freePort(t)

	_, err := h.Start(HandoffOpts{
		CDPURL:      "ws://127.0.0.1:9999/devtools/browser/test-id",
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
	h := NewHandoffServer(log.New(io.Discard, "", 0))
	port := freePort(t)

	_, err := h.Start(HandoffOpts{
		CDPURL:      "ws://127.0.0.1:9999/devtools/browser/test-id",
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
	h := NewHandoffServer(log.New(io.Discard, "", 0))
	port := freePort(t)

	info, err := h.Start(HandoffOpts{
		CDPURL:      "ws://127.0.0.1:9999/devtools/browser/test-id",
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
	h := NewHandoffServer(log.New(io.Discard, "", 0))
	port := freePort(t)

	info, err := h.Start(HandoffOpts{
		CDPURL: "ws://127.0.0.1:9999/devtools/browser/test-id",
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
		CDPURL: "ws://test",
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
	h := NewHandoffServer(log.New(io.Discard, "", 0))
	port := freePort(t)

	_, err := h.Start(HandoffOpts{
		CDPURL: "ws://127.0.0.1:9999/devtools/browser/test-id",
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
