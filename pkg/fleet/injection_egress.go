package fleet

import (
	"context"
	"log/slog"

	"github.com/SAP/astonish/pkg/sandbox/openshell"
)

// credentialEgressHints maps logical credential names to optional L7 egress hints.
// v1 returns empty — future versions may tighten network policy per credential type.
var credentialEgressHints = map[string][]openshell.EndpointSpec{
	// Example (disabled): "trading": {{Host: "api.alpaca.markets", Port: 443}},
}

// ApplyOptionalCredentialEgress merges credential-specific egress hints into the
// sandbox L7 policy when an OpenShell gateway is available. No-op when no hints
// are configured for the plan's credentials.
func ApplyOptionalCredentialEgress(ctx context.Context, gateway openshell.GatewayClient, sandboxName string, plan *FleetPlan) {
	if gateway == nil || plan == nil || sandboxName == "" {
		return
	}
	var ops []openshell.PolicyMergeOp
	for logicalName := range plan.Credentials {
		for _, ep := range credentialEgressHints[logicalName] {
			epCopy := ep
			ops = append(ops, openshell.PolicyMergeOp{
				Type:     openshell.PolicyMergeAddEndpoint,
				RuleName: "astonish-egress",
				Endpoint: &epCopy,
			})
		}
	}
	if len(ops) == 0 {
		return
	}
	if _, err := gateway.UpdateConfig(ctx, sandboxName, ops); err != nil {
		slog.Warn("optional credential egress policy merge failed",
			"component", "fleet-injection",
			"sandbox", sandboxName,
			"plan", plan.Key,
			"error", err,
		)
		return
	}
	slog.Info("optional credential egress policy applied",
		"component", "fleet-injection",
		"sandbox", sandboxName,
		"plan", plan.Key,
		"endpoints", len(ops),
	)
}
