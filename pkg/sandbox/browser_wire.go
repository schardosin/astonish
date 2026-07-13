package sandbox

import (
	"fmt"
	"io"
	"net"

	"github.com/schardosin/astonish/pkg/browser"
	incus "github.com/schardosin/astonish/pkg/sandbox/incus"
)

// WireIncusBrowserManager configures mgr so Chromium + KasmVNC run inside the
// session container (same path Studio chat uses). Returns false when the
// configured browser engine cannot run in a container.
func WireIncusBrowserManager(mgr *browser.Manager, client *IncusClient, touchActivity func(sessionID string)) bool {
	if mgr == nil || client == nil {
		return false
	}
	cfg := mgr.Config()
	engine := incus.DetectBrowserEngine(incus.BrowserContainerConfig{
		ChromePath: cfg.ChromePath,
	})
	if !incus.IsContainerCompatibleEngine(engine) {
		return false
	}

	bCfg := incus.BrowserContainerConfig{
		ViewportWidth:       cfg.ViewportWidth,
		ViewportHeight:      cfg.ViewportHeight,
		KasmVNCPort:         cfg.KasmVNCPort,
		KasmVNCPassword:     cfg.KasmVNCPassword,
		Proxy:               cfg.Proxy,
		ChromePath:          cfg.ChromePath,
		FingerprintSeed:     cfg.FingerprintSeed,
		FingerprintPlatform: cfg.FingerprintPlatform,
	}

	mgr.SandboxEnabled = true
	mgr.ContainerResolveFunc = func(sessionID string) (string, string, error) {
		containerName := SessionContainerName(sessionID)
		if !client.IsRunning(containerName) {
			return "", "", fmt.Errorf("session container %q is not running", containerName)
		}
		ip, err := client.GetContainerIPv4(containerName)
		if err != nil {
			return "", "", fmt.Errorf("failed to get IP for session container %q: %w", containerName, err)
		}
		return containerName, ip, nil
	}
	mgr.ContainerStartBrowserFunc = func(containerName string) (io.Closer, error) {
		return nil, incus.StartChromiumInContainer(client, containerName, bCfg)
	}
	mgr.ContainerDialFunc = func(containerName string, port int) (net.Conn, error) {
		dialer := &incus.ContainerDialer{Client: client}
		return dialer.Dial(containerName, port)
	}
	if touchActivity != nil {
		mgr.ActivityTouchFunc = touchActivity
	}
	return true
}
