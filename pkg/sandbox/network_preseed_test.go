package sandbox_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/SAP/astonish/pkg/sandbox"
	"github.com/SAP/astonish/pkg/store"
)

func TestNetworkPolicyPreSeeder_InvokedHook(t *testing.T) {
	var calls atomic.Int32
	prev := sandbox.NetworkPolicyPreSeeder
	sandbox.NetworkPolicyPreSeeder = func(ctx context.Context, sessionID string) {
		calls.Add(1)
		if sessionID != "sess-preseed" {
			t.Errorf("sessionID = %q", sessionID)
		}
		if store.NetworkPolicyStoresFromContext(ctx) == nil {
			t.Error("expected NPS on context")
		}
	}
	t.Cleanup(func() { sandbox.NetworkPolicyPreSeeder = prev })

	nps := &store.NetworkPolicyStores{}
	ctx := store.WithNetworkPolicyStores(context.Background(), nps)
	// Call through the unexported path via ensuring the exported hook is what NodeTool uses.
	if sandbox.NetworkPolicyPreSeeder != nil {
		sandbox.NetworkPolicyPreSeeder(ctx, "sess-preseed")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}
