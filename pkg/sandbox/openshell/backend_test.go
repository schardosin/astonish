package openshell

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/sandbox"
)

func TestOpenShellBackendKind(t *testing.T) {
	b := newTestBackend(t)
	if got, want := b.Kind(), sandbox.BackendKindOpenShell; got != want {
		t.Errorf("Kind() = %q, want %q", got, want)
	}
}

func TestOpenShellBackendCapabilities(t *testing.T) {
	b := newTestBackend(t)
	caps := b.Capabilities()
	if caps.Kind != sandbox.BackendKindOpenShell {
		t.Errorf("Capabilities.Kind = %q, want %q", caps.Kind, sandbox.BackendKindOpenShell)
	}
	if !caps.SupportsLiveEvict {
		t.Error("SupportsLiveEvict should be true")
	}
	if !caps.SupportsFastClone {
		t.Error("SupportsFastClone should be true")
	}
	if !caps.SupportsPortExpose {
		t.Error("SupportsPortExpose should be true")
	}
	if !caps.SupportsOrgIsolation {
		t.Error("SupportsOrgIsolation should be true")
	}
}

func TestOpenShellBackendHealth(t *testing.T) {
	b := newTestBackend(t)
	h, err := b.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error: %v", err)
	}
	if !h.Healthy {
		t.Errorf("Health.Healthy = false, want true")
	}
	if h.Details["gateway_addr"] == "" {
		t.Error("Health.Details[gateway_addr] should not be empty")
	}
	if h.Details["sandbox_image"] == "" {
		t.Error("Health.Details[sandbox_image] should not be empty")
	}
}

func TestOpenShellBackendHealth_CancelledContext(t *testing.T) {
	b := newTestBackend(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := b.Health(ctx)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestOpenShellBackendStubsReturnNotImplemented(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	_, err := b.CreateSession(ctx, sandbox.SessionSpec{})
	if !errors.Is(err, ErrNotImplementedYet) {
		t.Errorf("CreateSession: got %v, want ErrNotImplementedYet", err)
	}

	if err := b.StartSession(ctx, "test"); !errors.Is(err, ErrNotImplementedYet) {
		t.Errorf("StartSession: got %v, want ErrNotImplementedYet", err)
	}

	// StopSession and DestroySession are idempotent no-ops for nonexistent sessions
	// even with nil gateway.
	if err := b.StopSession(ctx, "test"); err != nil {
		t.Errorf("StopSession on nonexistent: got %v, want nil", err)
	}

	if err := b.DestroySession(ctx, "test"); err != nil {
		t.Errorf("DestroySession on nonexistent: got %v, want nil", err)
	}

	// Exec methods require a non-empty Command to pass validation before
	// hitting the gateway-nil check.
	_, err = b.Exec(ctx, "test", sandbox.ExecSpec{Command: []string{"echo", "hi"}})
	if !errors.Is(err, ErrNotImplementedYet) {
		t.Errorf("Exec: got %v, want ErrNotImplementedYet", err)
	}

	_, err = b.ExecInteractive(ctx, "test", sandbox.PTYSpec{Command: []string{"bash"}})
	if !errors.Is(err, ErrNotImplementedYet) {
		t.Errorf("ExecInteractive: got %v, want ErrNotImplementedYet", err)
	}

	_, err = b.ExecStreaming(ctx, "test", sandbox.ExecStreamSpec{Command: []string{"cat"}})
	if !errors.Is(err, ErrNotImplementedYet) {
		t.Errorf("ExecStreaming: got %v, want ErrNotImplementedYet", err)
	}

	if err := b.PushFile(ctx, "test", "/tmp/foo", strings.NewReader("x"), 0644); !errors.Is(err, ErrNotImplementedYet) {
		t.Errorf("PushFile: got %v, want ErrNotImplementedYet", err)
	}

	_, err = b.PullFile(ctx, "test", "/tmp/foo")
	if !errors.Is(err, ErrNotImplementedYet) {
		t.Errorf("PullFile: got %v, want ErrNotImplementedYet", err)
	}
}

func TestOpenShellBackendNew_RequiresSessions(t *testing.T) {
	_, err := New(Config{})
	if err == nil {
		t.Fatal("expected error for nil Sessions")
	}
}

func TestOpenShellBackendServerArchitecture(t *testing.T) {
	b := newTestBackend(t)
	if got := b.ServerArchitecture(); got != "amd64" {
		t.Errorf("ServerArchitecture() = %q, want %q", got, "amd64")
	}
}

func TestOpenShellBackendFactoryRegistration(t *testing.T) {
	sr, err := sandbox.NewSessionRegistry()
	if err != nil {
		t.Fatalf("NewSessionRegistry: %v", err)
	}
	b, err := sandbox.NewBackend(sandbox.BackendFactoryConfig{
		Kind:     sandbox.BackendKindOpenShell,
		Sessions: sr,
	})
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	if b.Kind() != sandbox.BackendKindOpenShell {
		t.Errorf("Kind() = %q, want %q", b.Kind(), sandbox.BackendKindOpenShell)
	}
}

// newTestBackend creates an OpenShellBackend with minimal valid config for testing.
func newTestBackend(t *testing.T) *OpenShellBackend {
	t.Helper()
	sr, err := sandbox.NewSessionRegistry()
	if err != nil {
		t.Fatalf("NewSessionRegistry: %v", err)
	}
	b, err := New(Config{Sessions: sr})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return b
}
