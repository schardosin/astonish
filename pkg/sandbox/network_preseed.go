package sandbox

import "context"

// NetworkPolicyPreSeeder pushes DB NetworkPolicyAllow endpoints into the
// OpenShell sandbox proxy after EnsureReady and before the first tool Call.
// Set by packages that can import netpolicy (daemon, api, launcher) to avoid
// an import cycle (sandbox → netpolicy → openshell → sandbox).
//
// Signature matches netpolicy.EnsurePreSeedFromContext.
var NetworkPolicyPreSeeder func(ctx context.Context, sessionID string)

func ensureNetworkPolicyPreSeed(ctx context.Context, sessionID string) {
	if NetworkPolicyPreSeeder == nil || sessionID == "" {
		return
	}
	NetworkPolicyPreSeeder(ctx, sessionID)
}
