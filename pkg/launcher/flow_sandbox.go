package launcher

import (
	"fmt"
	"net"

	"github.com/schardosin/astonish/pkg/browser"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/sandbox"
	"google.golang.org/adk/tool"
)

// setupFlowSandbox wraps internal tools with sandbox proxies for flow execution.
// Delegates to sandbox.SetupFlowSandbox and adapts the return signature for
// callers that expect (tools, cleanup, error).
func setupFlowSandbox(appCfg *config.AppConfig, internalTools []tool.Tool, _ bool) ([]tool.Tool, func(), error) {
	result, err := sandbox.SetupFlowSandbox(appCfg, internalTools)
	if err != nil {
		return nil, func() {}, err
	}
	return result.Tools, result.Cleanup, nil
}

// wireBrowserContainerCallbacks configures a browser Manager to run inside
// session containers when sandbox is available. The browser resolves the
// session container (already managed by NodeClientPool) and starts Chromium
// + KasmVNC inside it via ExecSimple.
//
// When ContainerDialFunc is wired, all CDP connections (HTTP for /json/version,
// WebSocket for the debugger) are tunneled through the Incus exec API (socat),
// making them work on both Linux native and Docker+Incus (macOS/Windows).
//
// Only called when sandbox is available. Custom/remote engines can't run in
// containers and are skipped (browser falls back to host mode).
func wireBrowserContainerCallbacks(mgr *browser.Manager) {
	cfg := mgr.Config()

	engine := sandbox.DetectBrowserEngine(sandbox.BrowserContainerConfig{
		ChromePath: cfg.ChromePath,
	})
	if !sandbox.IsContainerCompatibleEngine(engine) {
		return
	}

	mgr.SandboxEnabled = true

	// ContainerResolveFunc: resolve the session container name + IP.
	// The actual container is created/managed by NodeClientPool, so we just
	// need to look it up. This version creates a sandbox client lazily.
	mgr.ContainerResolveFunc = func(sessionID string) (string, string, error) {
		client, err := sandbox.SetupSandboxRuntime()
		if err != nil {
			return "", "", fmt.Errorf("sandbox runtime not available: %w", err)
		}
		containerName := sandbox.SessionContainerName(sessionID)
		if !client.IsRunning(containerName) {
			return "", "", fmt.Errorf("session container %q is not running", containerName)
		}
		ip, err := client.GetContainerIPv4(containerName)
		if err != nil {
			return "", "", fmt.Errorf("failed to get IP for session container %q: %w", containerName, err)
		}
		return containerName, ip, nil
	}

	// ContainerStartBrowserFunc: start Chromium + KasmVNC inside the container.
	bCfg := sandbox.BrowserContainerConfig{
		ViewportWidth:       cfg.ViewportWidth,
		ViewportHeight:      cfg.ViewportHeight,
		KasmVNCPort:         cfg.KasmVNCPort,
		KasmVNCPassword:     cfg.KasmVNCPassword,
		Proxy:               cfg.Proxy,
		ChromePath:          cfg.ChromePath,
		FingerprintSeed:     cfg.FingerprintSeed,
		FingerprintPlatform: cfg.FingerprintPlatform,
	}
	mgr.ContainerStartBrowserFunc = func(containerName string) error {
		client, err := sandbox.SetupSandboxRuntime()
		if err != nil {
			return fmt.Errorf("sandbox runtime not available: %w", err)
		}
		return sandbox.StartChromiumInContainer(client, containerName, bCfg)
	}

	// ContainerDialFunc: tunnel TCP connections through the Incus exec API.
	// This makes CDP (and /json/version HTTP) work even when container bridge
	// IPs are not routable from the host (Docker+Incus on macOS).
	mgr.ContainerDialFunc = func(containerName string, port int) (net.Conn, error) {
		client, err := sandbox.SetupSandboxRuntime()
		if err != nil {
			return nil, fmt.Errorf("sandbox runtime not available: %w", err)
		}
		dialer := &sandbox.ContainerDialer{Client: client}
		return dialer.Dial(containerName, port)
	}
}
