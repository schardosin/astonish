package api

import (
	"testing"

	"github.com/schardosin/astonish/pkg/store"
)

func TestHostMatches(t *testing.T) {
	tests := []struct {
		pattern string
		host    string
		want    bool
	}{
		// Exact match
		{"api.example.com", "api.example.com", true},
		{"api.example.com", "other.example.com", false},
		{"api.example.com", "API.Example.COM", true}, // case-insensitive

		// Bare wildcards
		{"*", "anything.example.com", true},
		{"**", "deep.sub.anything.com", true},

		// Single-level wildcard
		{"*.example.com", "api.example.com", true},
		{"*.example.com", "sub.example.com", true},
		{"*.example.com", "deep.sub.example.com", false}, // too deep
		{"*.example.com", "example.com", false},          // no subdomain

		// Multi-level wildcard
		{"**.example.com", "api.example.com", true},
		{"**.example.com", "deep.sub.example.com", true},
		{"**.example.com", "a.b.c.d.example.com", true},
		{"**.example.com", "example.com", true},  // suffix itself matches
		{"**.example.com", "notexample.com", false},
		{"**.example.com", "fakeexample.com", false},

		// Edge cases
		{"*.com", "example.com", true},
		{"*.com", "sub.example.com", false},
		{"**.com", "sub.example.com", true},
		{"localhost", "localhost", true},
		{"localhost", "LOCALHOST", true},
		{"*.localhost", "sub.localhost", true},
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
	tests := []struct {
		name     string
		policy   EffectivePolicy
		host     string
		port     uint32
		want     PolicyDecision
	}{
		{
			name:   "no rules → unknown",
			policy: EffectivePolicy{},
			host:   "api.example.com",
			port:   443,
			want:   PolicyUnknown,
		},
		{
			name: "platform allow → allow",
			policy: EffectivePolicy{
				Platform: []store.NetworkPolicyRule{
					{Host: "*.example.com", Port: 0, Action: store.NetworkPolicyAllow},
				},
			},
			host: "api.example.com",
			port: 443,
			want: PolicyAllow,
		},
		{
			name: "platform deny → deny (even if team allows)",
			policy: EffectivePolicy{
				Platform: []store.NetworkPolicyRule{
					{Host: "**.evil.com", Port: 0, Action: store.NetworkPolicyDeny},
				},
				Team: []store.NetworkPolicyRule{
					{Host: "safe.evil.com", Port: 443, Action: store.NetworkPolicyAllow},
				},
			},
			host: "safe.evil.com",
			port: 443,
			want: PolicyDeny,
		},
		{
			name: "org deny overrides team allow",
			policy: EffectivePolicy{
				Org: []store.NetworkPolicyRule{
					{Host: "blocked.com", Port: 0, Action: store.NetworkPolicyDeny},
				},
				Team: []store.NetworkPolicyRule{
					{Host: "blocked.com", Port: 8080, Action: store.NetworkPolicyAllow},
				},
			},
			host: "blocked.com",
			port: 8080,
			want: PolicyDeny,
		},
		{
			name: "team allow without higher deny → allow",
			policy: EffectivePolicy{
				Team: []store.NetworkPolicyRule{
					{Host: "api.service.com", Port: 443, Action: store.NetworkPolicyAllow},
				},
			},
			host: "api.service.com",
			port: 443,
			want: PolicyAllow,
		},
		{
			name: "port 0 matches any port",
			policy: EffectivePolicy{
				Team: []store.NetworkPolicyRule{
					{Host: "api.service.com", Port: 0, Action: store.NetworkPolicyAllow},
				},
			},
			host: "api.service.com",
			port: 9999,
			want: PolicyAllow,
		},
		{
			name: "specific port does not match different port",
			policy: EffectivePolicy{
				Team: []store.NetworkPolicyRule{
					{Host: "api.service.com", Port: 443, Action: store.NetworkPolicyAllow},
				},
			},
			host: "api.service.com",
			port: 8080,
			want: PolicyUnknown,
		},
		{
			name: "deny at same tier wins over allow at same tier",
			policy: EffectivePolicy{
				Team: []store.NetworkPolicyRule{
					{Host: "*.example.com", Port: 0, Action: store.NetworkPolicyAllow},
					{Host: "bad.example.com", Port: 0, Action: store.NetworkPolicyDeny},
				},
			},
			host: "bad.example.com",
			port: 443,
			want: PolicyDeny,
		},
		{
			name: "no match for unrelated host → unknown",
			policy: EffectivePolicy{
				Platform: []store.NetworkPolicyRule{
					{Host: "*.internal.com", Port: 0, Action: store.NetworkPolicyAllow},
				},
				Org: []store.NetworkPolicyRule{
					{Host: "**.corp.net", Port: 0, Action: store.NetworkPolicyAllow},
				},
			},
			host: "random.internet.org",
			port: 443,
			want: PolicyUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.policy.Check(tt.host, tt.port)
			if got != tt.want {
				t.Errorf("Check(%q, %d) = %v, want %v", tt.host, tt.port, got, tt.want)
			}
		})
	}
}
