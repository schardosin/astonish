package launcher

import (
	"context"
	"testing"

	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/sandbox/netpolicy"
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
