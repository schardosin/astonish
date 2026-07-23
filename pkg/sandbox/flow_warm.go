package sandbox

import (
	"context"
	"log/slog"

	"google.golang.org/adk/tool"
)

// WarmFlowSession begins sandbox provisioning for sessionID so cold start
// overlaps the first LLM call / early flow work within the same run
// (same idea as NodeTool.ProcessRequest → BindSession in Chat).
//
// Isolation is unchanged: each flow run still uses its own session ID and
// callers must Cleanup/destroy after the run. This must not be used to
// share sandboxes across independent flow runs.
//
// Safe no-op when sessionID is empty, tools are not sandbox-wrapped, or
// the pool cannot vend a client.
func WarmFlowSession(ctx context.Context, tools []tool.Tool, sessionID string) {
	if sessionID == "" {
		return
	}
	nt := firstNodeTool(tools)
	if nt == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	client := nt.getClientFromContext(ctx, sessionID)
	if client == nil {
		return
	}
	nt.applyNetworkAllowEndpoints(ctx, client)
	client.BindSession(sessionID)

	// Finish ready + network PreSeed in the background so the caller can
	// proceed (first LLM node, MCP already done, etc.) while the pod comes up.
	go func() {
		if err := client.EnsureReady(sessionID); err != nil {
			slog.Debug("flow sandbox warm EnsureReady",
				"component", "sandbox", "session", sessionID, "error", err)
			return
		}
		ensureNetworkPolicyPreSeed(ctx, sessionID)
	}()
}

func firstNodeTool(tools []tool.Tool) *NodeTool {
	for _, t := range tools {
		if nt, ok := t.(*NodeTool); ok {
			return nt
		}
	}
	return nil
}
