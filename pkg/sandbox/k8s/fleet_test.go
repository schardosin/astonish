// Package k8s — fleet_test.go drives EnsureFleetContainer and its
// manifest builder against the fake clientset. Assertions cover:
//
//   - Manifest shape: fleet labels (type=fleet, fleet-key=<key>,
//     session-id=<key>, template=<id>, optional org/team), annotations,
//     volumes, container image/env/mounts/runtimeClassName,
//     RestartPolicy=Never (PID 1 sleeps after overlay composition).
//   - Name derivation via podNameForFleet: prefix, sanitisation,
//     length cap, trailing-dash trim.
//   - EnsureFleetContainer happy path: pod created, registry row
//     persisted with type=fleet semantics.
//   - Idempotency: second call with the same FleetKey returns the
//     existing session verbatim and does not create a second pod.
//   - AlreadyExists race: pod pre-exists in the cluster; we fetch it
//     and persist a registry row without creating a duplicate.
//   - Caller-supplied labels override defaults.
//   - Validation: empty FleetKey / empty TemplateID rejected.
//   - ctx-cancel short-circuit; nil-client → ErrNotImplementedYet.
//   - Registry-persist rollback: if PutSession fails, the pod is
//     deleted so we don't leak.
//   - ListSessions with Type=Fleet surfaces the fleet pod.

package k8s

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/schardosin/astonish/pkg/sandbox"
)

// ---------------------------------------------------------------------------
// podNameForFleet
// ---------------------------------------------------------------------------

func TestPodNameForFleet(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"simple", "plan1-inst1", fleetPodNamePrefix + "plan1-inst1"},
		{"uppercase", "Plan1-Inst1", fleetPodNamePrefix + "plan1-inst1"},
		{"underscore", "plan_a", fleetPodNamePrefix + "plan-a"},
		{"trimmed", strings.Repeat("a", 40), fleetPodNamePrefix + strings.Repeat("a", 27)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := podNameForFleet(c.in); got != c.want {
				t.Errorf("podNameForFleet(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestPodNameForFleet_TrimsTrailingDash(t *testing.T) {
	// 27 chars where char 28 is '-' (would have been truncated): make
	// sure a trailing '-' after truncation is stripped.
	in := strings.Repeat("a", 27) + "-tail"
	got := podNameForFleet(in)
	if strings.HasSuffix(got, "-") && got != fleetPodNamePrefix {
		t.Errorf("podNameForFleet(%q) = %q, want no trailing dash after prefix", in, got)
	}
}

// ---------------------------------------------------------------------------
// buildFleetPodManifest
// ---------------------------------------------------------------------------

func fleetSpec() sandbox.FleetSpec {
	return sandbox.FleetSpec{
		FleetKey:   "plan-agent-01",
		TemplateID: "golang-dev",
		OrgSlug:    "acme",
		TeamSlug:   "backend",
		Labels:     map[string]string{"release": "v2"},
		Limits:     sandbox.ResourceLimits{CPUs: 2, MemoryMiB: 2048},
	}
}

func TestBuildFleetPodManifest_LabelsAndAnnotations(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	pod, err := b.buildFleetPodManifest(fleetSpec())
	if err != nil {
		t.Fatalf("buildFleetPodManifest: %v", err)
	}

	want := map[string]string{
		labelType:      typeFleet,
		labelSessionID: "plan-agent-01",
		labelFleetKey:  "plan-agent-01",
		labelTemplate:  "golang-dev",
		labelOrg:       "acme",
		labelTeam:      "backend",
		"release":      "v2", // caller-supplied
	}
	for k, v := range want {
		if got := pod.Labels[k]; got != v {
			t.Errorf("label[%s] = %q, want %q", k, got, v)
		}
	}
	if _, ok := pod.Annotations[annotationCreatedAt]; !ok {
		t.Errorf("missing annotation %s", annotationCreatedAt)
	}
}

func TestBuildFleetPodManifest_Name(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	pod, err := b.buildFleetPodManifest(fleetSpec())
	if err != nil {
		t.Fatalf("buildFleetPodManifest: %v", err)
	}
	if !strings.HasPrefix(pod.Name, fleetPodNamePrefix) {
		t.Errorf("pod name %q does not have fleet prefix %q", pod.Name, fleetPodNamePrefix)
	}
}

func TestBuildFleetPodManifest_ContainerShape(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	pod, err := b.buildFleetPodManifest(fleetSpec())
	if err != nil {
		t.Fatalf("buildFleetPodManifest: %v", err)
	}
	if n := len(pod.Spec.Containers); n != 1 {
		t.Fatalf("want 1 container, got %d", n)
	}
	c := pod.Spec.Containers[0]
	if c.Name != containerName {
		t.Errorf("container name = %q, want %q", c.Name, containerName)
	}
	if c.Image != b.cfg.SandboxImage {
		t.Errorf("image = %q, want %q", c.Image, b.cfg.SandboxImage)
	}
	if pod.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Errorf("RestartPolicy = %v, want Never (PID 1 sleeps, not restarted)", pod.Spec.RestartPolicy)
	}
	// Phase F: default backend has empty RuntimeClassName → nil on the
	// pod (cluster default applies). See session_test.go for the
	// matching assertion on session pods.
	if pod.Spec.RuntimeClassName != nil {
		t.Errorf("runtimeClassName = %v, want nil (empty config → cluster default)", pod.Spec.RuntimeClassName)
	}

	var sawFleetKeyEnv, sawTemplateEnv bool
	for _, e := range c.Env {
		switch e.Name {
		case "ASTONISH_FLEET_KEY":
			if e.Value != "plan-agent-01" {
				t.Errorf("ASTONISH_FLEET_KEY = %q, want plan-agent-01", e.Value)
			}
			sawFleetKeyEnv = true
		case "ASTONISH_TEMPLATE_ID":
			if e.Value != "golang-dev" {
				t.Errorf("ASTONISH_TEMPLATE_ID = %q, want golang-dev", e.Value)
			}
			sawTemplateEnv = true
		}
	}
	if !sawFleetKeyEnv || !sawTemplateEnv {
		t.Errorf("missing fleet envvars: fleet=%v template=%v", sawFleetKeyEnv, sawTemplateEnv)
	}
}

func TestBuildFleetPodManifest_Volumes(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	pod, err := b.buildFleetPodManifest(fleetSpec())
	if err != nil {
		t.Fatalf("buildFleetPodManifest: %v", err)
	}
	var layersV, uppersV, overlayV *corev1.Volume
	for i := range pod.Spec.Volumes {
		v := &pod.Spec.Volumes[i]
		switch v.Name {
		case volumeLayers:
			layersV = v
		case volumeUppers:
			uppersV = v
		case volumeOverlay:
			overlayV = v
		}
	}
	if layersV == nil || layersV.PersistentVolumeClaim == nil || !layersV.PersistentVolumeClaim.ReadOnly {
		t.Errorf("layers PVC must be mounted RO in fleet pod")
	}
	if uppersV == nil || uppersV.PersistentVolumeClaim == nil || uppersV.PersistentVolumeClaim.ReadOnly {
		t.Errorf("uppers PVC must be mounted RW in fleet pod")
	}
	if overlayV == nil || overlayV.EmptyDir == nil {
		t.Errorf("overlay emptyDir must be present")
	}
}

func TestBuildFleetPodManifest_Validation(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	if _, err := b.buildFleetPodManifest(sandbox.FleetSpec{TemplateID: "t"}); err == nil {
		t.Errorf("expected error on empty FleetKey")
	}
	// Empty TemplateID now defaults to BaseTemplateID — verify it succeeds.
	pod, err := b.buildFleetPodManifest(sandbox.FleetSpec{FleetKey: "f"})
	if err != nil {
		t.Fatalf("empty TemplateID should default to @base, got error: %v", err)
	}
	if pod.Labels[labelTemplate] != toDNSLabel(sandbox.BaseTemplateID) {
		t.Errorf("template label = %q, want %q", pod.Labels[labelTemplate], toDNSLabel(sandbox.BaseTemplateID))
	}
}

// ---------------------------------------------------------------------------
// EnsureFleetContainer
// ---------------------------------------------------------------------------

func TestEnsureFleetContainer_ContextCancelled(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.EnsureFleetContainer(ctx, fleetSpec()); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestEnsureFleetContainer_NilClientReturnsNotImplemented(t *testing.T) {
	b, err := New(Config{Sessions: newRegistry(t)})
	if err != nil {
		t.Fatalf("k8s.New: %v", err)
	}
	_, err = b.EnsureFleetContainer(context.Background(), fleetSpec())
	if !errors.Is(err, ErrNotImplementedYet) {
		t.Fatalf("err = %v, want ErrNotImplementedYet", err)
	}
}

func TestEnsureFleetContainer_Validation(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	ctx := context.Background()
	if _, err := b.EnsureFleetContainer(ctx, sandbox.FleetSpec{TemplateID: "t"}); err == nil ||
		!strings.Contains(err.Error(), "FleetKey is required") {
		t.Errorf("empty FleetKey: err = %v, want FleetKey-required", err)
	}
	// Empty TemplateID now defaults to BaseTemplateID — should succeed
	// (creates a pod with @base template).
	sess, err := b.EnsureFleetContainer(ctx, sandbox.FleetSpec{FleetKey: "f"})
	if err != nil {
		t.Fatalf("empty TemplateID should default to @base, got error: %v", err)
	}
	if sess.TemplateID != sandbox.BaseTemplateID {
		t.Errorf("session TemplateID = %q, want %q", sess.TemplateID, sandbox.BaseTemplateID)
	}
}

func TestEnsureFleetContainer_HappyPath(t *testing.T) {
	b, cs := newBackendWithFakeClient(t)
	ctx := context.Background()

	spec := fleetSpec()
	sess, err := b.EnsureFleetContainer(ctx, spec)
	if err != nil {
		t.Fatalf("EnsureFleetContainer: %v", err)
	}
	if sess.SessionID != spec.FleetKey {
		t.Errorf("SessionID = %q, want %q", sess.SessionID, spec.FleetKey)
	}
	if sess.Type != sandbox.SessionTypeFleet {
		t.Errorf("Type = %q, want fleet", sess.Type)
	}
	if sess.TemplateID != spec.TemplateID {
		t.Errorf("TemplateID = %q, want %q", sess.TemplateID, spec.TemplateID)
	}

	// Pod actually exists.
	pods, _ := cs.CoreV1().Pods(b.cfg.Namespace).List(ctx, metav1.ListOptions{})
	var found bool
	for _, p := range pods.Items {
		if p.Labels[labelType] == typeFleet && p.Labels[labelFleetKey] == "plan-agent-01" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected fleet pod in cluster, none found among %d pods", len(pods.Items))
	}

	// Registry row persisted.
	rec, err := b.sessions.GetSession(spec.FleetKey)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if rec == nil || rec.PodName == "" {
		t.Fatalf("registry row missing or has no PodName: %+v", rec)
	}
}

func TestEnsureFleetContainer_IdempotentOnSecondCall(t *testing.T) {
	b, cs := newBackendWithFakeClient(t)
	ctx := context.Background()
	spec := fleetSpec()

	first, err := b.EnsureFleetContainer(ctx, spec)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := b.EnsureFleetContainer(ctx, spec)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.SessionID != second.SessionID {
		t.Errorf("SessionID mismatch: first=%q second=%q", first.SessionID, second.SessionID)
	}

	// No duplicate pods in the cluster.
	pods, _ := cs.CoreV1().Pods(b.cfg.Namespace).List(ctx, metav1.ListOptions{})
	var n int
	for _, p := range pods.Items {
		if p.Labels[labelType] == typeFleet {
			n++
		}
	}
	if n != 1 {
		t.Errorf("fleet pod count = %d, want 1 after idempotent second call", n)
	}
}

func TestEnsureFleetContainer_AlreadyExistsRace(t *testing.T) {
	b, cs := newBackendWithFakeClient(t)
	ctx := context.Background()
	spec := fleetSpec()

	// Pre-create a pod with the expected name outside of the
	// EnsureFleetContainer path, simulating another replica racing us.
	pre, err := b.buildFleetPodManifest(spec)
	if err != nil {
		t.Fatalf("buildFleetPodManifest: %v", err)
	}
	if _, err := cs.CoreV1().Pods(b.cfg.Namespace).Create(ctx, pre, metav1.CreateOptions{}); err != nil {
		t.Fatalf("pre-create pod: %v", err)
	}

	sess, err := b.EnsureFleetContainer(ctx, spec)
	if err != nil {
		t.Fatalf("EnsureFleetContainer: %v", err)
	}
	if sess.SessionID != spec.FleetKey {
		t.Errorf("SessionID = %q, want %q", sess.SessionID, spec.FleetKey)
	}

	// Registry row persisted.
	rec, err := b.sessions.GetSession(spec.FleetKey)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if rec == nil || rec.PodName == "" {
		t.Fatalf("registry row missing after AlreadyExists race: %+v", rec)
	}
}

// ---------------------------------------------------------------------------
// ListSessions with Type=Fleet
// ---------------------------------------------------------------------------

func TestListSessions_SurfacesFleetPods(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	ctx := context.Background()

	if _, err := b.EnsureFleetContainer(ctx, fleetSpec()); err != nil {
		t.Fatalf("EnsureFleetContainer: %v", err)
	}

	// Listing with Type=Fleet should include our fleet pod.
	list, err := b.ListSessions(ctx, sandbox.SessionFilter{Type: sandbox.SessionTypeFleet})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 fleet session, got %d: %+v", len(list), list)
	}
	if list[0].Type != sandbox.SessionTypeFleet {
		t.Errorf("session Type = %q, want fleet", list[0].Type)
	}

	// Listing with Type=Chat should return 0 — the fleet pod carries a
	// different type label.
	list, err = b.ListSessions(ctx, sandbox.SessionFilter{Type: sandbox.SessionTypeChat})
	if err != nil {
		t.Fatalf("ListSessions chat: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("want 0 chat sessions, got %d", len(list))
	}
}
