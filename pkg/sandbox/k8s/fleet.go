// Package k8s — fleet container operations (§3.5, §5.8).
//
// Fleet containers are long-running worker pods provisioned from a
// team-scoped fleet template. They share the session pod shape (same
// image, same volumes, same RuntimeClass) but carry the label
// astonish.io/type=fleet so operators and NetworkPolicies can
// distinguish them, and they use a dedicated `astn-fleet-` name prefix.
//
// EnsureFleetContainer is the sole entry point and is **idempotent by
// FleetKey**: repeated calls with the same FleetKey return the existing
// session without re-creating the pod. This is essential for the fleet
// controller's hot-path reconciliation loop (§3.5: "MUST be idempotent
// and cheap on the hot path").
//
// Lifecycle handoff: once created, a fleet pod is treated exactly like
// a session pod — DestroySession, SessionState, Exec, ExecInteractive,
// PushFile, PullFile all work against it unchanged. The fleet-specific
// controller is free to tear it down via DestroySession when the
// fleet plan retires the instance.

package k8s

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/schardosin/astonish/pkg/sandbox"
	"github.com/schardosin/astonish/pkg/store"
)

// labelFleetKey records the caller-supplied FleetKey on the pod so
// operators can reverse-lookup without consulting the registry:
//
//     kubectl -n astonish-sandboxes get pods \
//       -l astonish.io/type=fleet,astonish.io/fleet-key=<key>
//
// Distinct from labelSessionID so ListSessions selectors remain
// unambiguous across session / fleet pods.
const labelFleetKey = "astonish.io/fleet-key"

// fleetPodNamePrefix is the stable prefix for fleet pods. Matches the
// spec's "astn-fleet-<plan>-<instance>" naming (§5.8), with FleetKey
// playing the role of the "<plan>-<instance>" composite.
const fleetPodNamePrefix = "astn-fleet-"

// podNameForFleet returns the deterministic pod name used for a
// FleetKey. Mirrors podNameForSession's shape so the two prefixes
// share their DNS-1123 discipline.
func podNameForFleet(fleetKey string) string {
	const maxKeyLen = 27
	clean := toDNSLabel(fleetKey)
	if len(clean) > maxKeyLen {
		clean = clean[:maxKeyLen]
	}
	for len(clean) > 0 && clean[len(clean)-1] == '-' {
		clean = clean[:len(clean)-1]
	}
	return fleetPodNamePrefix + clean
}

// ---------------------------------------------------------------------------
// EnsureFleetContainer
// ---------------------------------------------------------------------------

// EnsureFleetContainer creates (if absent) a long-running fleet pod
// from the supplied spec, or returns the existing one. Idempotent on
// FleetKey.
//
// The returned *sandbox.Session reflects the registry view — for a
// real-time phase query, callers SHOULD follow up with SessionState.
func (b *K8sBackend) EnsureFleetContainer(ctx context.Context, spec sandbox.FleetSpec) (*sandbox.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.client == nil {
		return nil, fmt.Errorf("EnsureFleetContainer: %w", ErrNotImplementedYet)
	}
	if spec.FleetKey == "" {
		return nil, errors.New("sandbox/k8s: EnsureFleetContainer: FleetKey is required")
	}
	if spec.TemplateID == "" {
		spec.TemplateID = sandbox.BaseTemplateID
	}

	// Idempotency: if this FleetKey has already been registered, return
	// the recorded session view verbatim. The registry is the source of
	// truth for "have we seen this fleet instance before?" — mirroring
	// the CreateSession contract.
	if existing, err := b.sessions.GetSession(spec.FleetKey); err == nil && existing != nil {
		return sessionFromStore(existing, sandbox.SessionTypeFleet), nil
	}

	pod, err := b.buildFleetPodManifest(spec)
	if err != nil {
		return nil, fmt.Errorf("sandbox/k8s: EnsureFleetContainer: build manifest: %w", err)
	}

	created, err := b.client.CoreV1().Pods(b.cfg.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("sandbox/k8s: EnsureFleetContainer: create pod: %w", err)
	}
	// On AlreadyExists, fetch the incumbent pod so NodeName/UID reflect
	// the actual cluster state. This also catches the race where the
	// pod was created out-of-band (another Astonish pod in a multi-
	// replica deployment beating us to the create).
	if apierrors.IsAlreadyExists(err) {
		created, err = b.client.CoreV1().Pods(b.cfg.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("sandbox/k8s: EnsureFleetContainer: fetch existing pod: %w", err)
		}
	}

	now := time.Now().UTC()
	rec := &store.SandboxSession{
		SessionID:     spec.FleetKey,
		ChatSessionID: spec.FleetKey,
		Backend:       string(sandbox.BackendKindK8s),
		TemplateID:    spec.TemplateID,
		State:         store.SandboxSessionStateCreating,
		PodName:       created.Name,
		NodeName:      created.Spec.NodeName,
		CreatedAt:     now,
		UpdatedAt:     now,
		LastActiveAt:  now,
	}
	if err := b.sessions.PutSession(rec); err != nil {
		// Best-effort rollback: if the registry insert fails, the pod
		// we just created is invisible to the rest of the system, so
		// leaving it behind would leak.
		_ = b.client.CoreV1().Pods(b.cfg.Namespace).Delete(ctx, created.Name, metav1.DeleteOptions{})
		return nil, fmt.Errorf("sandbox/k8s: EnsureFleetContainer: persist registry: %w", err)
	}

	return sessionFromStore(rec, sandbox.SessionTypeFleet), nil
}

// ---------------------------------------------------------------------------
// Manifest builder
// ---------------------------------------------------------------------------

// buildFleetPodManifest materialises a fleet-container Pod spec from a
// FleetSpec. Exposed at package scope for unit testability without a
// live client.
//
// Shape decisions mirror session pods (same image, RuntimeClass, volume
// set) so a fleet pod is a drop-in Exec / PushFile / PullFile target.
// Differences are limited to:
//
//   - Pod name prefix: `astn-fleet-` vs `astn-sess-`.
//   - Type label: `astonish.io/type=fleet` vs `=session`.
//   - Extra discriminator label: `astonish.io/fleet-key`.
//   - Session-ID label still present (using FleetKey) so selectors that
//     enumerate every astonish pod by session-id still resolve fleet
//     pods.
func (b *K8sBackend) buildFleetPodManifest(spec sandbox.FleetSpec) (*corev1.Pod, error) {
	if spec.FleetKey == "" {
		return nil, errors.New("FleetSpec.FleetKey is required")
	}
	if spec.TemplateID == "" {
		spec.TemplateID = sandbox.BaseTemplateID
	}

	name := podNameForFleet(spec.FleetKey)

	labels := map[string]string{
		labelType:      typeFleet,
		labelSessionID: toDNSLabel(spec.FleetKey),
		labelFleetKey:  toDNSLabel(spec.FleetKey),
		labelTemplate:  toDNSLabel(spec.TemplateID),
	}
	if spec.OrgSlug != "" {
		labels[labelOrg] = toDNSLabel(spec.OrgSlug)
	}
	if spec.TeamSlug != "" {
		labels[labelTeam] = toDNSLabel(spec.TeamSlug)
	}
	// Caller-supplied labels override defaults so operators can pin
	// values (e.g., a `release=v2` selector) without this helper
	// needing to know about them.
	for k, v := range spec.Labels {
		labels[k] = v
	}

	annotations := map[string]string{
		annotationCreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// RuntimeClassName, SecurityContext, HostUsers, FUSE device
	// resource, and overlay-mode env vars are owned by
	// applyPodSecurityHardening at the bottom of this function so the
	// rules stay aligned across session / fleet / template pods.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   b.cfg.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
			{
				Name:            containerName,
				Image:           b.cfg.SandboxImage,
				ImagePullPolicy: imagePullPolicy(b.cfg.SandboxImage),
				Env: []corev1.EnvVar{
					{Name: "ASTONISH_FLEET_KEY", Value: spec.FleetKey},
					{Name: "ASTONISH_TEMPLATE_ID", Value: spec.TemplateID},
					{Name: "ASTONISH_UPPER_DIR", Value: mountUpper},
					{Name: "ASTONISH_WORK_DIR", Value: mountWork},
					{Name: "ASTONISH_LAYERS_DIR", Value: mountLayers},
					{Name: "ASTONISH_UPPERS_DIR", Value: mountUppers},
					// PID 1 sleeps after overlay composition; tool calls
					// arrive via Backend.Exec with attached stdio.
					{Name: "ASTONISH_HANDOFF", Value: "/bin/sleep"},
					{Name: "ASTONISH_HANDOFF_ARGS", Value: "infinity"},
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: volumeLayers, MountPath: mountLayers, ReadOnly: true},
					{Name: volumeUppers, MountPath: mountUppers},
					{Name: volumeUpper, MountPath: mountUpper},
					{Name: volumeWork, MountPath: mountWork},
				},
				Resources: buildResourceRequirements(spec.Limits),
			},
			},
			Volumes: []corev1.Volume{
				{
					Name: volumeLayers,
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: b.cfg.LayersPVCName,
							ReadOnly:  true,
						},
					},
				},
				{
					Name: volumeUppers,
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: b.cfg.UppersPVCName,
						},
					},
				},
				{Name: volumeUpper, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				{Name: volumeWork, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			},
		},
	}

	b.applyPodSecurityHardening(pod)
	return pod, nil
}

// Listing fleet pods: labelSelectorFor in session.go already honours
// SessionFilter.Type == SessionTypeFleet and swaps the type label
// accordingly, so ListSessions with Type=Fleet transparently surfaces
// fleet pods without fleet.go needing its own list entry point.
