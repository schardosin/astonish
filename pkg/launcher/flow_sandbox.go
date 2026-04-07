package launcher

import (
	"fmt"

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

// wireBrowserContainerCallbacks sets the ContainerLaunchFunc and
// ContainerDestroyFunc on a browser Manager, and marks it as sandbox-enabled.
// This avoids the browser package importing the sandbox package (circular dep).
// The sandbox client is created lazily on first container launch.
//
// Should only be called when sandbox is available. The Manager's SandboxEnabled
// field is set to true, which makes IsContainerMode() return true and routes
// GetOrLaunch() to the container path.
func wireBrowserContainerCallbacks(mgr *browser.Manager) {
	cfg := mgr.Config()

	// Validate: custom engine paths can't run in a container (could be macOS binary).
	engine := sandbox.DetectBrowserEngine(sandbox.BrowserContainerConfig{
		ChromePath: cfg.ChromePath,
	})
	if !sandbox.IsContainerCompatibleEngine(engine) {
		// Don't enable container mode — browser will run on host as fallback.
		return
	}

	mgr.SandboxEnabled = true
	mgr.ContainerLaunchFunc = func(sessionID string, shared bool) (string, string, error) {
		client, err := sandbox.SetupSandboxRuntime()
		if err != nil {
			return "", "", fmt.Errorf("sandbox runtime not available for browser container: %w", err)
		}
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
		info, err := sandbox.LaunchBrowserContainer(client, sessionID, shared, bCfg)
		if err != nil {
			return "", "", err
		}
		return info.ContainerName, info.ContainerIP, nil
	}
	mgr.ContainerDestroyFunc = func(containerName string) error {
		client, err := sandbox.SetupSandboxRuntime()
		if err != nil {
			return fmt.Errorf("sandbox runtime not available for browser container cleanup: %w", err)
		}
		return sandbox.DestroyBrowserContainer(client, containerName)
	}
}
