package openshell

import (
	"testing"

	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/sandbox"
)

func TestMergeSessionNetworkAllows_AppendsCloudSAP(t *testing.T) {
	cfg := config.SandboxOpenShellConfig{
		NetworkPolicy: config.NetworkPolicyConfig{Presets: []string{"search"}},
	}
	policy := mergeSessionNetworkAllows(defaultSandboxPolicy(cfg), []sandbox.NetworkAllowEndpoint{
		{Host: "**.cloud.sap", Port: 443},
		{Host: "identity-3.qa-de-1.cloud.sap", Port: 443},
	})
	if policy == nil || policy.NetworkPolicies == nil {
		t.Fatal("expected network policies")
	}
	egress := policy.NetworkPolicies["egress"]
	if egress == nil {
		t.Fatal("expected astonish-egress rule")
	}
	foundBroader, foundExact := false, false
	for _, ep := range egress.Endpoints {
		if ep.Host == "**.cloud.sap" && ep.Port == 443 {
			foundBroader = true
		}
		if ep.Host == "identity-3.qa-de-1.cloud.sap" {
			foundExact = true
		}
	}
	if !foundBroader || !foundExact {
		t.Fatalf("endpoints missing cloud.sap allows: broader=%v exact=%v got=%+v",
			foundBroader, foundExact, egress.Endpoints)
	}
}

func TestMergeSessionNetworkAllows_CreatesEgressWhenEmpty(t *testing.T) {
	policy := &SandboxPolicySpec{Version: 1}
	policy = mergeSessionNetworkAllows(policy, []sandbox.NetworkAllowEndpoint{
		{Host: "**.cloud.sap", Port: 443},
	})
	egress := policy.NetworkPolicies["egress"]
	if egress == nil || len(egress.Endpoints) != 1 || egress.Endpoints[0].Host != "**.cloud.sap" {
		t.Fatalf("unexpected policy: %+v", policy.NetworkPolicies)
	}
}
