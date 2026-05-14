// Package mock provides an in-memory implementation of sandbox.Backend
// for tests. Phase B.4.
//
// MockBackend records every call, maintains session state entirely in
// memory, and lets tests assert on observed behavior without standing up
// a real Incus daemon or Kubernetes cluster.
//
// Usage:
//
//	mock := mock.New()
//	mock.Capabilities = sandbox.BackendCapabilities{Kind: sandbox.BackendKindMock, SupportsPortExpose: true}
//	// inject mock into the code under test via the sandbox.Backend interface
//	someComponent := NewComponent(mock)
//	// ...
//	if calls := mock.CreateSessionCalls(); len(calls) != 1 {
//	    t.Errorf("expected 1 CreateSession call, got %d", len(calls))
//	}
//
// MockBackend is safe for concurrent use.
package mock

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/schardosin/astonish/pkg/sandbox"
)

// MockBackend is an in-memory sandbox.Backend. The zero value is NOT
// usable; construct via New().
//
// Every method call is recorded in the corresponding *Calls slice so tests
// can assert on invocation count, ordering, and arguments. Session state
// (lifecycle, port exposures, exposed files) is tracked in concurrent-safe
// maps so tests can drive realistic multi-step scenarios.
//
// Callers MAY override any field (capabilities, custom error injectors, the
// health-report generator) before invoking the mock.
type MockBackend struct {
	mu sync.Mutex

	// Caps is returned by Capabilities(). Defaults to a sane set (see New).
	Caps sandbox.BackendCapabilities

	// HealthFn, when non-nil, is invoked by Health(). When nil, Health
	// returns a healthy default. Tests that want to simulate daemon
	// failures set this to a function that returns an unhealthy report
	// or an error.
	HealthFn func(ctx context.Context) (*sandbox.BackendHealth, error)

	// ExecResultFn, when non-nil, generates the ExecResult for an Exec
	// call. When nil, Exec returns a zero-exit empty result.
	ExecResultFn func(sessionID string, spec sandbox.ExecSpec) (*sandbox.ExecResult, error)

	// ExecStreamFn, when non-nil, generates the ExecStream for an
	// ExecInteractive call. When nil, ExecInteractive returns a stub
	// stream that echoes stdin to stdout and exits with code 0 on Close.
	ExecStreamFn func(sessionID string, spec sandbox.PTYSpec) (sandbox.ExecStream, error)

	// PullFileFn, when non-nil, generates the content returned by
	// PullFile. When nil, PullFile returns os.ErrNotExist unless the
	// file was previously pushed via PushFile.
	PullFileFn func(sessionID, path string) (io.ReadCloser, error)

	// ErrForMethod, when non-nil, is consulted before every method. If
	// it returns a non-nil error for the given method name, that error
	// is returned instead of executing the method. Useful for fault
	// injection. Method names are the Go method names ("CreateSession",
	// "StartSession", ...).
	ErrForMethod func(method string) error

	// State, keyed by SessionID.
	sessions map[string]*sandbox.Session
	ports    map[string]map[int]string // sessionID → port → proto
	files    map[string]map[string][]byte // sessionID → path → content

	// Call logs (append-only).
	createSessionCalls         []CreateSessionCall
	startSessionCalls          []string
	stopSessionCalls           []string
	destroySessionCalls        []string
	sessionStateCalls          []string
	listSessionsCalls          []sandbox.SessionFilter
	execCalls                  []ExecCall
	execInteractiveCalls       []ExecInteractiveCall
	pushFileCalls              []PushFileCall
	pullFileCalls              []PullFileCall
	buildTemplateCalls         []sandbox.TemplateBuildSpec
	saveSessionAsTemplateCalls []string
	refreshTemplateCalls       []string
	deleteTemplateCalls        []DeleteTemplateCall
	ensureOrgNetworkCalls      []string
	deleteOrgNetworkCalls      []string
	exposePortCalls            []ExposePortCall
	unexposePortCalls          []UnexposePortCall
	ensureFleetContainerCalls  []sandbox.FleetSpec
	capabilitiesCalls          int
	healthCalls                int
	kindCalls                  int
}

// Call-log record types.

type CreateSessionCall struct {
	Spec sandbox.SessionSpec
	At   time.Time
}

type ExecCall struct {
	SessionID string
	Spec      sandbox.ExecSpec
}

type ExecInteractiveCall struct {
	SessionID string
	Spec      sandbox.PTYSpec
}

type PushFileCall struct {
	SessionID string
	Path      string
	Mode      os.FileMode
	// Content is captured at call time so later edits do not affect
	// recorded bytes.
	Content []byte
}

type PullFileCall struct {
	SessionID string
	Path      string
}

type DeleteTemplateCall struct {
	TemplateID string
	Force      bool
}

type ExposePortCall struct {
	SessionID string
	Port      int
	Proto     string
}

type UnexposePortCall struct {
	SessionID string
	Port      int
}

// New constructs an empty MockBackend with sane default capabilities.
func New() *MockBackend {
	return &MockBackend{
		Caps: sandbox.BackendCapabilities{
			Kind:                 sandbox.BackendKindMock,
			SupportsLiveEvict:    true,
			SupportsFastClone:    true,
			SupportsPortExpose:   true,
			SupportsOrgIsolation: true,
		},
		sessions: make(map[string]*sandbox.Session),
		ports:    make(map[string]map[int]string),
		files:    make(map[string]map[string][]byte),
	}
}

// Compile-time assertion.
var _ sandbox.Backend = (*MockBackend)(nil)

// init registers the mock backend with sandbox.NewBackend so that
// callers requesting BackendKindMock receive a fresh *MockBackend. This
// package-level side effect is what the registration-hook pattern buys
// us: pkg/sandbox never imports pkg/sandbox/mock (which would cycle),
// but any test that imports pkg/sandbox/mock makes the mock available
// through the public factory.
//
// The BackendFactoryConfig is ignored: MockBackend has no external
// dependencies. Tests that want a pre-seeded or custom-capability mock
// should construct it directly via New() and inject it rather than go
// through the factory.
func init() {
	sandbox.RegisterBackendFactory(sandbox.BackendKindMock, func(_ sandbox.BackendFactoryConfig) (sandbox.Backend, error) {
		return New(), nil
	})
}

// ---------------------------------------------------------------------------
// Diagnostics
// ---------------------------------------------------------------------------

func (m *MockBackend) Kind() sandbox.BackendKind {
	m.mu.Lock()
	m.kindCalls++
	m.mu.Unlock()
	return sandbox.BackendKindMock
}

func (m *MockBackend) Capabilities() sandbox.BackendCapabilities {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.capabilitiesCalls++
	caps := m.Caps
	if caps.Kind == "" {
		caps.Kind = sandbox.BackendKindMock
	}
	return caps
}

func (m *MockBackend) Health(ctx context.Context) (*sandbox.BackendHealth, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := m.injectedErr("Health"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.healthCalls++
	fn := m.HealthFn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	return &sandbox.BackendHealth{
		Healthy:   true,
		CheckedAt: time.Now().UTC(),
		Details:   map[string]string{"backend": "mock"},
	}, nil
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

func (m *MockBackend) CreateSession(ctx context.Context, spec sandbox.SessionSpec) (*sandbox.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := m.injectedErr("CreateSession"); err != nil {
		return nil, err
	}
	if spec.SessionID == "" {
		return nil, errors.New("mock.CreateSession: SessionID is required")
	}
	if spec.TemplateID == "" {
		return nil, errors.New("mock.CreateSession: TemplateID is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.createSessionCalls = append(m.createSessionCalls, CreateSessionCall{Spec: spec, At: time.Now().UTC()})

	// Idempotent: if the session already exists, return the existing
	// record without modification.
	if existing, ok := m.sessions[spec.SessionID]; ok {
		return cloneSession(existing), nil
	}

	sType := spec.Type
	if sType == "" {
		sType = sandbox.SessionTypeChat
	}
	sess := &sandbox.Session{
		SessionID:  spec.SessionID,
		Type:       sType,
		TemplateID: spec.TemplateID,
		OrgSlug:    spec.OrgSlug,
		TeamSlug:   spec.TeamSlug,
		State:      sandbox.SessionStateRunning,
		BackendRef: "mock/" + spec.SessionID,
		Labels:     cloneLabels(spec.Labels),
		CreatedAt:  time.Now().UTC(),
	}
	m.sessions[spec.SessionID] = sess
	return cloneSession(sess), nil
}

func (m *MockBackend) StartSession(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.injectedErr("StartSession"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startSessionCalls = append(m.startSessionCalls, sessionID)
	sess, ok := m.sessions[sessionID]
	if !ok {
		return fmt.Errorf("mock.StartSession: session %q not found", sessionID)
	}
	sess.State = sandbox.SessionStateRunning
	return nil
}

func (m *MockBackend) StopSession(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.injectedErr("StopSession"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopSessionCalls = append(m.stopSessionCalls, sessionID)
	sess, ok := m.sessions[sessionID]
	if !ok {
		return nil // idempotent
	}
	sess.State = sandbox.SessionStateStopped
	return nil
}

func (m *MockBackend) DestroySession(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.injectedErr("DestroySession"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.destroySessionCalls = append(m.destroySessionCalls, sessionID)
	delete(m.sessions, sessionID)
	delete(m.ports, sessionID)
	delete(m.files, sessionID)
	return nil // idempotent
}

func (m *MockBackend) SessionState(ctx context.Context, sessionID string) (sandbox.SessionState, error) {
	if err := ctx.Err(); err != nil {
		return sandbox.SessionStateGone, err
	}
	if err := m.injectedErr("SessionState"); err != nil {
		return sandbox.SessionStateGone, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionStateCalls = append(m.sessionStateCalls, sessionID)
	if sess, ok := m.sessions[sessionID]; ok {
		return sess.State, nil
	}
	return sandbox.SessionStateGone, nil
}

func (m *MockBackend) ListSessions(ctx context.Context, filter sandbox.SessionFilter) ([]*sandbox.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := m.injectedErr("ListSessions"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listSessionsCalls = append(m.listSessionsCalls, filter)
	out := make([]*sandbox.Session, 0, len(m.sessions))
	for _, sess := range m.sessions {
		if filter.Type != "" && filter.Type != sess.Type {
			continue
		}
		if filter.OrgSlug != "" && filter.OrgSlug != sess.OrgSlug {
			continue
		}
		if filter.TeamSlug != "" && filter.TeamSlug != sess.TeamSlug {
			continue
		}
		if filter.State != "" && filter.State != sess.State {
			continue
		}
		out = append(out, cloneSession(sess))
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Exec and file I/O
// ---------------------------------------------------------------------------

func (m *MockBackend) Exec(ctx context.Context, sessionID string, opts sandbox.ExecSpec) (*sandbox.ExecResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := m.injectedErr("Exec"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.execCalls = append(m.execCalls, ExecCall{SessionID: sessionID, Spec: opts})
	fn := m.ExecResultFn
	_, exists := m.sessions[sessionID]
	m.mu.Unlock()
	if !exists {
		return nil, fmt.Errorf("mock.Exec: session %q not found", sessionID)
	}
	if fn != nil {
		return fn(sessionID, opts)
	}
	return &sandbox.ExecResult{ExitCode: 0}, nil
}

func (m *MockBackend) ExecInteractive(ctx context.Context, sessionID string, opts sandbox.PTYSpec) (sandbox.ExecStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := m.injectedErr("ExecInteractive"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.execInteractiveCalls = append(m.execInteractiveCalls, ExecInteractiveCall{SessionID: sessionID, Spec: opts})
	fn := m.ExecStreamFn
	_, exists := m.sessions[sessionID]
	m.mu.Unlock()
	if !exists {
		return nil, fmt.Errorf("mock.ExecInteractive: session %q not found", sessionID)
	}
	if fn != nil {
		return fn(sessionID, opts)
	}
	return newStubExecStream(), nil
}

func (m *MockBackend) PushFile(ctx context.Context, sessionID, path string, content io.Reader, mode os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.injectedErr("PushFile"); err != nil {
		return err
	}
	buf, err := io.ReadAll(content)
	if err != nil {
		return fmt.Errorf("mock.PushFile: read source: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pushFileCalls = append(m.pushFileCalls, PushFileCall{
		SessionID: sessionID,
		Path:      path,
		Mode:      mode,
		Content:   append([]byte(nil), buf...),
	})
	if _, ok := m.sessions[sessionID]; !ok {
		return fmt.Errorf("mock.PushFile: session %q not found", sessionID)
	}
	if m.files[sessionID] == nil {
		m.files[sessionID] = make(map[string][]byte)
	}
	m.files[sessionID][path] = append([]byte(nil), buf...)
	return nil
}

func (m *MockBackend) PullFile(ctx context.Context, sessionID, path string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := m.injectedErr("PullFile"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.pullFileCalls = append(m.pullFileCalls, PullFileCall{SessionID: sessionID, Path: path})
	fn := m.PullFileFn
	_, exists := m.sessions[sessionID]
	var data []byte
	var have bool
	if fs, ok := m.files[sessionID]; ok {
		data, have = fs[path]
	}
	m.mu.Unlock()
	if !exists {
		return nil, fmt.Errorf("mock.PullFile: session %q not found", sessionID)
	}
	if fn != nil {
		return fn(sessionID, path)
	}
	if !have {
		return nil, fmt.Errorf("mock.PullFile(%s, %s): %w", sessionID, path, os.ErrNotExist)
	}
	return io.NopCloser(newByteReader(data)), nil
}

// ---------------------------------------------------------------------------
// Templates
// ---------------------------------------------------------------------------

func (m *MockBackend) BuildTemplate(ctx context.Context, spec sandbox.TemplateBuildSpec) (*sandbox.TemplateArtifact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := m.injectedErr("BuildTemplate"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buildTemplateCalls = append(m.buildTemplateCalls, spec)
	return &sandbox.TemplateArtifact{
		LayerID:   "mock-layer-" + spec.TemplateID,
		SizeBytes: 0,
		CreatedAt: time.Now().UTC(),
	}, nil
}

func (m *MockBackend) SaveSessionAsTemplate(ctx context.Context, sessionID string) (*sandbox.TemplateArtifact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := m.injectedErr("SaveSessionAsTemplate"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveSessionAsTemplateCalls = append(m.saveSessionAsTemplateCalls, sessionID)
	if _, ok := m.sessions[sessionID]; !ok {
		return nil, fmt.Errorf("mock.SaveSessionAsTemplate: session %q not found", sessionID)
	}
	return &sandbox.TemplateArtifact{
		LayerID:   "mock-upper-" + sessionID,
		CreatedAt: time.Now().UTC(),
	}, nil
}

func (m *MockBackend) RefreshTemplate(ctx context.Context, templateID string) (*sandbox.TemplateArtifact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := m.injectedErr("RefreshTemplate"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshTemplateCalls = append(m.refreshTemplateCalls, templateID)
	return &sandbox.TemplateArtifact{
		LayerID:   "mock-layer-" + templateID,
		CreatedAt: time.Now().UTC(),
	}, nil
}

func (m *MockBackend) DeleteTemplate(ctx context.Context, templateID string, force bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.injectedErr("DeleteTemplate"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteTemplateCalls = append(m.deleteTemplateCalls, DeleteTemplateCall{TemplateID: templateID, Force: force})
	return nil
}

// ---------------------------------------------------------------------------
// Networking
// ---------------------------------------------------------------------------

func (m *MockBackend) EnsureOrgNetwork(ctx context.Context, orgSlug string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.injectedErr("EnsureOrgNetwork"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureOrgNetworkCalls = append(m.ensureOrgNetworkCalls, orgSlug)
	return nil
}

func (m *MockBackend) DeleteOrgNetwork(ctx context.Context, orgSlug string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.injectedErr("DeleteOrgNetwork"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteOrgNetworkCalls = append(m.deleteOrgNetworkCalls, orgSlug)
	return nil
}

func (m *MockBackend) ExposePort(ctx context.Context, sessionID string, port int, proto string) (*sandbox.ExposedAddr, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := m.injectedErr("ExposePort"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.exposePortCalls = append(m.exposePortCalls, ExposePortCall{SessionID: sessionID, Port: port, Proto: proto})
	if _, ok := m.sessions[sessionID]; !ok {
		return nil, fmt.Errorf("mock.ExposePort: session %q not found", sessionID)
	}
	if m.ports[sessionID] == nil {
		m.ports[sessionID] = make(map[int]string)
	}
	if proto == "" {
		proto = "tcp"
	}
	m.ports[sessionID][port] = proto
	return &sandbox.ExposedAddr{
		Host:     "mock.local",
		Port:     port,
		Protocol: proto,
	}, nil
}

func (m *MockBackend) UnexposePort(ctx context.Context, sessionID string, port int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.injectedErr("UnexposePort"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unexposePortCalls = append(m.unexposePortCalls, UnexposePortCall{SessionID: sessionID, Port: port})
	if ports, ok := m.ports[sessionID]; ok {
		delete(ports, port)
	}
	return nil // idempotent
}

// ---------------------------------------------------------------------------
// Fleet
// ---------------------------------------------------------------------------

func (m *MockBackend) EnsureFleetContainer(ctx context.Context, spec sandbox.FleetSpec) (*sandbox.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := m.injectedErr("EnsureFleetContainer"); err != nil {
		return nil, err
	}
	if spec.FleetKey == "" {
		return nil, errors.New("mock.EnsureFleetContainer: FleetKey is required")
	}
	if spec.TemplateID == "" {
		return nil, errors.New("mock.EnsureFleetContainer: TemplateID is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureFleetContainerCalls = append(m.ensureFleetContainerCalls, spec)

	if existing, ok := m.sessions[spec.FleetKey]; ok {
		return cloneSession(existing), nil
	}
	sess := &sandbox.Session{
		SessionID:  spec.FleetKey,
		Type:       sandbox.SessionTypeFleet,
		TemplateID: spec.TemplateID,
		OrgSlug:    spec.OrgSlug,
		TeamSlug:   spec.TeamSlug,
		State:      sandbox.SessionStateRunning,
		BackendRef: "mock/fleet/" + spec.FleetKey,
		Labels:     cloneLabels(spec.Labels),
		CreatedAt:  time.Now().UTC(),
	}
	m.sessions[spec.FleetKey] = sess
	return cloneSession(sess), nil
}

// ---------------------------------------------------------------------------
// Call-log accessors (read-only, thread-safe)
// ---------------------------------------------------------------------------

func (m *MockBackend) CreateSessionCalls() []CreateSessionCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]CreateSessionCall(nil), m.createSessionCalls...)
}

func (m *MockBackend) StartSessionCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.startSessionCalls...)
}

func (m *MockBackend) StopSessionCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.stopSessionCalls...)
}

func (m *MockBackend) DestroySessionCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.destroySessionCalls...)
}

func (m *MockBackend) SessionStateCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.sessionStateCalls...)
}

func (m *MockBackend) ListSessionsCalls() []sandbox.SessionFilter {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]sandbox.SessionFilter(nil), m.listSessionsCalls...)
}

func (m *MockBackend) ExecCalls() []ExecCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ExecCall(nil), m.execCalls...)
}

func (m *MockBackend) ExecInteractiveCalls() []ExecInteractiveCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ExecInteractiveCall(nil), m.execInteractiveCalls...)
}

func (m *MockBackend) PushFileCalls() []PushFileCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]PushFileCall(nil), m.pushFileCalls...)
}

func (m *MockBackend) PullFileCalls() []PullFileCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]PullFileCall(nil), m.pullFileCalls...)
}

func (m *MockBackend) BuildTemplateCalls() []sandbox.TemplateBuildSpec {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]sandbox.TemplateBuildSpec(nil), m.buildTemplateCalls...)
}

func (m *MockBackend) SaveSessionAsTemplateCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.saveSessionAsTemplateCalls...)
}

func (m *MockBackend) RefreshTemplateCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.refreshTemplateCalls...)
}

func (m *MockBackend) DeleteTemplateCalls() []DeleteTemplateCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]DeleteTemplateCall(nil), m.deleteTemplateCalls...)
}

func (m *MockBackend) EnsureOrgNetworkCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.ensureOrgNetworkCalls...)
}

func (m *MockBackend) DeleteOrgNetworkCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.deleteOrgNetworkCalls...)
}

func (m *MockBackend) ExposePortCalls() []ExposePortCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ExposePortCall(nil), m.exposePortCalls...)
}

func (m *MockBackend) UnexposePortCalls() []UnexposePortCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]UnexposePortCall(nil), m.unexposePortCalls...)
}

func (m *MockBackend) EnsureFleetContainerCalls() []sandbox.FleetSpec {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]sandbox.FleetSpec(nil), m.ensureFleetContainerCalls...)
}

// Reset clears all call logs and session state. Useful for shared fixtures.
func (m *MockBackend) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions = make(map[string]*sandbox.Session)
	m.ports = make(map[string]map[int]string)
	m.files = make(map[string]map[string][]byte)
	m.createSessionCalls = nil
	m.startSessionCalls = nil
	m.stopSessionCalls = nil
	m.destroySessionCalls = nil
	m.sessionStateCalls = nil
	m.listSessionsCalls = nil
	m.execCalls = nil
	m.execInteractiveCalls = nil
	m.pushFileCalls = nil
	m.pullFileCalls = nil
	m.buildTemplateCalls = nil
	m.saveSessionAsTemplateCalls = nil
	m.refreshTemplateCalls = nil
	m.deleteTemplateCalls = nil
	m.ensureOrgNetworkCalls = nil
	m.deleteOrgNetworkCalls = nil
	m.exposePortCalls = nil
	m.unexposePortCalls = nil
	m.ensureFleetContainerCalls = nil
	m.capabilitiesCalls = 0
	m.healthCalls = 0
	m.kindCalls = 0
}

// SeedSession inserts a ready-made session into the mock's state without
// going through CreateSession. Useful for tests that want to simulate
// pre-existing sessions without recording a CreateSession call.
func (m *MockBackend) SeedSession(sess *sandbox.Session) {
	if sess == nil || sess.SessionID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[sess.SessionID] = cloneSession(sess)
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (m *MockBackend) injectedErr(method string) error {
	m.mu.Lock()
	fn := m.ErrForMethod
	m.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn(method)
}

func cloneSession(s *sandbox.Session) *sandbox.Session {
	if s == nil {
		return nil
	}
	cp := *s
	cp.Labels = cloneLabels(s.Labels)
	return &cp
}

func cloneLabels(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

// byteReader is a minimal io.Reader over a []byte that does not keep a
// reference to the source slice after exhaustion.
type byteReader struct {
	data []byte
	pos  int
}

func newByteReader(b []byte) *byteReader { return &byteReader{data: b} }

func (r *byteReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// stubExecStream is the default ExecStream returned by ExecInteractive. It
// echoes stdin writes back through stdout reads and reports exit code 0
// after Close.
type stubExecStream struct {
	mu       sync.Mutex
	buf      []byte
	closed   bool
	exitCode int
	waitCh   chan struct{}
}

func newStubExecStream() *stubExecStream {
	return &stubExecStream{waitCh: make(chan struct{})}
}

func (s *stubExecStream) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.buf) == 0 {
		if s.closed {
			return 0, io.EOF
		}
		return 0, nil
	}
	n := copy(p, s.buf)
	s.buf = s.buf[n:]
	return n, nil
}

func (s *stubExecStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, io.ErrClosedPipe
	}
	s.buf = append(s.buf, p...)
	return len(p), nil
}

func (s *stubExecStream) Resize(rows, cols int) error { return nil }

func (s *stubExecStream) Wait() (int, error) {
	<-s.waitCh
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitCode, nil
}

func (s *stubExecStream) Close() error {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.waitCh)
	}
	s.mu.Unlock()
	return nil
}
