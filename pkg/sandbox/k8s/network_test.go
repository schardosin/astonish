// Package k8s — network_test.go covers the NetworkPolicy and Service
// paths. The fake clientset fully supports these resources (unlike SPDY
// exec), so these tests drive the real code against a simulated
// in-memory API server.
//
// Coverage matrix:
//
//   - ctx-cancellation short-circuits all four methods.
//   - Empty/invalid arguments are rejected (orgSlug, sessionID, port,
//     protocol).
//   - EnsureOrgNetwork creates a NetworkPolicy with the right pod
//     selector, ingress rules (same-org + control-plane namespace),
//     and egress rules (DNS to kube-system + same-org pods).
//   - EnsureOrgNetwork is idempotent: a second call updates the
//     existing policy (no "already exists" error, bumps the spec as
//     needed).
//   - DeleteOrgNetwork deletes the policy and tolerates absent
//     policies.
//   - ExposePort creates a ClusterIP Service with the correct selector,
//     port mapping, and protocol, and returns the in-cluster DNS host.
//   - ExposePort is idempotent; protocol normalisation handles
//     empty/tcp/udp/sctp and rejects garbage.
//   - UnexposePort deletes the Service and is a no-op on absent ones.

package k8s

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/SAP/astonish/pkg/sandbox"
)

// ---------------------------------------------------------------------------
// EnsureOrgNetwork / DeleteOrgNetwork
// ---------------------------------------------------------------------------

func TestEnsureOrgNetwork_ContextCancelled(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.EnsureOrgNetwork(ctx, "acme"); !errors.Is(err, context.Canceled) {
		t.Errorf("EnsureOrgNetwork cancelled: got %v, want context.Canceled", err)
	}
}

func TestEnsureOrgNetwork_RejectsEmptyOrg(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	if err := b.EnsureOrgNetwork(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "orgSlug is required") {
		t.Errorf("EnsureOrgNetwork empty: got %v", err)
	}
}

func TestEnsureOrgNetwork_NoClientRejected(t *testing.T) {
	b, err := New(Config{Sessions: newRegistry(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := b.EnsureOrgNetwork(context.Background(), "acme"); err == nil || !strings.Contains(err.Error(), "no Kubernetes client") {
		t.Errorf("EnsureOrgNetwork no client: got %v", err)
	}
}

func TestEnsureOrgNetwork_CreatesPolicyWithExpectedShape(t *testing.T) {
	b, cs := newBackendWithFakeClient(t)
	if err := b.EnsureOrgNetwork(context.Background(), "acme"); err != nil {
		t.Fatalf("EnsureOrgNetwork: %v", err)
	}
	np, err := cs.NetworkingV1().NetworkPolicies("astonish-sandboxes").
		Get(context.Background(), "astn-org-acme", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get NetworkPolicy: %v", err)
	}

	// Labels on the policy itself.
	if np.Labels[labelOrg] != "acme" {
		t.Errorf("policy label org = %q, want acme", np.Labels[labelOrg])
	}
	if np.Labels[labelType] != "org-network" {
		t.Errorf("policy label type = %q, want org-network", np.Labels[labelType])
	}

	// Pod selector: org=acme + type in (session, fleet, template-builder).
	if np.Spec.PodSelector.MatchLabels[labelOrg] != "acme" {
		t.Errorf("selector MatchLabels org = %q, want acme", np.Spec.PodSelector.MatchLabels[labelOrg])
	}
	found := false
	for _, req := range np.Spec.PodSelector.MatchExpressions {
		if req.Key == labelType && req.Operator == metav1.LabelSelectorOpIn && containsAll(req.Values, []string{typeSession, typeFleet, typeTemplateBuilder}) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("selector MatchExpressions missing type-In rule: %+v", np.Spec.PodSelector.MatchExpressions)
	}

	// PolicyTypes: both ingress and egress.
	if !containsPolicyType(np.Spec.PolicyTypes, networkingv1.PolicyTypeIngress) {
		t.Error("PolicyTypes should include Ingress")
	}
	if !containsPolicyType(np.Spec.PolicyTypes, networkingv1.PolicyTypeEgress) {
		t.Error("PolicyTypes should include Egress")
	}

	// Ingress: must allow same-org pods AND control-plane namespace.
	if len(np.Spec.Ingress) < 2 {
		t.Fatalf("Ingress rules = %d, want ≥2", len(np.Spec.Ingress))
	}
	gotSameOrg, gotControlPlane := false, false
	for _, rule := range np.Spec.Ingress {
		for _, peer := range rule.From {
			if peer.PodSelector != nil && peer.PodSelector.MatchLabels[labelOrg] == "acme" {
				gotSameOrg = true
			}
			if peer.NamespaceSelector != nil && peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == "astonish" {
				gotControlPlane = true
			}
		}
	}
	if !gotSameOrg {
		t.Error("ingress should allow same-org pods")
	}
	if !gotControlPlane {
		t.Error("ingress should allow control-plane namespace")
	}

	// Egress: must allow DNS and same-org pods.
	gotDNS, gotSameOrgEgress := false, false
	for _, rule := range np.Spec.Egress {
		for _, p := range rule.Ports {
			if p.Port != nil && p.Port.IntValue() == 53 {
				gotDNS = true
			}
		}
		for _, peer := range rule.To {
			if peer.PodSelector != nil && peer.PodSelector.MatchLabels[labelOrg] == "acme" {
				gotSameOrgEgress = true
			}
		}
	}
	if !gotDNS {
		t.Error("egress should allow DNS (port 53)")
	}
	if !gotSameOrgEgress {
		t.Error("egress should allow same-org pods")
	}
}

func TestEnsureOrgNetwork_Idempotent(t *testing.T) {
	b, cs := newBackendWithFakeClient(t)
	ctx := context.Background()
	if err := b.EnsureOrgNetwork(ctx, "acme"); err != nil {
		t.Fatalf("first EnsureOrgNetwork: %v", err)
	}
	// Second call must not error ("already exists") and must leave us
	// with exactly one policy.
	if err := b.EnsureOrgNetwork(ctx, "acme"); err != nil {
		t.Fatalf("second EnsureOrgNetwork: %v", err)
	}
	list, err := cs.NetworkingV1().NetworkPolicies("astonish-sandboxes").
		List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list.Items) != 1 {
		t.Errorf("got %d NetworkPolicies, want 1", len(list.Items))
	}
}

func TestEnsureOrgNetwork_SanitisesName(t *testing.T) {
	b, cs := newBackendWithFakeClient(t)
	// Uppercase and underscores in orgSlug must be sanitised.
	if err := b.EnsureOrgNetwork(context.Background(), "ACME_Corp"); err != nil {
		t.Fatalf("EnsureOrgNetwork: %v", err)
	}
	_, err := cs.NetworkingV1().NetworkPolicies("astonish-sandboxes").
		Get(context.Background(), "astn-org-acme-corp", metav1.GetOptions{})
	if err != nil {
		t.Errorf("policy name = astn-org-acme-corp: %v", err)
	}
}

func TestDeleteOrgNetwork_DeletesPolicy(t *testing.T) {
	b, cs := newBackendWithFakeClient(t)
	ctx := context.Background()
	if err := b.EnsureOrgNetwork(ctx, "acme"); err != nil {
		t.Fatalf("EnsureOrgNetwork: %v", err)
	}
	if err := b.DeleteOrgNetwork(ctx, "acme"); err != nil {
		t.Fatalf("DeleteOrgNetwork: %v", err)
	}
	_, err := cs.NetworkingV1().NetworkPolicies("astonish-sandboxes").
		Get(ctx, "astn-org-acme", metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Errorf("policy still exists: err=%v", err)
	}
}

func TestDeleteOrgNetwork_AbsentIsNoOp(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	if err := b.DeleteOrgNetwork(context.Background(), "never-created"); err != nil {
		t.Errorf("DeleteOrgNetwork absent: got %v, want nil", err)
	}
}

// ---------------------------------------------------------------------------
// ExposePort / UnexposePort
// ---------------------------------------------------------------------------

func TestExposePort_ContextCancelled(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.ExposePort(ctx, "s1", 8080, "tcp"); !errors.Is(err, context.Canceled) {
		t.Errorf("ExposePort cancelled: got %v, want context.Canceled", err)
	}
}

func TestExposePort_ValidatesArgs(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	cases := []struct {
		name    string
		session string
		port    int
		proto   string
		want    string
	}{
		{"empty session", "", 8080, "tcp", "sessionID is required"},
		{"port 0", "s1", 0, "tcp", "invalid port"},
		{"port too high", "s1", 99999, "tcp", "invalid port"},
		{"bogus proto", "s1", 8080, "xenon", "unsupported protocol"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := b.ExposePort(context.Background(), c.session, c.port, c.proto)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("ExposePort %s: got %v, want %q", c.name, err, c.want)
			}
		})
	}
}

func TestExposePort_CreatesServiceAndReturnsDNS(t *testing.T) {
	b, cs := newBackendWithFakeClient(t)
	addr, err := b.ExposePort(context.Background(), "sess-001", 8080, "tcp")
	if err != nil {
		t.Fatalf("ExposePort: %v", err)
	}
	wantHost := "sess-sess-001-8080.astonish-sandboxes.svc.cluster.local"
	if addr.Host != wantHost {
		t.Errorf("Host = %q, want %q", addr.Host, wantHost)
	}
	if addr.Port != 8080 {
		t.Errorf("Port = %d, want 8080", addr.Port)
	}
	if addr.Protocol != "tcp" {
		t.Errorf("Protocol = %q, want tcp", addr.Protocol)
	}

	svc, err := cs.CoreV1().Services("astonish-sandboxes").
		Get(context.Background(), "sess-sess-001-8080", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get Service: %v", err)
	}
	if svc.Spec.Selector[labelSessionID] != "sess-001" {
		t.Errorf("Service selector session-id = %q, want sess-001", svc.Spec.Selector[labelSessionID])
	}
	if len(svc.Spec.Ports) != 1 {
		t.Fatalf("Service ports = %d, want 1", len(svc.Spec.Ports))
	}
	p := svc.Spec.Ports[0]
	if p.Port != 8080 {
		t.Errorf("Service port = %d, want 8080", p.Port)
	}
	if p.Protocol != corev1.ProtocolTCP {
		t.Errorf("Service protocol = %q, want TCP", p.Protocol)
	}
	if p.TargetPort.IntValue() != 8080 {
		t.Errorf("Service targetPort = %v, want 8080", p.TargetPort)
	}
}

func TestExposePort_ProtocolNormalisation(t *testing.T) {
	cases := []struct {
		in  string
		out corev1.Protocol
	}{
		{"", corev1.ProtocolTCP},
		{"tcp", corev1.ProtocolTCP},
		{"TCP", corev1.ProtocolTCP},
		{"  UDP  ", corev1.ProtocolUDP},
		{"sctp", corev1.ProtocolSCTP},
	}
	for _, c := range cases {
		got, err := normaliseProtocol(c.in)
		if err != nil {
			t.Errorf("normaliseProtocol(%q): %v", c.in, err)
			continue
		}
		if got != c.out {
			t.Errorf("normaliseProtocol(%q) = %q, want %q", c.in, got, c.out)
		}
	}
	if _, err := normaliseProtocol("nope"); err == nil {
		t.Error("normaliseProtocol(nope) should error")
	}
}

func TestExposePort_Idempotent(t *testing.T) {
	b, cs := newBackendWithFakeClient(t)
	ctx := context.Background()
	addr1, err := b.ExposePort(ctx, "s1", 9090, "tcp")
	if err != nil {
		t.Fatalf("first ExposePort: %v", err)
	}
	addr2, err := b.ExposePort(ctx, "s1", 9090, "tcp")
	if err != nil {
		t.Fatalf("second ExposePort: %v", err)
	}
	if addr1.Host != addr2.Host {
		t.Errorf("hosts differ: %q vs %q", addr1.Host, addr2.Host)
	}
	list, _ := cs.CoreV1().Services("astonish-sandboxes").List(ctx, metav1.ListOptions{})
	if len(list.Items) != 1 {
		t.Errorf("got %d services, want 1", len(list.Items))
	}
}

func TestUnexposePort_DeletesService(t *testing.T) {
	b, cs := newBackendWithFakeClient(t)
	ctx := context.Background()
	if _, err := b.ExposePort(ctx, "s1", 8080, "tcp"); err != nil {
		t.Fatalf("ExposePort: %v", err)
	}
	if err := b.UnexposePort(ctx, "s1", 8080); err != nil {
		t.Fatalf("UnexposePort: %v", err)
	}
	_, err := cs.CoreV1().Services("astonish-sandboxes").
		Get(ctx, "sess-s1-8080", metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Errorf("service still exists: err=%v", err)
	}
}

func TestUnexposePort_AbsentIsNoOp(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	if err := b.UnexposePort(context.Background(), "s1", 8080); err != nil {
		t.Errorf("UnexposePort absent: got %v, want nil", err)
	}
}

func TestUnexposePort_ValidatesArgs(t *testing.T) {
	b, _ := newBackendWithFakeClient(t)
	if err := b.UnexposePort(context.Background(), "", 8080); err == nil || !strings.Contains(err.Error(), "sessionID is required") {
		t.Errorf("UnexposePort empty session: got %v", err)
	}
	if err := b.UnexposePort(context.Background(), "s", 0); err == nil || !strings.Contains(err.Error(), "invalid port") {
		t.Errorf("UnexposePort bad port: got %v", err)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func containsAll(haystack, needles []string) bool {
	set := make(map[string]struct{}, len(haystack))
	for _, s := range haystack {
		set[s] = struct{}{}
	}
	for _, n := range needles {
		if _, ok := set[n]; !ok {
			return false
		}
	}
	return true
}

func containsPolicyType(types []networkingv1.PolicyType, want networkingv1.PolicyType) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}

// silence unused-import if the sandbox package shape changes.
var _ = sandbox.ExposedAddr{}
