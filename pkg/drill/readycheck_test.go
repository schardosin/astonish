package drill

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/schardosin/astonish/pkg/config"
)

func TestRunReadyCheckNil(t *testing.T) {
	err := RunReadyCheck(context.Background(), nil)
	if err != nil {
		t.Errorf("nil ready check should return nil error, got: %v", err)
	}
}

func TestReadyCheckHTTPSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer server.Close()

	rc := &config.ReadyCheck{
		Type:     "http",
		URL:      server.URL,
		Timeout:  5,
		Interval: 1,
	}

	err := RunReadyCheck(context.Background(), rc)
	if err != nil {
		t.Errorf("expected success, got: %v", err)
	}
}

func TestReadyCheckHTTPTimeout(t *testing.T) {
	// Server that always returns 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()

	rc := &config.ReadyCheck{
		Type:     "http",
		URL:      server.URL,
		Timeout:  2,
		Interval: 1,
	}

	err := RunReadyCheck(context.Background(), rc)
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestReadyCheckHTTPBecomeReady(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	rc := &config.ReadyCheck{
		Type:     "http",
		URL:      server.URL,
		Timeout:  10,
		Interval: 1,
	}

	err := RunReadyCheck(context.Background(), rc)
	if err != nil {
		t.Errorf("expected success after retries, got: %v", err)
	}
	if attempts < 3 {
		t.Errorf("expected at least 3 attempts, got %d", attempts)
	}
}

func TestReadyCheckHTTPMissingURL(t *testing.T) {
	rc := &config.ReadyCheck{
		Type:    "http",
		Timeout: 1,
	}

	err := RunReadyCheck(context.Background(), rc)
	if err == nil {
		t.Error("expected error for missing URL")
	}
}

func TestReadyCheckPortSuccess(t *testing.T) {
	// Start a TCP listener
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	_, portStr, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portStr)

	rc := &config.ReadyCheck{
		Type:     "port",
		Host:     "127.0.0.1",
		Port:     port,
		Timeout:  5,
		Interval: 1,
	}

	err = RunReadyCheck(context.Background(), rc)
	if err != nil {
		t.Errorf("expected success, got: %v", err)
	}
}

func TestReadyCheckPortTimeout(t *testing.T) {
	// Use a port that nothing is listening on
	rc := &config.ReadyCheck{
		Type:     "port",
		Host:     "127.0.0.1",
		Port:     59999,
		Timeout:  2,
		Interval: 1,
	}

	err := RunReadyCheck(context.Background(), rc)
	if err == nil {
		t.Error("expected timeout error for closed port")
	}
}

func TestReadyCheckPortMissingPort(t *testing.T) {
	rc := &config.ReadyCheck{
		Type:    "port",
		Timeout: 1,
	}

	err := RunReadyCheck(context.Background(), rc)
	if err == nil {
		t.Error("expected error for missing port")
	}
}

func TestReadyCheckContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a short delay
	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()

	rc := &config.ReadyCheck{
		Type:     "http",
		URL:      server.URL,
		Timeout:  30,
		Interval: 1,
	}

	err := RunReadyCheck(ctx, rc)
	if err == nil {
		t.Error("expected error on context cancellation")
	}
}

func TestReadyCheckDefaultTimeouts(t *testing.T) {
	// Server that's immediately ready
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer server.Close()

	rc := &config.ReadyCheck{
		Type: "http",
		URL:  server.URL,
		// Timeout and Interval are 0 — should use defaults
	}

	err := RunReadyCheck(context.Background(), rc)
	if err != nil {
		t.Errorf("expected success with defaults, got: %v", err)
	}
}

func TestReadyCheckUnknownType(t *testing.T) {
	rc := &config.ReadyCheck{
		Type:    "unknown",
		Timeout: 1,
	}

	err := RunReadyCheck(context.Background(), rc)
	if err == nil {
		t.Error("expected error for unknown type")
	}
	if err != nil {
		expected := fmt.Sprintf("ready check timed out after 1s (type: unknown)")
		// The error could be from checkOnce or from the timeout
		_ = expected
	}
}

func TestCheckOutputContains(t *testing.T) {
	if !CheckOutputContains("Server listening on port 3000", "listening on") {
		t.Error("expected match")
	}
	if CheckOutputContains("Starting server...", "listening on") {
		t.Error("expected no match")
	}
}
