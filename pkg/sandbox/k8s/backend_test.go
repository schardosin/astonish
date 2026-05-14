// Package k8s — contract tests for the skeleton.
//
// The K8sBackend skeleton must already pass the shared
// sandbox.BackendContract suite even though every state-mutating method
// returns ErrNotImplementedYet. The contract asserts:
//
//   - Kind() returns a stable, lowercase identifier (here "k8s").
//   - Capabilities().Kind matches Kind().
//   - A cancelled context causes every state-mutating method to return
//     ctx.Err() before doing any work (here: before returning
//     ErrNotImplementedYet).
//   - PullFile has the io.ReadCloser signature.
//
// Skeleton-specific coverage in this file:
//
//   - New rejects missing Sessions.
//   - Factory registration path through sandbox.NewBackend succeeds when
//     Sessions is supplied and returns a functional (if stubby) backend.
//   - Every stubbed state-mutating method wraps ErrNotImplementedYet
//     with a prefix that identifies the method (so errors.Is still
//     matches and the error string is actionable).

package k8s

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/schardosin/astonish/pkg/sandbox"
)

// newRegistry builds a SessionRegistry backed by the sandbox package's
// registered default store factory. The default factory lives in
// pkg/sandbox/session_store_local.go and writes to the user's data
// directory; for tests we don't actually call any registry methods, so
// simply constructing the registry is enough. If the default factory is
// not registered (e.g., someone moved the init), fall back to a direct
// construction pointed at a test temp dir.
//
// We deliberately avoid depending on pkg/sandbox internal test helpers
// (newTestRegistry) because those live in a _test.go file and are not
// exported across packages.
func newRegistry(t *testing.T) *sandbox.SessionRegistry {
	t.Helper()
	// The default factory resolves the user data dir. For unit tests
	// we want an isolated location; override HOME to a temp dir so the
	// resolver lands there.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	r, err := sandbox.NewSessionRegistry()
	if err != nil {
		t.Fatalf("sandbox.NewSessionRegistry: %v", err)
	}
	return r
}

// TestK8sBackendContract runs the shared Backend contract suite against
// the skeleton. Every state-mutating method returns ErrNotImplementedYet
// wrapped with a method prefix; the contract suite only checks that
// ctx.Err() is returned when the context is cancelled, which our stubs
// do before the not-implemented error.
func TestK8sBackendContract(t *testing.T) {
	sandbox.RunBackendContract(t, func(t *testing.T) (sandbox.Backend, string) {
		b, err := New(Config{Sessions: newRegistry(t)})
		if err != nil {
			t.Fatalf("k8s.New: %v", err)
		}
		return b, ""
	})
}

// TestK8sBackendKind verifies the Kind identifier.
func TestK8sBackendKind(t *testing.T) {
	b, err := New(Config{Sessions: newRegistry(t)})
	if err != nil {
		t.Fatalf("k8s.New: %v", err)
	}
	if got, want := b.Kind(), sandbox.BackendKindK8s; got != want {
		t.Errorf("Kind() = %q, want %q", got, want)
	}
}

// TestK8sBackendCapabilities verifies the advertised feature matrix
// matches the intended production shape (§3.6, §5.3).
func TestK8sBackendCapabilities(t *testing.T) {
	b, err := New(Config{Sessions: newRegistry(t)})
	if err != nil {
		t.Fatalf("k8s.New: %v", err)
	}
	caps := b.Capabilities()
	if caps.Kind != sandbox.BackendKindK8s {
		t.Errorf("Capabilities.Kind = %q, want %q", caps.Kind, sandbox.BackendKindK8s)
	}
	if !caps.SupportsLiveEvict {
		t.Error("k8s backend MUST advertise SupportsLiveEvict")
	}
	if !caps.SupportsFastClone {
		t.Error("k8s backend MUST advertise SupportsFastClone")
	}
	if !caps.SupportsPortExpose {
		t.Error("k8s backend MUST advertise SupportsPortExpose")
	}
	if !caps.SupportsOrgIsolation {
		t.Error("k8s backend MUST advertise SupportsOrgIsolation")
	}
}

// TestK8sBackendHealth verifies the no-client skeleton health report.
func TestK8sBackendHealth(t *testing.T) {
	b, err := New(Config{Sessions: newRegistry(t)})
	if err != nil {
		t.Fatalf("k8s.New: %v", err)
	}
	h, err := b.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: unexpected error %v", err)
	}
	if h.Healthy {
		t.Error("Health without Client should report Healthy=false")
	}
	if !strings.Contains(h.Reason, "no Kubernetes client configured") {
		t.Errorf("Health.Reason = %q, want to mention missing Kubernetes client", h.Reason)
	}
	if h.Details["namespace"] == "" {
		t.Error("Health.Details should include namespace")
	}
}

// TestK8sBackendNewRequiresSessions verifies argument validation.
func TestK8sBackendNewRequiresSessions(t *testing.T) {
	_, err := New(Config{})
	if err == nil || !strings.Contains(err.Error(), "Sessions registry is required") {
		t.Errorf("New with missing Sessions: got %v, want Sessions-required error", err)
	}
}

// TestK8sBackendConfigDefaults verifies applyDefaults wiring.
func TestK8sBackendConfigDefaults(t *testing.T) {
	b, err := New(Config{Sessions: newRegistry(t)})
	if err != nil {
		t.Fatalf("k8s.New: %v", err)
	}
	if b.cfg.Namespace != "astonish-sandboxes" {
		t.Errorf("default Namespace = %q, want %q", b.cfg.Namespace, "astonish-sandboxes")
	}
	if b.cfg.RuntimeClassName != "sysbox-runc" {
		t.Errorf("default RuntimeClassName = %q, want %q", b.cfg.RuntimeClassName, "sysbox-runc")
	}
	if b.cfg.LayersPath != "/mnt/astonish-layers" {
		t.Errorf("default LayersPath = %q", b.cfg.LayersPath)
	}
	if b.cfg.UppersPath != "/mnt/astonish-uppers" {
		t.Errorf("default UppersPath = %q", b.cfg.UppersPath)
	}
	if b.cfg.MaxChainDepth != 20 {
		t.Errorf("default MaxChainDepth = %d, want 20", b.cfg.MaxChainDepth)
	}
	if b.cfg.MaxConcurrentEvictions != 8 {
		t.Errorf("default MaxConcurrentEvictions = %d, want 8", b.cfg.MaxConcurrentEvictions)
	}
}

// TestK8sBackendPendingSlicesReturnNotImplemented exercises each method
// whose implementation has not yet landed (templates, networking,
// fleet) and verifies the returned error wraps ErrNotImplementedYet.
// Session-lifecycle methods (CreateSession, StartSession, StopSession,
// DestroySession, SessionState, ListSessions) are covered by
// session_test.go; Exec and ExecInteractive by exec_test.go; PushFile
// and PullFile by files_test.go.
func TestK8sBackendPendingSlicesReturnNotImplemented(t *testing.T) {
	b, err := New(Config{Sessions: newRegistry(t)})
	if err != nil {
		t.Fatalf("k8s.New: %v", err)
	}
	ctx := context.Background()

	checks := []struct {
		name string
		run  func() error
	}{
		{"BuildTemplate", func() error {
			_, err := b.BuildTemplate(ctx, sandbox.TemplateBuildSpec{TemplateID: "t"})
			return err
		}},
		{"SaveSessionAsTemplate", func() error {
			_, err := b.SaveSessionAsTemplate(ctx, "s")
			return err
		}},
		{"RefreshTemplate", func() error {
			_, err := b.RefreshTemplate(ctx, "t")
			return err
		}},
		{"DeleteTemplate", func() error { return b.DeleteTemplate(ctx, "t", false) }},
		{"EnsureOrgNetwork", func() error { return b.EnsureOrgNetwork(ctx, "org") }},
		{"DeleteOrgNetwork", func() error { return b.DeleteOrgNetwork(ctx, "org") }},
		{"ExposePort", func() error {
			_, err := b.ExposePort(ctx, "s", 8080, "tcp")
			return err
		}},
		{"UnexposePort", func() error { return b.UnexposePort(ctx, "s", 8080) }},
		{"EnsureFleetContainer", func() error {
			_, err := b.EnsureFleetContainer(ctx, sandbox.FleetSpec{FleetKey: "f", TemplateID: "t"})
			return err
		}},
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			err := c.run()
			if err == nil {
				t.Fatal("expected ErrNotImplementedYet, got nil")
			}
			if !errors.Is(err, ErrNotImplementedYet) {
				t.Errorf("err = %v, want errors.Is(err, ErrNotImplementedYet)", err)
			}
			if !strings.Contains(err.Error(), c.name) {
				t.Errorf("err = %v, want error to mention %q", err, c.name)
			}
		})
	}
}

// TestK8sBackendFactoryRegistration verifies that importing this package
// registered the k8s factory with sandbox.NewBackend.
func TestK8sBackendFactoryRegistration(t *testing.T) {
	sr := newRegistry(t)

	b, err := sandbox.NewBackend(sandbox.BackendFactoryConfig{
		Kind:     sandbox.BackendKindK8s,
		Sessions: sr,
	})
	if err != nil {
		t.Fatalf("NewBackend(k8s): %v", err)
	}
	if b.Kind() != sandbox.BackendKindK8s {
		t.Errorf("Kind() = %q, want %q", b.Kind(), sandbox.BackendKindK8s)
	}

	// Without Sessions the factory must fail clearly.
	if _, err := sandbox.NewBackend(sandbox.BackendFactoryConfig{
		Kind: sandbox.BackendKindK8s,
	}); err == nil || !strings.Contains(err.Error(), "Sessions is required") {
		t.Errorf("NewBackend(k8s) with missing Sessions: got %v, want Sessions-required error", err)
	}
}

// TestPodNameForSession covers the deterministic naming helper.
func TestPodNameForSession(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"00000000-0000-0000-0000-000000000001", "astn-sess-00000000-0000-0000-0000-000"},
		{"SESSION_with_underscores", "astn-sess-session-with-underscores"},
		{"--trim-edges--", "astn-sess-trim-edges"},
		{"abc", "astn-sess-abc"},
	}
	for _, c := range cases {
		if got := podNameForSession(c.in); got != c.want {
			t.Errorf("podNameForSession(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
