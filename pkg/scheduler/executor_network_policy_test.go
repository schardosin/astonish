package scheduler

import (
	"context"
	"testing"

	"github.com/SAP/astonish/pkg/sandbox/netpolicy"
	"github.com/SAP/astonish/pkg/sandbox/openshell"
	"github.com/SAP/astonish/pkg/store"
)

func TestExecuteRoutine_AttachesGatewayConfig(t *testing.T) {
	var gotCtx context.Context
	e := &Executor{
		GatewayConfig: &openshell.GRPCClientConfig{Addr: "openshell.example:8443"},
		FlowResolver: func(string) (string, error) {
			return "name: my-flow\nnodes: []\n", nil
		},
		RunHeadless: func(ctx context.Context, cfg *HeadlessRunConfig) (string, error) {
			gotCtx = ctx
			return "ok", nil
		},
	}
	teamStore := &stubNetPolicyStore{}
	ctx := store.WithNetworkPolicyStores(context.Background(), &store.NetworkPolicyStores{Team: teamStore})
	job := &Job{
		Name: "routine-netpol",
		Mode: ModeRoutine,
		Payload: JobPayload{
			Flow: "my-flow",
		},
	}

	out, err := e.executeRoutine(ctx, job)
	if err != nil {
		t.Fatalf("executeRoutine: %v", err)
	}
	if out != "ok" {
		t.Fatalf("result = %q, want ok", out)
	}
	gw := netpolicy.GatewayConfigFromContext(gotCtx)
	if gw == nil || gw.Addr != "openshell.example:8443" {
		t.Fatalf("expected gateway on headless ctx, got %+v", gw)
	}
	nps := store.NetworkPolicyStoresFromContext(gotCtx)
	if nps == nil || nps.Team != teamStore {
		t.Fatalf("expected team network policy store preserved, got %+v", nps)
	}
}

func TestExecuteRoutine_NoGatewayWhenUnset(t *testing.T) {
	var gotCtx context.Context
	e := &Executor{
		FlowResolver: func(string) (string, error) {
			return "name: my-flow\nnodes: []\n", nil
		},
		RunHeadless: func(ctx context.Context, _ *HeadlessRunConfig) (string, error) {
			gotCtx = ctx
			return "ok", nil
		},
	}
	job := &Job{Name: "routine", Mode: ModeRoutine, Payload: JobPayload{Flow: "my-flow"}}
	if _, err := e.executeRoutine(context.Background(), job); err != nil {
		t.Fatalf("executeRoutine: %v", err)
	}
	if gw := netpolicy.GatewayConfigFromContext(gotCtx); gw != nil {
		t.Fatalf("expected no gateway, got %+v", gw)
	}
}

type stubNetPolicyStore struct{}

func (s *stubNetPolicyStore) List(context.Context) ([]store.NetworkPolicyRule, error) {
	return nil, nil
}
func (s *stubNetPolicyStore) Get(context.Context, string) (*store.NetworkPolicyRule, error) {
	return nil, nil
}
func (s *stubNetPolicyStore) Save(context.Context, *store.NetworkPolicyRule) error { return nil }
func (s *stubNetPolicyStore) Delete(context.Context, string) error                 { return nil }
