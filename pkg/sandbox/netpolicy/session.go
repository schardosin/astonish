package netpolicy

import (
	"context"

	"github.com/SAP/astonish/pkg/sandbox/openshell"
	"github.com/SAP/astonish/pkg/store"
)

// SessionBridge applies network policy to a headless session (scheduler, etc.).
type SessionBridge struct {
	GatewayCfg *openshell.GRPCClientConfig
	SessionID  string
	Stores     *store.NetworkPolicyStores
	seeded     bool
}

// OnToolResult pre-seeds allow rules on first tool result and auto-approves
// PolicyAllow denials. Safe to call for every tool response.
func (b *SessionBridge) OnToolResult(ctx context.Context, toolName string, resp map[string]any, fallbackURL string) {
	if b == nil || b.SessionID == "" {
		return
	}
	if !b.seeded {
		b.seeded = true
		PreSeedFromStores(ctx, b.GatewayCfg, b.SessionID, b.Stores)
	}
	if resp == nil {
		return
	}

	ep := LoadFromStores(ctx, b.Stores)
	var denials []map[string]any
	switch {
	case toolName == "shell_command" && LooksLikeNetworkDenial(resp):
		stdout, _ := resp["stdout"].(string)
		denials = ExtractDenialsFromOutput(stdout)
	case IsNetworkTool(toolName) && LooksLikeNetworkToolDenial(resp):
		denials = ExtractDenialFromToolError(resp, fallbackURL)
	}
	if len(denials) == 0 {
		return
	}
	// Discard unknown — headless has no approval UI.
	_ = ApplyPolicyAllowDenials(ctx, b.GatewayCfg, b.SessionID, ep, denials)
}
