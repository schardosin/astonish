// Package api — backend-agnostic sandbox helpers.
//
// sandboxBackendForRequest builds a sandbox.Backend from the current
// request's effective AppConfig. It supports both Incus and K8s backends
// transparently, allowing handler code to use the Backend interface
// rather than calling Incus directly.
//
// The returned cleanup func MUST be deferred by the caller (it may be nil).
package api

import (
	"fmt"
	"net/http"

	"github.com/schardosin/astonish/pkg/sandbox"
)

// sandboxBackendForRequest constructs a sandbox.Backend appropriate for
// the deployment's sandbox.backend config. Returns the backend, a cleanup
// function (may be nil), and any construction error.
//
// Callers MUST defer cleanup when non-nil:
//
//	backend, cleanup, err := sandboxBackendForRequest(r)
//	if cleanup != nil { defer cleanup() }
func sandboxBackendForRequest(r *http.Request) (sandbox.Backend, func(), error) {
	appCfg := effectiveAppConfig(r)
	if appCfg == nil {
		return nil, nil, fmt.Errorf("sandbox unavailable: unable to load application config")
	}
	if !sandbox.IsSandboxEnabled(&appCfg.Sandbox) {
		return nil, nil, fmt.Errorf("sandbox is not enabled in configuration")
	}
	b, cleanup, err := sandbox.BackendFromAppConfig(appCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox unavailable: %w", err)
	}
	return b, cleanup, nil
}

// teamTemplateSessionID returns the canonical session ID used by the
// team-template editor for a given team slug. This is a well-known,
// deterministic ID so the editor pod/container can be found across
// handler invocations without extra DB state.
func teamTemplateSessionID(teamSlug string) string {
	return "team-template-" + teamSlug
}
