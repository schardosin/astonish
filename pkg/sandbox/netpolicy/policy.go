// Package netpolicy provides shared OpenShell egress policy matching and
// gateway apply helpers used by Studio chat and the adaptive scheduler.
package netpolicy

import (
	"context"
	"log/slog"
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

// LoadFromStores builds an EffectivePolicy from context network policy stores.
func LoadFromStores(ctx context.Context, nps *store.NetworkPolicyStores) *EffectivePolicy {
	if nps == nil {
		return &EffectivePolicy{}
	}
	return &EffectivePolicy{
		Platform: LoadRulesQuiet(ctx, nps.Platform),
		Org:      LoadRulesQuiet(ctx, nps.Org),
		Team:     LoadRulesQuiet(ctx, nps.Team),
	}
}

// LoadRulesQuiet loads rules from a store, returning empty slice on nil or error.
func LoadRulesQuiet(ctx context.Context, s store.NetworkPolicyStore) []store.NetworkPolicyRule {
	if s == nil {
		return nil
	}
	rules, err := s.List(ctx)
	if err != nil {
		slog.Warn("failed to load network policy rules", "error", err)
		return nil
	}
	return rules
}

// CollectAllowEndpoints returns all allow-list endpoints across tiers.
func CollectAllowEndpoints(ctx context.Context, nps *store.NetworkPolicyStores) []Endpoint {
	if nps == nil {
		return nil
	}
	var endpoints []Endpoint
	for _, rules := range [][]store.NetworkPolicyRule{
		LoadRulesQuiet(ctx, nps.Platform),
		LoadRulesQuiet(ctx, nps.Org),
		LoadRulesQuiet(ctx, nps.Team),
	} {
		for _, r := range rules {
			if r.Action == store.NetworkPolicyAllow {
				endpoints = append(endpoints, Endpoint{Host: r.Host, Port: r.Port})
			}
		}
	}
	return endpoints
}

// Endpoint is a host:port pair for gateway policy merge ops.
type Endpoint struct {
	Host string
	Port uint32
}

// Check evaluates the effective policy for a given host:port.
//
// Deny-wins-from-above semantics:
//  1. If ANY rule at platform level matches and is deny → PolicyDeny (cannot be overridden).
//  2. If ANY rule at org level matches and is deny → PolicyDeny (cannot be overridden by team).
//  3. If ANY rule at team level matches and is deny → PolicyDeny.
//  4. If ANY rule at any level matches and is allow → PolicyAllow.
//  5. Otherwise → PolicyUnknown.
func (ep *EffectivePolicy) Check(host string, port uint32) PolicyDecision {
	if ep == nil {
		return PolicyUnknown
	}
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

func ruleMatchesEndpoint(rule store.NetworkPolicyRule, host string, port uint32) bool {
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
	pattern = strings.ToLower(pattern)
	host = strings.ToLower(host)

	if pattern == host {
		return true
	}
	if pattern == "*" || pattern == "**" {
		return true
	}
	if strings.HasPrefix(pattern, "**.") {
		suffix := pattern[3:]
		if host == suffix {
			return true
		}
		return strings.HasSuffix(host, "."+suffix)
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[2:]
		if !strings.HasSuffix(host, "."+suffix) {
			return false
		}
		prefix := host[:len(host)-len(suffix)-1]
		return !strings.Contains(prefix, ".")
	}
	return false
}
