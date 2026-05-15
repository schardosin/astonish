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
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/schardosin/astonish/pkg/sandbox"
	"github.com/schardosin/astonish/pkg/store"
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

// resolveTemplateLayerChain resolves a template slug (e.g. "team-general")
// to an ordered layer chain (oldest-first, e.g. ["@base", "<sha256>"]).
//
// On K8s, the returned chain is passed as SessionSpec.LayerChain to the
// backend, which sets ASTONISH_LAYER_CHAIN in the pod env. Each element is
// a literal directory name under /mnt/astonish-layers/ on the PVC.
//
// Returns nil if:
//   - The platform PGStore is not available (personal mode).
//   - The template doesn't exist in the DB (not yet saved).
//   - Resolution fails (broken DAG).
//
// Callers MUST tolerate nil and fall back gracefully (e.g. use @base).
func resolveTemplateLayerChain(ctx context.Context, templateSlug string) []string {
	pgStore := getPlatformPGStore()
	if pgStore == nil {
		return nil
	}
	templates := pgStore.SandboxTemplates()
	if templates == nil {
		return nil
	}

	// Derive the lookup parameters from the slug convention.
	// Template slugs are "team-<teamSlug>"; scope=team, owner_id=<teamSlug>.
	teamSlug := strings.TrimPrefix(templateSlug, "team-")
	if teamSlug == templateSlug {
		// Not a team-prefixed template; might be a different scope.
		// For now, only team templates are supported.
		return nil
	}

	tpl, err := templates.GetBySlug(ctx, store.SandboxTemplateScopeTeam, teamSlug, templateSlug)
	if err != nil || tpl == nil {
		slog.Debug("template not found in platform DB, falling back to @base",
			"slug", templateSlug, "error", err)
		return nil
	}

	// Resolve the parent chain → ordered list of layer IDs.
	chain, err := templates.Resolve(ctx, tpl.ID)
	if err != nil || chain == nil || len(chain.LayerIDs) == 0 {
		slog.Warn("failed to resolve template layer chain",
			"template", tpl.ID, "slug", templateSlug, "error", err)
		return nil
	}

	return chain.LayerIDs
}
