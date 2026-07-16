package api

import (
	"context"
	"strings"

	"github.com/SAP/astonish/pkg/store"
)

// PolicyDecision represents the outcome of an effective policy check.
type PolicyDecision int

const (
	// PolicyUnknown means no rule matched — show interactive approval dialog.
	PolicyUnknown PolicyDecision = iota
	// PolicyAllow means the endpoint is explicitly allowed — auto-approve silently.
	PolicyAllow
	// PolicyDeny means the endpoint is explicitly denied — suppress dialog entirely.
	PolicyDeny
)

// EffectivePolicy holds all three tiers of network policy rules and implements
// the deny-wins-from-above merge evaluation.
type EffectivePolicy struct {
	Platform []store.NetworkPolicyRule
	Org      []store.NetworkPolicyRule
	Team     []store.NetworkPolicyRule
}

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

// Check evaluates the effective policy for a given host:port.
//
// Deny-wins-from-above semantics:
//  1. If ANY rule at platform level matches and is deny → PolicyDeny (cannot be overridden).
//  2. If ANY rule at org level matches and is deny → PolicyDeny (cannot be overridden by team).
//  3. If ANY rule at team level matches and is deny → PolicyDeny.
//  4. If ANY rule at any level matches and is allow → PolicyAllow.
//  5. Otherwise → PolicyUnknown.
//
// Within each tier, deny takes priority over allow for the same endpoint.
func (ep *EffectivePolicy) Check(host string, port uint32) PolicyDecision {
	// Check from highest tier (platform) to lowest (team).
	// Any deny at a higher tier is absolute.
	tiers := [][]store.NetworkPolicyRule{ep.Platform, ep.Org, ep.Team}

	hasAnyAllow := false

	for _, rules := range tiers {
		tierHasDeny := false
		tierHasAllow := false

		for _, rule := range rules {
			if !ruleMatchesEndpoint(rule, host, port) {
				continue
			}
			switch rule.Action {
			case store.NetworkPolicyDeny:
				tierHasDeny = true
			case store.NetworkPolicyAllow:
				tierHasAllow = true
			}
		}

		// Deny at any tier is final — cannot be overridden by lower tiers.
		if tierHasDeny {
			return PolicyDeny
		}
		if tierHasAllow {
			hasAnyAllow = true
		}
	}

	if hasAnyAllow {
		return PolicyAllow
	}
	return PolicyUnknown
}

// ruleMatchesEndpoint checks if a rule's host pattern and port match the given endpoint.
func ruleMatchesEndpoint(rule store.NetworkPolicyRule, host string, port uint32) bool {
	// Port check: rule.Port == 0 means "any port"
	if rule.Port != 0 && rule.Port != port {
		return false
	}
	return HostMatches(rule.Host, host)
}

// HostMatches returns true if the pattern matches the given host.
//
// Patterns:
//   - Exact match: "api.example.com" matches only "api.example.com"
//   - Single-level wildcard: "*.example.com" matches "api.example.com"
//     but NOT "deep.sub.example.com"
//   - Multi-level wildcard: "**.example.com" matches "api.example.com",
//     "deep.sub.example.com", and any depth of subdomains.
//   - Bare wildcard: "*" or "**" matches everything.
func HostMatches(pattern, host string) bool {
	// Normalize to lowercase
	pattern = strings.ToLower(pattern)
	host = strings.ToLower(host)

	// Exact match
	if pattern == host {
		return true
	}

	// Bare wildcards
	if pattern == "*" || pattern == "**" {
		return true
	}

	// Multi-level wildcard: **.suffix
	if strings.HasPrefix(pattern, "**.") {
		suffix := pattern[3:] // everything after "**."
		// Matches if host equals suffix or ends with "."+suffix
		if host == suffix {
			return true
		}
		return strings.HasSuffix(host, "."+suffix)
	}

	// Single-level wildcard: *.suffix
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[2:] // everything after "*."
		// Must end with .suffix AND have exactly one more label before it
		if !strings.HasSuffix(host, "."+suffix) {
			return false
		}
		prefix := host[:len(host)-len(suffix)-1] // the part before ".suffix"
		// Single level means no dots in the prefix
		return !strings.Contains(prefix, ".")
	}

	return false
}
