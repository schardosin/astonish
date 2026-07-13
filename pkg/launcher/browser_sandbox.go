package launcher

import (
	"github.com/schardosin/astonish/pkg/browser"
	"github.com/schardosin/astonish/pkg/sandbox"
	incus "github.com/schardosin/astonish/pkg/sandbox/incus"
	"github.com/schardosin/astonish/pkg/sandbox/openshell"
)

// WireIncusBrowserManager configures mgr for in-container Chromium (Incus).
func WireIncusBrowserManager(mgr *browser.Manager, client *sandbox.IncusClient, touchActivity func(sessionID string)) bool {
	return sandbox.WireIncusBrowserManager(mgr, client, touchActivity)
}

// WireOpenShellBrowserManager configures mgr for in-container CloakBrowser (OpenShell).
func WireOpenShellBrowserManager(
	mgr *browser.Manager,
	gw openshell.GatewayClient,
	sessReg *sandbox.SessionRegistry,
	touchActivity func(sessionID string),
) bool {
	return openshell.WireBrowserManager(mgr, gw, sessReg, touchActivity)
}

// wireBrowserContainerCallbacks configures a browser Manager for Incus session
// containers using a lazily opened sandbox client. Used by console/flow paths
// that do not already hold a NodeClientPool.
func wireBrowserContainerCallbacks(mgr *browser.Manager) {
	cfg := mgr.Config()
	engine := incus.DetectBrowserEngine(incus.BrowserContainerConfig{
		ChromePath: cfg.ChromePath,
	})
	if !incus.IsContainerCompatibleEngine(engine) {
		return
	}

	client, err := sandbox.SetupSandboxRuntime()
	if err != nil {
		return
	}
	sandbox.WireIncusBrowserManager(mgr, client, nil)
}
