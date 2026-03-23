package launcher

import (
	"fmt"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/sandbox"
	"google.golang.org/adk/tool"
)

// setupFlowSandbox wraps internal tools with sandbox proxies for flow execution.
// Flow sessions are short-lived: the container is created lazily on first tool
// call and destroyed when the returned cleanup function is called.
//
// If sandbox is not enabled, returns the original tools and a no-op cleanup.
func setupFlowSandbox(appCfg *config.AppConfig, internalTools []tool.Tool, debugMode bool) ([]tool.Tool, func(), error) {
	noop := func() {}

	if appCfg == nil || !sandbox.IsSandboxEnabled(&appCfg.Sandbox) {
		return internalTools, noop, nil
	}

	client, err := sandbox.SetupSandboxRuntime()
	if err != nil {
		return nil, noop, fmt.Errorf("sandbox runtime not available: %w", err)
	}

	sessRegistry, err := sandbox.NewSessionRegistry()
	if err != nil {
		return nil, noop, fmt.Errorf("session registry failed: %w", err)
	}

	tplRegistry, tplErr := sandbox.NewTemplateRegistry()
	if tplErr != nil && debugMode {
		fmt.Printf("Warning: Failed to create template registry: %v\n", tplErr)
	}

	pool := sandbox.NewNodeClientPool(client, sessRegistry, tplRegistry, "")
	wrapped := sandbox.WrapToolsWithNode(internalTools, pool)

	cleanup := func() {
		pool.Cleanup()
	}

	return wrapped, cleanup, nil
}
