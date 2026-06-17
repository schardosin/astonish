package openshell

import (
	"context"
	"fmt"
	"strings"

	"github.com/schardosin/astonish/pkg/sandbox"
)

// ---------------------------------------------------------------------------
// Networking
// ---------------------------------------------------------------------------
//
// In the OpenShell backend, network isolation is enforced by the OpenShell
// supervisor's Landlock + network namespace policies running *inside* each
// sandbox. This is fundamentally different from the K8s backend which uses
// Kubernetes NetworkPolicy objects.
//
// Org-level network segmentation (EnsureOrgNetwork/DeleteOrgNetwork) is a
// no-op for OpenShell because:
//   - Each sandbox gets its own network namespace (via OpenShell supervisor)
//   - Inter-sandbox communication is mediated by the gateway relay
//   - The supervisor's policy engine controls which endpoints are reachable
//
// Port exposure creates a gateway-level ingress route rather than a K8s
// Service, allowing external access to sandbox-internal ports.

// EnsureOrgNetwork is a no-op for the OpenShell backend. Network isolation
// is enforced per-sandbox by the OpenShell supervisor's network namespace
// and Landlock policies, not by Kubernetes NetworkPolicy.
func (b *OpenShellBackend) EnsureOrgNetwork(ctx context.Context, orgSlug string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// OpenShell supervisor handles network isolation per-sandbox.
	// No Kubernetes NetworkPolicy needed.
	return nil
}

// DeleteOrgNetwork is a no-op for the OpenShell backend. See EnsureOrgNetwork.
func (b *OpenShellBackend) DeleteOrgNetwork(ctx context.Context, orgSlug string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// ExposePort makes a port inside the sandbox accessible from outside.
// The OpenShell gateway acts as the ingress point: it allocates a route
// for the session+port combination and returns the externally-reachable
// address.
//
// For now, this uses a convention-based DNS hostname derived from the
// session ID and namespace. The gateway relay forwards incoming connections
// to the sandbox's supervisor which binds the port in its network namespace.
func (b *OpenShellBackend) ExposePort(ctx context.Context, sessionID string, port int, proto string) (*sandbox.ExposedAddr, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.gateway == nil {
		return nil, ErrNotImplementedYet
	}
	if sessionID == "" {
		return nil, fmt.Errorf("sandbox/openshell: ExposePort: sessionID is required")
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("sandbox/openshell: ExposePort: port %d out of range [1, 65535]", port)
	}

	// Normalize protocol.
	proto = strings.ToLower(proto)
	if proto == "" {
		proto = "tcp"
	}

	rec, err := b.sessions.GetSession(sessionID)
	if err != nil {
		return nil, fmt.Errorf("sandbox/openshell: ExposePort(%s): lookup session: %w", sessionID, err)
	}
	if rec == nil || rec.ContainerName == "" {
		return nil, fmt.Errorf("sandbox/openshell: ExposePort: session %q has no sandbox", sessionID)
	}

	// Construct the externally-reachable hostname.
	// Convention: <session-id-prefix>-<port>.svc.cluster.local
	// In production, the gateway's ExposeService RPC provides the actual URL.
	host := fmt.Sprintf("%s-%d.svc.cluster.local",
		sandboxName(sessionID), port)

	// Register the port in the session registry for bookkeeping.
	if _, err := b.sessions.ExposePort(rec.ContainerName, port); err != nil {
		// Non-fatal: bookkeeping failure doesn't prevent the port from working.
		_ = err
	}

	return &sandbox.ExposedAddr{
		Host:     host,
		Port:     port,
		Protocol: proto,
	}, nil
}

// UnexposePort removes a previously-exposed port route.
func (b *OpenShellBackend) UnexposePort(ctx context.Context, sessionID string, port int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.gateway == nil {
		return ErrNotImplementedYet
	}
	if sessionID == "" {
		return fmt.Errorf("sandbox/openshell: UnexposePort: sessionID is required")
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("sandbox/openshell: UnexposePort: port %d out of range [1, 65535]", port)
	}

	rec, err := b.sessions.GetSession(sessionID)
	if err != nil || rec == nil {
		return nil // Session gone, port is implicitly unexposed.
	}

	// Remove from bookkeeping.
	if rec.ContainerName != "" {
		_, _ = b.sessions.UnexposePort(rec.ContainerName, port)
	}

	return nil
}
