package launcher

import (
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
