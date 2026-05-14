// Package k8s provides the Kubernetes + Sysbox implementation of the
// sandbox.Backend interface. Phase C.
//
// This file owns the K8sBackend type, its Config, construction, the
// factory registration, the pure-accessor methods (Kind, Capabilities),
// diagnostics (Health), and the stubs for the operations whose
// implementation has not yet landed. Real operations live in their own
// files:
//
//   - session.go — pod lifecycle: CreateSession, StartSession, StopSession,
//                  DestroySession, SessionState, ListSessions.
//   - exec.go    — SPDY exec (Exec, ExecInteractive). TODO.
//   - files.go   — tar-over-exec (PushFile, PullFile). TODO.
//   - template.go, layer_store.go, layer_gc.go, network.go, fleet.go,
//     overlay_entrypoint.go — per §11 Phase C.
//
// Design invariants:
//
//   - Every method returns ctx.Err() before performing any state-mutating
//     work. The contract suite (sandbox.RunBackendContract) asserts this.
//   - K8sBackend MUST be safe for concurrent use.
//   - Session persistence lives in store.SandboxSessionStore (Phase A),
//     wrapped as *sandbox.SessionRegistry. K8sBackend never owns the
//     registry; it receives one fully constructed from the caller.
//   - Template metadata lives in store.SandboxTemplateStore (Phase A).
//     K8sBackend accepts it in Config for forward compatibility; slices
//     that depend on it will wire it in as they land.

package k8s

import (
	"context"
	"errors"
	"fmt"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/schardosin/astonish/pkg/sandbox"
	"github.com/schardosin/astonish/pkg/store"
)

// ErrNotImplementedYet is returned by K8sBackend methods whose
// implementation has not yet landed. Callers MAY check for this error
// with errors.Is to gate feature rollout or fall back to another backend
// during staged migrations.
var ErrNotImplementedYet = errors.New("sandbox/k8s: not implemented yet (Phase C)")

// Config bundles the K8sBackend dependencies. Fields are validated by
// New; missing required fields produce a clear error.
type Config struct {
	// Client is the Kubernetes API client. Required. Use kubernetes.NewForConfig
	// in production; tests MAY substitute k8s.io/client-go/kubernetes/fake.NewSimpleClientset.
	Client kubernetes.Interface

	// RESTConfig is the REST configuration used for subresource
	// transports (exec, attach, portforward). Required for Exec and
	// ExecInteractive in production; tests that exercise exec MUST
	// either supply a RESTConfig and a stub transport OR override the
	// executor factory via the unexported test hook.
	RESTConfig *rest.Config

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

	// ControlPlaneNamespace is the namespace where the Astonish API and
	// Worker pods run. Used by EnsureOrgNetwork to allow ingress from
	// the control plane into sandbox pods. Defaults to "astonish".
	ControlPlaneNamespace string

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

	// LayersPVCName is the name of the PersistentVolumeClaim that backs
	// LayersPath (mounted read-only into sandbox pods). Defaults to
	// "astonish-layers".
	LayersPVCName string

	// UppersPVCName is the name of the PersistentVolumeClaim that backs
	// UppersPath (mounted RW for eviction/resume). Defaults to
	// "astonish-uppers".
	UppersPVCName string

	// SandboxImage is the container image used for sandbox pods. It
	// MUST ship the astonish-sandbox-entrypoint binary and the node
	// runtime. Defaults to
	// "schardosin/astonish-sandbox-base:latest" (§10 "Astonish ships").
	SandboxImage string

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
	if c.ControlPlaneNamespace == "" {
		c.ControlPlaneNamespace = "astonish"
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
	if c.LayersPVCName == "" {
		c.LayersPVCName = "astonish-layers"
	}
	if c.UppersPVCName == "" {
		c.UppersPVCName = "astonish-uppers"
	}
	if c.SandboxImage == "" {
		c.SandboxImage = "schardosin/astonish-sandbox-base:latest"
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
type K8sBackend struct {
	cfg        Config
	client     kubernetes.Interface
	restConfig *rest.Config
	sessions   *sandbox.SessionRegistry

	// execExecutorFactory constructs a remotecommand-compatible
	// Executor for a given pod/container/command. Defaults to the real
	// SPDY factory; tests override this to inject a stub.
	execExecutorFactory execExecutorFactory

	// startedAt records construction time for Health reporting.
	startedAt time.Time
}

// New constructs a K8sBackend from a Config. Returns an error if any
// required field is missing or invalid.
//
// Client is optional ONLY in contexts where the backend is constructed for
// the contract test / skeleton introspection. When Client is nil, every
// state-mutating method returns ErrNotImplementedYet (after checking
// ctx.Err()); this preserves the prior skeleton behavior during staged
// rollout. Production callers MUST supply a Client.
func New(cfg Config) (*K8sBackend, error) {
	if cfg.Sessions == nil {
		return nil, errors.New("sandbox/k8s: Sessions registry is required")
	}
	cfg.applyDefaults()
	return &K8sBackend{
		cfg:                 cfg,
		client:              cfg.Client,
		restConfig:          cfg.RESTConfig,
		sessions:            cfg.Sessions,
		execExecutorFactory: defaultExecExecutorFactory,
		startedAt:           time.Now().UTC(),
	}, nil
}

// init registers K8sBackend with sandbox.NewBackend. Importing
// pkg/sandbox/k8s makes BackendKindK8s available to the factory.
//
// The BackendFactoryConfig shape (pre-Phase-A) lacks a
// store.SandboxTemplateStore slot, a kubernetes.Interface, and a
// k8s-specific Config. Rather than thread those through the shared
// factory, callers that need the full Config wire K8sBackend directly
// via New. The factory hook here supports the skeleton path where the
// ambient Sessions registry is sufficient, and surfaces an actionable
// error otherwise.
func init() {
	sandbox.RegisterBackendFactory(sandbox.BackendKindK8s, func(fc sandbox.BackendFactoryConfig) (sandbox.Backend, error) {
		if fc.Sessions == nil {
			return nil, errors.New("sandbox/k8s: BackendFactoryConfig.Sessions is required; call k8s.New directly for richer configuration")
		}
		return New(Config{Sessions: fc.Sessions})
	})
}

// ---------------------------------------------------------------------------
// Diagnostics
// ---------------------------------------------------------------------------

// Kind returns BackendKindK8s.
func (b *K8sBackend) Kind() sandbox.BackendKind {
	return sandbox.BackendKindK8s
}

// Capabilities advertises the final (post-implementation) feature set.
// Flags are truthful about the backend's *design*; callers that need a
// runtime readiness signal should use Health.
func (b *K8sBackend) Capabilities() sandbox.BackendCapabilities {
	return sandbox.BackendCapabilities{
		Kind:                 sandbox.BackendKindK8s,
		SupportsLiveEvict:    true,
		SupportsFastClone:    true,
		SupportsPortExpose:   true,
		SupportsOrgIsolation: true,
	}
}

// Health reports the backend's readiness. Honours ctx cancellation.
//
// When a real Client is configured, Health issues a lightweight
// server-version probe to verify API-server connectivity. When Client is
// nil (skeleton / misconfigured backend), Health reports unhealthy with
// a descriptive reason.
//
// Future slices will extend Health to verify Sysbox RuntimeClass
// existence, CephFS mount readiness, and other backend prerequisites.
func (b *K8sBackend) Health(ctx context.Context) (*sandbox.BackendHealth, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	details := map[string]string{
		"namespace":     b.cfg.Namespace,
		"runtime_class": b.cfg.RuntimeClassName,
		"layers_path":   b.cfg.LayersPath,
		"uppers_path":   b.cfg.UppersPath,
		"started_at":    b.startedAt.Format(time.RFC3339),
	}

	if b.client == nil {
		return &sandbox.BackendHealth{
			Healthy:   false,
			Reason:    "k8s backend: no Kubernetes client configured",
			CheckedAt: time.Now().UTC(),
			Details:   details,
		}, nil
	}

	version, err := b.client.Discovery().ServerVersion()
	if err != nil {
		return &sandbox.BackendHealth{
			Healthy:   false,
			Reason:    fmt.Sprintf("k8s API-server unreachable: %v", err),
			CheckedAt: time.Now().UTC(),
			Details:   details,
		}, nil
	}
	details["server_version"] = version.GitVersion
	details["server_platform"] = version.Platform
	return &sandbox.BackendHealth{
		Healthy:   true,
		CheckedAt: time.Now().UTC(),
		Details:   details,
	}, nil
}

// ---------------------------------------------------------------------------
// Exec and file I/O — Exec/ExecInteractive live in exec.go; PushFile /
// PullFile live in files.go.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Templates — BuildTemplate / SaveSessionAsTemplate / RefreshTemplate /
// DeleteTemplate live in template.go.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Networking — EnsureOrgNetwork / DeleteOrgNetwork / ExposePort /
// UnexposePort live in network.go.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Fleet — EnsureFleetContainer lives in fleet.go.
// ---------------------------------------------------------------------------
