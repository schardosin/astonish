// Package mock — unit tests for MockBackend (Phase B.4).
//
// These tests exercise mock-specific behavior: call recording, idempotency,
// seed helpers, fault injection, and the init()-registered factory hook.
// The shared Backend-interface contract is validated separately by
// TestMockBackendContract below, which delegates to sandbox.RunBackendContract.

package mock_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/schardosin/astonish/pkg/sandbox"
	"github.com/schardosin/astonish/pkg/sandbox/mock"
)

// -----------------------------------------------------------------------------
// Contract: MockBackend must satisfy the full Backend interface suite.
// -----------------------------------------------------------------------------

func TestMockBackendContract(t *testing.T) {
	sandbox.RunBackendContract(t, func(t *testing.T) (sandbox.Backend, string) {
		return mock.New(), ""
	})
}

// -----------------------------------------------------------------------------
// Diagnostics
// -----------------------------------------------------------------------------

func TestMockBackend_KindAndCapabilities(t *testing.T) {
	m := mock.New()
	if got := m.Kind(); got != sandbox.BackendKindMock {
		t.Errorf("Kind() = %q, want %q", got, sandbox.BackendKindMock)
	}
	caps := m.Capabilities()
	if caps.Kind != sandbox.BackendKindMock {
		t.Errorf("Capabilities().Kind = %q, want %q", caps.Kind, sandbox.BackendKindMock)
	}
	if !caps.SupportsPortExpose || !caps.SupportsFastClone || !caps.SupportsOrgIsolation || !caps.SupportsLiveEvict {
		t.Errorf("default capabilities unexpectedly missing flags: %+v", caps)
	}
}

func TestMockBackend_HealthDefaultHealthy(t *testing.T) {
	m := mock.New()
	h, err := m.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() err = %v", err)
	}
	if !h.Healthy {
		t.Errorf("Health() Healthy = false, want true")
	}
	if h.Details["backend"] != "mock" {
		t.Errorf("Health() Details[backend] = %q, want %q", h.Details["backend"], "mock")
	}
}

func TestMockBackend_HealthFnOverride(t *testing.T) {
	m := mock.New()
	want := errors.New("daemon exploded")
	m.HealthFn = func(_ context.Context) (*sandbox.BackendHealth, error) {
		return nil, want
	}
	_, err := m.Health(context.Background())
	if !errors.Is(err, want) {
		t.Errorf("Health() err = %v, want %v", err, want)
	}
}

// -----------------------------------------------------------------------------
// Session lifecycle
// -----------------------------------------------------------------------------

func TestMockBackend_CreateSession_RecordsAndReturnsClone(t *testing.T) {
	m := mock.New()
	spec := sandbox.SessionSpec{
		SessionID:  "sess-1",
		TemplateID: "tmpl-a",
		OrgSlug:    "acme",
		TeamSlug:   "platform",
		Labels:     map[string]string{"chat": "c1"},
	}
	sess, err := m.CreateSession(context.Background(), spec)
	if err != nil {
		t.Fatalf("CreateSession() err = %v", err)
	}
	if sess.SessionID != "sess-1" || sess.State != sandbox.SessionStateRunning {
		t.Errorf("session unexpected: %+v", sess)
	}
	if got := m.CreateSessionCalls(); len(got) != 1 || got[0].Spec.SessionID != "sess-1" {
		t.Errorf("CreateSessionCalls = %+v", got)
	}

	// Returned value is a clone — mutating it must not leak back.
	sess.State = sandbox.SessionStateStopped
	state, err := m.SessionState(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("SessionState() err = %v", err)
	}
	if state != sandbox.SessionStateRunning {
		t.Errorf("SessionState() = %q, want Running (mutation leaked)", state)
	}
}

func TestMockBackend_CreateSession_Idempotent(t *testing.T) {
	m := mock.New()
	spec := sandbox.SessionSpec{SessionID: "s", TemplateID: "t"}
	_, err := m.CreateSession(context.Background(), spec)
	if err != nil {
		t.Fatalf("first CreateSession err = %v", err)
	}
	_, err = m.CreateSession(context.Background(), spec)
	if err != nil {
		t.Fatalf("second CreateSession err = %v", err)
	}
	// Two call records, one session.
	if got := len(m.CreateSessionCalls()); got != 2 {
		t.Errorf("CreateSessionCalls len = %d, want 2", got)
	}
	list, err := m.ListSessions(context.Background(), sandbox.SessionFilter{})
	if err != nil {
		t.Fatalf("ListSessions err = %v", err)
	}
	if len(list) != 1 {
		t.Errorf("ListSessions len = %d, want 1", len(list))
	}
}

func TestMockBackend_CreateSession_RequiresFields(t *testing.T) {
	m := mock.New()
	if _, err := m.CreateSession(context.Background(), sandbox.SessionSpec{TemplateID: "t"}); err == nil {
		t.Error("CreateSession with empty SessionID: err = nil, want error")
	}
	if _, err := m.CreateSession(context.Background(), sandbox.SessionSpec{SessionID: "s"}); err == nil {
		t.Error("CreateSession with empty TemplateID: err = nil, want error")
	}
}

func TestMockBackend_StartStopDestroy_Lifecycle(t *testing.T) {
	m := mock.New()
	ctx := context.Background()
	if _, err := m.CreateSession(ctx, sandbox.SessionSpec{SessionID: "s", TemplateID: "t"}); err != nil {
		t.Fatalf("CreateSession err = %v", err)
	}
	if err := m.StopSession(ctx, "s"); err != nil {
		t.Fatalf("StopSession err = %v", err)
	}
	state, _ := m.SessionState(ctx, "s")
	if state != sandbox.SessionStateStopped {
		t.Errorf("SessionState after stop = %q, want Stopped", state)
	}
	if err := m.StartSession(ctx, "s"); err != nil {
		t.Fatalf("StartSession err = %v", err)
	}
	state, _ = m.SessionState(ctx, "s")
	if state != sandbox.SessionStateRunning {
		t.Errorf("SessionState after restart = %q, want Running", state)
	}
	if err := m.DestroySession(ctx, "s"); err != nil {
		t.Fatalf("DestroySession err = %v", err)
	}
	state, _ = m.SessionState(ctx, "s")
	if state != sandbox.SessionStateGone {
		t.Errorf("SessionState after destroy = %q, want Gone", state)
	}
	// Destroy is idempotent.
	if err := m.DestroySession(ctx, "s"); err != nil {
		t.Errorf("second DestroySession err = %v, want nil", err)
	}
	// Stop on unknown session is idempotent.
	if err := m.StopSession(ctx, "nonexistent"); err != nil {
		t.Errorf("StopSession on unknown: err = %v, want nil", err)
	}
	// Start on unknown session must fail (can't transition what doesn't exist).
	if err := m.StartSession(ctx, "nonexistent"); err == nil {
		t.Error("StartSession on unknown: err = nil, want error")
	}
}

func TestMockBackend_ListSessions_Filters(t *testing.T) {
	m := mock.New()
	ctx := context.Background()
	_, _ = m.CreateSession(ctx, sandbox.SessionSpec{SessionID: "s1", TemplateID: "t", OrgSlug: "acme", TeamSlug: "a"})
	_, _ = m.CreateSession(ctx, sandbox.SessionSpec{SessionID: "s2", TemplateID: "t", OrgSlug: "acme", TeamSlug: "b"})
	_, _ = m.CreateSession(ctx, sandbox.SessionSpec{SessionID: "s3", TemplateID: "t", OrgSlug: "umbrella", TeamSlug: "a"})

	cases := []struct {
		name   string
		filter sandbox.SessionFilter
		want   int
	}{
		{"all", sandbox.SessionFilter{}, 3},
		{"by-org", sandbox.SessionFilter{OrgSlug: "acme"}, 2},
		{"by-team", sandbox.SessionFilter{TeamSlug: "a"}, 2},
		{"by-org-and-team", sandbox.SessionFilter{OrgSlug: "acme", TeamSlug: "a"}, 1},
		{"no-match", sandbox.SessionFilter{OrgSlug: "nope"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			list, err := m.ListSessions(ctx, tc.filter)
			if err != nil {
				t.Fatalf("ListSessions err = %v", err)
			}
			if len(list) != tc.want {
				t.Errorf("len = %d, want %d", len(list), tc.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Exec and file I/O
// -----------------------------------------------------------------------------

func TestMockBackend_Exec_DefaultZeroExit(t *testing.T) {
	m := mock.New()
	ctx := context.Background()
	_, _ = m.CreateSession(ctx, sandbox.SessionSpec{SessionID: "s", TemplateID: "t"})

	res, err := m.Exec(ctx, "s", sandbox.ExecSpec{Command: []string{"true"}})
	if err != nil {
		t.Fatalf("Exec err = %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("default Exec ExitCode = %d, want 0", res.ExitCode)
	}
	if got := m.ExecCalls(); len(got) != 1 {
		t.Errorf("ExecCalls len = %d, want 1", len(got))
	}
}

func TestMockBackend_Exec_ResultFnOverride(t *testing.T) {
	m := mock.New()
	ctx := context.Background()
	_, _ = m.CreateSession(ctx, sandbox.SessionSpec{SessionID: "s", TemplateID: "t"})

	m.ExecResultFn = func(sid string, spec sandbox.ExecSpec) (*sandbox.ExecResult, error) {
		return &sandbox.ExecResult{ExitCode: 42, Stdout: []byte("hi from " + sid)}, nil
	}
	res, err := m.Exec(ctx, "s", sandbox.ExecSpec{Command: []string{"whatever"}})
	if err != nil {
		t.Fatalf("Exec err = %v", err)
	}
	if res.ExitCode != 42 {
		t.Errorf("ExitCode = %d, want 42", res.ExitCode)
	}
	if string(res.Stdout) != "hi from s" {
		t.Errorf("Stdout = %q", res.Stdout)
	}
}

func TestMockBackend_Exec_RequiresSession(t *testing.T) {
	m := mock.New()
	if _, err := m.Exec(context.Background(), "ghost", sandbox.ExecSpec{Command: []string{"true"}}); err == nil {
		t.Error("Exec on unknown session: err = nil, want error")
	}
}

func TestMockBackend_PushPullFile_RoundTrip(t *testing.T) {
	m := mock.New()
	ctx := context.Background()
	_, _ = m.CreateSession(ctx, sandbox.SessionSpec{SessionID: "s", TemplateID: "t"})

	payload := []byte("hello, file")
	if err := m.PushFile(ctx, "s", "/tmp/x", bytes.NewReader(payload), os.FileMode(0o644)); err != nil {
		t.Fatalf("PushFile err = %v", err)
	}
	rc, err := m.PullFile(ctx, "s", "/tmp/x")
	if err != nil {
		t.Fatalf("PullFile err = %v", err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("ReadAll err = %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("pulled = %q, want %q", got, payload)
	}
	if calls := m.PushFileCalls(); len(calls) != 1 || calls[0].Path != "/tmp/x" || calls[0].Mode != os.FileMode(0o644) {
		t.Errorf("PushFileCalls = %+v", calls)
	}
	if calls := m.PullFileCalls(); len(calls) != 1 || calls[0].Path != "/tmp/x" {
		t.Errorf("PullFileCalls = %+v", calls)
	}
}

func TestMockBackend_PullFile_MissingReturnsNotExist(t *testing.T) {
	m := mock.New()
	ctx := context.Background()
	_, _ = m.CreateSession(ctx, sandbox.SessionSpec{SessionID: "s", TemplateID: "t"})

	_, err := m.PullFile(ctx, "s", "/nope")
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("PullFile missing: err = %v, want os.ErrNotExist", err)
	}
}

// -----------------------------------------------------------------------------
// Networking
// -----------------------------------------------------------------------------

func TestMockBackend_ExposePort_RecordsAndIdempotent(t *testing.T) {
	m := mock.New()
	ctx := context.Background()
	_, _ = m.CreateSession(ctx, sandbox.SessionSpec{SessionID: "s", TemplateID: "t"})

	addr, err := m.ExposePort(ctx, "s", 8080, "tcp")
	if err != nil {
		t.Fatalf("ExposePort err = %v", err)
	}
	if addr == nil || addr.Port == 0 {
		t.Errorf("ExposePort returned invalid addr: %+v", addr)
	}
	if err := m.UnexposePort(ctx, "s", 8080); err != nil {
		t.Fatalf("UnexposePort err = %v", err)
	}
	// Unexpose of unmapped port is idempotent.
	if err := m.UnexposePort(ctx, "s", 8080); err != nil {
		t.Errorf("second UnexposePort err = %v, want nil", err)
	}
	if got := m.ExposePortCalls(); len(got) != 1 || got[0].Port != 8080 || got[0].Proto != "tcp" {
		t.Errorf("ExposePortCalls = %+v", got)
	}
}

func TestMockBackend_EnsureOrgNetwork_TracksCalls(t *testing.T) {
	m := mock.New()
	ctx := context.Background()
	if err := m.EnsureOrgNetwork(ctx, "acme"); err != nil {
		t.Fatalf("EnsureOrgNetwork err = %v", err)
	}
	if err := m.DeleteOrgNetwork(ctx, "acme"); err != nil {
		t.Fatalf("DeleteOrgNetwork err = %v", err)
	}
	if got := m.EnsureOrgNetworkCalls(); len(got) != 1 || got[0] != "acme" {
		t.Errorf("EnsureOrgNetworkCalls = %+v", got)
	}
	if got := m.DeleteOrgNetworkCalls(); len(got) != 1 || got[0] != "acme" {
		t.Errorf("DeleteOrgNetworkCalls = %+v", got)
	}
}

// -----------------------------------------------------------------------------
// Templates
// -----------------------------------------------------------------------------

func TestMockBackend_TemplateMethods_RecordCalls(t *testing.T) {
	m := mock.New()
	ctx := context.Background()
	_, _ = m.CreateSession(ctx, sandbox.SessionSpec{SessionID: "s", TemplateID: "t"})

	if _, err := m.BuildTemplate(ctx, sandbox.TemplateBuildSpec{TemplateID: "t1"}); err != nil {
		t.Fatalf("BuildTemplate err = %v", err)
	}
	if _, err := m.SaveSessionAsTemplate(ctx, "s"); err != nil {
		t.Fatalf("SaveSessionAsTemplate err = %v", err)
	}
	if _, err := m.RefreshTemplate(ctx, "t1"); err != nil {
		t.Fatalf("RefreshTemplate err = %v", err)
	}
	if err := m.DeleteTemplate(ctx, "t1", true); err != nil {
		t.Fatalf("DeleteTemplate err = %v", err)
	}
	if got := m.BuildTemplateCalls(); len(got) != 1 || got[0].TemplateID != "t1" {
		t.Errorf("BuildTemplateCalls = %+v", got)
	}
	if got := m.SaveSessionAsTemplateCalls(); len(got) != 1 || got[0] != "s" {
		t.Errorf("SaveSessionAsTemplateCalls = %+v", got)
	}
	if got := m.RefreshTemplateCalls(); len(got) != 1 || got[0] != "t1" {
		t.Errorf("RefreshTemplateCalls = %+v", got)
	}
	if got := m.DeleteTemplateCalls(); len(got) != 1 || got[0].TemplateID != "t1" || !got[0].Force {
		t.Errorf("DeleteTemplateCalls = %+v", got)
	}
}

// -----------------------------------------------------------------------------
// Fleet
// -----------------------------------------------------------------------------

func TestMockBackend_EnsureFleetContainer_Idempotent(t *testing.T) {
	m := mock.New()
	ctx := context.Background()
	spec := sandbox.FleetSpec{FleetKey: "f1", TemplateID: "t"}

	s1, err := m.EnsureFleetContainer(ctx, spec)
	if err != nil {
		t.Fatalf("first EnsureFleetContainer err = %v", err)
	}
	s2, err := m.EnsureFleetContainer(ctx, spec)
	if err != nil {
		t.Fatalf("second EnsureFleetContainer err = %v", err)
	}
	if s1.SessionID != s2.SessionID {
		t.Errorf("fleet session not idempotent: %q vs %q", s1.SessionID, s2.SessionID)
	}
	if got := m.EnsureFleetContainerCalls(); len(got) != 2 {
		t.Errorf("EnsureFleetContainerCalls len = %d, want 2", len(got))
	}
}

// -----------------------------------------------------------------------------
// Helpers, reset, fault injection
// -----------------------------------------------------------------------------

func TestMockBackend_Reset_ClearsState(t *testing.T) {
	m := mock.New()
	ctx := context.Background()
	_, _ = m.CreateSession(ctx, sandbox.SessionSpec{SessionID: "s", TemplateID: "t"})
	m.Reset()

	list, _ := m.ListSessions(ctx, sandbox.SessionFilter{})
	if len(list) != 0 {
		t.Errorf("after Reset: ListSessions len = %d, want 0", len(list))
	}
	if got := m.CreateSessionCalls(); len(got) != 0 {
		t.Errorf("after Reset: CreateSessionCalls len = %d, want 0", len(got))
	}
}

func TestMockBackend_SeedSession(t *testing.T) {
	m := mock.New()
	m.SeedSession(&sandbox.Session{SessionID: "pre", TemplateID: "t", State: sandbox.SessionStateRunning})

	state, err := m.SessionState(context.Background(), "pre")
	if err != nil {
		t.Fatalf("SessionState err = %v", err)
	}
	if state != sandbox.SessionStateRunning {
		t.Errorf("SessionState = %q, want Running", state)
	}
}

func TestMockBackend_ErrForMethod_Injects(t *testing.T) {
	m := mock.New()
	want := errors.New("boom")
	m.ErrForMethod = func(method string) error {
		if method == "CreateSession" {
			return want
		}
		return nil
	}
	_, err := m.CreateSession(context.Background(), sandbox.SessionSpec{SessionID: "s", TemplateID: "t"})
	if !errors.Is(err, want) {
		t.Errorf("CreateSession err = %v, want %v", err, want)
	}
	// Other methods still work.
	if _, err := m.ListSessions(context.Background(), sandbox.SessionFilter{}); err != nil {
		t.Errorf("ListSessions should not be blocked: %v", err)
	}
}

// -----------------------------------------------------------------------------
// Factory registration
// -----------------------------------------------------------------------------

func TestMockBackend_RegisteredWithFactory(t *testing.T) {
	b, err := sandbox.NewBackend(sandbox.BackendFactoryConfig{Kind: sandbox.BackendKindMock})
	if err != nil {
		t.Fatalf("NewBackend(mock) err = %v", err)
	}
	if b == nil {
		t.Fatal("NewBackend(mock) returned nil Backend")
	}
	if b.Kind() != sandbox.BackendKindMock {
		t.Errorf("factory mock Kind() = %q, want %q", b.Kind(), sandbox.BackendKindMock)
	}
	if _, ok := b.(*mock.MockBackend); !ok {
		t.Errorf("factory returned %T, want *mock.MockBackend", b)
	}
}

// Guard against a regression where the default Health report string
// changes silently; downstream tests look at Details["backend"].
func TestMockBackend_HealthDetailsShape(t *testing.T) {
	m := mock.New()
	h, _ := m.Health(context.Background())
	if !strings.EqualFold(h.Details["backend"], "mock") {
		t.Errorf("Health Details backend = %q, want mock", h.Details["backend"])
	}
}
