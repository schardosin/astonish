package openshell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/sandbox"
	"github.com/SAP/astonish/pkg/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// mockGateway implements GatewayClient for testing session lifecycle.
type mockGateway struct {
	createFn func(ctx context.Context, req CreateSandboxRequest) (*CreateSandboxResponse, error)
	deleteFn func(ctx context.Context, sandboxID string) error
	statusFn func(ctx context.Context, sandboxID string) (*SandboxStatus, error)
	execFn   func(ctx context.Context, sandboxID string, req ExecRequest) (*ExecResponse, error)
	streamFn func(ctx context.Context, sandboxID string, req ExecRequest) (ExecStreamConn, error)
}

func (m *mockGateway) CreateSandbox(ctx context.Context, req CreateSandboxRequest) (*CreateSandboxResponse, error) {
	if m.createFn != nil {
		return m.createFn(ctx, req)
	}
	return &CreateSandboxResponse{SandboxID: "sb-" + req.Name, PodName: "pod-" + req.Name}, nil
}

func (m *mockGateway) DeleteSandbox(ctx context.Context, sandboxID string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, sandboxID)
	}
	return nil
}

func (m *mockGateway) GetSandboxStatus(ctx context.Context, sandboxID string) (*SandboxStatus, error) {
	if m.statusFn != nil {
		return m.statusFn(ctx, sandboxID)
	}
	return &SandboxStatus{State: SandboxStateRunning}, nil
}

func (m *mockGateway) ExecCommand(ctx context.Context, sandboxID string, req ExecRequest) (*ExecResponse, error) {
	if m.execFn != nil {
		return m.execFn(ctx, sandboxID, req)
	}
	return &ExecResponse{ExitCode: 0}, nil
}

func (m *mockGateway) ExecStream(ctx context.Context, sandboxID string, req ExecRequest) (ExecStreamConn, error) {
	if m.streamFn != nil {
		return m.streamFn(ctx, sandboxID, req)
	}
	return nil, errors.New("not implemented in mock")
}

func (m *mockGateway) ListSandboxes(ctx context.Context, labelSelector string) ([]SandboxSummary, error) {
	return nil, nil
}

func (m *mockGateway) GetDraftPolicy(ctx context.Context, sandboxName string, statusFilter string) (*DraftPolicyResponse, error) {
	return &DraftPolicyResponse{}, nil
}

func (m *mockGateway) ApproveDraftChunk(ctx context.Context, sandboxName string, chunkID string) (*ApproveChunkResponse, error) {
	return &ApproveChunkResponse{}, nil
}

func (m *mockGateway) RejectDraftChunk(ctx context.Context, sandboxName string, chunkID string, reason string) error {
	return nil
}

func (m *mockGateway) UpdateConfig(ctx context.Context, sandboxName string, ops []PolicyMergeOp) (*UpdateConfigResponse, error) {
	return &UpdateConfigResponse{}, nil
}

func (m *mockGateway) GetPolicyStatus(ctx context.Context, sandboxName string, version uint32) (*PolicyStatusResponse, error) {
	return &PolicyStatusResponse{ActiveVersion: version, Status: "loaded"}, nil
}

func (m *mockGateway) WatchSandbox(ctx context.Context, sandboxName string, opts WatchOpts) (SandboxEventStream, error) {
	return nil, errors.New("not implemented in mock")
}

func (m *mockGateway) Close() error {
	return nil
}

func (m *mockGateway) CreateProvider(_ context.Context, _, _ string, _ map[string]string) error {
	return nil
}

func (m *mockGateway) DeleteProvider(_ context.Context, _ string) error {
	return nil
}

func (m *mockGateway) AttachSandboxProvider(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockGateway) DetachSandboxProvider(_ context.Context, _, _ string) error {
	return nil
}

// newTestBackendWithGateway creates an OpenShellBackend with a mock gateway.
func newTestBackendWithGateway(t *testing.T, gw GatewayClient) *OpenShellBackend {
	t.Helper()
	sr, err := sandbox.NewSessionRegistry()
	if err != nil {
		t.Fatalf("NewSessionRegistry: %v", err)
	}
	b, err := New(Config{Sessions: sr, Gateway: gw})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return b
}

func TestCreateSession_Success(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)

	spec := sandbox.SessionSpec{
		SessionID:  "sess-001",
		Type:       sandbox.SessionTypeChat,
		TemplateID: "python-dev",
		OrgSlug:    "acme",
		TeamSlug:   "eng",
	}

	sess, err := b.CreateSession(context.Background(), spec)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.SessionID != "sess-001" {
		t.Errorf("SessionID = %q, want %q", sess.SessionID, "sess-001")
	}
	if sess.TemplateID != "python-dev" {
		t.Errorf("TemplateID = %q, want %q", sess.TemplateID, "python-dev")
	}
	if sess.State != sandbox.SessionStateCreating {
		t.Errorf("State = %q, want %q", sess.State, sandbox.SessionStateCreating)
	}
	// BackendRef stores the sandbox ID (via ContainerName).
	if sess.BackendRef == "" {
		t.Error("BackendRef should not be empty")
	}
}

func TestCreateSession_WithCertBundles(t *testing.T) {
	var captured CreateSandboxRequest
	gw := &mockGateway{
		createFn: func(ctx context.Context, req CreateSandboxRequest) (*CreateSandboxResponse, error) {
			captured = req
			return &CreateSandboxResponse{SandboxID: "sb-certs", GatewayID: "gw-certs", PodName: "pod-certs"}, nil
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
				SubPath:   "ca-bundle.crt",
			}},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	id := fmt.Sprintf("sess-certs-%d", time.Now().UnixNano())
	_, err = b.CreateSession(context.Background(), sandbox.SessionSpec{
		SessionID:  id,
		TemplateID: "@base",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if captured.DriverConfig == nil {
		t.Fatal("expected DriverConfig on CreateSandboxRequest")
	}
	k8s, ok := captured.DriverConfig["kubernetes"].(map[string]any)
	if !ok {
		t.Fatalf("kubernetes block missing: %#v", captured.DriverConfig)
	}
	if _, ok := k8s["volumes"].([]any); !ok {
		t.Errorf("volumes missing: %#v", k8s)
	}
	if captured.Env["SSL_CERT_FILE"] != "/etc/astonish-ca/ca-bundle.crt" {
		t.Errorf("SSL_CERT_FILE = %q", captured.Env["SSL_CERT_FILE"])
	}
	foundRO := false
	for _, p := range captured.Policy.Filesystem.ReadOnly {
		if p == "/etc/astonish-ca/ca-bundle.crt" {
			foundRO = true
			break
		}
	}
	if !foundRO {
		t.Error("expected cert mount path in policy ReadOnly")
	}
}

func TestCreateSession_Idempotent(t *testing.T) {
	callCount := 0
	gw := &mockGateway{
		createFn: func(ctx context.Context, req CreateSandboxRequest) (*CreateSandboxResponse, error) {
			callCount++
			return &CreateSandboxResponse{SandboxID: "sb-1", PodName: "pod-1"}, nil
		},
	}
	b := newTestBackendWithGateway(t, gw)

	// Use a unique ID to avoid collision with persisted state from prior runs.
	id := fmt.Sprintf("sess-idem-%d", time.Now().UnixNano())
	spec := sandbox.SessionSpec{SessionID: id, TemplateID: "@base"}

	// First call creates.
	sess1, err := b.CreateSession(context.Background(), spec)
	if err != nil {
		t.Fatalf("first CreateSession: %v", err)
	}
	if callCount != 1 {
		t.Fatalf("first call: gateway CreateSandbox called %d times, want 1", callCount)
	}

	// Second call returns cached without hitting gateway again.
	sess2, err := b.CreateSession(context.Background(), spec)
	if err != nil {
		t.Fatalf("second CreateSession: %v", err)
	}
	if sess1.SessionID != sess2.SessionID {
		t.Error("idempotent call should return same session")
	}
	if callCount != 1 {
		t.Errorf("gateway CreateSandbox called %d times total, want 1 (idempotent)", callCount)
	}
}

func TestCreateSession_HealsStaleRegistryWhenGatewayGone(t *testing.T) {
	createCount := 0
	id := fmt.Sprintf("sess-stale-%d", time.Now().UnixNano())
	name := sandboxName(id)

	gw := &mockGateway{
		statusFn: func(ctx context.Context, sandboxID string) (*SandboxStatus, error) {
			return nil, status.Error(codes.NotFound, "sandbox not found")
		},
		createFn: func(ctx context.Context, req CreateSandboxRequest) (*CreateSandboxResponse, error) {
			createCount++
			return &CreateSandboxResponse{
				SandboxID: req.Name,
				GatewayID: "gw-" + req.Name,
				PodName:   "pod-" + req.Name,
			}, nil
		},
	}
	b := newTestBackendWithGateway(t, gw)

	// Stale registry row: DB thinks the sandbox exists.
	if err := b.sessions.PutSession(&store.SandboxSession{
		SessionID:     id,
		ChatSessionID: id,
		Backend:       "openshell",
		ContainerName: name,
		TemplateID:    "@base",
		State:         store.SandboxSessionStateRunning,
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("PutSession: %v", err)
	}

	sess, err := b.CreateSession(context.Background(), sandbox.SessionSpec{SessionID: id, TemplateID: "@base"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if createCount != 1 {
		t.Fatalf("CreateSandbox called %d times, want 1 (heal recreate)", createCount)
	}
	if sess.SessionID != id {
		t.Fatalf("SessionID = %q, want %q", sess.SessionID, id)
	}
	rec, err := b.sessions.GetSession(id)
	if err != nil || rec == nil {
		t.Fatalf("expected registry row after heal, err=%v rec=%v", err, rec)
	}
}

func TestCreateSession_HealsStaleRegistryWhenStateGone(t *testing.T) {
	createCount := 0
	id := fmt.Sprintf("sess-gone-%d", time.Now().UnixNano())
	name := sandboxName(id)

	gw := &mockGateway{
		statusFn: func(ctx context.Context, sandboxID string) (*SandboxStatus, error) {
			return &SandboxStatus{State: SandboxStateGone}, nil
		},
		createFn: func(ctx context.Context, req CreateSandboxRequest) (*CreateSandboxResponse, error) {
			createCount++
			return &CreateSandboxResponse{SandboxID: req.Name, GatewayID: "gw-new"}, nil
		},
	}
	b := newTestBackendWithGateway(t, gw)

	if err := b.sessions.PutSession(&store.SandboxSession{
		SessionID:     id,
		ChatSessionID: id,
		Backend:       "openshell",
		ContainerName: name,
		TemplateID:    "@base",
		State:         store.SandboxSessionStateEvicted,
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("PutSession: %v", err)
	}

	if _, err := b.CreateSession(context.Background(), sandbox.SessionSpec{SessionID: id, TemplateID: "@base"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if createCount != 1 {
		t.Fatalf("CreateSandbox called %d times, want 1", createCount)
	}
}

func TestCreateSession_IdempotentWhenGatewayStillRunning(t *testing.T) {
	createCount := 0
	id := fmt.Sprintf("sess-alive-%d", time.Now().UnixNano())
	name := sandboxName(id)

	gw := &mockGateway{
		statusFn: func(ctx context.Context, sandboxID string) (*SandboxStatus, error) {
			return &SandboxStatus{State: SandboxStateRunning, GatewayID: "gw-1"}, nil
		},
		createFn: func(ctx context.Context, req CreateSandboxRequest) (*CreateSandboxResponse, error) {
			createCount++
			return &CreateSandboxResponse{SandboxID: req.Name}, nil
		},
	}
	b := newTestBackendWithGateway(t, gw)

	if err := b.sessions.PutSession(&store.SandboxSession{
		SessionID:     id,
		ChatSessionID: id,
		Backend:       "openshell",
		ContainerName: name,
		TemplateID:    "@base",
		State:         store.SandboxSessionStateRunning,
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("PutSession: %v", err)
	}

	sess, err := b.CreateSession(context.Background(), sandbox.SessionSpec{SessionID: id, TemplateID: "@base"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if createCount != 0 {
		t.Fatalf("CreateSandbox called %d times, want 0 (still running)", createCount)
	}
	if sess.SessionID != id {
		t.Fatalf("SessionID = %q, want %q", sess.SessionID, id)
	}
}

func TestCreateSession_GatewayError(t *testing.T) {
	gw := &mockGateway{
		createFn: func(ctx context.Context, req CreateSandboxRequest) (*CreateSandboxResponse, error) {
			return nil, errors.New("gateway unavailable")
		},
	}
	b := newTestBackendWithGateway(t, gw)

	_, err := b.CreateSession(context.Background(), sandbox.SessionSpec{SessionID: "fail"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, errors.New("")) {
		// Just check the error message wrapping.
		if got := err.Error(); got == "" {
			t.Error("error should have content")
		}
	}
}

func TestCreateSession_NilGateway(t *testing.T) {
	b := newTestBackend(t) // no gateway
	_, err := b.CreateSession(context.Background(), sandbox.SessionSpec{SessionID: "x"})
	if !errors.Is(err, ErrNotImplementedYet) {
		t.Errorf("got %v, want ErrNotImplementedYet", err)
	}
}

func TestStartSession_Success(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)

	// Pre-seed a stopped session.
	rec := &store.SandboxSession{
		SessionID:     "sess-start",
		ChatSessionID: "sess-start",
		Backend:       "openshell",
		TemplateID:    "node-dev",
		State:         store.SandboxSessionStateEvicted,
	}
	if err := b.sessions.PutSession(rec); err != nil {
		t.Fatalf("PutSession: %v", err)
	}

	if err := b.StartSession(context.Background(), "sess-start"); err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	// Verify state updated.
	updated, _ := b.sessions.GetSession("sess-start")
	if updated.State != store.SandboxSessionStateCreating {
		t.Errorf("state = %q, want %q", updated.State, store.SandboxSessionStateCreating)
	}
	if updated.ContainerName == "" {
		t.Error("ContainerName should be set after StartSession")
	}
}

func TestStartSession_AlreadyRunning(t *testing.T) {
	gw := &mockGateway{
		createFn: func(ctx context.Context, req CreateSandboxRequest) (*CreateSandboxResponse, error) {
			t.Error("CreateSandbox should not be called for running session")
			return nil, nil
		},
	}
	b := newTestBackendWithGateway(t, gw)

	rec := &store.SandboxSession{
		SessionID:     "sess-running",
		ChatSessionID: "sess-running",
		Backend:       "openshell",
		ContainerName: "sb-existing",
		State:         store.SandboxSessionStateRunning,
	}
	if err := b.sessions.PutSession(rec); err != nil {
		t.Fatalf("PutSession: %v", err)
	}

	if err := b.StartSession(context.Background(), "sess-running"); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
}

func TestStartSession_NotFound(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)

	err := b.StartSession(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestStopSession_Success(t *testing.T) {
	deleteCalled := false
	gw := &mockGateway{
		execFn: func(ctx context.Context, sandboxID string, req ExecRequest) (*ExecResponse, error) {
			return &ExecResponse{ExitCode: 0}, nil
		},
		deleteFn: func(ctx context.Context, sandboxID string) error {
			deleteCalled = true
			if sandboxID != "sb-stop" {
				t.Errorf("DeleteSandbox called with %q, want %q", sandboxID, "sb-stop")
			}
			return nil
		},
	}
	b := newTestBackendWithGateway(t, gw)

	rec := &store.SandboxSession{
		SessionID:     "sess-stop",
		ChatSessionID: "sess-stop",
		Backend:       "openshell",
		ContainerName: "sb-stop",
		PodName:       "pod-stop",
		State:         store.SandboxSessionStateRunning,
	}
	if err := b.sessions.PutSession(rec); err != nil {
		t.Fatalf("PutSession: %v", err)
	}

	if err := b.StopSession(context.Background(), "sess-stop"); err != nil {
		t.Fatalf("StopSession: %v", err)
	}

	if !deleteCalled {
		t.Error("DeleteSandbox should have been called")
	}

	// Verify state.
	updated, _ := b.sessions.GetSession("sess-stop")
	if updated.State != store.SandboxSessionStateEvicted {
		t.Errorf("state = %q, want %q", updated.State, store.SandboxSessionStateEvicted)
	}
	if updated.ContainerName != "" {
		t.Errorf("ContainerName should be empty after stop, got %q", updated.ContainerName)
	}
}

func TestStopSession_NonexistentIsNoop(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)

	// No error for a session that doesn't exist.
	if err := b.StopSession(context.Background(), "ghost"); err != nil {
		t.Fatalf("StopSession on nonexistent: %v", err)
	}
}

func TestDestroySession_Success(t *testing.T) {
	deleteCalled := false
	gw := &mockGateway{
		deleteFn: func(ctx context.Context, sandboxID string) error {
			deleteCalled = true
			return nil
		},
	}
	b := newTestBackendWithGateway(t, gw)

	rec := &store.SandboxSession{
		SessionID:     "sess-destroy",
		ChatSessionID: "sess-destroy",
		Backend:       "openshell",
		ContainerName: "sb-destroy",
		State:         store.SandboxSessionStateRunning,
	}
	if err := b.sessions.PutSession(rec); err != nil {
		t.Fatalf("PutSession: %v", err)
	}

	if err := b.DestroySession(context.Background(), "sess-destroy"); err != nil {
		t.Fatalf("DestroySession: %v", err)
	}

	if !deleteCalled {
		t.Error("DeleteSandbox should have been called")
	}

	// Verify removed from registry.
	got, _ := b.sessions.GetSession("sess-destroy")
	if got != nil {
		t.Error("session should be removed from registry after destroy")
	}
}

func TestDestroySession_NoRecord_DeletesByDerivedName(t *testing.T) {
	var deletedName string
	gw := &mockGateway{
		deleteFn: func(ctx context.Context, sandboxID string) error {
			deletedName = sandboxID
			return nil
		},
	}
	b := newTestBackendWithGateway(t, gw)

	// No session record exists — should still attempt to delete the pod
	// using the derived sandbox name.
	if err := b.DestroySession(context.Background(), "never-existed"); err != nil {
		t.Fatalf("DestroySession on missing: %v", err)
	}

	want := sandboxName("never-existed")
	if deletedName != want {
		t.Errorf("DeleteSandbox called with %q, want derived name %q", deletedName, want)
	}
}

func TestDestroySession_NoRecord_GatewayNotFound(t *testing.T) {
	gw := &mockGateway{
		deleteFn: func(ctx context.Context, sandboxID string) error {
			return status.Error(codes.NotFound, "sandbox not found")
		},
	}
	b := newTestBackendWithGateway(t, gw)

	// No record and pod already gone — should be idempotent (no error).
	if err := b.DestroySession(context.Background(), "phantom"); err != nil {
		t.Fatalf("DestroySession: %v", err)
	}
}

func TestDestroySession_NoRecord_GatewayTransientError(t *testing.T) {
	gw := &mockGateway{
		deleteFn: func(ctx context.Context, sandboxID string) error {
			return status.Error(codes.Unavailable, "connection refused")
		},
	}
	b := newTestBackendWithGateway(t, gw)

	// No record and transient gateway error — best effort, no error returned
	// (there's nothing to preserve for the reconciler).
	if err := b.DestroySession(context.Background(), "orphan-pod"); err != nil {
		t.Fatalf("DestroySession: expected nil for best-effort, got %v", err)
	}
}

func TestDestroySession_GatewayNotFound_RemovesRecord(t *testing.T) {
	gw := &mockGateway{
		deleteFn: func(ctx context.Context, sandboxID string) error {
			return status.Error(codes.NotFound, "sandbox not found")
		},
	}
	b := newTestBackendWithGateway(t, gw)

	rec := &store.SandboxSession{
		SessionID:     "sess-gone",
		ChatSessionID: "sess-gone",
		Backend:       "openshell",
		ContainerName: "sb-gone",
		State:         store.SandboxSessionStateRunning,
	}
	if err := b.sessions.PutSession(rec); err != nil {
		t.Fatalf("PutSession: %v", err)
	}

	// NotFound means sandbox is already gone — record should be removed.
	if err := b.DestroySession(context.Background(), "sess-gone"); err != nil {
		t.Fatalf("DestroySession: %v", err)
	}
	got, _ := b.sessions.GetSession("sess-gone")
	if got != nil {
		t.Error("session should be removed when gateway returns NotFound")
	}
}

func TestDestroySession_GatewayTransientError_KeepsRecord(t *testing.T) {
	gw := &mockGateway{
		deleteFn: func(ctx context.Context, sandboxID string) error {
			return status.Error(codes.Unavailable, "connection refused")
		},
	}
	b := newTestBackendWithGateway(t, gw)

	rec := &store.SandboxSession{
		SessionID:     "sess-retry",
		ChatSessionID: "sess-retry",
		Backend:       "openshell",
		ContainerName: "sb-retry",
		State:         store.SandboxSessionStateRunning,
	}
	if err := b.sessions.PutSession(rec); err != nil {
		t.Fatalf("PutSession: %v", err)
	}

	// Transient error — record should be kept for reconciler retry.
	err := b.DestroySession(context.Background(), "sess-retry")
	if err == nil {
		t.Fatal("expected error for transient gateway failure")
	}
	got, _ := b.sessions.GetSession("sess-retry")
	if got == nil {
		t.Error("session record should be preserved on transient error")
	}
}

func TestDestroySession_EmptyContainerName_UsesDerivedName(t *testing.T) {
	var deletedName string
	gw := &mockGateway{
		deleteFn: func(ctx context.Context, sandboxID string) error {
			deletedName = sandboxID
			return nil
		},
	}
	b := newTestBackendWithGateway(t, gw)

	// Session record exists but ContainerName is empty (e.g. evicted session).
	rec := &store.SandboxSession{
		SessionID:     "sess-evicted",
		ChatSessionID: "sess-evicted",
		Backend:       "openshell",
		ContainerName: "",
		State:         store.SandboxSessionStateEvicted,
	}
	if err := b.sessions.PutSession(rec); err != nil {
		t.Fatalf("PutSession: %v", err)
	}

	if err := b.DestroySession(context.Background(), "sess-evicted"); err != nil {
		t.Fatalf("DestroySession: %v", err)
	}

	want := sandboxName("sess-evicted")
	if deletedName != want {
		t.Errorf("DeleteSandbox called with %q, want derived name %q", deletedName, want)
	}

	// Record should be removed.
	got2, _ := b.sessions.GetSession("sess-evicted")
	if got2 != nil {
		t.Error("session should be removed after successful destroy")
	}
}

func TestSessionState_Running(t *testing.T) {
	gw := &mockGateway{
		statusFn: func(ctx context.Context, sandboxID string) (*SandboxStatus, error) {
			return &SandboxStatus{State: SandboxStateRunning}, nil
		},
	}
	b := newTestBackendWithGateway(t, gw)

	rec := &store.SandboxSession{
		SessionID:     "sess-state",
		ChatSessionID: "sess-state",
		Backend:       "openshell",
		ContainerName: "sb-state",
		State:         store.SandboxSessionStateRunning,
	}
	if err := b.sessions.PutSession(rec); err != nil {
		t.Fatalf("PutSession: %v", err)
	}

	state, err := b.SessionState(context.Background(), "sess-state")
	if err != nil {
		t.Fatalf("SessionState: %v", err)
	}
	if state != sandbox.SessionStateRunning {
		t.Errorf("state = %q, want %q", state, sandbox.SessionStateRunning)
	}
}

func TestSessionState_NoSandboxID_FallsBackToStore(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)

	rec := &store.SandboxSession{
		SessionID:     "sess-nosb",
		ChatSessionID: "sess-nosb",
		Backend:       "openshell",
		ContainerName: "", // no sandbox
		State:         store.SandboxSessionStateEvicted,
	}
	if err := b.sessions.PutSession(rec); err != nil {
		t.Fatalf("PutSession: %v", err)
	}

	state, err := b.SessionState(context.Background(), "sess-nosb")
	if err != nil {
		t.Fatalf("SessionState: %v", err)
	}
	if state != sandbox.SessionStateStopped {
		t.Errorf("state = %q, want %q (evicted maps to stopped)", state, sandbox.SessionStateStopped)
	}
}

func TestSessionState_Gone(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)

	state, err := b.SessionState(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("SessionState: %v", err)
	}
	if state != sandbox.SessionStateGone {
		t.Errorf("state = %q, want %q", state, sandbox.SessionStateGone)
	}
}

func TestWaitForSessionReady_ImmediatelyReady(t *testing.T) {
	gw := &mockGateway{
		statusFn: func(ctx context.Context, sandboxID string) (*SandboxStatus, error) {
			return &SandboxStatus{State: SandboxStateRunning}, nil
		},
	}
	b := newTestBackendWithGateway(t, gw)

	rec := &store.SandboxSession{
		SessionID:     "sess-wait",
		ChatSessionID: "sess-wait",
		Backend:       "openshell",
		ContainerName: "sb-wait",
		State:         store.SandboxSessionStateCreating,
	}
	if err := b.sessions.PutSession(rec); err != nil {
		t.Fatalf("PutSession: %v", err)
	}

	if err := b.WaitForSessionReady(context.Background(), "sess-wait"); err != nil {
		t.Fatalf("WaitForSessionReady: %v", err)
	}

	// Verify state updated to running.
	updated, _ := b.sessions.GetSession("sess-wait")
	if updated.State != store.SandboxSessionStateRunning {
		t.Errorf("state = %q, want %q", updated.State, store.SandboxSessionStateRunning)
	}
}

func TestWaitForSessionReady_Failed(t *testing.T) {
	gw := &mockGateway{
		statusFn: func(ctx context.Context, sandboxID string) (*SandboxStatus, error) {
			return &SandboxStatus{State: SandboxStateFailed, Message: "OOM"}, nil
		},
	}
	b := newTestBackendWithGateway(t, gw)

	rec := &store.SandboxSession{
		SessionID:     "sess-fail",
		ChatSessionID: "sess-fail",
		Backend:       "openshell",
		ContainerName: "sb-fail",
		State:         store.SandboxSessionStateCreating,
	}
	if err := b.sessions.PutSession(rec); err != nil {
		t.Fatalf("PutSession: %v", err)
	}

	err := b.WaitForSessionReady(context.Background(), "sess-fail")
	if err == nil {
		t.Fatal("expected error for failed sandbox")
	}
}

func TestWaitForSessionReady_ContextCancelled(t *testing.T) {
	gw := &mockGateway{
		statusFn: func(ctx context.Context, sandboxID string) (*SandboxStatus, error) {
			return &SandboxStatus{State: SandboxStateCreating}, nil
		},
	}
	b := newTestBackendWithGateway(t, gw)

	rec := &store.SandboxSession{
		SessionID:     "sess-ctx",
		ChatSessionID: "sess-ctx",
		Backend:       "openshell",
		ContainerName: "sb-ctx",
		State:         store.SandboxSessionStateCreating,
	}
	if err := b.sessions.PutSession(rec); err != nil {
		t.Fatalf("PutSession: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Already cancelled.

	err := b.WaitForSessionReady(ctx, "sess-ctx")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestListSessions(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)

	// Seed some sessions with a unique prefix so we can identify them.
	ids := []string{"ls-unique-1", "ls-unique-2", "ls-unique-3"}
	for _, id := range ids {
		rec := &store.SandboxSession{
			SessionID:     id,
			ChatSessionID: id,
			Backend:       "openshell",
			ContainerName: "sb-" + id,
			State:         store.SandboxSessionStateRunning,
		}
		if err := b.sessions.PutSession(rec); err != nil {
			t.Fatalf("PutSession(%s): %v", id, err)
		}
	}

	sessions, err := b.ListSessions(context.Background(), sandbox.SessionFilter{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	// The registry is shared (file-backed), so we just check our sessions are present.
	found := 0
	for _, s := range sessions {
		for _, id := range ids {
			if s.SessionID == id {
				found++
				break
			}
		}
	}
	if found != 3 {
		t.Errorf("found %d of our 3 sessions in results (total %d)", found, len(sessions))
	}
}

func TestSandboxName(t *testing.T) {
	tests := []struct {
		sessionID string
		want      string
	}{
		{"abc12345-full-uuid", "astn-sess-abc12345-full-uuid"},
		{"short", "astn-sess-short"},
		{"12345678", "astn-sess-12345678"},
		// Synthetic app-mcp session IDs must not produce trailing hyphens.
		{"app-mcp-bf345b3a-ebd9-4947-96ff-c1d6f7450923", "astn-sess-app-mcp-bf345b3a-ebd9-4947"},
		// Uppercase is lowercased.
		{"ABC-DEF-123", "astn-sess-abc-def-123"},
		// Leading hyphens are stripped.
		{"---leading", "astn-sess-leading"},
		// Trailing hyphens after truncation are trimmed.
		{"aaaaaaaaaaaaaaaaaaaaaaaaaaa-", "astn-sess-aaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		// Colons (invalid in DNS labels) are replaced with hyphens.
		{"scheduler:adaptive:15306607-314c-4631-afc9-7315aea8e389", "astn-sess-scheduler-adaptive-15306607"},
	}
	for _, tt := range tests {
		t.Run(tt.sessionID, func(t *testing.T) {
			got := sandboxName(tt.sessionID)
			if got != tt.want {
				t.Errorf("sandboxName(%q) = %q, want %q", tt.sessionID, got, tt.want)
			}
		})
	}
}

// Suppress unused import for io (used by ExecStreamConn interface in mock).
var _ io.Reader

func TestSanitizeLabelValue(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"@base", "base"},
		{"my-template", "my-template"},
		{"my_template.v2", "my_template.v2"},
		{"@base-config-123", "base-config-123"},
		{"hello world!", "helloworld"},
		{"valid-123_name.ok", "valid-123_name.ok"},
		{"", ""},
		// Colons are stripped (scheduler session keys).
		{"scheduler:adaptive:abc-123", "scheduleradaptiveabc-123"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeLabelValue(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeLabelValue(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
