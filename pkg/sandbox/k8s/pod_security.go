// Package k8s — shared pod-security hardening for sandbox pods.
//
// Phase F replaces the hard Sysbox dependency with a portable overlay
// strategy (fuse-overlayfs by default, kernel overlayfs where supported,
// auto fallback). The strategy needs to be reflected on the pods the
// backend submits: the entrypoint reads ASTONISH_OVERLAY_MODE / _ENSURE_
// FUSE_DEVICE to pick a code path, and the pod itself needs the right
// SecurityContext + resource shape so the chosen strategy actually works
// at runtime.
//
// applyPodSecurityHardening is the single place that owns these
// mutations. It is called from:
//
//   - buildPodManifest (session.go)
//   - buildFleetPodManifest (fleet.go)
//   - buildTemplateBuilderPodManifest (template.go)
//
// Keeping the logic here (rather than duplicating it in three builders)
// guarantees that a future addition — a new env var, a new resource
// request, a new security-context knob — lands in one place and applies
// uniformly to every sandbox-pod flavour.
//
// Design notes:
//
//   - RuntimeClassName: the field is a *string on PodSpec. Kubernetes
//     treats pointer-to-empty-string and nil differently (the former is
//     rejected by validation on some versions). When the operator hasn't
//     picked a runtime, we emit nil — the cluster default applies.
//
//   - Privileged: mutates each container's SecurityContext. We do NOT
//     set runAsUser / fsGroup here; those belong to a separate concern
//     (user-namespace semantics) and the defaults the image ships with
//     are correct for both privileged and unprivileged paths.
//
//   - HostUsers: a *bool on PodSpec. Pointer-to-false requests a user
//     namespace (K8s 1.33+ beta-on, 1.36 GA). Nil leaves the field
//     unset so the cluster admission default applies.
//
//   - FuseDeviceResource: when non-empty, every container gets a
//     resource limit of quantity 1 on that extended resource. The
//     kubelet's device-plugin framework then plugs /dev/fuse into the
//     container. Mirrored into resource requests (K8s requires
//     requests==limits for extended resources).
//
//   - Overlay env vars: surfaced as ASTONISH_OVERLAY_MODE and
//     ASTONISH_ENSURE_FUSE_DEVICE. The entrypoint's script generator
//     reads these into EntrypointScriptOptions at image-build time;
//     emitting them on the pod makes the chosen mode observable to
//     operators (kubectl describe / logs) and lets a future entrypoint
//     version switch modes at runtime without re-baking the image.

package k8s

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	// envOverlayMode selects the entrypoint's overlay strategy. One of
	// "fuse", "kernel", "auto". See overlay_entrypoint.go OverlayMode.
	envOverlayMode = "ASTONISH_OVERLAY_MODE"

	// envEnsureFuseDevice, when "1", instructs the fuse path to create
	// /dev/fuse via mknod before launching fuse-overlayfs. Needed in
	// privileged pods without a FUSE device plugin.
	envEnsureFuseDevice = "ASTONISH_ENSURE_FUSE_DEVICE"
)

// applyPodSecurityHardening mutates pod in place to reflect the
// backend's overlay / security configuration. It is safe to call
// multiple times and idempotent (env vars with the same name are
// deduplicated, overwriting the prior value).
//
// Called from every buildXxxPodManifest helper to keep session, fleet,
// and template-builder pods consistent with the overlay strategy.
func (b *K8sBackend) applyPodSecurityHardening(pod *corev1.Pod) {
	if pod == nil {
		return
	}

	// RuntimeClassName: emit nil when unset so Kubernetes falls back
	// to the cluster default. Emitting pointer-to-empty-string would
	// serialise as `runtimeClassName: ""` and be rejected by
	// validation on some clusters.
	if b.cfg.RuntimeClassName == "" {
		pod.Spec.RuntimeClassName = nil
	} else {
		rc := b.cfg.RuntimeClassName
		pod.Spec.RuntimeClassName = &rc
	}

	// HostUsers: emit when configured; older clusters that don't know
	// the field will drop it or reject the pod (surface the error to
	// the caller rather than feature-detect).
	if b.cfg.HostUsers != nil {
		v := *b.cfg.HostUsers
		pod.Spec.HostUsers = &v
	}

	// Overlay mode: derive the EnsureFuseDevice default from
	// Privileged. The entrypoint ships a compile-time default (see
	// EntrypointScriptOptions.applyDefaults), but surfacing the
	// values as env vars makes the pod's intent observable and keeps
	// future entrypoints honest.
	ensureFuseDevice := b.cfg.Privileged && b.cfg.FuseDeviceResource == ""

	mode := string(b.cfg.OverlayMode)
	if mode == "" {
		mode = string(OverlayModeFuse)
	}

	overlayEnv := []corev1.EnvVar{
		{Name: envOverlayMode, Value: mode},
		{Name: envEnsureFuseDevice, Value: boolToEnvValue(ensureFuseDevice)},
	}

	// Per-container mutations: SecurityContext, resource requests/
	// limits for the FUSE device plugin, and overlay env vars.
	for i := range pod.Spec.Containers {
		c := &pod.Spec.Containers[i]

		if b.cfg.Privileged {
			if c.SecurityContext == nil {
				c.SecurityContext = &corev1.SecurityContext{}
			}
			priv := true
			c.SecurityContext.Privileged = &priv
		}

		if b.cfg.FuseDeviceResource != "" {
			applyFuseDeviceResource(c, b.cfg.FuseDeviceResource)
		}

		c.Env = upsertEnv(c.Env, overlayEnv...)
	}
}

// applyFuseDeviceResource adds a quantity-1 request/limit for the
// named extended resource (e.g. "smarter-devices/fuse"). Kubernetes
// requires requests==limits for extended resources, so we mirror.
func applyFuseDeviceResource(c *corev1.Container, name string) {
	qty := *resource.NewQuantity(1, resource.DecimalSI)
	rn := corev1.ResourceName(name)

	if c.Resources.Limits == nil {
		c.Resources.Limits = corev1.ResourceList{}
	}
	c.Resources.Limits[rn] = qty

	if c.Resources.Requests == nil {
		c.Resources.Requests = corev1.ResourceList{}
	}
	c.Resources.Requests[rn] = qty
}

// upsertEnv appends env vars to existing, overwriting any entry with
// the same Name. Preserves original ordering for stable equality in
// tests; new names are appended in the order supplied.
func upsertEnv(existing []corev1.EnvVar, adds ...corev1.EnvVar) []corev1.EnvVar {
	if len(adds) == 0 {
		return existing
	}
	idx := make(map[string]int, len(existing))
	for i, e := range existing {
		idx[e.Name] = i
	}
	for _, a := range adds {
		if i, ok := idx[a.Name]; ok {
			existing[i] = a
			continue
		}
		idx[a.Name] = len(existing)
		existing = append(existing, a)
	}
	return existing
}

// boolToEnvValue renders a bool as the shell-friendly "1" / "0" used
// by the entrypoint script. The script's `if [ "$VAR" = "1" ]` idiom
// is narrower than strconv.FormatBool's "true"/"false" output.
func boolToEnvValue(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
