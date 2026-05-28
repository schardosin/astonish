// Package sandbox — IncusBackend adapter (Phase B.2 deliverable 2).
//
// This file provides the IncusBackend type, which satisfies the Backend
// interface defined in backend.go by delegating to the existing Incus
// machinery (free functions in lifecycle.go, template.go, etc., plus
// methods on *IncusClient from pkg/sandbox/incus).
//
// Scope:
//   - Lifecycle, exec/IO, networking, fleet, and diagnostics methods are
//     implemented by routing into existing sandbox helpers.
//   - Template methods that depend on the Phase A layer-store / DAG model
//     (BuildTemplate, SaveSessionAsTemplate, RefreshTemplate with a
//     template-id argument) return ErrUnsupportedInPhaseB2 until Phase A
//     lands. DeleteTemplate is implemented against today's template name
//     model because the existing free function already exists.
//
// The adapter lives in pkg/sandbox/ (not pkg/sandbox/incus/) because it
// must call back into orchestration helpers (EnsureSessionContainer,
// DestroyForSession, CreateTemplate, etc.) that live in pkg/sandbox/.
// Placing it here avoids an import cycle: pkg/sandbox already imports
// pkg/sandbox/incus for *IncusClient.

package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/sandbox/incus"
)

// ErrUnsupportedInPhaseB2 is returned by backend methods whose semantics
// depend on the Phase A layer store / template DAG and cannot be mapped
// faithfully to today's single-template model. Callers MAY check for this
// error with errors.Is to gate feature rollout.
var ErrUnsupportedInPhaseB2 = errors.New("sandbox: operation not supported by IncusBackend in Phase B.2 (requires Phase A layer store)")

// IncusBackend is the Backend implementation backed by a running Incus
// daemon plus the existing pkg/sandbox orchestration helpers.
//
// Zero value is NOT usable; construct via NewIncusBackend.
type IncusBackend struct {
	client      *IncusClient
	sessions    *SessionRegistry
	templates   *TemplateRegistry
	defaultLims *config.SandboxLimits
}

// IncusBackendConfig bundles the dependencies required by IncusBackend.
// All fields are required.
type IncusBackendConfig struct {
	Client     *IncusClient
	Sessions   *SessionRegistry
	Templates  *TemplateRegistry
	DefaultLim *config.SandboxLimits // may be nil → backend defaults
}

// NewIncusBackend constructs an IncusBackend. Returns an error if any
// required field is nil.
func NewIncusBackend(cfg IncusBackendConfig) (*IncusBackend, error) {
	if cfg.Client == nil {
		return nil, errors.New("IncusBackend: Client is required")
	}
	if cfg.Sessions == nil {
		return nil, errors.New("IncusBackend: Sessions registry is required")
	}
	if cfg.Templates == nil {
		return nil, errors.New("IncusBackend: Templates registry is required")
	}
	return &IncusBackend{
		client:      cfg.Client,
		sessions:    cfg.Sessions,
		templates:   cfg.Templates,
		defaultLims: cfg.DefaultLim,
	}, nil
}

// Compile-time assertion: *IncusBackend satisfies Backend.
var _ Backend = (*IncusBackend)(nil)

// ---------------------------------------------------------------------------
// Diagnostics (§3.6)
// ---------------------------------------------------------------------------

// Kind returns the backend identifier.
func (b *IncusBackend) Kind() BackendKind { return BackendKindIncus }

// ServerArchitecture returns the architecture of the Incus server
// (normalized to "amd64" or "arm64"). This is the architecture of containers
// it will create.
func (b *IncusBackend) ServerArchitecture() string {
	arch, err := b.client.ServerArchitecture()
	if err != nil {
		return "amd64" // conservative fallback
	}
	switch arch {
	case "aarch64", "arm64":
		return "arm64"
	case "x86_64", "amd64":
		return "amd64"
	default:
		return "amd64"
	}
}

// Capabilities reports the feature flags of the Incus backend.
func (b *IncusBackend) Capabilities() BackendCapabilities {
	return BackendCapabilities{
		Kind:                 BackendKindIncus,
		SupportsLiveEvict:    false, // Incus keeps overlay mounted; no upper-tarball evict
		SupportsFastClone:    true,  // overlayfs fast-clone is the Incus path
		SupportsPortExpose:   true,
		SupportsOrgIsolation: true, // per-org bridge + profile
	}
}

// Health performs a lightweight connectivity check against the Incus daemon.
func (b *IncusBackend) Health(ctx context.Context) (*BackendHealth, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	now := time.Now().UTC()
	info, err := b.client.GetServerInfo()
	if err != nil {
		return &BackendHealth{
			Healthy:   false,
			Reason:    fmt.Sprintf("incus daemon unreachable: %v", err),
			CheckedAt: now,
		}, nil
	}
	details := map[string]string{}
	if info != nil {
		details["api_status"] = info.APIStatus
		details["api_version"] = info.APIVersion
		details["server_name"] = info.Environment.ServerName
	}
	return &BackendHealth{
		Healthy:   true,
		CheckedAt: now,
		Details:   details,
	}, nil
}

// ---------------------------------------------------------------------------
// Session lifecycle (§3.1)
// ---------------------------------------------------------------------------

// CreateSession materialises a new sandbox. Idempotent on SessionID.
func (b *IncusBackend) CreateSession(ctx context.Context, spec SessionSpec) (*Session, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if spec.SessionID == "" {
		return nil, errors.New("CreateSession: SessionID is required")
	}
	if spec.TemplateID == "" {
		spec.TemplateID = BaseTemplateID
	}

	limits := b.mapLimits(spec.Limits)

	containerName, err := EnsureOrgSessionContainer(
		b.client, b.sessions, b.templates,
		spec.SessionID, spec.TemplateID, limits,
		spec.OrgSlug, spec.TeamSlug,
	)
	if err != nil {
		return nil, fmt.Errorf("CreateSession(%s): %w", spec.SessionID, err)
	}

	state := b.observeState(containerName)
	return &Session{
		SessionID:  spec.SessionID,
		Type:       defaultSessionType(spec.Type),
		TemplateID: spec.TemplateID,
		OrgSlug:    spec.OrgSlug,
		TeamSlug:   spec.TeamSlug,
		State:      state,
		BackendRef: containerName,
		Labels:     spec.Labels,
		CreatedAt:  time.Now().UTC(),
	}, nil
}

// StartSession resumes a stopped session.
func (b *IncusBackend) StartSession(ctx context.Context, sessionID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	entry := b.sessions.Get(sessionID)
	if entry == nil {
		return fmt.Errorf("StartSession: session %q not found", sessionID)
	}
	if b.client.IsRunning(entry.ContainerName) {
		return nil // idempotent no-op
	}
	// Re-mount overlay if needed, then start.
	if err := EnsureOverlayMounted(b.client, entry.ContainerName, entry.TemplateName, b.templates); err != nil {
		return fmt.Errorf("StartSession(%s): remount overlay: %w", sessionID, err)
	}
	if err := b.client.StartInstance(entry.ContainerName); err != nil {
		return fmt.Errorf("StartSession(%s): %w", sessionID, err)
	}
	return nil
}

// StopSession pauses a running session without destroying its data.
func (b *IncusBackend) StopSession(ctx context.Context, sessionID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	entry := b.sessions.Get(sessionID)
	if entry == nil {
		return nil // idempotent: nothing to stop
	}
	if !b.client.IsRunning(entry.ContainerName) {
		return nil
	}
	if err := b.client.StopInstance(entry.ContainerName, false); err != nil {
		return fmt.Errorf("StopSession(%s): %w", sessionID, err)
	}
	return nil
}

// DestroySession permanently removes a session and its writable layer.
func (b *IncusBackend) DestroySession(ctx context.Context, sessionID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := DestroyForSession(b.client, b.sessions, sessionID); err != nil {
		return fmt.Errorf("DestroySession(%s): %w", sessionID, err)
	}
	return nil
}

// SessionState returns the current backend-observed state of the session.
func (b *IncusBackend) SessionState(ctx context.Context, sessionID string) (SessionState, error) {
	if ctx.Err() != nil {
		return SessionStateGone, ctx.Err()
	}
	entry := b.sessions.Get(sessionID)
	if entry == nil {
		return SessionStateGone, nil
	}
	return b.observeState(entry.ContainerName), nil
}

// WaitForSessionReady polls until the session's container is Running.
// On Incus this is nearly instant since CreateSession starts the container
// synchronously, but we poll for robustness.
func (b *IncusBackend) WaitForSessionReady(ctx context.Context, sessionID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	entry := b.sessions.Get(sessionID)
	if entry == nil {
		return fmt.Errorf("WaitForSessionReady: session %q not found", sessionID)
	}
	// Fast path: already running
	if b.client.IsRunning(entry.ContainerName) {
		return nil
	}
	// Poll with backoff (should rarely be needed for Incus)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("WaitForSessionReady(%s): %w", sessionID, ctx.Err())
		case <-ticker.C:
			if b.client.IsRunning(entry.ContainerName) {
				return nil
			}
		}
	}
}

// ListSessions returns sessions matching the filter.
func (b *IncusBackend) ListSessions(ctx context.Context, filter SessionFilter) ([]*Session, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	entries := b.sessions.List()
	out := make([]*Session, 0, len(entries))
	for _, e := range entries {
		// Derive session type from container-name prefix.
		sType := SessionTypeChat
		if strings.HasPrefix(e.ContainerName, FleetPrefix) {
			sType = SessionTypeFleet
		}
		if filter.Type != "" && filter.Type != sType {
			continue
		}
		state := b.observeState(e.ContainerName)
		if filter.State != "" && filter.State != state {
			continue
		}
		s := &Session{
			SessionID:  e.SessionID,
			Type:       sType,
			TemplateID: e.TemplateName,
			State:      state,
			BackendRef: e.ContainerName,
		}
		// Org/team filters cannot be applied without registry support for
		// those fields; skip those filters for today's registry.
		if filter.OrgSlug != "" || filter.TeamSlug != "" {
			// Best-effort: today's SessionEntry has no org/team; we return
			// the entry unconditionally so the caller at least sees it.
		}
		out = append(out, s)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Exec and file I/O (§3.2)
// ---------------------------------------------------------------------------

// Exec runs a command non-interactively and captures stdout/stderr + exit.
func (b *IncusBackend) Exec(ctx context.Context, sessionID string, opts ExecSpec) (*ExecResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	entry := b.sessions.Get(sessionID)
	if entry == nil {
		return nil, fmt.Errorf("Exec: session %q not found", sessionID)
	}
	if len(opts.Command) == 0 {
		return nil, errors.New("Exec: Command is required")
	}

	// Use ExecNonInteractive so we get separate stdout/stderr plumbing.
	proc, err := incus.ExecNonInteractive(b.client, entry.ContainerName, opts.Command, incus.ExecOpts{
		WorkDir: opts.WorkDir,
		Env:     opts.Env,
	})
	if err != nil {
		return nil, fmt.Errorf("Exec(%s): %w", sessionID, err)
	}
	defer proc.Close()

	// Optional stdin.
	if opts.Stdin != nil {
		go func() {
			_, _ = io.Copy(proc.Stdin, opts.Stdin)
			_ = proc.Stdin.Close()
		}()
	} else {
		_ = proc.Stdin.Close()
	}

	// Read all stdout (stderr is merged by ExecNonInteractive when
	// SeparateStderr is nil; see pkg/sandbox/incus/exec.go).
	outBytes, readErr := io.ReadAll(proc.Stdout)
	code, waitErr := proc.Wait()
	if waitErr != nil {
		return nil, fmt.Errorf("Exec(%s): wait: %w", sessionID, waitErr)
	}
	if readErr != nil {
		return nil, fmt.Errorf("Exec(%s): read stdout: %w", sessionID, readErr)
	}
	return &ExecResult{
		ExitCode: code,
		Stdout:   outBytes,
		Stderr:   nil, // merged into Stdout
	}, nil
}

// ExecInteractive starts a PTY-attached process.
func (b *IncusBackend) ExecInteractive(ctx context.Context, sessionID string, opts PTYSpec) (ExecStream, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	entry := b.sessions.Get(sessionID)
	if entry == nil {
		return nil, fmt.Errorf("ExecInteractive: session %q not found", sessionID)
	}
	if len(opts.Command) == 0 {
		return nil, errors.New("ExecInteractive: Command is required")
	}
	proc, err := incus.ExecInteractive(b.client, entry.ContainerName, opts.Command, incus.ExecOpts{
		WorkDir:        opts.WorkDir,
		Env:            opts.Env,
		Rows:           opts.Rows,
		Cols:           opts.Cols,
		SeparateStderr: opts.SeparateStderr,
	})
	if err != nil {
		return nil, fmt.Errorf("ExecInteractive(%s): %w", sessionID, err)
	}
	return &incusExecStream{proc: proc}, nil
}

// ExecStreaming starts a non-interactive streaming process (no PTY).
func (b *IncusBackend) ExecStreaming(ctx context.Context, sessionID string, opts ExecStreamSpec) (ExecStream, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	entry := b.sessions.Get(sessionID)
	if entry == nil {
		return nil, fmt.Errorf("ExecStreaming: session %q not found", sessionID)
	}
	if len(opts.Command) == 0 {
		return nil, errors.New("ExecStreaming: Command is required")
	}
	proc, err := incus.ExecNonInteractive(b.client, entry.ContainerName, opts.Command, incus.ExecOpts{
		WorkDir:        opts.WorkDir,
		Env:            opts.Env,
		SeparateStderr: opts.SeparateStderr,
	})
	if err != nil {
		return nil, fmt.Errorf("ExecStreaming(%s): %w", sessionID, err)
	}
	return &incusExecStream{proc: proc}, nil
}

// PushFile writes content to a path inside the sandbox.
func (b *IncusBackend) PushFile(ctx context.Context, sessionID, path string, content io.Reader, mode os.FileMode) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	entry := b.sessions.Get(sessionID)
	if entry == nil {
		return fmt.Errorf("PushFile: session %q not found", sessionID)
	}
	// IncusClient.PushFile requires io.ReadSeeker. Buffer the reader.
	buf, err := io.ReadAll(content)
	if err != nil {
		return fmt.Errorf("PushFile(%s): read source: %w", sessionID, err)
	}
	return b.client.PushFile(entry.ContainerName, path, newByteSeeker(buf), int(mode.Perm()))
}

// PullFile reads a file from the sandbox.
func (b *IncusBackend) PullFile(ctx context.Context, sessionID, path string) (io.ReadCloser, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	entry := b.sessions.Get(sessionID)
	if entry == nil {
		return nil, fmt.Errorf("PullFile: session %q not found", sessionID)
	}
	rc, _, err := b.client.PullFile(entry.ContainerName, path)
	if err != nil {
		return nil, fmt.Errorf("PullFile(%s): %w", sessionID, err)
	}
	return rc, nil
}

// ---------------------------------------------------------------------------
// Templates (§3.3)
// ---------------------------------------------------------------------------

// BuildTemplate creates a new template layer by running build steps directly
// inside the @base template container and re-snapshotting it. The snapshot's
// rootfs is then hashed to produce a content-addressed layer artifact for the
// platform's layer store.
//
// Unlike the K8s backend (which uses a throwaway overlay builder pod), the
// Incus backend modifies @base in-place because:
//   - Incus sessions use @base's snapshot as the overlay lower layer
//   - The snapshot IS the source of truth (no CephFS layer DAG)
//   - Running steps directly avoids the complexity of merging overlays
//
// If any step fails, @base is left in a potentially modified but un-snapshotted
// state. The caller should handle this (e.g., re-init sandbox).
func (b *IncusBackend) BuildTemplate(ctx context.Context, spec TemplateBuildSpec) (*TemplateArtifact, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if spec.TemplateID == "" {
		return nil, errors.New("BuildTemplate: TemplateID is required")
	}
	if len(spec.Steps) == 0 {
		return nil, errors.New("BuildTemplate: at least one build step is required")
	}

	// Verify @base exists.
	baseName := TemplateName(BaseTemplate)
	if !b.client.InstanceExists(baseName) {
		return nil, fmt.Errorf("BuildTemplate: base template does not exist; run 'astonish sandbox init' first")
	}

	// Start @base directly (it may already be stopped from a previous snapshot).
	if !b.client.IsRunning(baseName) {
		if err := b.client.StartInstance(baseName); err != nil {
			return nil, fmt.Errorf("BuildTemplate: start @base: %w", err)
		}
	}
	if err := waitForReady(b.client, baseName, 60*time.Second); err != nil {
		return nil, fmt.Errorf("BuildTemplate: @base not ready: %w", err)
	}

	// Wait for network connectivity (DHCP + DNS).
	if err := waitForNetwork(b.client, baseName, 30*time.Second); err != nil {
		return nil, fmt.Errorf("BuildTemplate: network not ready: %w", err)
	}

	// Execute build steps sequentially inside @base.
	// Use ExecWithOutput so we capture the real stderr/stdout (critical for
	// diagnosing apt-get failures, pip errors, etc. during base template builds).
	for i, step := range spec.Steps {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		exitCode, output, err := incus.ExecWithOutput(b.client, baseName, []string{"/bin/sh", "-c", step})
		if err != nil {
			return nil, fmt.Errorf("BuildTemplate: step %d (%q): %w\nOutput:\n%s", i+1, truncate(step, 120), err, output)
		}
		if exitCode != 0 {
			return nil, fmt.Errorf("BuildTemplate: step %d (%q) exited with code %d\nOutput:\n%s", i+1, truncate(step, 120), exitCode, output)
		}
	}

	// Stop @base and re-snapshot (SnapshotTemplate handles stop, shift, snapshot).
	if err := SnapshotTemplate(b.client, b.templates, BaseTemplate); err != nil {
		return nil, fmt.Errorf("BuildTemplate: re-snapshot @base: %w", err)
	}

	// Compute a content hash of the snapshot rootfs for the layer artifact.
	// We hash the snapshot directory listing (fast, deterministic) rather than
	// tar-ing the entire multi-GB rootfs.
	poolName, err := GetPoolForProfile(b.client)
	if err != nil {
		return nil, fmt.Errorf("BuildTemplate: get storage pool: %w", err)
	}
	poolPath, err := GetPoolSourcePath(b.client, poolName)
	if err != nil {
		return nil, fmt.Errorf("BuildTemplate: get pool path: %w", err)
	}
	snapshotRootfs := SnapshotRootfsPath(poolPath, BaseTemplate)

	artifact, err := hashSnapshotRootfs(snapshotRootfs)
	if err != nil {
		return nil, fmt.Errorf("BuildTemplate: hash snapshot: %w", err)
	}

	artifact.CephFSPath = snapshotRootfs
	if len(spec.ParentLayers) > 0 {
		artifact.ParentLayer = spec.ParentLayers[len(spec.ParentLayers)-1]
	}

	return artifact, nil
}

// hashSnapshotRootfs computes a fast content hash of the @base snapshot rootfs.
// Instead of tar-ing the entire multi-GB rootfs (which would be slow), we hash
// a listing of file paths + sizes + mtimes. This gives a stable identifier that
// changes whenever the rootfs content changes.
func hashSnapshotRootfs(snapshotRootfs string) (*TemplateArtifact, error) {
	// Use find + stat to produce a deterministic listing, then SHA-256 it.
	// This is fast (~1s for a full OS rootfs) and produces a reproducible hash.
	script := fmt.Sprintf(`set -e
ROOTFS=%q
if [ ! -d "$ROOTFS" ]; then
  echo "SHA=0000000000000000000000000000000000000000000000000000000000000000"
  echo "SIZE=0"
  exit 0
fi
SHA=$(find "$ROOTFS" -printf '%%P %%s %%T@\n' 2>/dev/null | LC_ALL=C sort | sha256sum | awk '{print $1}')
SIZE=$(du -sb "$ROOTFS" | awk '{print $1}')
echo "SHA=$SHA"
echo "SIZE=$SIZE"
`, snapshotRootfs)

	out, err := incus.ExecOnSandboxHost([]string{"sh", "-c", script})
	if err != nil {
		return nil, fmt.Errorf("hash script failed: %w (output: %s)", err, string(out))
	}

	sha, size, err := parseIncusCaptureOutput(string(out))
	if err != nil {
		return nil, fmt.Errorf("parse hash output: %w (output: %s)", err, string(out))
	}

	return &TemplateArtifact{
		LayerID:   sha,
		SizeBytes: size,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// SaveSessionAsTemplate captures the upper layer of a running session.
// Requires Phase A layer store; returns ErrUnsupportedInPhaseB2 today.
func (b *IncusBackend) SaveSessionAsTemplate(ctx context.Context, sessionID string) (*TemplateArtifact, error) {
	return nil, ErrUnsupportedInPhaseB2
}

// RefreshTemplate re-runs the template's build steps. The existing free
// function RefreshTemplate takes a template NAME, which matches the
// Backend.RefreshTemplate templateID argument in today's single-tenant
// model. Returns no artifact because today's path does not produce one
// (the Phase A model does).
func (b *IncusBackend) RefreshTemplate(ctx context.Context, templateID string) (*TemplateArtifact, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if templateID == "" {
		return nil, errors.New("RefreshTemplate: templateID is required")
	}
	if err := RefreshTemplate(b.client, b.templates, templateID); err != nil {
		return nil, fmt.Errorf("RefreshTemplate(%s): %w", templateID, err)
	}
	// No TemplateArtifact in today's model; return a synthetic record that
	// callers MAY ignore. The LayerID is the template name to give callers
	// a stable identifier; Phase A will replace this with a real sha256.
	return &TemplateArtifact{
		LayerID:   templateID,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// DeleteTemplate removes a template.
func (b *IncusBackend) DeleteTemplate(ctx context.Context, templateID string, force bool) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if templateID == "" {
		return errors.New("DeleteTemplate: templateID is required")
	}
	// Today's DeleteTemplate does not take a force flag — it unconditionally
	// deletes if the template exists. We log the force flag for forward
	// compatibility but cannot honor it until Phase A ref-counting lands.
	_ = force
	if err := DeleteTemplate(b.client, b.templates, templateID); err != nil {
		return fmt.Errorf("DeleteTemplate(%s): %w", templateID, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Networking (§3.4)
// ---------------------------------------------------------------------------

// EnsureOrgNetwork provisions the org-scoped network primitives.
func (b *IncusBackend) EnsureOrgNetwork(ctx context.Context, orgSlug string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if orgSlug == "" {
		return errors.New("EnsureOrgNetwork: orgSlug is required")
	}
	if err := EnsureOrgNetwork(b.client, orgSlug); err != nil {
		return fmt.Errorf("EnsureOrgNetwork(%s): %w", orgSlug, err)
	}
	return nil
}

// DeleteOrgNetwork removes org-scoped network primitives.
func (b *IncusBackend) DeleteOrgNetwork(ctx context.Context, orgSlug string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if orgSlug == "" {
		return errors.New("DeleteOrgNetwork: orgSlug is required")
	}
	// Existing free function returns no error (best-effort cleanup).
	DeleteOrgNetwork(b.client, orgSlug)
	return nil
}

// ExposePort opens an inbound route to a port inside the session container.
// Port exposure state is tracked by SessionRegistry; the actual Incus proxy
// device is NOT created here because today's code uses a separate ingress
// path (SessionRegistry.ExposePort records the intent; the proxy wiring
// lives in pkg/api). For Phase B.2 we record the intent and return a
// loopback address.
func (b *IncusBackend) ExposePort(ctx context.Context, sessionID string, port int, proto string) (*ExposedAddr, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	entry := b.sessions.Get(sessionID)
	if entry == nil {
		return nil, fmt.Errorf("ExposePort: session %q not found", sessionID)
	}
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("ExposePort: invalid port %d", port)
	}
	if _, err := b.sessions.ExposePort(entry.ContainerName, port); err != nil {
		return nil, fmt.Errorf("ExposePort(%s): %w", sessionID, err)
	}
	host, err := b.client.GetContainerIPv4(entry.ContainerName)
	if err != nil || host == "" {
		host = "127.0.0.1"
	}
	scheme := strings.ToLower(proto)
	if scheme == "" {
		scheme = "tcp"
	}
	return &ExposedAddr{
		Host:     host,
		Port:     port,
		Protocol: scheme,
	}, nil
}

// UnexposePort closes a previously-exposed port.
func (b *IncusBackend) UnexposePort(ctx context.Context, sessionID string, port int) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	entry := b.sessions.Get(sessionID)
	if entry == nil {
		return nil // idempotent
	}
	if _, err := b.sessions.UnexposePort(entry.ContainerName, port); err != nil {
		return fmt.Errorf("UnexposePort(%s): %w", sessionID, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Fleet (§3.5)
// ---------------------------------------------------------------------------

// EnsureFleetContainer creates (if absent) a long-running fleet container.
// Maps to today's EnsureOrgSessionContainer with a fleet-scoped session ID.
// The FleetKey becomes the session ID; the template ID is the template name.
func (b *IncusBackend) EnsureFleetContainer(ctx context.Context, spec FleetSpec) (*Session, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if spec.FleetKey == "" {
		return nil, errors.New("EnsureFleetContainer: FleetKey is required")
	}
	if spec.TemplateID == "" {
		spec.TemplateID = BaseTemplateID
	}

	limits := b.mapLimits(spec.Limits)

	// Treat FleetKey as the session ID for the underlying container.
	containerName, err := EnsureOrgSessionContainer(
		b.client, b.sessions, b.templates,
		spec.FleetKey, spec.TemplateID, limits,
		spec.OrgSlug, spec.TeamSlug,
	)
	if err != nil {
		return nil, fmt.Errorf("EnsureFleetContainer(%s): %w", spec.FleetKey, err)
	}
	return &Session{
		SessionID:  spec.FleetKey,
		Type:       SessionTypeFleet,
		TemplateID: spec.TemplateID,
		OrgSlug:    spec.OrgSlug,
		TeamSlug:   spec.TeamSlug,
		State:      b.observeState(containerName),
		BackendRef: containerName,
		Labels:     spec.Labels,
		CreatedAt:  time.Now().UTC(),
	}, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// mapLimits converts the Backend-neutral ResourceLimits into the
// config.SandboxLimits shape understood by today's orchestration helpers.
// If the caller didn't specify any limit, returns the default configured
// at construction time (which may also be nil → incus built-in defaults).
func (b *IncusBackend) mapLimits(rl ResourceLimits) *config.SandboxLimits {
	if rl == (ResourceLimits{}) {
		return b.defaultLims
	}
	out := &config.SandboxLimits{
		CPU:       rl.CPUs,
		Processes: rl.PIDs,
	}
	if rl.MemoryMiB > 0 {
		out.Memory = fmt.Sprintf("%dMB", rl.MemoryMiB)
	}
	return out
}

// observeState maps an Incus instance status to a SessionState value.
func (b *IncusBackend) observeState(containerName string) SessionState {
	if !b.client.InstanceExists(containerName) {
		return SessionStateGone
	}
	state, err := b.client.GetInstanceState(containerName)
	if err != nil || state == nil {
		return SessionStateGone
	}
	switch strings.ToLower(state.Status) {
	case "running":
		return SessionStateRunning
	case "stopped":
		return SessionStateStopped
	case "starting":
		return SessionStateResuming
	case "stopping":
		return SessionStateEvicting
	default:
		return SessionStateCreating
	}
}

// defaultSessionType returns SessionTypeChat when the caller-supplied type
// is empty, preserving today's implicit default.
func defaultSessionType(t SessionType) SessionType {
	if t == "" {
		return SessionTypeChat
	}
	return t
}

// byteSeeker is a minimal io.ReadSeeker over a []byte, used by PushFile
// since IncusClient.PushFile requires a seekable source.
type byteSeeker struct {
	buf []byte
	pos int64
}

func newByteSeeker(b []byte) *byteSeeker { return &byteSeeker{buf: b} }

func (s *byteSeeker) Read(p []byte) (int, error) {
	if s.pos >= int64(len(s.buf)) {
		return 0, io.EOF
	}
	n := copy(p, s.buf[s.pos:])
	s.pos += int64(n)
	return n, nil
}

func (s *byteSeeker) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = s.pos + offset
	case io.SeekEnd:
		abs = int64(len(s.buf)) + offset
	default:
		return 0, fmt.Errorf("byteSeeker.Seek: invalid whence %d", whence)
	}
	if abs < 0 {
		return 0, fmt.Errorf("byteSeeker.Seek: negative position %d", abs)
	}
	s.pos = abs
	return abs, nil
}

// incusExecStream adapts *incus.ContainerProcess to the ExecStream interface.
type incusExecStream struct {
	proc *incus.ContainerProcess
}

func (s *incusExecStream) Read(p []byte) (int, error)  { return s.proc.Stdout.Read(p) }
func (s *incusExecStream) Write(p []byte) (int, error) { return s.proc.Stdin.Write(p) }
func (s *incusExecStream) Resize(rows, cols int) error { return s.proc.Resize(cols, rows) }
func (s *incusExecStream) Wait() (int, error)          { return s.proc.Wait() }
func (s *incusExecStream) Close() error                { return s.proc.Close() }

// ---------------------------------------------------------------------------
// BuildTemplate helpers
// ---------------------------------------------------------------------------

// waitForNetwork polls the container until DNS resolution works or the timeout
// expires. This is needed because overlay containers start almost instantly but
// DHCP and DNS resolver setup happen asynchronously after boot.
func waitForNetwork(client *IncusClient, containerName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// Try DNS resolution — a reliable indicator of full network readiness.
		exitCode, err := client.ExecSimple(containerName, []string{
			"sh", "-c", "getent hosts deb.debian.org >/dev/null 2>&1 || getent hosts archive.ubuntu.com >/dev/null 2>&1",
		})
		if err == nil && exitCode == 0 {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for network in container %q", containerName)
}

// parseIncusCaptureOutput extracts SHA= and SIZE= lines from the capture
// script's stdout. Same protocol as K8s captureUpperAsLayer.
func parseIncusCaptureOutput(output string) (sha string, size int64, err error) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "SHA="):
			sha = strings.TrimPrefix(line, "SHA=")
		case strings.HasPrefix(line, "SIZE="):
			v := strings.TrimPrefix(line, "SIZE=")
			n, perr := fmt.Sscanf(v, "%d", &size)
			if perr != nil || n != 1 {
				return "", 0, fmt.Errorf("SIZE= is not an integer: %q", v)
			}
		}
	}
	if sha == "" {
		return "", 0, errors.New("SHA= line missing from capture output")
	}
	if len(sha) != 64 {
		return "", 0, fmt.Errorf("SHA= value %q is not a valid 64-char hex hash", sha)
	}
	return sha, size, nil
}
