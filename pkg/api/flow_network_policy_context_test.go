package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/sandbox/netpolicy"
	"github.com/SAP/astonish/pkg/store"
)

func TestFlowRuntimeNetworkPolicyContext_AttachesStoresAndGateway(t *testing.T) {
	platformStore := &stubNetworkPolicyStore{rules: []store.NetworkPolicyRule{{
		Host:   "platform.example.com",
		Port:   443,
		Action: store.NetworkPolicyAllow,
	}}}
	orgStore := &stubNetworkPolicyStore{rules: []store.NetworkPolicyRule{{
		Host:   "org.example.com",
		Port:   443,
		Action: store.NetworkPolicyAllow,
	}}}
	teamStore := &stubNetworkPolicyStore{rules: []store.NetworkPolicyRule{{
		Host:   "api.example.com",
		Port:   443,
		Action: store.NetworkPolicyAllow,
	}}}
	r := httptest.NewRequest(http.MethodPost, "/api/agents/test/run", nil)
	r = r.WithContext(store.WithServices(r.Context(), &store.Services{
		PlatformNetworkPolicies: platformStore,
		NetworkPolicies:         orgStore,
		TeamNetworkPolicies:     teamStore,
	}))

	appCfg := &config.AppConfig{}
	appCfg.Sandbox.OpenShell.GatewayAddr = "openshell.example:8443"

	ctx := withRuntimeNetworkPolicyContext(context.Background(), r, appCfg)
	nps := store.NetworkPolicyStoresFromContext(ctx)
	if nps == nil || nps.Platform != platformStore || nps.Org != orgStore || nps.Team != teamStore {
		t.Fatalf("expected all network policy stores on flow context, got %+v", nps)
	}
	gw := netpolicy.GatewayConfigFromContext(ctx)
	if gw == nil || gw.Addr != "openshell.example:8443" {
		t.Fatalf("expected OpenShell gateway config on flow context, got %+v", gw)
	}
}
