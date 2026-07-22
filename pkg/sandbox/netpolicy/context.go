package netpolicy

import (
	"context"
	"sync"

	"github.com/SAP/astonish/pkg/sandbox/openshell"
	"github.com/SAP/astonish/pkg/store"
)

type gatewayConfigKey struct{}

// WithGatewayConfig attaches OpenShell gateway gRPC settings so NodeTool can
// PreSeed network allow rules before the first in-sandbox HTTP call.
func WithGatewayConfig(ctx context.Context, cfg *openshell.GRPCClientConfig) context.Context {
	if ctx == nil || cfg == nil {
		return ctx
	}
	return context.WithValue(ctx, gatewayConfigKey{}, cfg)
}

// GatewayConfigFromContext returns the OpenShell gateway config, or nil.
func GatewayConfigFromContext(ctx context.Context) *openshell.GRPCClientConfig {
	if ctx == nil {
		return nil
	}
	cfg, _ := ctx.Value(gatewayConfigKey{}).(*openshell.GRPCClientConfig)
	return cfg
}

// seededSessions tracks session IDs that already received a PreSeed (or had
// nothing to seed). Shared by NodeTool, ChatRunner, and SessionBridge so
// post-result PreSeed is a no-op after the before-Call path.
var seededSessions sync.Map // map[string]struct{}

// MarkSessionSeeded records that allow-list PreSeed has been applied (or
// skipped as a no-op) for sessionID.
func MarkSessionSeeded(sessionID string) {
	if sessionID == "" {
		return
	}
	seededSessions.Store(sessionID, struct{}{})
}

// SessionIsSeeded reports whether PreSeed already ran for sessionID.
func SessionIsSeeded(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	_, ok := seededSessions.Load(sessionID)
	return ok
}

// ClearSessionSeeded removes the seeded mark (tests / session destroy).
func ClearSessionSeeded(sessionID string) {
	if sessionID == "" {
		return
	}
	seededSessions.Delete(sessionID)
}

// EnsurePreSeedFromContext pushes DB NetworkPolicyAllow endpoints into the
// sandbox proxy before first egress. Idempotent per sessionID once a seed
// succeeds (or there is nothing to seed). Best-effort for callers: failures
// are logged inside PreSeedAllow and left unseeded so the next request retries.
func EnsurePreSeedFromContext(ctx context.Context, sessionID string) {
	if sessionID == "" || SessionIsSeeded(sessionID) {
		return
	}
	gw := GatewayConfigFromContext(ctx)
	nps := store.NetworkPolicyStoresFromContext(ctx)
	if gw == nil {
		return
	}
	endpoints := CollectAllowEndpoints(ctx, nps)
	if len(endpoints) == 0 {
		MarkSessionSeeded(sessionID)
		return
	}
	if err := PreSeedAllow(ctx, gw, sessionID, endpoints); err != nil {
		return
	}
	MarkSessionSeeded(sessionID)
}
