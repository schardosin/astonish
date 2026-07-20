package api

import (
	"context"

	"github.com/SAP/astonish/pkg/sandbox/netpolicy"
	"github.com/SAP/astonish/pkg/store"
)

// PolicyDecision represents the outcome of an effective policy check.
type PolicyDecision = netpolicy.PolicyDecision

const (
	// PolicyUnknown means no rule matched — show interactive approval dialog.
	PolicyUnknown = netpolicy.PolicyUnknown
	// PolicyAllow means the endpoint is explicitly allowed — auto-approve silently.
	PolicyAllow = netpolicy.PolicyAllow
	// PolicyDeny means the endpoint is explicitly denied — suppress dialog entirely.
	PolicyDeny = netpolicy.PolicyDeny
)

// EffectivePolicy holds all three tiers of network policy rules.
type EffectivePolicy = netpolicy.EffectivePolicy

// LoadEffectivePolicy loads rules from all available stores in Services.
func LoadEffectivePolicy(ctx context.Context, svc *store.Services) (*EffectivePolicy, error) {
	ep := &EffectivePolicy{}

	if svc.PlatformNetworkPolicies != nil {
		rules, err := svc.PlatformNetworkPolicies.List(ctx)
		if err != nil {
			return nil, err
		}
		ep.Platform = rules
	}
	if svc.NetworkPolicies != nil {
		rules, err := svc.NetworkPolicies.List(ctx)
		if err != nil {
			return nil, err
		}
		ep.Org = rules
	}
	if svc.TeamNetworkPolicies != nil {
		rules, err := svc.TeamNetworkPolicies.List(ctx)
		if err != nil {
			return nil, err
		}
		ep.Team = rules
	}

	return ep, nil
}

// HostMatches returns true if the pattern matches the given host.
func HostMatches(pattern, host string) bool {
	return netpolicy.HostMatches(pattern, host)
}
