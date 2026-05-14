// Package k8s — pod lifecycle helpers (Phase C, skeleton slice).
//
// This file will hold the pod-lifecycle implementation: materialising a
// *sandbox.SessionSpec into a corev1.Pod manifest, creating it against
// the API server with RuntimeClass=sysbox-runc, mounting the layer chain
// plus the writable upper (fresh or restored from CephFS), watching the
// pod to Ready, and tearing it down via streamed tar-over-exec eviction.
//
// The skeleton slice intentionally keeps this file nearly empty: the
// behaviour lives on K8sBackend in backend.go and currently returns
// ErrNotImplementedYet. This file is reserved so the first real slice
// can add code here without a surprise review of a brand-new file.
//
// Planned shape (not yet implemented):
//
//   type podBuilder struct { ... }
//   func (b *K8sBackend) buildPodSpec(spec sandbox.SessionSpec) (*corev1.Pod, error)
//   func (b *K8sBackend) createPod(ctx context.Context, pod *corev1.Pod) error
//   func (b *K8sBackend) waitForPodReady(ctx context.Context, name string) error
//   func (b *K8sBackend) evictPod(ctx context.Context, name string) error
//
// References:
//   - docs/architecture/sandbox-backends.md §3.1 (lifecycle),
//     §5.5 (eviction), §5.12 (overlay layer chain),
//     §6 (performance targets: <5 s SaveSessionAsTemplate p95).
//   - sandbox.Session / sandbox.SessionSpec in pkg/sandbox/backend.go.

package k8s

// podNameForSession returns the deterministic pod name used by the K8s
// backend. Exposed here (rather than inlined) so that session.go,
// exec.go, files.go, and fleet.go agree on the naming scheme.
//
// The scheme is "astn-" + the first 32 chars of the session ID
// (lowercased) to stay well under Kubernetes' 253-char DNS-1123 limit
// while remaining trivially reversible for operators reading kubectl
// output.
func podNameForSession(sessionID string) string {
	const prefix = "astn-"
	const maxIDLen = 32
	if len(sessionID) > maxIDLen {
		sessionID = sessionID[:maxIDLen]
	}
	return prefix + toDNSLabel(sessionID)
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
	// Strip leading/trailing '-'.
	for len(out) > 0 && out[0] == '-' {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	return string(out)
}
