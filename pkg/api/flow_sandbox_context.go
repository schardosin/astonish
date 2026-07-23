package api

import (
	"context"
	"net/http"

	"github.com/SAP/astonish/pkg/store"
)

var (
	resolveRuntimeTemplateLayerChain = resolveTemplateLayerChain
	resolveRuntimeTemplateImage      = resolveTemplateImage
	resolveRuntimeBaseLayerChain     = resolveBaseLayerChain
	resolveRuntimeBaseImage          = resolveBaseImage
)

func withRuntimeSandboxContext(ctx context.Context, r *http.Request) context.Context {
	if r != nil {
		if svc := store.FromRequest(r); svc != nil && svc.Settings != nil {
			if settings, err := svc.Settings.Get(r.Context()); err == nil && settings != nil && settings.TemplateName != "" {
				ctx = store.WithSandboxTemplate(ctx, settings.TemplateName)
				if chain := resolveRuntimeTemplateLayerChain(r.Context(), settings.TemplateName); len(chain) > 0 {
					ctx = store.WithSandboxLayerChain(ctx, chain)
				}
				if img := resolveRuntimeTemplateImage(r.Context(), settings.TemplateName); img != "" {
					ctx = store.WithSandboxImage(ctx, img)
				}
			}
		}
	}
	if store.SandboxLayerChainFromContext(ctx) == nil {
		if chain := resolveRuntimeBaseLayerChain(ctx); len(chain) > 0 {
			ctx = store.WithSandboxLayerChain(ctx, chain)
		}
	}
	if store.SandboxImageFromContext(ctx) == "" {
		if img := resolveRuntimeBaseImage(ctx); img != "" {
			ctx = store.WithSandboxImage(ctx, img)
		}
	}
	return ctx
}
