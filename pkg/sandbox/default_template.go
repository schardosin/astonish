package sandbox

import (
	"context"
	"errors"
	"fmt"

	"github.com/schardosin/astonish/pkg/store"
)

// DefaultTemplateResolver resolves "the template a new chat session should
// use" via the cascade: personal > team > org > @base (global).
// See docs/architecture/sandbox-backends.md §5.13.
//
// In personal mode the cascade collapses to the single personal scope: the
// resolver simply returns the personal default (or an error if none is set
// and the caller did not specify a fallback).
type DefaultTemplateResolver struct {
	templates store.SandboxTemplateStore
}

// NewDefaultTemplateResolver constructs a resolver bound to a template store.
func NewDefaultTemplateResolver(templates store.SandboxTemplateStore) *DefaultTemplateResolver {
	return &DefaultTemplateResolver{templates: templates}
}

// DefaultTemplateRequest carries the identity triple used by the cascade.
// Zero-valued fields are treated as "scope not applicable" and skipped.
//
// In platform mode all three are typically populated. In personal mode only
// UserID is set and OrgID/TeamID are empty; the resolver returns the
// personal-scope default.
type DefaultTemplateRequest struct {
	UserID string
	TeamID string
	OrgID  string

	// PreferredSlug is consulted first within each scope. If empty, the
	// resolver returns the first template in the scope (ordered by slug)
	// or the scope's explicit default if one is recorded -- but default-
	// marker semantics are deferred (§5.13 notes the DB default marker
	// is a later concern).
	PreferredSlug string

	// Fallback is returned when no template matches the cascade. Leave
	// empty to have Resolve return an error instead.
	Fallback string
}

// ErrNoDefaultTemplate is returned when the cascade finds no match and the
// request has no Fallback.
var ErrNoDefaultTemplate = errors.New("no default template found in cascade")

// Resolve walks the cascade personal > team > org > @base and returns the
// first matching template. Returns ErrNoDefaultTemplate only if no candidate
// is found and req.Fallback is empty.
//
// The resolver calls through to the injected SandboxTemplateStore for each
// scope lookup; failures from that store (other than "not found") are
// propagated wrapped.
func (r *DefaultTemplateResolver) Resolve(ctx context.Context, req DefaultTemplateRequest) (*store.SandboxTemplate, error) {
	if r.templates == nil {
		return nil, errors.New("default-template resolver has no template store")
	}

	cascade := []struct {
		scope   store.SandboxTemplateScope
		ownerID string
	}{
		{store.SandboxTemplateScopePersonal, req.UserID},
		{store.SandboxTemplateScopeTeam, req.TeamID},
		{store.SandboxTemplateScopeOrg, req.OrgID},
		{store.SandboxTemplateScopeGlobal, ""},
	}

	for _, step := range cascade {
		if step.scope != store.SandboxTemplateScopeGlobal && step.ownerID == "" {
			continue // scope not applicable for this request
		}
		tpl, err := r.lookupInScope(ctx, step.scope, step.ownerID, req.PreferredSlug)
		if err != nil {
			// Filestore returns ErrUnsupported for scopes other than personal
			// (List ignores the filter but that's fine). The only other
			// interesting case is the filestore stub with nil registry
			// returning ErrUnsupported; we treat it as "not found" to let
			// the cascade continue, then fall through to the fallback.
			if errors.Is(err, store.ErrUnsupported) {
				continue
			}
			return nil, fmt.Errorf("lookup in scope %s: %w", step.scope, err)
		}
		if tpl != nil {
			return tpl, nil
		}
	}

	if req.Fallback != "" {
		return &store.SandboxTemplate{
			ID:    req.Fallback,
			Slug:  req.Fallback,
			Scope: store.SandboxTemplateScopePersonal,
			Name:  req.Fallback,
		}, nil
	}
	return nil, ErrNoDefaultTemplate
}

// lookupInScope fetches a template within a single scope. If preferredSlug is
// set, it returns that template by (scope, owner, slug); otherwise it returns
// the first template in scope alphabetically by slug.
func (r *DefaultTemplateResolver) lookupInScope(ctx context.Context, scope store.SandboxTemplateScope, ownerID, preferredSlug string) (*store.SandboxTemplate, error) {
	if preferredSlug != "" {
		return r.templates.GetBySlug(ctx, scope, ownerID, preferredSlug)
	}
	// For global scope the well-known slug is "base".
	if scope == store.SandboxTemplateScopeGlobal {
		return r.templates.GetBySlug(ctx, scope, "", "base")
	}
	tpls, err := r.templates.List(ctx, store.SandboxTemplateFilter{Scope: scope, OwnerID: ownerID})
	if err != nil {
		return nil, err
	}
	if len(tpls) == 0 {
		return nil, nil
	}
	return tpls[0], nil
}
