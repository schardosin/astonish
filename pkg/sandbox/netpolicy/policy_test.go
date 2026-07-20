package netpolicy

import (
	"testing"

	"github.com/SAP/astonish/pkg/store"
)

func TestHostMatches(t *testing.T) {
	tests := []struct {
		pattern string
		host    string
		want    bool
	}{
		{"api.example.com", "api.example.com", true},
		{"api.example.com", "other.example.com", false},
		{"api.example.com", "API.Example.COM", true},
		{"*", "anything.example.com", true},
		{"**", "deep.sub.anything.com", true},
		{"*.example.com", "api.example.com", true},
		{"*.example.com", "deep.sub.example.com", false},
		{"**.example.com", "api.example.com", true},
		{"**.example.com", "deep.sub.example.com", true},
		{"**.example.com", "example.com", true},
		{"**.cloud.sap", "identity-3.qa-de-1.cloud.sap", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_vs_"+tt.host, func(t *testing.T) {
			got := HostMatches(tt.pattern, tt.host)
			if got != tt.want {
				t.Errorf("HostMatches(%q, %q) = %v, want %v", tt.pattern, tt.host, got, tt.want)
			}
		})
	}
}

func TestEffectivePolicy_Check(t *testing.T) {
	ep := &EffectivePolicy{
		Team: []store.NetworkPolicyRule{
			{Host: "**.cloud.sap", Port: 443, Action: store.NetworkPolicyAllow},
		},
	}
	if got := ep.Check("identity-3.qa-de-1.cloud.sap", 443); got != PolicyAllow {
		t.Fatalf("Check = %v, want PolicyAllow", got)
	}
	if got := ep.Check("evil.example.com", 443); got != PolicyUnknown {
		t.Fatalf("Check = %v, want PolicyUnknown", got)
	}

	ep.Platform = []store.NetworkPolicyRule{
		{Host: "**.cloud.sap", Port: 0, Action: store.NetworkPolicyDeny},
	}
	if got := ep.Check("identity-3.qa-de-1.cloud.sap", 443); got != PolicyDeny {
		t.Fatalf("platform deny should win, got %v", got)
	}
}

func TestApplyPolicyAllowDenials_AutoApprovesAllow(t *testing.T) {
	ep := &EffectivePolicy{
		Team: []store.NetworkPolicyRule{
			{Host: "**.cloud.sap", Port: 0, Action: store.NetworkPolicyAllow},
		},
	}
	denials := []map[string]any{
		{"host": "identity-3.qa-de-1.cloud.sap", "port": 443},
		{"host": "unknown.example.com", "port": 443},
	}
	// No gateway — PolicyAllow cannot auto-approve, both return as unknown-ish;
	// with nil gateway, Allow denials are kept in unknown list.
	unknown := ApplyPolicyAllowDenials(t.Context(), nil, "sess-1", ep, denials)
	if len(unknown) != 2 {
		t.Fatalf("with nil gateway expected 2 unknowns, got %d", len(unknown))
	}

	// Deny is dropped even without gateway.
	ep.Team = []store.NetworkPolicyRule{
		{Host: "blocked.example.com", Port: 0, Action: store.NetworkPolicyDeny},
	}
	denials = []map[string]any{
		{"host": "blocked.example.com", "port": 443},
		{"host": "unknown.example.com", "port": 443},
	}
	unknown = ApplyPolicyAllowDenials(t.Context(), nil, "sess-1", ep, denials)
	if len(unknown) != 1 {
		t.Fatalf("expected 1 unknown after deny drop, got %d", len(unknown))
	}
	if unknown[0]["host"] != "unknown.example.com" {
		t.Fatalf("unexpected unknown: %v", unknown[0])
	}
}

func TestLooksLikeNetworkDenial_Proxy403(t *testing.T) {
	resp := map[string]any{
		"stdout":    "curl: (56) CONNECT tunnel failed, response 403\n",
		"exit_code": 0,
	}
	if !LooksLikeNetworkDenial(resp) {
		t.Fatal("expected proxy 403 to look like network denial")
	}
}
