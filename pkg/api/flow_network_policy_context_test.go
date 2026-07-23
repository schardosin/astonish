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
	teamStore := &stubNetworkPolicyStore{rules: []store.NetworkPolicyRule{{
		Host:   "api.example.com",
		Port:   443,
		Action: store.NetworkPolicyAllow,
	}}}
	r := httptest.NewRequest(http.MethodPost, "/api/agents/test/run", nil)
	r = r.WithContext(store.WithServices(r.Context(), &store.Services{
		TeamNetworkPolicies: teamStore,
	}))

	appCfg := &config.AppConfig{}
	appCfg.Sandbox.OpenShell.GatewayAddr = "openshell.example:8443"

	ctx := withRuntimeNetworkPolicyContext(context.Background(), r, appCfg)
	nps := store.NetworkPolicyStoresFromContext(ctx)
	if nps == nil || nps.Team != teamStore {
		t.Fatalf("expected team network policy store on flow context, got %+v", nps)
	}
	gw := netpolicy.GatewayConfigFromContext(ctx)
	if gw == nil || gw.Addr != "openshell.example:8443" {
		t.Fatalf("expected OpenShell gateway config on flow context, got %+v", gw)
	}
}
