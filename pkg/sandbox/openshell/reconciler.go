// Package openshell — orphan sandbox reconciler.
//
// ReconcileOrphans lists all sandboxes known to the OpenShell gateway,
// compares them against the set of session IDs tracked in the DB, and
// deletes any sandbox that:
//
//  1. Has no matching session record in the database.
//  2. Is older than the specified grace period.
//
// This handles two scenarios:
//   - Pods leftover from before the PG session registry fix (historical orphans).
//   - Pods that became orphaned due to unclean daemon shutdown (session was
//     destroyed in DB but DeleteSandbox gRPC call never fired).
//
// The function is safe to call concurrently from multiple replicas because
// DeleteSandbox is idempotent at the gateway (deleting a non-existent sandbox
// returns success).

package openshell

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// ReconcileOrphans lists all sandboxes from the gateway, diffs them against
// knownSessionIDs (session IDs tracked in the database), and deletes any
// untracked sandbox older than gracePeriod.
//
// The comparison uses the "astonish.io/session-id" label set on every sandbox
// pod during CreateSession. This label contains the logical session ID (the DB
// primary key), which is what knownSessionIDs is keyed by. This ensures that
// pods belonging to active sessions are NEVER deleted, even across daemon
// restarts or rolling deploys.
//
// Returns the names of sandboxes that were successfully deleted. Errors
// deleting individual sandboxes are logged but do not abort the reconciliation.
func ReconcileOrphans(ctx context.Context, gateway GatewayClient, knownSessionIDs map[string]bool, gracePeriod time.Duration) ([]string, error) {
	sandboxes, err := gateway.ListSandboxes(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("reconcile orphans: list sandboxes: %w", err)
	}

	now := time.Now()
	var deleted []string

	for _, sb := range sandboxes {
		// Primary check: use the session-id label set by CreateSession.
		// This is the authoritative link between a gateway sandbox and a DB
		// session record. The label value is the logical session ID (DB primary
		// key), which is what knownSessionIDs is keyed by.
		sessionID := sb.Labels["astonish.io/session-id"]
		if sessionID != "" && knownSessionIDs[sessionID] {
			continue // session exists in DB — NOT an orphan
		}

		// Fallback: also check by sandbox name directly. Handles edge cases
		// where the label might be missing on very old pods or pods created
		// by other tooling.
		if knownSessionIDs[sb.Name] {
			continue
		}

		// Skip sandboxes within the grace period (too young to be orphans).
		if !sb.CreatedAt.IsZero() && now.Sub(sb.CreatedAt) < gracePeriod {
			slog.Debug("reconcile orphans: skipping young untracked sandbox",
				"name", sb.Name, "age", now.Sub(sb.CreatedAt).Round(time.Second))
			continue
		}

		// This sandbox is untracked and past the grace period — it's an orphan.
		slog.Info("reconcile orphans: deleting orphan sandbox",
			"name", sb.Name,
			"phase", sb.Phase,
			"age", now.Sub(sb.CreatedAt).Round(time.Second),
			"session_id_label", sessionID,
			"labels", sb.Labels)

		if err := gateway.DeleteSandbox(ctx, sb.Name); err != nil {
			slog.Warn("reconcile orphans: failed to delete sandbox",
				"name", sb.Name, "error", err)
			continue
		}
		deleted = append(deleted, sb.Name)
	}

	return deleted, nil
}

// NewGatewayClientFromConfig creates a GatewayClient from the OpenShell
// config section. This is a convenience for callers (daemon, admin handler)
// that need a gateway connection outside of the Backend's internal client.
func NewGatewayClientFromConfig(cfg GRPCClientConfig) (GatewayClient, error) {
	return NewGRPCGatewayClient(cfg)
}
