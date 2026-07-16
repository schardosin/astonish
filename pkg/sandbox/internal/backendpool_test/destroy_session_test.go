// Package backendpool_test — unit tests for DestroySessionEverywhere + TryDestroySession
// using the mock backend (requires importing pkg/sandbox/mock to register).
//
// Lives here for the same reason as backend_pool_test.go: pkg/sandbox
// cannot import pkg/sandbox/mock (cycle).

package backendpool_test

import (
	"context"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/sandbox"
	"github.com/SAP/astonish/pkg/sandbox/mock"
)

// TestDestroySessionEverywhere_MockBackend exercises the full path through
// BackendFromAppConfig → Mock → DestroySession.
func TestDestroySessionEverywhere_MockBackend(t *testing.T) {
	appCfg := &config.AppConfig{}
	appCfg.Sandbox.Backend = "mock"

	err := sandbox.DestroySessionEverywhere(context.Background(), appCfg, "session-xyz", nil)
	if err != nil {
		t.Fatalf("DestroySessionEverywhere(mock): %v", err)
	}

	// Call again — idempotent.
	err = sandbox.DestroySessionEverywhere(context.Background(), appCfg, "session-xyz", nil)
	if err != nil {
		t.Fatalf("DestroySessionEverywhere(mock) idempotent: %v", err)
	}
}

// TestDestroySessionEverywhere_CancelledContext honours ctx.
func TestDestroySessionEverywhere_CancelledContext(t *testing.T) {
	appCfg := &config.AppConfig{}
	appCfg.Sandbox.Backend = "mock"

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	err := sandbox.DestroySessionEverywhere(ctx, appCfg, "session-xyz", nil)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("error = %v, want context canceled", err)
	}
}

// TestDestroySessionEverywhere_MockRecordsCall verifies the mock's call log.
func TestDestroySessionEverywhere_MockRecordsCall(t *testing.T) {
	appCfg := &config.AppConfig{}
	appCfg.Sandbox.Backend = "mock"

	// Build the backend independently so we can inspect the mock's state.
	b, cleanup, err := sandbox.BackendFromAppConfig(appCfg)
	if err != nil {
		t.Fatalf("BackendFromAppConfig: %v", err)
	}
	if cleanup != nil {
		defer cleanup()
	}

	mb := b.(*mock.MockBackend)

	// DestroySession directly through the mock.
	if err := mb.DestroySession(context.Background(), "sess-A"); err != nil {
		t.Fatalf("DestroySession: %v", err)
	}
	calls := mb.DestroySessionCalls()
	if len(calls) != 1 || calls[0] != "sess-A" {
		t.Errorf("DestroySessionCalls = %v, want [sess-A]", calls)
	}
}

// TestTryDestroySession_Mock runs end-to-end silently.
func TestTryDestroySession_Mock(t *testing.T) {
	appCfg := &config.AppConfig{}
	appCfg.Sandbox.Backend = "mock"
	sandbox.TryDestroySession(appCfg, "session-abc", nil)
}
