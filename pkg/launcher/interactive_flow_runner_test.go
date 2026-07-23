package launcher

import (
	"context"
	"testing"

	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/sandbox/netpolicy"
	"github.com/SAP/astonish/pkg/store"
)

func TestWithFlowGatewayConfig_AttachesGateway(t *testing.T) {
	appCfg := &config.AppConfig{}
	appCfg.Sandbox.OpenShell.GatewayAddr = "openshell.example:8443"

	ctx := withFlowGatewayConfig(context.Background(), appCfg)
	gw := netpolicy.GatewayConfigFromContext(ctx)
	if gw == nil || gw.Addr != "openshell.example:8443" {
		t.Fatalf("expected OpenShell gateway config on flow context, got %+v", gw)
	}
}

func TestWithFlowGatewayConfig_NoGatewayLeavesContextUnset(t *testing.T) {
	ctx := withFlowGatewayConfig(context.Background(), &config.AppConfig{})
	if gw := netpolicy.GatewayConfigFromContext(ctx); gw != nil {
		t.Fatalf("expected no gateway config, got %+v", gw)
	}
}

func TestWithFlowNetworkPolicyContext_AttachesStoresFromServicesAndGateway(t *testing.T) {
	platformStore := &stubFlowNetPolicyStore{}
	orgStore := &stubFlowNetPolicyStore{}
	teamStore := &stubFlowNetPolicyStore{}
	ctx := store.WithServices(context.Background(), &store.Services{
		PlatformNetworkPolicies: platformStore,
		NetworkPolicies:         orgStore,
		TeamNetworkPolicies:     teamStore,
	})
	appCfg := &config.AppConfig{}
	appCfg.Sandbox.OpenShell.GatewayAddr = "openshell.example:8443"

	ctx = withFlowNetworkPolicyContext(ctx, appCfg)
	nps := store.NetworkPolicyStoresFromContext(ctx)
	if nps == nil || nps.Platform != platformStore || nps.Org != orgStore || nps.Team != teamStore {
		t.Fatalf("expected network policy stores from Services, got %+v", nps)
	}
	gw := netpolicy.GatewayConfigFromContext(ctx)
	if gw == nil || gw.Addr != "openshell.example:8443" {
		t.Fatalf("expected gateway on flow context, got %+v", gw)
	}
}

func TestWithFlowNetworkPolicyContext_PreservesExistingStores(t *testing.T) {
	existingTeam := &stubFlowNetPolicyStore{}
	ctx := store.WithNetworkPolicyStores(context.Background(), &store.NetworkPolicyStores{Team: existingTeam})
	// Services with different stores must not overwrite ChatRunner-injected stores.
	ctx = store.WithServices(ctx, &store.Services{
		TeamNetworkPolicies: &stubFlowNetPolicyStore{},
	})
	appCfg := &config.AppConfig{}
	appCfg.Sandbox.OpenShell.GatewayAddr = "openshell.example:8443"

	ctx = withFlowNetworkPolicyContext(ctx, appCfg)
	nps := store.NetworkPolicyStoresFromContext(ctx)
	if nps == nil || nps.Team != existingTeam {
		t.Fatalf("expected existing team store preserved, got %+v", nps)
	}
}

type stubFlowNetPolicyStore struct{}

func (s *stubFlowNetPolicyStore) List(context.Context) ([]store.NetworkPolicyRule, error) {
	return nil, nil
}
func (s *stubFlowNetPolicyStore) Get(context.Context, string) (*store.NetworkPolicyRule, error) {
	return nil, nil
}
func (s *stubFlowNetPolicyStore) Save(context.Context, *store.NetworkPolicyRule) error { return nil }
func (s *stubFlowNetPolicyStore) Delete(context.Context, string) error                 { return nil }
