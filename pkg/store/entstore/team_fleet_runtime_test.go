package entstore

import (
	"context"
	"testing"
	"time"

	"github.com/SAP/astonish/pkg/store"
)

func TestFleetRunStateStore_UpsertGetHeartbeatRecoverableDelete(t *testing.T) {
	ctx := context.Background()
	_, client := setupTeamStore(t)
	runStates := &teamFleetRunStateStore{client: client}

	firstHeartbeat := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
	if err := runStates.Upsert(ctx, store.FleetRunStateSnapshot{
		SessionID:       "session-1",
		PlanKey:         "plan-a",
		State:           "processing",
		ActiveAgents:    []string{"dev"},
		Ball:            "agents",
		Progress:        map[string]any{"step": "started"},
		LastHeartbeatAt: firstHeartbeat,
	}); err != nil {
		t.Fatalf("upsert create: %v", err)
	}

	got, err := runStates.Get(ctx, "session-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("get returned nil")
	}
	if got.PlanKey != "plan-a" || got.State != "processing" || got.Ball != "agents" {
		t.Fatalf("unexpected snapshot: %+v", got)
	}
	if len(got.ActiveAgents) != 1 || got.ActiveAgents[0] != "dev" {
		t.Fatalf("active agents = %v, want [dev]", got.ActiveAgents)
	}

	if err := runStates.Upsert(ctx, store.FleetRunStateSnapshot{
		SessionID:       "session-1",
		PlanKey:         "plan-a",
		State:           "waiting_for_customer",
		WaitingAgent:    "po",
		Ball:            "customer",
		Progress:        map[string]any{"step": "waiting"},
		LastHeartbeatAt: firstHeartbeat,
	}); err != nil {
		t.Fatalf("upsert update: %v", err)
	}

	recoverable, err := runStates.ListRecoverable(ctx, "plan-a")
	if err != nil {
		t.Fatalf("list recoverable: %v", err)
	}
	if len(recoverable) != 0 {
		t.Fatalf("recoverable len = %d, want 0 (ball=customer sessions stay quiet until a human replies)", len(recoverable))
	}

	if err := runStates.Upsert(ctx, store.FleetRunStateSnapshot{
		SessionID:       "session-1",
		PlanKey:         "plan-a",
		State:           "processing",
		WaitingAgent:    "",
		Ball:            "agents",
		Progress:        map[string]any{"step": "building"},
		LastHeartbeatAt: firstHeartbeat,
	}); err != nil {
		t.Fatalf("upsert agents-ball: %v", err)
	}
	recoverable, err = runStates.ListRecoverable(ctx, "plan-a")
	if err != nil {
		t.Fatalf("list recoverable agents-ball: %v", err)
	}
	if len(recoverable) != 1 {
		t.Fatalf("recoverable len = %d, want 1", len(recoverable))
	}
	if recoverable[0].Ball != "agents" {
		t.Fatalf("recoverable snapshot = %+v", recoverable[0])
	}

	nextHeartbeat := time.Now().UTC().Truncate(time.Second)
	if err := runStates.Heartbeat(ctx, "session-1", nextHeartbeat); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	got, err = runStates.Get(ctx, "session-1")
	if err != nil {
		t.Fatalf("get after heartbeat: %v", err)
	}
	if got.LastHeartbeatAt.Before(nextHeartbeat) {
		t.Fatalf("heartbeat = %s, want >= %s", got.LastHeartbeatAt, nextHeartbeat)
	}

	if err := runStates.Upsert(ctx, store.FleetRunStateSnapshot{
		SessionID:       "session-1",
		PlanKey:         "plan-a",
		State:           "stopped",
		Ball:            "agents",
		LastHeartbeatAt: nextHeartbeat,
	}); err != nil {
		t.Fatalf("upsert stopped: %v", err)
	}
	recoverable, err = runStates.ListRecoverable(ctx, "plan-a")
	if err != nil {
		t.Fatalf("list stopped recoverable: %v", err)
	}
	if len(recoverable) != 0 {
		t.Fatalf("recoverable len = %d, want 0", len(recoverable))
	}

	if err := runStates.Delete(ctx, "session-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, err = runStates.Get(ctx, "session-1")
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if got != nil {
		t.Fatalf("get after delete = %+v, want nil", got)
	}
}
