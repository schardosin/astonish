package sandbox

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// TestIncusBackendInterfaceAssertion is a compile-time guarantee that
// *IncusBackend satisfies the Backend interface. If any method signature
// drifts, this test stops compiling.
func TestIncusBackendInterfaceAssertion(t *testing.T) {
	var _ Backend = (*IncusBackend)(nil)
}

// TestNewIncusBackendValidation covers constructor argument validation. We
// cannot construct a real client here (no Incus daemon in the test
// environment), but we can verify each required dependency is checked.
func TestNewIncusBackendValidation(t *testing.T) {
	cases := []struct {
		name    string
		cfg     IncusBackendConfig
		wantMsg string
	}{
		{
			name:    "missing client",
			cfg:     IncusBackendConfig{},
			wantMsg: "Client is required",
		},
		{
			name: "missing sessions registry",
			cfg: IncusBackendConfig{
				Client: &IncusClient{}, // zero-value placeholder, validated only for nil-ness
			},
			wantMsg: "Sessions registry is required",
		},
		{
			name: "missing templates registry",
			cfg: IncusBackendConfig{
				Client:   &IncusClient{},
				Sessions: &SessionRegistry{},
			},
			wantMsg: "Templates registry is required",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewIncusBackend(c.cfg)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", c.wantMsg)
			}
			if !strings.Contains(err.Error(), c.wantMsg) {
				t.Errorf("error = %q, want to contain %q", err, c.wantMsg)
			}
		})
	}
}

// TestIncusBackendKind verifies the Kind identifier.
func TestIncusBackendKind(t *testing.T) {
	b := &IncusBackend{} // zero-value is fine for pure-read accessors
	if got := b.Kind(); got != BackendKindIncus {
		t.Errorf("Kind() = %q, want %q", got, BackendKindIncus)
	}
}

// TestIncusBackendCapabilities verifies the advertised feature matrix.
// These flags are contract: the UI gates features against them.
func TestIncusBackendCapabilities(t *testing.T) {
	b := &IncusBackend{}
	caps := b.Capabilities()

	if caps.Kind != BackendKindIncus {
		t.Errorf("Capabilities.Kind = %q, want %q", caps.Kind, BackendKindIncus)
	}
	if caps.SupportsLiveEvict {
		t.Error("IncusBackend should NOT advertise SupportsLiveEvict (overlay stays mounted)")
	}
	if !caps.SupportsFastClone {
		t.Error("IncusBackend MUST advertise SupportsFastClone (overlayfs is the happy path)")
	}
	if !caps.SupportsPortExpose {
		t.Error("IncusBackend MUST advertise SupportsPortExpose")
	}
	if !caps.SupportsOrgIsolation {
		t.Error("IncusBackend MUST advertise SupportsOrgIsolation (per-org bridge + profile)")
	}
}

// TestIncusBackendTemplatePhaseB2 documents that SaveSessionAsTemplate
// (which depends on Phase A semantics) returns the sentinel error today.
// BuildTemplate is now fully implemented and validates its inputs instead.
func TestIncusBackendTemplatePhaseB2(t *testing.T) {
	b := &IncusBackend{}
	ctx := context.Background()

	// BuildTemplate with empty spec returns a validation error (TemplateID required).
	if _, err := b.BuildTemplate(ctx, TemplateBuildSpec{}); err == nil ||
		!strings.Contains(err.Error(), "TemplateID is required") {
		t.Errorf("BuildTemplate empty spec: got %v, want 'TemplateID is required'", err)
	}
	// BuildTemplate with TemplateID but no steps returns a validation error.
	if _, err := b.BuildTemplate(ctx, TemplateBuildSpec{TemplateID: "test"}); err == nil ||
		!strings.Contains(err.Error(), "at least one build step is required") {
		t.Errorf("BuildTemplate no steps: got %v, want 'at least one build step is required'", err)
	}
	if _, err := b.SaveSessionAsTemplate(ctx, "ignored"); !errors.Is(err, ErrUnsupportedInPhaseB2) {
		t.Errorf("SaveSessionAsTemplate: got %v, want ErrUnsupportedInPhaseB2", err)
	}
}

// TestIncusBackendArgValidation covers the pre-daemon arg-validation paths
// so we can exercise them without a live Incus daemon.
func TestIncusBackendArgValidation(t *testing.T) {
	b := &IncusBackend{
		sessions:  newTestRegistry(t),
		templates: &TemplateRegistry{},
	}
	ctx := context.Background()

	if _, err := b.CreateSession(ctx, SessionSpec{}); err == nil ||
		!strings.Contains(err.Error(), "SessionID is required") {
		t.Errorf("CreateSession empty SessionID: got %v", err)
	}
	// Empty TemplateID now defaults to BaseTemplateID ("@base"). With a
	// nil Incus client, the call panics downstream — recover and verify
	// the validation layer no longer rejects empty TemplateID.
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("CreateSession with nil client and empty TemplateID: expected panic (nil Incus client), got clean return")
			}
			// Panic from nil client dereference is expected; it proves
			// we passed validation and reached the Incus orchestration
			// code that requires a live client.
		}()
		_, _ = b.CreateSession(ctx, SessionSpec{SessionID: "s1"})
	}()
	if _, err := b.RefreshTemplate(ctx, ""); err == nil ||
		!strings.Contains(err.Error(), "templateID is required") {
		t.Errorf("RefreshTemplate empty ID: got %v", err)
	}
	if err := b.DeleteTemplate(ctx, "", false); err == nil ||
		!strings.Contains(err.Error(), "templateID is required") {
		t.Errorf("DeleteTemplate empty ID: got %v", err)
	}
	if err := b.EnsureOrgNetwork(ctx, ""); err == nil ||
		!strings.Contains(err.Error(), "orgSlug is required") {
		t.Errorf("EnsureOrgNetwork empty slug: got %v", err)
	}
	if err := b.DeleteOrgNetwork(ctx, ""); err == nil ||
		!strings.Contains(err.Error(), "orgSlug is required") {
		t.Errorf("DeleteOrgNetwork empty slug: got %v", err)
	}
	if _, err := b.EnsureFleetContainer(ctx, FleetSpec{}); err == nil ||
		!strings.Contains(err.Error(), "FleetKey is required") {
		t.Errorf("EnsureFleetContainer empty FleetKey: got %v", err)
	}
	// Empty TemplateID defaults to BaseTemplateID; verify we pass
	// validation (panics downstream on nil client, which is expected).
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("EnsureFleetContainer with nil client and empty TemplateID: expected panic, got clean return")
			}
		}()
		_, _ = b.EnsureFleetContainer(ctx, FleetSpec{FleetKey: "fk"})
	}()
}

// TestIncusBackendSessionNotFound covers the "session is not in the
// registry" paths, which should report cleanly (not panic) without
// touching the daemon.
func TestIncusBackendSessionNotFound(t *testing.T) {
	b := &IncusBackend{
		sessions:  newTestRegistry(t),
		templates: &TemplateRegistry{},
	}
	ctx := context.Background()

	if err := b.StartSession(ctx, "nope"); err == nil ||
		!strings.Contains(err.Error(), "not found") {
		t.Errorf("StartSession missing: got %v", err)
	}
	// Idempotent: StopSession on a non-existent session is a no-op.
	if err := b.StopSession(ctx, "nope"); err != nil {
		t.Errorf("StopSession idempotent no-op: got %v", err)
	}
	// Idempotent: UnexposePort on a non-existent session is a no-op.
	if err := b.UnexposePort(ctx, "nope", 8080); err != nil {
		t.Errorf("UnexposePort idempotent no-op: got %v", err)
	}
	// SessionState on absent session = Gone, no error.
	st, err := b.SessionState(ctx, "nope")
	if err != nil {
		t.Errorf("SessionState missing: unexpected err %v", err)
	}
	if st != SessionStateGone {
		t.Errorf("SessionState missing: got %q, want %q", st, SessionStateGone)
	}
	if _, err := b.Exec(ctx, "nope", ExecSpec{Command: []string{"true"}}); err == nil ||
		!strings.Contains(err.Error(), "not found") {
		t.Errorf("Exec missing: got %v", err)
	}
	if _, err := b.ExecInteractive(ctx, "nope", PTYSpec{Command: []string{"sh"}}); err == nil ||
		!strings.Contains(err.Error(), "not found") {
		t.Errorf("ExecInteractive missing: got %v", err)
	}
	if _, err := b.ExposePort(ctx, "nope", 8080, "tcp"); err == nil ||
		!strings.Contains(err.Error(), "not found") {
		t.Errorf("ExposePort missing: got %v", err)
	}
	if err := b.PushFile(ctx, "nope", "/x", strings.NewReader(""), 0o644); err == nil ||
		!strings.Contains(err.Error(), "not found") {
		t.Errorf("PushFile missing: got %v", err)
	}
	if _, err := b.PullFile(ctx, "nope", "/x"); err == nil ||
		!strings.Contains(err.Error(), "not found") {
		t.Errorf("PullFile missing: got %v", err)
	}
}

// TestIncusBackendContextCancelled ensures cancelled contexts short-circuit
// before any daemon interaction.
func TestIncusBackendContextCancelled(t *testing.T) {
	b := &IncusBackend{
		sessions:  newTestRegistry(t),
		templates: &TemplateRegistry{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := b.CreateSession(ctx, SessionSpec{SessionID: "s", TemplateID: "t"}); !errors.Is(err, context.Canceled) {
		t.Errorf("CreateSession cancelled: got %v", err)
	}
	if err := b.StartSession(ctx, "s"); !errors.Is(err, context.Canceled) {
		t.Errorf("StartSession cancelled: got %v", err)
	}
	if err := b.StopSession(ctx, "s"); !errors.Is(err, context.Canceled) {
		t.Errorf("StopSession cancelled: got %v", err)
	}
	if err := b.DestroySession(ctx, "s"); !errors.Is(err, context.Canceled) {
		t.Errorf("DestroySession cancelled: got %v", err)
	}
	if _, err := b.ListSessions(ctx, SessionFilter{}); !errors.Is(err, context.Canceled) {
		t.Errorf("ListSessions cancelled: got %v", err)
	}
	if _, err := b.Exec(ctx, "s", ExecSpec{}); !errors.Is(err, context.Canceled) {
		t.Errorf("Exec cancelled: got %v", err)
	}
	if _, err := b.Health(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Health cancelled: got %v", err)
	}
}

// TestMapLimits exercises the ResourceLimits → config.SandboxLimits mapping.
func TestMapLimits(t *testing.T) {
	b := &IncusBackend{}
	// Zero → nil (no default configured).
	if got := b.mapLimits(ResourceLimits{}); got != nil {
		t.Errorf("empty limits with no default: got %+v, want nil", got)
	}
	// Non-zero → populated struct.
	got := b.mapLimits(ResourceLimits{CPUs: 2, MemoryMiB: 512, PIDs: 100})
	if got == nil {
		t.Fatal("non-empty limits: got nil")
	}
	if got.CPU != 2 {
		t.Errorf("CPU = %d, want 2", got.CPU)
	}
	if got.Memory != "512MB" {
		t.Errorf("Memory = %q, want %q", got.Memory, "512MB")
	}
	if got.Processes != 100 {
		t.Errorf("Processes = %d, want 100", got.Processes)
	}
}

// TestDefaultSessionType verifies the empty-default fallback.
func TestDefaultSessionType(t *testing.T) {
	if got := defaultSessionType(""); got != SessionTypeChat {
		t.Errorf("defaultSessionType(\"\") = %q, want %q", got, SessionTypeChat)
	}
	if got := defaultSessionType(SessionTypeFleet); got != SessionTypeFleet {
		t.Errorf("defaultSessionType(fleet) = %q, want %q", got, SessionTypeFleet)
	}
}

// TestByteSeeker covers the io.ReadSeeker helper used by PushFile.
func TestByteSeeker(t *testing.T) {
	s := newByteSeeker([]byte("hello world"))

	buf := make([]byte, 5)
	n, err := s.Read(buf)
	if err != nil || n != 5 || string(buf) != "hello" {
		t.Fatalf("initial read: n=%d err=%v buf=%q", n, err, buf)
	}

	// SeekStart
	if pos, err := s.Seek(6, io.SeekStart); err != nil || pos != 6 {
		t.Fatalf("SeekStart: pos=%d err=%v", pos, err)
	}
	n, err = s.Read(buf)
	if err != nil || n != 5 || string(buf[:n]) != "world" {
		t.Fatalf("after SeekStart: n=%d err=%v buf=%q", n, err, buf[:n])
	}

	// SeekCurrent
	if _, err := s.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if pos, err := s.Seek(3, io.SeekCurrent); err != nil || pos != 3 {
		t.Fatalf("SeekCurrent: pos=%d err=%v", pos, err)
	}

	// SeekEnd
	if pos, err := s.Seek(-5, io.SeekEnd); err != nil || pos != 6 {
		t.Fatalf("SeekEnd: pos=%d err=%v", pos, err)
	}

	// Invalid whence
	if _, err := s.Seek(0, 99); err == nil {
		t.Error("expected error for invalid whence")
	}

	// Negative absolute position
	if _, err := s.Seek(-1, io.SeekStart); err == nil {
		t.Error("expected error for negative position")
	}

	// EOF
	s2 := newByteSeeker([]byte(""))
	if _, err := s2.Read(buf); !errors.Is(err, io.EOF) {
		t.Errorf("empty Read: got %v, want io.EOF", err)
	}
}
