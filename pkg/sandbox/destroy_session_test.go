package sandbox

import (
	"context"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/config"
)

// TestDestroySessionEverywhere_NilAppConfig ensures clear error.
func TestDestroySessionEverywhere_NilAppConfig(t *testing.T) {
	err := DestroySessionEverywhere(context.Background(), nil, "abc123", nil)
	if err == nil {
		t.Fatal("expected error for nil AppConfig")
	}
	if !strings.Contains(err.Error(), "nil app config") {
		t.Errorf("error = %v, want nil-app-config wording", err)
	}
}

// TestDestroySessionEverywhere_EmptySessionID is a no-op.
func TestDestroySessionEverywhere_EmptySessionID(t *testing.T) {
	appCfg := &config.AppConfig{}
	appCfg.Sandbox.Backend = "mock"
	err := DestroySessionEverywhere(context.Background(), appCfg, "", nil)
	if err != nil {
		t.Fatalf("empty sessionID should be no-op, got: %v", err)
	}
}

// TestDestroySessionEverywhere_UnknownBackend surfaces errors from
// BackendFromAppConfig when the backend kind is unrecognized.
func TestDestroySessionEverywhere_UnknownBackend(t *testing.T) {
	appCfg := &config.AppConfig{}
	appCfg.Sandbox.Backend = "docker"

	err := DestroySessionEverywhere(context.Background(), appCfg, "session-xyz", nil)
	if err == nil {
		t.Fatal("expected error for unknown backend kind")
	}
	if !strings.Contains(err.Error(), "backend init") {
		t.Errorf("error = %v, want backend init wording", err)
	}
}

// TestTryDestroySession_NilAppConfig silent no-op.
func TestTryDestroySession_NilAppConfig(t *testing.T) {
	// Should not panic.
	TryDestroySession(nil, "abc", nil)
}

// TestTryDestroySession_EmptySession silent no-op.
func TestTryDestroySession_EmptySession(t *testing.T) {
	appCfg := &config.AppConfig{}
	appCfg.Sandbox.Backend = "mock"
	TryDestroySession(appCfg, "", nil)
}
