package netpolicy

import (
	"context"
	"log/slog"
	"time"

	"github.com/SAP/astonish/pkg/sandbox/openshell"
	"github.com/SAP/astonish/pkg/store"
)

// newGRPCGatewayClient is the factory used by PreSeed/AutoApprove. Tests
// replace it to inject a mock gateway without dialing gRPC.
var newGRPCGatewayClient = openshell.NewGRPCGatewayClient

// WaitForPolicyLoad polls the gateway until the sandbox proxy confirms it has
// loaded the specified policy version (or a newer one).
func WaitForPolicyLoad(ctx context.Context, gateway openshell.GatewayClient, sandboxName string, targetVersion uint32) {
	const (
		pollInterval = 100 * time.Millisecond
		maxWait      = 5 * time.Second
		fallbackWait = 500 * time.Millisecond
	)

	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		status, err := gateway.GetPolicyStatus(ctx, sandboxName, targetVersion)
		if err != nil {
			slog.Debug("waitForPolicyLoad: GetPolicyStatus failed, using fallback delay",
				"sandbox", sandboxName, "version", targetVersion, "error", err)
			time.Sleep(fallbackWait)
			return
		}

		if status.ActiveVersion >= targetVersion || status.Status == "loaded" {
			slog.Debug("waitForPolicyLoad: policy loaded",
				"sandbox", sandboxName, "version", targetVersion,
				"activeVersion", status.ActiveVersion, "status", status.Status)
			return
		}

		if status.Status == "failed" {
			slog.Warn("waitForPolicyLoad: policy failed to load",
				"sandbox", sandboxName, "version", targetVersion, "status", status.Status)
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(pollInterval):
		}
	}

	slog.Warn("waitForPolicyLoad: timed out waiting for policy",
		"sandbox", sandboxName, "version", targetVersion, "maxWait", maxWait)
}

// PreSeedAllow pushes all allow-list endpoints into the sandbox proxy.
// Best-effort: errors are logged but never returned.
func PreSeedAllow(ctx context.Context, gatewayCfg *openshell.GRPCClientConfig, sessionID string, endpoints []Endpoint) {
	if gatewayCfg == nil || len(endpoints) == 0 || sessionID == "" {
		return
	}

	sandboxName := openshell.SandboxName(sessionID)
	gateway, err := newGRPCGatewayClient(*gatewayCfg)
	if err != nil {
		slog.Warn("pre-seed network policy: failed to create gateway client",
			"session", sessionID, "error", err)
		return
	}
	defer gateway.Close()

	ops := make([]openshell.PolicyMergeOp, 0, len(endpoints))
	for _, ep := range endpoints {
		ops = append(ops, openshell.PolicyMergeOp{
			Type:     openshell.PolicyMergeAddEndpoint,
			RuleName: "astonish-egress",
			Endpoint: &openshell.EndpointSpec{Host: ep.Host, Port: ep.Port},
		})
	}

	resp, err := gateway.UpdateConfig(ctx, sandboxName, ops)
	if err != nil {
		slog.Warn("pre-seed network policy: UpdateConfig failed",
			"sandbox", sandboxName, "session", sessionID, "endpoints", len(endpoints), "error", err)
		return
	}

	WaitForPolicyLoad(ctx, gateway, sandboxName, resp.PolicyVersion)
	slog.Info("pre-seeded network policy with allow-list endpoints",
		"sandbox", sandboxName, "session", sessionID, "count", len(endpoints),
		"policyVersion", resp.PolicyVersion)
}

// PreSeedFromStores collects allow endpoints from stores and pre-seeds the sandbox.
func PreSeedFromStores(ctx context.Context, gatewayCfg *openshell.GRPCClientConfig, sessionID string, nps *store.NetworkPolicyStores) {
	PreSeedAllow(ctx, gatewayCfg, sessionID, CollectAllowEndpoints(ctx, nps))
}

// AutoApproveEndpoint adds a single host:port to the running sandbox policy.
func AutoApproveEndpoint(ctx context.Context, gatewayCfg *openshell.GRPCClientConfig, sessionID, host string, port uint32) {
	if gatewayCfg == nil || sessionID == "" || host == "" {
		return
	}
	if port == 0 {
		port = 443
	}

	sandboxName := openshell.SandboxName(sessionID)
	gateway, err := newGRPCGatewayClient(*gatewayCfg)
	if err != nil {
		slog.Warn("auto-approve: failed to create gateway client",
			"host", host, "port", port, "error", err)
		return
	}
	defer gateway.Close()

	ops := []openshell.PolicyMergeOp{
		{
			Type:     openshell.PolicyMergeAddEndpoint,
			RuleName: "astonish-egress",
			Endpoint: &openshell.EndpointSpec{Host: host, Port: port},
		},
	}

	resp, err := gateway.UpdateConfig(ctx, sandboxName, ops)
	if err != nil {
		slog.Warn("auto-approve: failed to update sandbox policy",
			"host", host, "port", port, "sandbox", sandboxName, "error", err)
		return
	}

	WaitForPolicyLoad(ctx, gateway, sandboxName, resp.PolicyVersion)
	slog.Info("auto-approved network access via policy",
		"host", host, "port", port, "sandbox", sandboxName, "session", sessionID)
}

// ApplyPolicyAllowDenials auto-approves denials that match PolicyAllow and
// returns the remaining PolicyUnknown denials (for interactive UI). PolicyDeny
// denials are dropped. When gatewayCfg is nil, PolicyAllow denials are also
// returned as unknown (cannot auto-approve).
func ApplyPolicyAllowDenials(
	ctx context.Context,
	gatewayCfg *openshell.GRPCClientConfig,
	sessionID string,
	ep *EffectivePolicy,
	denials []map[string]any,
) []map[string]any {
	if len(denials) == 0 {
		return nil
	}
	if ep == nil {
		ep = &EffectivePolicy{}
	}

	var unknown []map[string]any
	for _, d := range denials {
		host, _ := d["host"].(string)
		port := uint32(443)
		switch p := d["port"].(type) {
		case int:
			port = uint32(p)
		case float64:
			port = uint32(p)
		case uint32:
			port = p
		}

		switch ep.Check(host, port) {
		case PolicyAllow:
			if gatewayCfg != nil {
				AutoApproveEndpoint(ctx, gatewayCfg, sessionID, host, port)
			} else {
				unknown = append(unknown, d)
			}
		case PolicyDeny:
			slog.Info("network denial suppressed by policy",
				"host", host, "port", port, "session", sessionID)
		default:
			unknown = append(unknown, d)
		}
	}
	return unknown
}
