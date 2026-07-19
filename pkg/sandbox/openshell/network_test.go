package openshell

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/sandbox"
	"github.com/SAP/astonish/pkg/store"
)

// ---------------------------------------------------------------------------
// EnsureOrgNetwork / DeleteOrgNetwork
// ---------------------------------------------------------------------------

func TestEnsureOrgNetwork_Noop(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)

	if err := b.EnsureOrgNetwork(context.Background(), "acme"); err != nil {
		t.Fatalf("EnsureOrgNetwork: %v", err)
	}
}

func TestEnsureOrgNetwork_CancelledContext(t *testing.T) {
	b := newTestBackend(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.EnsureOrgNetwork(ctx, "x"); err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestDeleteOrgNetwork_Noop(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)

	if err := b.DeleteOrgNetwork(context.Background(), "acme"); err != nil {
		t.Fatalf("DeleteOrgNetwork: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ExposePort
// ---------------------------------------------------------------------------

func TestExposePort_Success(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)
	seedSession(t, b, "sess-expose", "sb-expose")

	addr, err := b.ExposePort(context.Background(), "sess-expose", 8080, "tcp")
	if err != nil {
		t.Fatalf("ExposePort: %v", err)
	}
	if addr.Port != 8080 {
		t.Errorf("Port = %d, want 8080", addr.Port)
	}
	if addr.Protocol != "tcp" {
		t.Errorf("Protocol = %q, want %q", addr.Protocol, "tcp")
	}
	if addr.Host == "" {
		t.Error("Host should not be empty")
	}
}

func TestExposePort_DefaultProtocol(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)
	seedSession(t, b, "sess-defproto", "sb-defproto")

	addr, err := b.ExposePort(context.Background(), "sess-defproto", 3000, "")
	if err != nil {
		t.Fatalf("ExposePort: %v", err)
	}
	if addr.Protocol != "tcp" {
		t.Errorf("Protocol = %q, want default %q", addr.Protocol, "tcp")
	}
}

func TestExposePort_InvalidPort(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)

	_, err := b.ExposePort(context.Background(), "sess", 0, "tcp")
	if err == nil {
		t.Fatal("expected error for port 0")
	}

	_, err = b.ExposePort(context.Background(), "sess", 70000, "tcp")
	if err == nil {
		t.Fatal("expected error for port > 65535")
	}
}

func TestExposePort_EmptySessionID(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)

	_, err := b.ExposePort(context.Background(), "", 80, "tcp")
	if err == nil {
		t.Fatal("expected error for empty sessionID")
	}
}

func TestExposePort_NilGateway(t *testing.T) {
	b := newTestBackend(t)
	_, err := b.ExposePort(context.Background(), "x", 80, "tcp")
	if !errors.Is(err, ErrNotImplementedYet) {
		t.Errorf("got %v, want ErrNotImplementedYet", err)
	}
}

func TestExposePort_SessionNotFound(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)

	_, err := b.ExposePort(context.Background(), "ghost", 80, "tcp")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

// ---------------------------------------------------------------------------
// UnexposePort
// ---------------------------------------------------------------------------

func TestUnexposePort_Success(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)
	seedSession(t, b, "sess-unexpose", "sb-unexpose")

	// Expose first to have something to unexpose.
	_, _ = b.ExposePort(context.Background(), "sess-unexpose", 8080, "tcp")

	if err := b.UnexposePort(context.Background(), "sess-unexpose", 8080); err != nil {
		t.Fatalf("UnexposePort: %v", err)
	}
}

func TestUnexposePort_SessionGoneIsNoop(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)

	// Should not error for nonexistent session.
	if err := b.UnexposePort(context.Background(), "ghost", 8080); err != nil {
		t.Fatalf("UnexposePort on ghost session: %v", err)
	}
}

func TestUnexposePort_InvalidPort(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)

	if err := b.UnexposePort(context.Background(), "x", 0); err == nil {
		t.Fatal("expected error for port 0")
	}
}

func TestUnexposePort_NilGateway(t *testing.T) {
	b := newTestBackend(t)
	err := b.UnexposePort(context.Background(), "x", 80)
	if !errors.Is(err, ErrNotImplementedYet) {
		t.Errorf("got %v, want ErrNotImplementedYet", err)
	}
}

// ---------------------------------------------------------------------------
// EnsureFleetContainer
// ---------------------------------------------------------------------------

func TestEnsureFleetContainer_Success(t *testing.T) {
	gw := &mockGateway{
		statusFn: func(ctx context.Context, sandboxID string) (*SandboxStatus, error) {
			return &SandboxStatus{State: SandboxStateRunning}, nil
		},
	}
	b := newTestBackendWithGateway(t, gw)

	key := fmt.Sprintf("fleet-success-%d", time.Now().UnixNano())
	spec := sandbox.FleetSpec{
		FleetKey:   key,
		TemplateID: "python-dev",
		OrgSlug:    "acme",
		TeamSlug:   "eng",
	}

	sess, err := b.EnsureFleetContainer(context.Background(), spec)
	if err != nil {
		t.Fatalf("EnsureFleetContainer: %v", err)
	}
	if sess.SessionID != key {
		t.Errorf("SessionID = %q, want %q", sess.SessionID, key)
	}
	if sess.Type != sandbox.SessionTypeFleet {
		t.Errorf("Type = %q, want %q", sess.Type, sandbox.SessionTypeFleet)
	}
	if sess.OrgSlug != "acme" {
		t.Errorf("OrgSlug = %q, want %q", sess.OrgSlug, "acme")
	}
}

func TestEnsureFleetContainer_WithCertBundles(t *testing.T) {
	var captured CreateSandboxRequest
	gw := &mockGateway{
		createFn: func(ctx context.Context, req CreateSandboxRequest) (*CreateSandboxResponse, error) {
			captured = req
			return &CreateSandboxResponse{SandboxID: "sb-fleet-ca", GatewayID: "gw-fleet-ca", PodName: "pod-fleet-ca"}, nil
		},
	}
	sr, err := sandbox.NewSessionRegistry()
	if err != nil {
		t.Fatalf("NewSessionRegistry: %v", err)
	}
	b, err := New(Config{
		Sessions: sr,
		Gateway:  gw,
		AppConfig: config.SandboxOpenShellConfig{
			CertBundles: []config.CertBundleConfig{{
				Name:      "corp-root-ca",
				ClaimName: "astonish-corp-ca",
				MountPath: "/etc/astonish-ca/ca-bundle.crt",
			}},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	key := fmt.Sprintf("fleet-certs-%d", time.Now().UnixNano())
	_, err = b.EnsureFleetContainer(context.Background(), sandbox.FleetSpec{
		FleetKey:   key,
		TemplateID: "@base",
	})
	if err != nil {
		t.Fatalf("EnsureFleetContainer: %v", err)
	}
	if captured.DriverConfig == nil {
		t.Fatal("expected DriverConfig")
	}
	if captured.Env["SSL_CERT_FILE"] != "/etc/astonish-ca/ca-bundle.crt" {
		t.Errorf("SSL_CERT_FILE = %q", captured.Env["SSL_CERT_FILE"])
	}
}

func TestEnsureFleetContainer_Idempotent(t *testing.T) {
	createCount := 0
	gw := &mockGateway{
		createFn: func(ctx context.Context, req CreateSandboxRequest) (*CreateSandboxResponse, error) {
			createCount++
			return &CreateSandboxResponse{SandboxID: "sb-fleet", PodName: "pod-fleet"}, nil
		},
		statusFn: func(ctx context.Context, sandboxID string) (*SandboxStatus, error) {
			return &SandboxStatus{State: SandboxStateRunning}, nil
		},
	}
	b := newTestBackendWithGateway(t, gw)

	key := fmt.Sprintf("fleet-idem-%d", time.Now().UnixNano())
	spec := sandbox.FleetSpec{FleetKey: key, TemplateID: "@base"}

	_, err := b.EnsureFleetContainer(context.Background(), spec)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if createCount != 1 {
		t.Fatalf("first call: CreateSandbox called %d times, want 1", createCount)
	}

	// Second call should return existing without creating.
	_, err = b.EnsureFleetContainer(context.Background(), spec)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if createCount != 1 {
		t.Errorf("gateway CreateSandbox called %d times total, want 1 (idempotent)", createCount)
	}
}

func TestEnsureFleetContainer_ReplacesGoneSandbox(t *testing.T) {
	createCount := 0
	gw := &mockGateway{
		createFn: func(ctx context.Context, req CreateSandboxRequest) (*CreateSandboxResponse, error) {
			createCount++
			return &CreateSandboxResponse{SandboxID: "sb-new", PodName: "pod-new"}, nil
		},
		statusFn: func(ctx context.Context, sandboxID string) (*SandboxStatus, error) {
			// Report the old sandbox as gone.
			if sandboxID == "sb-old" {
				return &SandboxStatus{State: SandboxStateGone}, nil
			}
			return &SandboxStatus{State: SandboxStateRunning}, nil
		},
	}
	b := newTestBackendWithGateway(t, gw)

	// Seed an old fleet session with a dead sandbox.
	rec := &store.SandboxSession{
		SessionID:     "fleet-replace",
		ChatSessionID: "fleet-replace",
		Backend:       "openshell",
		ContainerName: "sb-old",
		State:         store.SandboxSessionStateRunning,
	}
	if err := b.sessions.PutSession(rec); err != nil {
		t.Fatalf("PutSession: %v", err)
	}

	spec := sandbox.FleetSpec{FleetKey: "fleet-replace", TemplateID: "@base"}
	sess, err := b.EnsureFleetContainer(context.Background(), spec)
	if err != nil {
		t.Fatalf("EnsureFleetContainer: %v", err)
	}
	if createCount != 1 {
		t.Errorf("CreateSandbox called %d times, want 1 (replace dead)", createCount)
	}
	if sess.BackendRef != "sb-new" {
		t.Errorf("BackendRef = %q, want %q", sess.BackendRef, "sb-new")
	}
}

func TestEnsureFleetContainer_EmptyFleetKey(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)

	_, err := b.EnsureFleetContainer(context.Background(), sandbox.FleetSpec{})
	if err == nil {
		t.Fatal("expected error for empty FleetKey")
	}
}

func TestEnsureFleetContainer_NilGateway(t *testing.T) {
	b := newTestBackend(t)
	_, err := b.EnsureFleetContainer(context.Background(), sandbox.FleetSpec{FleetKey: "x"})
	if !errors.Is(err, ErrNotImplementedYet) {
		t.Errorf("got %v, want ErrNotImplementedYet", err)
	}
}

func TestEnsureFleetContainer_GatewayCreateFails(t *testing.T) {
	gw := &mockGateway{
		createFn: func(ctx context.Context, req CreateSandboxRequest) (*CreateSandboxResponse, error) {
			return nil, errors.New("resource limit")
		},
		statusFn: func(ctx context.Context, sandboxID string) (*SandboxStatus, error) {
			return &SandboxStatus{State: SandboxStateGone}, nil
		},
	}
	b := newTestBackendWithGateway(t, gw)

	_, err := b.EnsureFleetContainer(context.Background(), sandbox.FleetSpec{
		FleetKey: "fleet-fail",
	})
	if err == nil {
		t.Fatal("expected error from gateway")
	}
}
