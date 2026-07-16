// Package k8s — NetworkPolicy + Service implementation (Phase C).
//
// This file owns four Backend methods: EnsureOrgNetwork,
// DeleteOrgNetwork, ExposePort, and UnexposePort.
//
// Design invariants (§5.7):
//
//   - Per-org isolation is done via labels + NetworkPolicy, not per-org
//     namespaces. Every sandbox pod already carries astonish.io/org=<slug>;
//     EnsureOrgNetwork renders a NetworkPolicy that allows intra-org pod
//     traffic plus ingress from the Astonish control-plane namespace, and
//     denies everything else by default.
//
//   - ExposePort creates an in-cluster ClusterIP Service named
//     sess-<sessionID>-<port>. The Service selector targets the single
//     pod via astonish.io/session-id. Ingress (external URL) is
//     deferred; when Config.BaseDomain is set in a later slice we will
//     optionally create an Ingress here.
//
//   - All operations are idempotent: Ensure methods use Get → Update →
//     (on NotFound) Create. Delete methods ignore NotFound.
//
//   - Every operation honours ctx.Err() before any API call.
//
// References:
//   - docs/architecture/sandbox-backends.md §5.7.

package k8s

import (
	"context"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/SAP/astonish/pkg/sandbox"
)

// ---------------------------------------------------------------------------
// EnsureOrgNetwork / DeleteOrgNetwork
// ---------------------------------------------------------------------------

// orgNetworkPolicyName returns the NetworkPolicy name for an org.
// Format: astn-org-<sanitized-slug>. DNS-1123 compliant.
func orgNetworkPolicyName(orgSlug string) string {
	return "astn-org-" + toDNSLabel(orgSlug)
}

// EnsureOrgNetwork creates or updates the NetworkPolicy that isolates
// sandbox pods belonging to orgSlug. The policy:
//
//   - Selects pods with astonish.io/type in {session, fleet,
//     template-builder} AND astonish.io/org = <slug>.
//
//   - Ingress: allows traffic from (a) any pod in the control-plane
//     namespace (Config.ControlPlaneNamespace, default "astonish") and
//     (b) any pod in the same namespace with a matching org label.
//
//   - Egress: allows DNS (UDP/53, TCP/53) to kube-system and traffic to
//     same-org pods. External egress is NOT opened here — that's the
//     job of a higher-level policy that can enforce per-org allow-lists.
//
// The method is idempotent. A pre-existing policy with the same name is
// updated in place.
func (b *K8sBackend) EnsureOrgNetwork(ctx context.Context, orgSlug string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if orgSlug == "" {
		return errors.New("sandbox/k8s: EnsureOrgNetwork: orgSlug is required")
	}
	if b.client == nil {
		return errors.New("sandbox/k8s: EnsureOrgNetwork: no Kubernetes client configured")
	}

	policy := b.buildOrgNetworkPolicy(orgSlug)

	nps := b.client.NetworkingV1().NetworkPolicies(b.cfg.Namespace)
	existing, err := nps.Get(ctx, policy.Name, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("sandbox/k8s: EnsureOrgNetwork(%s): get: %w", orgSlug, err)
		}
		if _, err := nps.Create(ctx, policy, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("sandbox/k8s: EnsureOrgNetwork(%s): create: %w", orgSlug, err)
		}
		return nil
	}

	// Update in place. Preserve the ResourceVersion so the API server
	// can enforce optimistic concurrency.
	policy.ResourceVersion = existing.ResourceVersion
	if _, err := nps.Update(ctx, policy, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("sandbox/k8s: EnsureOrgNetwork(%s): update: %w", orgSlug, err)
	}
	return nil
}

// DeleteOrgNetwork removes the NetworkPolicy for orgSlug. Absent
// policies are treated as successful no-ops.
//
// The spec (§5.7) leaves pod deletion to the org-delete cascade; this
// method is concerned only with network primitives.
func (b *K8sBackend) DeleteOrgNetwork(ctx context.Context, orgSlug string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if orgSlug == "" {
		return errors.New("sandbox/k8s: DeleteOrgNetwork: orgSlug is required")
	}
	if b.client == nil {
		return errors.New("sandbox/k8s: DeleteOrgNetwork: no Kubernetes client configured")
	}

	name := orgNetworkPolicyName(orgSlug)
	err := b.client.NetworkingV1().NetworkPolicies(b.cfg.Namespace).
		Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("sandbox/k8s: DeleteOrgNetwork(%s): %w", orgSlug, err)
	}
	return nil
}

// buildOrgNetworkPolicy renders the NetworkPolicy spec for orgSlug. The
// policy is deterministic: repeated calls with the same inputs produce
// bit-identical output (important for idempotent update).
func (b *K8sBackend) buildOrgNetworkPolicy(orgSlug string) *networkingv1.NetworkPolicy {
	// matchExpressions: type in (session, fleet, template-builder)
	// AND org = <slug>
	podSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			labelOrg: orgSlug,
		},
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      labelType,
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{typeSession, typeFleet, typeTemplateBuilder},
			},
		},
	}

	controlPlaneNS := b.cfg.ControlPlaneNamespace

	ingress := []networkingv1.NetworkPolicyIngressRule{
		// Rule 1: same-org pods in the sandbox namespace.
		{
			From: []networkingv1.NetworkPolicyPeer{
				{
					PodSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{labelOrg: orgSlug},
					},
				},
			},
		},
		// Rule 2: anything in the control-plane namespace. Matched by
		// the kubernetes.io/metadata.name label that kube-apiserver
		// stamps on every namespace automatically.
		{
			From: []networkingv1.NetworkPolicyPeer{
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/metadata.name": controlPlaneNS,
						},
					},
				},
			},
		},
	}

	// Egress: DNS (kube-system UDP/TCP 53) + same-org pods.
	dnsUDP := corev1.ProtocolUDP
	dnsTCP := corev1.ProtocolTCP
	dnsPort := intstr.FromInt(53)
	egress := []networkingv1.NetworkPolicyEgressRule{
		{
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &dnsUDP, Port: &dnsPort},
				{Protocol: &dnsTCP, Port: &dnsPort},
			},
			To: []networkingv1.NetworkPolicyPeer{
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/metadata.name": "kube-system",
						},
					},
				},
			},
		},
		{
			To: []networkingv1.NetworkPolicyPeer{
				{
					PodSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{labelOrg: orgSlug},
					},
				},
			},
		},
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      orgNetworkPolicyName(orgSlug),
			Namespace: b.cfg.Namespace,
			Labels: map[string]string{
				labelOrg:  orgSlug,
				labelType: "org-network",
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: podSelector,
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: ingress,
			Egress:  egress,
		},
	}
}

// ---------------------------------------------------------------------------
// ExposePort / UnexposePort
// ---------------------------------------------------------------------------

// serviceNameForPort returns the Service name for a session/port pair.
// Format: sess-<sanitized-sessionID>-<port>.
func serviceNameForPort(sessionID string, port int) string {
	return fmt.Sprintf("sess-%s-%d", toDNSLabel(sessionID), port)
}

// ExposePort creates a ClusterIP Service targeting the session's pod on
// the requested port. Returns the in-cluster DNS address
// (<svc>.<ns>.svc.cluster.local) in ExposedAddr.Host.
//
// The method is idempotent: re-exposing an already-exposed port
// returns the existing Service's address without error.
//
// Port must be in 1..65535. proto is "tcp" or "udp" (case-insensitive).
// An empty proto defaults to "tcp".
func (b *K8sBackend) ExposePort(ctx context.Context, sessionID string, port int, proto string) (*sandbox.ExposedAddr, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if sessionID == "" {
		return nil, errors.New("sandbox/k8s: ExposePort: sessionID is required")
	}
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("sandbox/k8s: ExposePort: invalid port %d", port)
	}
	if b.client == nil {
		return nil, errors.New("sandbox/k8s: ExposePort: no Kubernetes client configured")
	}
	k8sProto, err := normaliseProtocol(proto)
	if err != nil {
		return nil, fmt.Errorf("sandbox/k8s: ExposePort: %w", err)
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceNameForPort(sessionID, port),
			Namespace: b.cfg.Namespace,
			Labels: map[string]string{
				labelSessionID: sessionID,
				labelType:      "port-exposure",
			},
			Annotations: map[string]string{
				"astonish.io/port":     fmt.Sprintf("%d", port),
				"astonish.io/protocol": string(k8sProto),
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				labelSessionID: sessionID,
			},
			Ports: []corev1.ServicePort{{
				Name:       strings.ToLower(string(k8sProto)),
				Protocol:   k8sProto,
				Port:       int32(port),
				TargetPort: intstr.FromInt(port),
			}},
		},
	}

	svcs := b.client.CoreV1().Services(b.cfg.Namespace)
	existing, getErr := svcs.Get(ctx, svc.Name, metav1.GetOptions{})
	if getErr != nil {
		if !apierrors.IsNotFound(getErr) {
			return nil, fmt.Errorf("sandbox/k8s: ExposePort(%s, %d): get: %w", sessionID, port, getErr)
		}
		if _, err := svcs.Create(ctx, svc, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("sandbox/k8s: ExposePort(%s, %d): create: %w", sessionID, port, err)
		}
	} else {
		// Preserve ClusterIP (immutable on Update) and ResourceVersion.
		svc.Spec.ClusterIP = existing.Spec.ClusterIP
		svc.ResourceVersion = existing.ResourceVersion
		if _, err := svcs.Update(ctx, svc, metav1.UpdateOptions{}); err != nil {
			return nil, fmt.Errorf("sandbox/k8s: ExposePort(%s, %d): update: %w", sessionID, port, err)
		}
	}

	host := fmt.Sprintf("%s.%s.svc.cluster.local", svc.Name, b.cfg.Namespace)
	return &sandbox.ExposedAddr{
		Host:     host,
		Port:     port,
		Protocol: strings.ToLower(string(k8sProto)),
	}, nil
}

// UnexposePort deletes the Service created by ExposePort. Absent
// Services are treated as successful no-ops.
func (b *K8sBackend) UnexposePort(ctx context.Context, sessionID string, port int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if sessionID == "" {
		return errors.New("sandbox/k8s: UnexposePort: sessionID is required")
	}
	if port <= 0 || port > 65535 {
		return fmt.Errorf("sandbox/k8s: UnexposePort: invalid port %d", port)
	}
	if b.client == nil {
		return errors.New("sandbox/k8s: UnexposePort: no Kubernetes client configured")
	}
	name := serviceNameForPort(sessionID, port)
	err := b.client.CoreV1().Services(b.cfg.Namespace).
		Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("sandbox/k8s: UnexposePort(%s, %d): %w", sessionID, port, err)
	}
	return nil
}

// normaliseProtocol converts a sandbox-level protocol string to the
// corev1 enum. Defaults to TCP on empty.
func normaliseProtocol(proto string) (corev1.Protocol, error) {
	switch strings.ToLower(strings.TrimSpace(proto)) {
	case "", "tcp":
		return corev1.ProtocolTCP, nil
	case "udp":
		return corev1.ProtocolUDP, nil
	case "sctp":
		return corev1.ProtocolSCTP, nil
	default:
		return "", fmt.Errorf("unsupported protocol %q", proto)
	}
}
