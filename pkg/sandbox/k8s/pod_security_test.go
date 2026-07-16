// Package k8s — applyPodSecurityHardening tests.
//
// These tests pin the behaviour of the single helper that mutates every
// sandbox pod (session, fleet, template-builder) to reflect the overlay
// strategy and security knobs. The helper is covered by its own tests
// (rather than duplicating assertions across three builder tests) so
// the matrix stays readable.
//
// Test matrix:
//
//   - Defaults (Privileged=false, FuseDeviceResource="", HostUsers=nil,
//     RuntimeClassName="") → no SecurityContext mutation, no device
//     resource, no HostUsers, RuntimeClassName=nil, env vars emit
//     "fuse" / "0".
//
//   - Privileged=true, no FUSE device plugin → container gets
//     SecurityContext.Privileged=true and env ENSURE_FUSE_DEVICE=1
//     (in-container mknod path).
//
//   - FuseDeviceResource set → resource request/limit added, env
//     ENSURE_FUSE_DEVICE=0 (device plugin provides /dev/fuse).
//
//   - HostUsers pointer-to-false → PodSpec.HostUsers forwarded.
//
//   - RuntimeClassName set → pointer-to-value surfaced; empty keeps
//     nil.
//
//   - OverlayMode override → env var reflects the chosen mode.
//
//   - Idempotency: calling the helper twice gives the same pod shape
//     (env-var upsert is the load-bearing invariant).

package k8s

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/SAP/astonish/pkg/sandbox"
)

// buildHardenedPod is a helper that runs the helper over a pod
// constructed with the given Config.
func buildHardenedPod(t *testing.T, cfg Config) *corev1.Pod {
	t.Helper()
	cfg.Sessions = newRegistry(t)
	b, err := New(cfg)
	if err != nil {
		t.Fatalf("k8s.New: %v", err)
	}
	pod, err := b.buildPodManifest(sandbox.SessionSpec{
		SessionID:  "sess-1",
		TemplateID: "tmpl-1",
	})
	if err != nil {
		t.Fatalf("buildPodManifest: %v", err)
	}
	return pod
}

func findEnv(env []corev1.EnvVar, name string) (corev1.EnvVar, bool) {
	for _, e := range env {
		if e.Name == name {
			return e, true
		}
	}
	return corev1.EnvVar{}, false
}

// TestApplyPodSecurityHardening_Defaults: empty config → safe minimum.
// RuntimeClassName nil, no SecurityContext mutation, overlay env emits
// fuse mode with EnsureFuseDevice=0 (no device plugin and no privileged
// → fuse-overlayfs will fail at runtime, but that's the operator's
// problem; the helper never silently picks a mode).
func TestApplyPodSecurityHardening_Defaults(t *testing.T) {
	pod := buildHardenedPod(t, Config{})

	if pod.Spec.RuntimeClassName != nil {
		t.Errorf("RuntimeClassName = %v, want nil", pod.Spec.RuntimeClassName)
	}
	if pod.Spec.HostUsers != nil {
		t.Errorf("HostUsers = %v, want nil", pod.Spec.HostUsers)
	}

	c := pod.Spec.Containers[0]
	if c.SecurityContext != nil && c.SecurityContext.Privileged != nil && *c.SecurityContext.Privileged {
		t.Errorf("Privileged set despite cfg.Privileged=false")
	}

	mode, ok := findEnv(c.Env, "ASTONISH_OVERLAY_MODE")
	if !ok {
		t.Fatalf("ASTONISH_OVERLAY_MODE env missing")
	}
	if mode.Value != "fuse" {
		t.Errorf("OVERLAY_MODE = %q, want fuse", mode.Value)
	}

	ensure, ok := findEnv(c.Env, "ASTONISH_ENSURE_FUSE_DEVICE")
	if !ok {
		t.Fatalf("ASTONISH_ENSURE_FUSE_DEVICE env missing")
	}
	if ensure.Value != "0" {
		t.Errorf("ENSURE_FUSE_DEVICE = %q, want 0 (no privileged, no device plugin)", ensure.Value)
	}
}

// TestApplyPodSecurityHardening_PrivilegedDev: the dev / simple path.
// Privileged pod with no FUSE device plugin → helper flips both
// SecurityContext.Privileged and env ENSURE_FUSE_DEVICE=1 so the
// entrypoint will mknod /dev/fuse before launching fuse-overlayfs.
func TestApplyPodSecurityHardening_PrivilegedDev(t *testing.T) {
	pod := buildHardenedPod(t, Config{
		Privileged: true,
	})

	c := pod.Spec.Containers[0]
	if c.SecurityContext == nil || c.SecurityContext.Privileged == nil || !*c.SecurityContext.Privileged {
		t.Fatalf("SecurityContext.Privileged not set; got %+v", c.SecurityContext)
	}

	ensure, _ := findEnv(c.Env, "ASTONISH_ENSURE_FUSE_DEVICE")
	if ensure.Value != "1" {
		t.Errorf("ENSURE_FUSE_DEVICE = %q, want 1 (privileged + no device plugin)", ensure.Value)
	}
}

// TestApplyPodSecurityHardening_DeviceResource: the production path.
// Operator wires in a FUSE device plugin → helper adds the resource
// request/limit and sets ENSURE_FUSE_DEVICE=0 so the entrypoint skips
// the mknod step (the plugin has already plugged the device in).
func TestApplyPodSecurityHardening_DeviceResource(t *testing.T) {
	pod := buildHardenedPod(t, Config{
		FuseDeviceResource: "smarter-devices/fuse",
	})

	c := pod.Spec.Containers[0]
	rn := corev1.ResourceName("smarter-devices/fuse")
	limit, ok := c.Resources.Limits[rn]
	if !ok {
		t.Fatalf("resource limit %q missing; got %+v", rn, c.Resources.Limits)
	}
	want := resource.NewQuantity(1, resource.DecimalSI)
	if limit.Cmp(*want) != 0 {
		t.Errorf("limit %q = %s, want 1", rn, limit.String())
	}
	req, ok := c.Resources.Requests[rn]
	if !ok || req.Cmp(*want) != 0 {
		t.Errorf("resource request %q missing or != 1: %+v", rn, c.Resources.Requests)
	}

	ensure, _ := findEnv(c.Env, "ASTONISH_ENSURE_FUSE_DEVICE")
	if ensure.Value != "0" {
		t.Errorf("ENSURE_FUSE_DEVICE = %q, want 0 (device plugin provides /dev/fuse)", ensure.Value)
	}

	// Device-plugin path should NOT imply privileged — operators use
	// this path specifically to avoid it.
	if c.SecurityContext != nil && c.SecurityContext.Privileged != nil && *c.SecurityContext.Privileged {
		t.Errorf("Privileged set on device-plugin path; got %+v", c.SecurityContext)
	}
}

// TestApplyPodSecurityHardening_HostUsers: pointer-to-false forwarded.
func TestApplyPodSecurityHardening_HostUsers(t *testing.T) {
	fv := false
	pod := buildHardenedPod(t, Config{
		HostUsers: &fv,
	})
	if pod.Spec.HostUsers == nil {
		t.Fatal("HostUsers nil, want pointer")
	}
	if *pod.Spec.HostUsers {
		t.Errorf("HostUsers = true, want false (userns requested)")
	}
}

// TestApplyPodSecurityHardening_RuntimeClass: explicit runtime class
// surfaces as pointer-to-value.
func TestApplyPodSecurityHardening_RuntimeClass(t *testing.T) {
	pod := buildHardenedPod(t, Config{
		RuntimeClassName: "sysbox-runc",
	})
	if pod.Spec.RuntimeClassName == nil {
		t.Fatal("RuntimeClassName nil, want sysbox-runc")
	}
	if *pod.Spec.RuntimeClassName != "sysbox-runc" {
		t.Errorf("RuntimeClassName = %q, want sysbox-runc", *pod.Spec.RuntimeClassName)
	}
}

// TestApplyPodSecurityHardening_OverlayModeOverride: non-default mode
// propagates to the env var.
func TestApplyPodSecurityHardening_OverlayModeOverride(t *testing.T) {
	cases := []OverlayMode{OverlayModeFuse, OverlayModeKernel, OverlayModeAuto}
	for _, m := range cases {
		m := m
		t.Run(string(m), func(t *testing.T) {
			pod := buildHardenedPod(t, Config{OverlayMode: m})
			c := pod.Spec.Containers[0]
			mode, _ := findEnv(c.Env, "ASTONISH_OVERLAY_MODE")
			if mode.Value != string(m) {
				t.Errorf("OVERLAY_MODE = %q, want %q", mode.Value, string(m))
			}
		})
	}
}

// TestApplyPodSecurityHardening_Idempotent: calling the helper twice
// doesn't duplicate env vars or resource entries. This is the
// load-bearing invariant for a future extraction into a webhook.
func TestApplyPodSecurityHardening_Idempotent(t *testing.T) {
	b, err := New(Config{
		Sessions:           newRegistry(t),
		Privileged:         true,
		FuseDeviceResource: "smarter-devices/fuse",
	})
	if err != nil {
		t.Fatalf("k8s.New: %v", err)
	}
	pod, err := b.buildPodManifest(sandbox.SessionSpec{
		SessionID:  "sess-1",
		TemplateID: "tmpl-1",
	})
	if err != nil {
		t.Fatalf("buildPodManifest: %v", err)
	}

	before := len(pod.Spec.Containers[0].Env)
	b.applyPodSecurityHardening(pod)
	after := len(pod.Spec.Containers[0].Env)
	if before != after {
		t.Errorf("env grew on second call: before=%d after=%d", before, after)
	}

	// Also re-applying must not double the resource limit quantities.
	rn := corev1.ResourceName("smarter-devices/fuse")
	if q, ok := pod.Spec.Containers[0].Resources.Limits[rn]; ok {
		want := resource.NewQuantity(1, resource.DecimalSI)
		if q.Cmp(*want) != 0 {
			t.Errorf("limit %q = %s, want 1 after idempotent re-apply", rn, q.String())
		}
	}
}

// TestApplyPodSecurityHardening_AppliesToAllBuilders: the helper must
// run over session, fleet, and template-builder manifests. Detects
// silent drift if a new builder lands without wiring the helper.
func TestApplyPodSecurityHardening_AppliesToAllBuilders(t *testing.T) {
	b, err := New(Config{
		Sessions:   newRegistry(t),
		Privileged: true,
	})
	if err != nil {
		t.Fatalf("k8s.New: %v", err)
	}

	session, err := b.buildPodManifest(sandbox.SessionSpec{
		SessionID:  "s1",
		TemplateID: "t1",
	})
	if err != nil {
		t.Fatalf("buildPodManifest: %v", err)
	}

	fleet, err := b.buildFleetPodManifest(sandbox.FleetSpec{
		FleetKey:   "f1",
		TemplateID: "t1",
	})
	if err != nil {
		t.Fatalf("buildFleetPodManifest: %v", err)
	}

	builder, err := b.buildTemplateBuilderPodManifest(sandbox.TemplateBuildSpec{
		TemplateID: "t1",
	})
	if err != nil {
		t.Fatalf("buildTemplateBuilderPodManifest: %v", err)
	}

	for _, tc := range []struct {
		name string
		pod  *corev1.Pod
	}{
		{"session", session},
		{"fleet", fleet},
		{"template-builder", builder},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c := tc.pod.Spec.Containers[0]
			if c.SecurityContext == nil || c.SecurityContext.Privileged == nil || !*c.SecurityContext.Privileged {
				t.Errorf("Privileged not applied to %s pod", tc.name)
			}
			if _, ok := findEnv(c.Env, "ASTONISH_OVERLAY_MODE"); !ok {
				t.Errorf("OVERLAY_MODE env missing on %s pod", tc.name)
			}
		})
	}
}
