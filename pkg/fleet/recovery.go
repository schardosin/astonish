package fleet

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/schardosin/astonish/pkg/store"
)

// RecoverActiveSessions recovers non-stopped fleet sessions for a plan using
// durable run-state snapshots. It deliberately preserves FleetRecoverFunc's
// one-session-at-a-time signature.
func RecoverActiveSessions(ctx context.Context, plan *FleetPlan, teamSlug string, runStates store.FleetRunStateStore, recoverFn FleetRecoverFunc) (int, error) {
	if plan == nil {
		return 0, fmt.Errorf("plan is required")
	}
	if runStates == nil || recoverFn == nil {
		return 0, nil
	}
	snaps, err := runStates.ListRecoverable(ctx, plan.Key)
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, snap := range snaps {
		if snap.State == string(StateStopped) {
			continue
		}
		cfg := RecoverFleetConfig{
			Plan:      plan,
			SessionID: snap.SessionID,
			UserID:    plan.CreatedBy,
			TeamSlug:  teamSlug,
		}
		if err := recoverFn(ctx, cfg); err != nil {
			slog.Warn("failed to recover active fleet session", "component", "fleet-recovery", "plan", plan.Key, "session_id", snap.SessionID, "error", err)
			continue
		}
		recovered++
	}
	return recovered, nil
}
