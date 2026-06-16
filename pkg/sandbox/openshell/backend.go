// Package openshell provides the OpenShell-backed implementation of the
// sandbox.Backend interface.
//
// The OpenShell backend creates sandbox pods that run NVIDIA OpenShell's
// supervisor as PID 1 (after astonish-boot composes the overlay and
// pivot_roots). All exec operations flow through the OpenShell Gateway
// relay, which enforces Landlock, seccomp, network namespace, and OCSF
// audit policies per-process.
//
// This file owns the OpenShellBackend type, Config, factory registration,
// Kind, Capabilities, Health, and stubs for operations pending implementation.
//
// Reference: docs/architecture/openshell-sandbox-backend.md

package openshell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/sandbox"
)

// ErrNotImplementedYet is returned by methods whose implementation has
// not yet landed. Callers MAY check for this error with errors.Is to
// gate feature rollout.
var ErrNotImplementedYet = errors.New("sandbox/openshell: not implemented yet")

// Config bundles the OpenShellBackend dependencies.
type Config struct {
	// Sessions is the session registry. Required.
	Sessions *sandbox.SessionRegistry

	// GatewayAddr is the gRPC address of the OpenShell Gateway.
	// Default: "openshell-gateway.openshell-system.svc.cluster.local:443"
	GatewayAddr string

	// Namespace is the Kubernetes namespace for sandbox pods.
	// Default: "astonish-sandboxes"
	Namespace string

	// SandboxImage is the container image for OpenShell sandbox pods.
	// Default: "schardosin/astonish-sandbox-openshell:latest"
	SandboxImage string

	// K8s carries the shared Kubernetes configuration from app config.
	K8s config.SandboxKubernetesConfig
}

func (c *Config) applyDefaults() {
	if c.GatewayAddr == "" {
		c.GatewayAddr = "openshell-gateway.openshell-system.svc.cluster.local:443"
	}
	if c.Namespace == "" {
		c.Namespace = "astonish-sandboxes"
	}
	if c.SandboxImage == "" {
		c.SandboxImage = "schardosin/astonish-sandbox-openshell:latest"
	}
}

// OpenShellBackend satisfies sandbox.Backend by routing session lifecycle
// and exec operations through the OpenShell Gateway.
type OpenShellBackend struct {
	cfg       Config
	sessions  *sandbox.SessionRegistry
	startedAt time.Time
}

// New creates a new OpenShellBackend. Returns an error if required
// configuration is missing.
func New(cfg Config) (*OpenShellBackend, error) {
	if cfg.Sessions == nil {
		return nil, errors.New("sandbox/openshell: Sessions registry is required")
	}
	cfg.applyDefaults()
	return &OpenShellBackend{
		cfg:       cfg,
		sessions:  cfg.Sessions,
		startedAt: time.Now().UTC(),
	}, nil
}

// init registers OpenShellBackend with sandbox.NewBackend.
func init() {
	sandbox.RegisterBackendFactory(sandbox.BackendKindOpenShell, func(fc sandbox.BackendFactoryConfig) (sandbox.Backend, error) {
		if fc.Sessions == nil {
			return nil, errors.New("sandbox/openshell: BackendFactoryConfig.Sessions is required")
		}
		cfg := Config{
			Sessions:     fc.Sessions,
			Namespace:    fc.K8s.Namespace,
			SandboxImage: fc.K8s.SandboxImage,
			K8s:          fc.K8s,
		}
		return New(cfg)
	})
}

// ---------------------------------------------------------------------------
// Diagnostics
// ---------------------------------------------------------------------------

// Kind returns BackendKindOpenShell.
func (b *OpenShellBackend) Kind() sandbox.BackendKind {
	return sandbox.BackendKindOpenShell
}

// ServerArchitecture returns "amd64" (conservative default).
func (b *OpenShellBackend) ServerArchitecture() string {
	return "amd64"
}

// Capabilities advertises the designed feature set.
func (b *OpenShellBackend) Capabilities() sandbox.BackendCapabilities {
	return sandbox.BackendCapabilities{
		Kind:                 sandbox.BackendKindOpenShell,
		SupportsLiveEvict:    true,
		SupportsFastClone:    true,
		SupportsPortExpose:   true,
		SupportsOrgIsolation: true,
	}
}

// Health reports readiness. Currently returns healthy if the config is
// valid; future slices will probe Gateway connectivity.
func (b *OpenShellBackend) Health(ctx context.Context) (*sandbox.BackendHealth, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	details := map[string]string{
		"namespace":    b.cfg.Namespace,
		"gateway_addr": b.cfg.GatewayAddr,
		"sandbox_image": b.cfg.SandboxImage,
		"started_at":  b.startedAt.Format(time.RFC3339),
	}
	return &sandbox.BackendHealth{
		Healthy:   true,
		Reason:    "openshell backend configured (gateway connectivity check pending implementation)",
		CheckedAt: time.Now().UTC(),
		Details:   details,
	}, nil
}

// ---------------------------------------------------------------------------
// Session lifecycle
// ---------------------------------------------------------------------------

func (b *OpenShellBackend) CreateSession(ctx context.Context, spec sandbox.SessionSpec) (*sandbox.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, ErrNotImplementedYet
}

func (b *OpenShellBackend) StartSession(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrNotImplementedYet
}

func (b *OpenShellBackend) StopSession(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrNotImplementedYet
}

func (b *OpenShellBackend) DestroySession(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrNotImplementedYet
}

func (b *OpenShellBackend) SessionState(ctx context.Context, sessionID string) (sandbox.SessionState, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "", ErrNotImplementedYet
}

func (b *OpenShellBackend) WaitForSessionReady(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrNotImplementedYet
}

func (b *OpenShellBackend) ListSessions(ctx context.Context, filter sandbox.SessionFilter) ([]*sandbox.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, ErrNotImplementedYet
}

// ---------------------------------------------------------------------------
// Exec and file I/O
// ---------------------------------------------------------------------------

func (b *OpenShellBackend) Exec(ctx context.Context, sessionID string, opts sandbox.ExecSpec) (*sandbox.ExecResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, ErrNotImplementedYet
}

func (b *OpenShellBackend) ExecInteractive(ctx context.Context, sessionID string, opts sandbox.PTYSpec) (sandbox.ExecStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, ErrNotImplementedYet
}

func (b *OpenShellBackend) ExecStreaming(ctx context.Context, sessionID string, opts sandbox.ExecStreamSpec) (sandbox.ExecStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, ErrNotImplementedYet
}

func (b *OpenShellBackend) PushFile(ctx context.Context, sessionID, path string, content io.Reader, mode os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrNotImplementedYet
}

func (b *OpenShellBackend) PullFile(ctx context.Context, sessionID, path string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, ErrNotImplementedYet
}

// ---------------------------------------------------------------------------
// Templates
// ---------------------------------------------------------------------------

func (b *OpenShellBackend) BuildTemplate(ctx context.Context, spec sandbox.TemplateBuildSpec) (*sandbox.TemplateArtifact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, ErrNotImplementedYet
}

func (b *OpenShellBackend) SaveSessionAsTemplate(ctx context.Context, sessionID string) (*sandbox.TemplateArtifact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, ErrNotImplementedYet
}

func (b *OpenShellBackend) RefreshTemplate(ctx context.Context, templateID string) (*sandbox.TemplateArtifact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, ErrNotImplementedYet
}

func (b *OpenShellBackend) DeleteTemplate(ctx context.Context, templateID string, force bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrNotImplementedYet
}

// ---------------------------------------------------------------------------
// Networking
// ---------------------------------------------------------------------------

func (b *OpenShellBackend) EnsureOrgNetwork(ctx context.Context, orgSlug string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrNotImplementedYet
}

func (b *OpenShellBackend) DeleteOrgNetwork(ctx context.Context, orgSlug string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrNotImplementedYet
}

func (b *OpenShellBackend) ExposePort(ctx context.Context, sessionID string, port int, proto string) (*sandbox.ExposedAddr, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, ErrNotImplementedYet
}

func (b *OpenShellBackend) UnexposePort(ctx context.Context, sessionID string, port int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrNotImplementedYet
}

// ---------------------------------------------------------------------------
// Fleet
// ---------------------------------------------------------------------------

func (b *OpenShellBackend) EnsureFleetContainer(ctx context.Context, spec sandbox.FleetSpec) (*sandbox.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, ErrNotImplementedYet
}

// Ensure compile-time interface compliance.
var _ sandbox.Backend = (*OpenShellBackend)(nil)

// Suppress unused import warnings for packages used in stubs.
var _ = fmt.Sprintf
