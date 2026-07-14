package openshell

import (
	"context"
	"fmt"
	"io"
	"net"

	"github.com/schardosin/astonish/pkg/browser"
	"github.com/schardosin/astonish/pkg/sandbox"
)

// WireBrowserManager configures mgr so CloakBrowser + KasmVNC run inside the
// OpenShell sandbox (same path Studio chat uses).
func WireBrowserManager(
	mgr *browser.Manager,
	gw GatewayClient,
	sessReg *sandbox.SessionRegistry,
	touchActivity func(sessionID string),
) bool {
	if mgr == nil || gw == nil || sessReg == nil {
		return false
	}

	bcfg := mgr.Config()
	mgr.SandboxEnabled = true
	mgr.ContainerResolveFunc = func(sessionID string) (string, string, error) {
		rec, err := sessReg.GetSession(sessionID)
		if err != nil || rec == nil || rec.PodName == "" {
			return "", "", fmt.Errorf("no running sandbox for session %q", sessionID)
		}
		return rec.PodName, "127.0.0.1", nil
	}
	mgr.ContainerStartBrowserFunc = func(podName string) (io.Closer, error) {
		return StartBrowserInSandbox(context.Background(), gw, podName, BrowserLaunchConfig{
			ViewportWidth:       bcfg.ViewportWidth,
			ViewportHeight:      bcfg.ViewportHeight,
			FingerprintSeed:     bcfg.FingerprintSeed,
			FingerprintPlatform: bcfg.FingerprintPlatform,
			Proxy:               bcfg.Proxy,
			KasmVNCPort:         bcfg.KasmVNCPort,
		})
	}
	mgr.ContainerDialFunc = func(podName string, port int) (net.Conn, error) {
		return DialSandboxPort(context.Background(), gw, podName, port)
	}
	if touchActivity != nil {
		mgr.ActivityTouchFunc = touchActivity
	}
	return true
}
