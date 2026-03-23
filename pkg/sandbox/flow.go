package sandbox

import (
	"fmt"

	"github.com/schardosin/astonish/pkg/config"
	"google.golang.org/adk/tool"
)

// FlowSandboxResult holds the wrapped tools and cleanup function returned by
// SetupFlowSandbox. Callers must invoke Cleanup when the flow session ends.
type FlowSandboxResult struct {
	Tools   []tool.Tool
	Cleanup func()
}

// SetupFlowSandbox wraps internal tools with sandbox proxies for flow execution.
// Flow sessions are short-lived: the container is created lazily on first tool
// call and destroyed when the returned cleanup function is called.
//
// If sandbox is not enabled, returns the original tools and a no-op cleanup.
func SetupFlowSandbox(appCfg *config.AppConfig, internalTools []tool.Tool) (*FlowSandboxResult, error) {
	noop := &FlowSandboxResult{Tools: internalTools, Cleanup: func() {}}

	if appCfg == nil || !IsSandboxEnabled(&appCfg.Sandbox) {
		return noop, nil
	}

	client, err := SetupSandboxRuntime()
	if err != nil {
		return nil, fmt.Errorf("sandbox runtime not available: %w", err)
	}

	sessRegistry, err := NewSessionRegistry()
	if err != nil {
		return nil, fmt.Errorf("session registry failed: %w", err)
	}

	tplRegistry, _ := NewTemplateRegistry()

	pool := NewNodeClientPool(client, sessRegistry, tplRegistry, "")
	wrapped := WrapToolsWithNode(internalTools, pool)

	return &FlowSandboxResult{
		Tools:   wrapped,
		Cleanup: pool.Cleanup,
	}, nil
}
