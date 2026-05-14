// Package k8s provides the Kubernetes + Sysbox implementation of the
// sandbox.Backend interface. Phase C (skeleton slice).
//
// This slice introduces the *K8sBackend* type and registers it with
// sandbox.NewBackend. All interface methods are stubs that return
// ErrNotImplementedYet — except pure accessors (Kind, Capabilities) and
// diagnostics (Health), which report the configured-but-not-ready state.
// Subsequent slices fill in pod lifecycle (session.go), SPDY exec
// (exec.go), tar-over-exec file I/O (files.go), templates (template.go),
// layer store (layer_store.go), GC (layer_gc.go), networking
// (network.go), fleet (fleet.go), and overlay entrypoint
// (overlay_entrypoint.go) as enumerated in
// docs/architecture/sandbox-backends.md §11 Phase C.
//
// Design invariants (preserved by all future slices):
//
//   - K8sBackend MUST honour ctx cancellation: every method returns
//     ctx.Err() before performing any state-mutating work. The contract
//     suite (sandbox.RunBackendContract) asserts this.
//   - K8sBackend MUST be safe for concurrent use.
//   - Session persistence lives in store.SandboxSessionStore (Phase A)
//     wrapped as *sandbox.SessionRegistry. K8sBackend never owns the
//     registry; it receives one fully constructed from the caller. In
//     platform deployments the caller wires in
//     pgstore.NewPGSandboxSessionStore; in development the local JSON
//     store may be substituted without code changes.
//   - Template metadata lives in store.SandboxTemplateStore (Phase A).
//     K8sBackend does NOT hold a *sandbox.TemplateRegistry directly;
//     future slices accept a SandboxTemplateStore through config.
//
// This skeleton intentionally depends only on the standard library and
// pkg/sandbox. The Kubernetes client-go dependency is introduced in the
// first implementation slice that needs it (session lifecycle), not here,
// to keep the import graph minimal during review.

package k8s

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/schardosin/astonish/pkg/sandbox"
	"github.com/schardosin/astonish/pkg/store"
)

// ErrNotImplementedYet is returned by every K8sBackend method whose
// implementation has not yet landed. Callers MAY check for this error
// with errors.Is to gate feature rollout or fall back to another backend
// during staged migrations.
var ErrNotImplementedYet = errors.New("sandbox/k8s: not implemented yet (Phase C skeleton)")

// Config bundles the K8sBackend dependencies. Fields are validated by
// New; missing required fields produce a clear error. Future slices will
// add kubernetes.Interface, dynamic client, informers, etc. Today the
// skeleton only needs Sessions to participate in the contract test and
// the factory plumbing.
type Config struct {
	// Sessions is the session registry, backed by a
	// store.SandboxSessionStore (Phase A). Required.
	//
	// In platform mode construct with:
	//
	//     st := pgstore.NewPGSandboxSessionStore(db, teamSchema)
	//     sr := sandbox.NewSessionRegistryFromStore(st)
	//
	// In personal mode (single-pod dev):
	//
	//     sr := sandbox.NewSessionRegistry()
	Sessions *sandbox.SessionRegistry

	// Templates is the template metadata store (Phase A). Required
	// once the template operations land; accepted here for forward
	// compatibility so callers can wire it up now.
	Templates store.SandboxTemplateStore

	// Namespace is the Kubernetes namespace in which sandbox pods are
	// created. Defaults to "astonish-sandboxes".
	Namespace string

	// RuntimeClassName is the RuntimeClass used for sandbox pods.
	// Defaults to "sysbox-runc". See
	// docs/architecture/sandbox-backends.md §10.
	RuntimeClassName string

	// LayersPath is the node-local mount point for the RWX CephFS layer
	// store. Defaults to "/mnt/astonish-layers".
	LayersPath string

	// UppersPath is the node-local mount point for the RWX CephFS upper
	// store (persisted evicted upper layers). Defaults to
	// "/mnt/astonish-uppers".
	UppersPath string

	// MaxChainDepth caps the overlayfs lowerdir chain (default 20).
	// Must be ≤ node kernel's overlay.max_lower.
	MaxChainDepth int

	// MaxConcurrentEvictions caps parallel tar-stream evictions
	// (default 8).
	MaxConcurrentEvictions int
}

func (c *Config) applyDefaults() {
	if c.Namespace == "" {
		c.Namespace = "astonish-sandboxes"
	}
	if c.RuntimeClassName == "" {
		c.RuntimeClassName = "sysbox-runc"
	}
	if c.LayersPath == "" {
		c.LayersPath = "/mnt/astonish-layers"
	}
	if c.UppersPath == "" {
		c.UppersPath = "/mnt/astonish-uppers"
	}
	if c.MaxChainDepth <= 0 {
		c.MaxChainDepth = 20
	}
	if c.MaxConcurrentEvictions <= 0 {
		c.MaxConcurrentEvictions = 8
	}
}

// K8sBackend is the Backend implementation backed by Kubernetes +
// Sysbox. Zero value is NOT usable; construct via New.
//
// This struct is intentionally small in the skeleton slice. Future
// slices add fields incrementally (kubernetes.Interface client, informer
// caches, layer-store handle, GC reconciler, etc.) without breaking
// existing call sites — all callers hold the *sandbox.Backend interface,
// not *K8sBackend.
type K8sBackend struct {
	cfg      Config
	sessions *sandbox.SessionRegistry

	// startedAt records construction time for Health reporting.
	startedAt time.Time
}

// New constructs a K8sBackend from a Config. Returns an error if any
// required field is missing.
func New(cfg Config) (*K8sBackend, error) {
	if cfg.Sessions == nil {
		return nil, errors.New("sandbox/k8s: Sessions registry is required")
	}
	cfg.applyDefaults()
	return &K8sBackend{
		cfg:       cfg,
		sessions:  cfg.Sessions,
		startedAt: time.Now().UTC(),
	}, nil
}

// init registers K8sBackend with sandbox.NewBackend. Importing
// pkg/sandbox/k8s makes BackendKindK8s available to the factory.
//
// The BackendFactoryConfig shape (pre-Phase-A) lacks a
// store.SandboxTemplateStore slot and a richer k8s-specific config;
// rather than thread those through the shared factory now, callers that
// need the full Config wire K8sBackend directly via New. The factory
// hook here supports the common path where the ambient Sessions
// registry is sufficient to construct a backend, and surfaces an
// actionable error otherwise.
func init() {
	sandbox.RegisterBackendFactory(sandbox.BackendKindK8s, func(fc sandbox.BackendFactoryConfig) (sandbox.Backend, error) {
		if fc.Sessions == nil {
			return nil, errors.New("sandbox/k8s: BackendFactoryConfig.Sessions is required; call k8s.New directly for richer configuration")
		}
		return New(Config{Sessions: fc.Sessions})
	})
}

// ---------------------------------------------------------------------------
// Diagnostics (implemented)
// ---------------------------------------------------------------------------

// Kind returns BackendKindK8s.
func (b *K8sBackend) Kind() sandbox.BackendKind {
	return sandbox.BackendKindK8s
}

// Capabilities advertises the final (post-implementation) feature set.
// Flags are truthful about the backend's *design*; callers that need a
// runtime readiness signal should use Health.
//
// Rationale: the UI gates features (port expose, org isolation) at
// config time against Capabilities. Reporting "false" here during the
// skeleton phase would force every UI caller to know about the phased
// rollout. Instead the skeleton reports the intended capability set and
// relies on the not-implemented errors to surface during actual use.
func (b *K8sBackend) Capabilities() sandbox.BackendCapabilities {
	return sandbox.BackendCapabilities{
		Kind:                 sandbox.BackendKindK8s,
		SupportsLiveEvict:    true,
		SupportsFastClone:    true,
		SupportsPortExpose:   true,
		SupportsOrgIsolation: true,
	}
}

// Health reports the skeleton's configured-but-not-ready state. It
// honours ctx cancellation (the contract suite asserts this).
//
// Future slices replace the body with a real readiness probe
// (API-server ping, Sysbox RuntimeClass existence, CephFS mount check).
func (b *K8sBackend) Health(ctx context.Context) (*sandbox.BackendHealth, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &sandbox.BackendHealth{
		Healthy:   false,
		Reason:    "k8s backend skeleton: implementation pending (Phase C)",
		CheckedAt: time.Now().UTC(),
		Details: map[string]string{
			"namespace":      b.cfg.Namespace,
			"runtime_class":  b.cfg.RuntimeClassName,
			"layers_path":    b.cfg.LayersPath,
			"uppers_path":    b.cfg.UppersPath,
			"started_at":     b.startedAt.Format(time.RFC3339),
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Session lifecycle (stubs — Phase C slice: session.go)
// ---------------------------------------------------------------------------

func (b *K8sBackend) CreateSession(ctx context.Context, spec sandbox.SessionSpec) (*sandbox.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("CreateSession: %w", ErrNotImplementedYet)
}

func (b *K8sBackend) StartSession(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("StartSession: %w", ErrNotImplementedYet)
}

func (b *K8sBackend) StopSession(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("StopSession: %w", ErrNotImplementedYet)
}

func (b *K8sBackend) DestroySession(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("DestroySession: %w", ErrNotImplementedYet)
}

func (b *K8sBackend) SessionState(ctx context.Context, sessionID string) (sandbox.SessionState, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("SessionState: %w", ErrNotImplementedYet)
}

func (b *K8sBackend) ListSessions(ctx context.Context, filter sandbox.SessionFilter) ([]*sandbox.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("ListSessions: %w", ErrNotImplementedYet)
}

// ---------------------------------------------------------------------------
// Exec and file I/O (stubs — Phase C slices: exec.go, files.go)
// ---------------------------------------------------------------------------

func (b *K8sBackend) Exec(ctx context.Context, sessionID string, opts sandbox.ExecSpec) (*sandbox.ExecResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("Exec: %w", ErrNotImplementedYet)
}

func (b *K8sBackend) ExecInteractive(ctx context.Context, sessionID string, opts sandbox.PTYSpec) (sandbox.ExecStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("ExecInteractive: %w", ErrNotImplementedYet)
}

func (b *K8sBackend) PushFile(ctx context.Context, sessionID, path string, content io.Reader, mode os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("PushFile: %w", ErrNotImplementedYet)
}

func (b *K8sBackend) PullFile(ctx context.Context, sessionID, path string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("PullFile: %w", ErrNotImplementedYet)
}

// ---------------------------------------------------------------------------
// Templates (stubs — Phase C slice: template.go)
// ---------------------------------------------------------------------------

func (b *K8sBackend) BuildTemplate(ctx context.Context, spec sandbox.TemplateBuildSpec) (*sandbox.TemplateArtifact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("BuildTemplate: %w", ErrNotImplementedYet)
}

func (b *K8sBackend) SaveSessionAsTemplate(ctx context.Context, sessionID string) (*sandbox.TemplateArtifact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("SaveSessionAsTemplate: %w", ErrNotImplementedYet)
}

func (b *K8sBackend) RefreshTemplate(ctx context.Context, templateID string) (*sandbox.TemplateArtifact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("RefreshTemplate: %w", ErrNotImplementedYet)
}

func (b *K8sBackend) DeleteTemplate(ctx context.Context, templateID string, force bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("DeleteTemplate: %w", ErrNotImplementedYet)
}

// ---------------------------------------------------------------------------
// Networking (stubs — Phase C slice: network.go)
// ---------------------------------------------------------------------------

func (b *K8sBackend) EnsureOrgNetwork(ctx context.Context, orgSlug string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("EnsureOrgNetwork: %w", ErrNotImplementedYet)
}

func (b *K8sBackend) DeleteOrgNetwork(ctx context.Context, orgSlug string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("DeleteOrgNetwork: %w", ErrNotImplementedYet)
}

func (b *K8sBackend) ExposePort(ctx context.Context, sessionID string, port int, proto string) (*sandbox.ExposedAddr, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("ExposePort: %w", ErrNotImplementedYet)
}

func (b *K8sBackend) UnexposePort(ctx context.Context, sessionID string, port int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("UnexposePort: %w", ErrNotImplementedYet)
}

// ---------------------------------------------------------------------------
// Fleet (stub — Phase C slice: fleet.go)
// ---------------------------------------------------------------------------

func (b *K8sBackend) EnsureFleetContainer(ctx context.Context, spec sandbox.FleetSpec) (*sandbox.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("EnsureFleetContainer: %w", ErrNotImplementedYet)
}
