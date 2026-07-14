package openshell

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/schardosin/astonish/pkg/sandbox"
	"github.com/schardosin/astonish/pkg/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// invalidLabelChars matches any character not valid in a Kubernetes label value.
// Label values must be alphanumeric, '-', '_', or '.'.
var invalidLabelChars = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

// sanitizeLabelValue strips characters that are invalid in Kubernetes label
// values. Labels allow only alphanumeric characters, '-', '_', and '.'.
// This is needed because Astonish uses '@' as a prefix for built-in template
// IDs (e.g., "@base") which is not a valid K8s label character.
func sanitizeLabelValue(v string) string {
	return invalidLabelChars.ReplaceAllString(v, "")
}

// isAlreadyExists returns true if the error (possibly wrapped) contains a
// gRPC AlreadyExists status code.
func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	if st, ok := status.FromError(err); ok {
		return st.Code() == codes.AlreadyExists
	}
	// The client wraps the gRPC error with fmt.Errorf("gateway CreateSandbox: %w", err).
	// status.FromError only works on the innermost error, so we try unwrapping.
	type unwrapper interface{ Unwrap() error }
	if u, ok := err.(unwrapper); ok {
		return isAlreadyExists(u.Unwrap())
	}
	return false
}

// isNotFound returns true if the error (possibly wrapped) contains a
// gRPC NotFound status code. Used by DestroySession to distinguish
// "sandbox already gone" (safe to remove record) from transient failures
// (record should be kept for retry by the reconciler).
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	if st, ok := status.FromError(err); ok {
		return st.Code() == codes.NotFound
	}
	type unwrapper interface{ Unwrap() error }
	if u, ok := err.(unwrapper); ok {
		return isNotFound(u.Unwrap())
	}
	return false
}

// CreateSession creates a new sandbox via the OpenShell Gateway and
// registers it in the session registry.
func (b *OpenShellBackend) CreateSession(ctx context.Context, spec sandbox.SessionSpec) (*sandbox.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.gateway == nil {
		return nil, ErrNotImplementedYet
	}

	// Default TemplateID to BaseTemplateID when not set. This is required
	// for the PG-backed session registry (template_id is NOT NULL).
	if spec.TemplateID == "" {
		spec.TemplateID = sandbox.BaseTemplateID
	}

	// Check idempotency: if session already exists, return it.
	if existing, err := b.sessions.GetSession(spec.SessionID); err == nil && existing != nil {
		return sessionFromStore(existing), nil
	}

	// Build the sandbox name.
	name := sandboxName(spec.SessionID)

	// Build env vars for the agent process inside the sandbox.
	env := map[string]string{
		"ASTONISH_SESSION_ID": spec.SessionID,
	}
	for k, v := range spec.Env {
		env[k] = v
	}

	// Build labels. Sanitize session ID for K8s label compliance (values
	// allow only [a-zA-Z0-9._-]). The primary fix is at session-key creation
	// (scheduler/executor.go); this is defense-in-depth.
	labels := map[string]string{
		"astonish.io/type":       "session",
		"astonish.io/session-id": sanitizeLabelValue(spec.SessionID),
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

	// Resolve the container image: prefer per-session override (from template),
	// fall back to the global config default.
	image := b.cfg.SandboxImage
	if spec.Image != "" {
		image = spec.Image
	}

	// Create the sandbox via the gateway.
	resp, err := b.gateway.CreateSandbox(ctx, CreateSandboxRequest{
		Name:   name,
		Image:  image,
		Env:    env,
		Labels: labels,
		Policy: defaultSandboxPolicy(b.cfg.AppConfig),
	})
	if err != nil {
		if !isAlreadyExists(err) {
			return nil, fmt.Errorf("openshell: create sandbox: %w", err)
		}
		// Sandbox already exists at the gateway (e.g., registry was cleared
		// but gateway retained it). Adopt the existing sandbox.
		st, getErr := b.gateway.GetSandboxStatus(ctx, name)
		if getErr != nil {
			return nil, fmt.Errorf("openshell: create sandbox: already exists and get failed: %w", getErr)
		}
		resp = &CreateSandboxResponse{
			SandboxID: name,
			GatewayID: st.GatewayID,
			PodName:   st.PodName,
		}
	}

	// Register the session in the store.
	// ContainerName stores the sandbox name (for Get/Delete/WaitForReady).
	// PodName stores the gateway UUID (for ExecSandbox).
	rec := &store.SandboxSession{
		SessionID:     spec.SessionID,
		ChatSessionID: spec.SessionID,
		Backend:       string(sandbox.BackendKindOpenShell),
		ContainerName: resp.SandboxID,
		PodName:       resp.GatewayID,
		TemplateID:    spec.TemplateID,
		Image:         image,
		State:         store.SandboxSessionStateCreating,
		CreatedAt:     time.Now().UTC(),
	}

	if err := b.sessions.PutSession(rec); err != nil {
		// Best-effort cleanup.
		_ = b.gateway.DeleteSandbox(ctx, resp.SandboxID)
		return nil, fmt.Errorf("openshell: register session: %w", err)
	}

	return sessionFromStore(rec), nil
}

// StartSession resumes a stopped/evicted session by creating a new sandbox.
func (b *OpenShellBackend) StartSession(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.gateway == nil {
		return ErrNotImplementedYet
	}

	rec, err := b.sessions.GetSession(sessionID)
	if err != nil {
		return fmt.Errorf("openshell: get session %s: %w", sessionID, err)
	}
	if rec == nil {
		return fmt.Errorf("openshell: session %s not found", sessionID)
	}

	// Already running or being created (CreateSession just succeeded)? No-op.
	if rec.State == store.SandboxSessionStateRunning || rec.State == store.SandboxSessionStateCreating {
		return nil
	}

	env := map[string]string{
		"ASTONISH_SESSION_ID": sessionID,
	}

	labels := map[string]string{
		"astonish.io/type":       "session",
		"astonish.io/session-id": sanitizeLabelValue(sessionID),
	}

	// Resolve the container image: prefer the image stored in the session
	// record (set at CreateSession time), fall back to the global config.
	image := b.cfg.SandboxImage
	if rec.Image != "" {
		image = rec.Image
	}

	resp, err := b.gateway.CreateSandbox(ctx, CreateSandboxRequest{
		Name:   sandboxName(sessionID),
		Image:  image,
		Env:    env,
		Labels: labels,
		Policy: defaultSandboxPolicy(b.cfg.AppConfig),
	})
	if err != nil {
		if !isAlreadyExists(err) {
			return fmt.Errorf("openshell: restart sandbox for session %s: %w", sessionID, err)
		}
		// Sandbox still exists at the gateway — adopt it.
		st, getErr := b.gateway.GetSandboxStatus(ctx, sandboxName(sessionID))
		if getErr != nil {
			return fmt.Errorf("openshell: restart sandbox for session %s: already exists and get failed: %w", sessionID, getErr)
		}
		resp = &CreateSandboxResponse{
			SandboxID: sandboxName(sessionID),
			GatewayID: st.GatewayID,
			PodName:   st.PodName,
		}
	}

	// Update session state and backend ref.
	rec.ContainerName = resp.SandboxID
	rec.PodName = resp.GatewayID
	rec.State = store.SandboxSessionStateCreating
	return b.sessions.PutSession(rec)
}

// StopSession stops the session by deleting the sandbox.
func (b *OpenShellBackend) StopSession(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	rec, err := b.sessions.GetSession(sessionID)
	if err != nil || rec == nil {
		return nil // Already gone.
	}

	sandboxID := rec.ContainerName
	if sandboxID == "" {
		return nil // No sandbox to stop.
	}

	if b.gateway == nil {
		return ErrNotImplementedYet
	}

	// Delete the sandbox.
	if err := b.gateway.DeleteSandbox(ctx, sandboxID); err != nil {
		return fmt.Errorf("openshell: delete sandbox for %s: %w", sessionID, err)
	}

	// Update session state.
	rec.State = store.SandboxSessionStateEvicted
	rec.ContainerName = ""
	rec.PodName = ""
	return b.sessions.PutSession(rec)
}

// DestroySession permanently removes the session and its data.
func (b *OpenShellBackend) DestroySession(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	rec, _ := b.sessions.GetSession(sessionID)

	if b.gateway == nil {
		if rec != nil {
			return ErrNotImplementedYet
		}
		// No gateway and no record — truly nothing to do.
		return nil
	}

	// Determine the sandbox name to delete. Prefer the stored container
	// name from the session record; fall back to deriving it from the
	// session ID. This handles the case where the session DB record was
	// already removed (e.g. chat session deleted first) but the pod is
	// still running in the gateway.
	name := ""
	if rec != nil {
		name = rec.ContainerName
	}
	if name == "" {
		name = sandboxName(sessionID)
	}

	// Delete the sandbox via the gateway.
	if err := b.gateway.DeleteSandbox(ctx, name); err != nil {
		// If the gateway says "not found", the sandbox is already gone —
		// proceed to remove the DB record. Any other error is transient;
		// keep the record alive so the periodic reconciler can retry.
		if !isNotFound(err) {
			if rec != nil {
				return fmt.Errorf("openshell: delete sandbox %s: %w", name, err)
			}
			// No DB record and gateway error — best-effort, nothing to
			// preserve for the reconciler.
			return nil
		}
	}

	// Remove the DB record if it exists.
	if rec != nil {
		return b.sessions.Remove(sessionID)
	}
	return nil
}

// SessionState queries the gateway for the current sandbox state.
func (b *OpenShellBackend) SessionState(ctx context.Context, sessionID string) (sandbox.SessionState, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if b.gateway == nil {
		return "", ErrNotImplementedYet
	}

	rec, err := b.sessions.GetSession(sessionID)
	if err != nil || rec == nil {
		return sandbox.SessionStateGone, nil
	}

	sandboxID := rec.ContainerName
	if sandboxID == "" {
		return storeStateToSessionState(rec.State), nil
	}

	status, err := b.gateway.GetSandboxStatus(ctx, sandboxID)
	if err != nil {
		// Gateway might not know about this sandbox anymore.
		return storeStateToSessionState(rec.State), nil
	}

	// Map gateway state to sandbox state.
	switch status.State {
	case SandboxStateRunning:
		return sandbox.SessionStateRunning, nil
	case SandboxStateCreating:
		return sandbox.SessionStateCreating, nil
	case SandboxStateStopped:
		return sandbox.SessionStateStopped, nil
	case SandboxStateFailed, SandboxStateGone:
		return sandbox.SessionStateGone, nil
	default:
		return storeStateToSessionState(rec.State), nil
	}
}

// WaitForSessionReady polls the gateway until the sandbox is running
// or the context expires.
func (b *OpenShellBackend) WaitForSessionReady(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.gateway == nil {
		return ErrNotImplementedYet
	}

	rec, err := b.sessions.GetSession(sessionID)
	if err != nil || rec == nil {
		return fmt.Errorf("openshell: session %s not found", sessionID)
	}

	sandboxID := rec.ContainerName
	if sandboxID == "" {
		return fmt.Errorf("openshell: session %s has no sandbox ID", sessionID)
	}

	// Poll every 2 seconds.
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			status, err := b.gateway.GetSandboxStatus(ctx, sandboxID)
			if err != nil {
				continue // Transient error, retry.
			}
			switch status.State {
			case SandboxStateRunning:
				// Update session state.
				rec.State = store.SandboxSessionStateRunning
				_ = b.sessions.PutSession(rec)
				return nil
			case SandboxStateFailed:
				return fmt.Errorf("openshell: sandbox %s failed: %s", sandboxID, status.Message)
			case SandboxStateGone:
				return fmt.Errorf("openshell: sandbox %s gone", sandboxID)
			}
			// Still creating, continue polling.
		}
	}
}

// ListSessions returns sessions matching the filter from the registry.
func (b *OpenShellBackend) ListSessions(ctx context.Context, filter sandbox.SessionFilter) ([]*sandbox.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Use the store's List via the registry, then convert to sandbox.Session.
	entries := b.sessions.List()
	out := make([]*sandbox.Session, 0, len(entries))
	for _, e := range entries {
		sess := &sandbox.Session{
			SessionID:  e.SessionID,
			TemplateID: e.TemplateName,
			BackendRef: e.ContainerName,
			CreatedAt:  e.CreatedAt,
		}
		// Apply filter.
		if filter.State != "" && sess.State != filter.State {
			continue
		}
		out = append(out, sess)
	}
	return out, nil
}

// --- Helpers ---

// sessionFromStore converts a store.SandboxSession to the public sandbox.Session.
func sessionFromStore(rec *store.SandboxSession) *sandbox.Session {
	if rec == nil {
		return nil
	}
	return &sandbox.Session{
		SessionID:  rec.SessionID,
		Type:       sandbox.SessionTypeChat,
		TemplateID: rec.TemplateID,
		State:      storeStateToSessionState(rec.State),
		BackendRef: rec.ContainerName, // Sandbox ID is stored in ContainerName.
		CreatedAt:  rec.CreatedAt,
		LastActive: rec.LastActiveAt,
	}
}

// storeStateToSessionState maps store session states to public session states.
func storeStateToSessionState(state store.SandboxSessionState) sandbox.SessionState {
	switch state {
	case store.SandboxSessionStateRunning:
		return sandbox.SessionStateRunning
	case store.SandboxSessionStateCreating:
		return sandbox.SessionStateCreating
	case store.SandboxSessionStateEvicting:
		return sandbox.SessionStateEvicting
	case store.SandboxSessionStateEvicted:
		return sandbox.SessionStateStopped
	case store.SandboxSessionStateResuming:
		return sandbox.SessionStateResuming
	case store.SandboxSessionStateTerminated:
		return sandbox.SessionStateGone
	default:
		return sandbox.SessionStateGone
	}
}

func sandboxName(sessionID string) string {
	const prefix = "astn-sess-"
	const maxIDLen = 27
	id := toDNSLabel(sessionID)
	if len(id) > maxIDLen {
		id = id[:maxIDLen]
	}
	// Re-trim trailing '-' in case truncation left one.
	for len(id) > 0 && id[len(id)-1] == '-' {
		id = id[:len(id)-1]
	}
	if id == "" {
		id = "x"
	}
	return prefix + id
}

// SandboxName returns the deterministic sandbox (pod) name for a given
// session ID. This is the canonical mapping used by the OpenShell backend
// when creating sandboxes.
func SandboxName(sessionID string) string {
	return sandboxName(sessionID)
}

// toDNSLabel lower-cases the input and replaces any character that is
// not [a-z0-9-] with '-'. Leading/trailing '-' are stripped. The result
// is guaranteed to be a valid DNS-1123 label fragment.
func toDNSLabel(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			out = append(out, c+('a'-'A'))
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			out = append(out, c)
		case c == '-':
			out = append(out, c)
		default:
			out = append(out, '-')
		}
	}
	for len(out) > 0 && out[0] == '-' {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	return string(out)
}
