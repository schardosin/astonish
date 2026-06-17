package openshell

import (
	"context"
	"fmt"
	"time"

	"github.com/schardosin/astonish/pkg/sandbox"
	"github.com/schardosin/astonish/pkg/store"
)

// ---------------------------------------------------------------------------
// Fleet
// ---------------------------------------------------------------------------
//
// Fleet containers are pre-warmed sandboxes that can be instantly assigned
// to incoming sessions. The OpenShell backend's fleet implementation creates
// sandboxes via the gateway with fleet-specific labels and registers them
// in the session registry as SessionTypeFleet.

// EnsureFleetContainer creates or returns an existing pre-warmed fleet
// sandbox matching the given FleetSpec.
func (b *OpenShellBackend) EnsureFleetContainer(ctx context.Context, spec sandbox.FleetSpec) (*sandbox.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.gateway == nil {
		return nil, ErrNotImplementedYet
	}
	if spec.FleetKey == "" {
		return nil, fmt.Errorf("sandbox/openshell: EnsureFleetContainer: FleetKey is required")
	}

	// Check if a fleet container with this key already exists.
	existing, err := b.sessions.GetSession(spec.FleetKey)
	if err == nil && existing != nil && existing.ContainerName != "" {
		// Verify it's still alive.
		status, err := b.gateway.GetSandboxStatus(ctx, existing.ContainerName)
		if err == nil && (status.State == SandboxStateRunning || status.State == SandboxStateCreating) {
			sess := sessionFromStore(existing)
			sess.Type = sandbox.SessionTypeFleet
			sess.OrgSlug = spec.OrgSlug
			sess.TeamSlug = spec.TeamSlug
			return sess, nil
		}
		// Sandbox is gone — clean up and recreate.
		_ = b.sessions.Remove(spec.FleetKey)
	}

	// Build sandbox name with fleet prefix.
	name := fmt.Sprintf("astn-fleet-%s", truncateID(spec.FleetKey, 8))

	env := map[string]string{
		"ASTONISH_SESSION_ID": spec.FleetKey,
	}

	labels := map[string]string{
		"astonish.io/type":       "fleet",
		"astonish.io/fleet-key":  spec.FleetKey,
		"astonish.io/session-id": spec.FleetKey,
	}
	if spec.OrgSlug != "" {
		labels["astonish.io/org"] = spec.OrgSlug
	}
	if spec.TeamSlug != "" {
		labels["astonish.io/team"] = spec.TeamSlug
	}
	if spec.TemplateID != "" {
		labels["astonish.io/template"] = sanitizeLabelValue(spec.TemplateID)
	}
	for k, v := range spec.Labels {
		labels[k] = v
	}

	resp, err := b.gateway.CreateSandbox(ctx, CreateSandboxRequest{
		Name:   name,
		Image:  b.cfg.SandboxImage,
		Env:    env,
		Labels: labels,
		Policy: defaultSandboxPolicy(),
	})
	if err != nil {
		return nil, fmt.Errorf("sandbox/openshell: EnsureFleetContainer(%s): create: %w", spec.FleetKey, err)
	}

	// Register in session store.
	templateID := spec.TemplateID
	if templateID == "" {
		templateID = "@base"
	}

	rec := &store.SandboxSession{
		SessionID:     spec.FleetKey,
		ChatSessionID: spec.FleetKey,
		Backend:       string(sandbox.BackendKindOpenShell),
		ContainerName: resp.SandboxID,
		PodName:       resp.GatewayID,
		TemplateID:    templateID,
		State:         store.SandboxSessionStateCreating,
		CreatedAt:     time.Now().UTC(),
	}

	if err := b.sessions.PutSession(rec); err != nil {
		// Best-effort cleanup.
		_ = b.gateway.DeleteSandbox(ctx, resp.SandboxID)
		return nil, fmt.Errorf("sandbox/openshell: EnsureFleetContainer(%s): register: %w", spec.FleetKey, err)
	}

	sess := sessionFromStore(rec)
	sess.Type = sandbox.SessionTypeFleet
	sess.OrgSlug = spec.OrgSlug
	sess.TeamSlug = spec.TeamSlug

	return sess, nil
}

// truncateID returns the first n characters of s.
func truncateID(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
