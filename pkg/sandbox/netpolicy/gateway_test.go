package netpolicy

import (
	"context"
	"sync"
	"testing"

	"github.com/SAP/astonish/pkg/sandbox/openshell"
	"github.com/SAP/astonish/pkg/store"
)

type recordingGateway struct {
	mu             sync.Mutex
	updateCalls    int
	lastOps        []openshell.PolicyMergeOp
	waitedVersions []uint32
}

func (g *recordingGateway) CreateSandbox(context.Context, openshell.CreateSandboxRequest) (*openshell.CreateSandboxResponse, error) {
	return nil, nil
}
func (g *recordingGateway) DeleteSandbox(context.Context, string) error { return nil }
func (g *recordingGateway) GetSandboxStatus(context.Context, string) (*openshell.SandboxStatus, error) {
	return &openshell.SandboxStatus{State: openshell.SandboxStateRunning}, nil
}
func (g *recordingGateway) ExecCommand(context.Context, string, openshell.ExecRequest) (*openshell.ExecResponse, error) {
	return nil, nil
}
func (g *recordingGateway) ExecStream(context.Context, string, openshell.ExecRequest) (openshell.ExecStreamConn, error) {
	return nil, nil
}
func (g *recordingGateway) ListSandboxes(context.Context, string) ([]openshell.SandboxSummary, error) {
	return nil, nil
}
func (g *recordingGateway) GetDraftPolicy(context.Context, string, string) (*openshell.DraftPolicyResponse, error) {
	return nil, nil
}
func (g *recordingGateway) ApproveDraftChunk(context.Context, string, string) (*openshell.ApproveChunkResponse, error) {
	return nil, nil
}
func (g *recordingGateway) RejectDraftChunk(context.Context, string, string, string) error {
	return nil
}
func (g *recordingGateway) UpdateConfig(_ context.Context, _ string, ops []openshell.PolicyMergeOp) (*openshell.UpdateConfigResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.updateCalls++
	g.lastOps = append([]openshell.PolicyMergeOp(nil), ops...)
	return &openshell.UpdateConfigResponse{PolicyVersion: 7}, nil
}
func (g *recordingGateway) GetPolicyStatus(_ context.Context, _ string, version uint32) (*openshell.PolicyStatusResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.waitedVersions = append(g.waitedVersions, version)
	return &openshell.PolicyStatusResponse{ActiveVersion: version, Status: "loaded"}, nil
}
func (g *recordingGateway) WatchSandbox(context.Context, string, openshell.WatchOpts) (openshell.SandboxEventStream, error) {
	return nil, nil
}
func (g *recordingGateway) Close() error { return nil }
func (g *recordingGateway) CreateProvider(context.Context, string, string, map[string]string) error {
	return nil
}
func (g *recordingGateway) DeleteProvider(context.Context, string) error { return nil }
func (g *recordingGateway) AttachSandboxProvider(context.Context, string, string) error {
	return nil
}
func (g *recordingGateway) DetachSandboxProvider(context.Context, string, string) error {
	return nil
}

func TestPreSeedAllow_WaitsForPolicyLoad(t *testing.T) {
	rec := &recordingGateway{}
	prev := newGRPCGatewayClient
	newGRPCGatewayClient = func(openshell.GRPCClientConfig) (openshell.GatewayClient, error) {
		return rec, nil
	}
	t.Cleanup(func() { newGRPCGatewayClient = prev })

	cfg := &openshell.GRPCClientConfig{Addr: "unused:1"}
	PreSeedAllow(context.Background(), cfg, "sess-wait", []Endpoint{
		{Host: "**.cloud.sap", Port: 443},
	})

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.updateCalls != 1 {
		t.Fatalf("UpdateConfig calls = %d, want 1", rec.updateCalls)
	}
	if len(rec.waitedVersions) != 1 || rec.waitedVersions[0] != 7 {
		t.Fatalf("WaitForPolicyLoad versions = %v, want [7]", rec.waitedVersions)
	}
	if len(rec.lastOps) != 1 || rec.lastOps[0].Endpoint == nil || rec.lastOps[0].Endpoint.Host != "**.cloud.sap" {
		t.Fatalf("ops = %+v, want **.cloud.sap", rec.lastOps)
	}
}

func TestEnsurePreSeedFromContext_Idempotent(t *testing.T) {
	ClearSessionSeeded("sess-idem")
	t.Cleanup(func() { ClearSessionSeeded("sess-idem") })

	rec := &recordingGateway{}
	prev := newGRPCGatewayClient
	newGRPCGatewayClient = func(openshell.GRPCClientConfig) (openshell.GatewayClient, error) {
		return rec, nil
	}
	t.Cleanup(func() { newGRPCGatewayClient = prev })

	nps := &store.NetworkPolicyStores{
		Team: &staticPolicyStore{rules: []store.NetworkPolicyRule{
			{Host: "**.cloud.sap", Port: 443, Action: store.NetworkPolicyAllow},
		}},
	}
	ctx := store.WithNetworkPolicyStores(context.Background(), nps)
	ctx = WithGatewayConfig(ctx, &openshell.GRPCClientConfig{Addr: "unused:1"})

	EnsurePreSeedFromContext(ctx, "sess-idem")
	EnsurePreSeedFromContext(ctx, "sess-idem")

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.updateCalls != 1 {
		t.Fatalf("UpdateConfig calls = %d, want 1 (idempotent)", rec.updateCalls)
	}
	if !SessionIsSeeded("sess-idem") {
		t.Fatal("expected session marked seeded")
	}
}

type staticPolicyStore struct {
	rules []store.NetworkPolicyRule
}

func (s *staticPolicyStore) List(context.Context) ([]store.NetworkPolicyRule, error) {
	return append([]store.NetworkPolicyRule(nil), s.rules...), nil
}
func (s *staticPolicyStore) Get(context.Context, string) (*store.NetworkPolicyRule, error) {
	return nil, nil
}
func (s *staticPolicyStore) Save(context.Context, *store.NetworkPolicyRule) error { return nil }
func (s *staticPolicyStore) Delete(context.Context, string) error                 { return nil }
