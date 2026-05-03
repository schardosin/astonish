package store

import (
	"context"
	"net/http"
)

type contextKey string

const servicesKey contextKey = "astonish_services"
const credStoreKey contextKey = "astonish_credential_store"
const memoryStoreKey contextKey = "astonish_memory_store"
const memorySearcherKey contextKey = "astonish_memory_searcher"
const flowStoreKey contextKey = "astonish_flow_store"

// WithServices returns a new context containing the Services instance.
func WithServices(ctx context.Context, svc *Services) context.Context {
	return context.WithValue(ctx, servicesKey, svc)
}

// FromContext retrieves the Services instance from the context.
// Returns nil if no Services is present (e.g., in personal mode before
// Services is wired, or in tests).
func FromContext(ctx context.Context) *Services {
	svc, _ := ctx.Value(servicesKey).(*Services)
	return svc
}

// FromRequest retrieves the Services instance from an HTTP request's context.
// This is a convenience wrapper for handler functions.
func FromRequest(r *http.Request) *Services {
	return FromContext(r.Context())
}

// Middleware returns an HTTP middleware that injects the Services instance
// into every request's context. This should be applied early in the
// middleware chain (after auth, before handlers).
func Middleware(svc *Services) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := WithServices(r.Context(), svc)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// WithCredentialStore returns a new context containing a CredentialStore.
// This is used to propagate the tenant-scoped credential store into the
// ADK runner context so that tool functions can access it without globals.
func WithCredentialStore(ctx context.Context, cs CredentialStore) context.Context {
	return context.WithValue(ctx, credStoreKey, cs)
}

// CredentialStoreFromContext retrieves the CredentialStore from a context.
// Returns nil if no CredentialStore is present (personal mode or tests).
// Tool functions should call this and fall back to the package-level global
// credential store when nil.
func CredentialStoreFromContext(ctx context.Context) CredentialStore {
	cs, _ := ctx.Value(credStoreKey).(CredentialStore)
	return cs
}

// WithMemoryStore returns a new context containing a tenant-scoped MemoryStore.
// Used to propagate the PG team memory store into the ADK runner context.
func WithMemoryStore(ctx context.Context, ms MemoryStore) context.Context {
	return context.WithValue(ctx, memoryStoreKey, ms)
}

// MemoryStoreFromContext retrieves the MemoryStore from a context.
// Returns nil if no MemoryStore is present (personal mode or tests).
func MemoryStoreFromContext(ctx context.Context) MemoryStore {
	ms, _ := ctx.Value(memoryStoreKey).(MemoryStore)
	return ms
}

// WithThreeTierSearcher returns a new context containing a ThreeTierSearcher.
// Used to propagate the cross-tier memory searcher into the ADK runner context.
func WithThreeTierSearcher(ctx context.Context, ts ThreeTierSearcher) context.Context {
	return context.WithValue(ctx, memorySearcherKey, ts)
}

// ThreeTierSearcherFromContext retrieves the ThreeTierSearcher from a context.
// Returns nil if no ThreeTierSearcher is present (personal mode or tests).
func ThreeTierSearcherFromContext(ctx context.Context) ThreeTierSearcher {
	ts, _ := ctx.Value(memorySearcherKey).(ThreeTierSearcher)
	return ts
}

// WithFlowStore returns a new context containing a tenant-scoped FlowStore.
// Used to propagate the PG flow store into the ADK runner context so that
// drill tools (save_drill, list_drills, etc.) can read/write flows from the
// database rather than the local filesystem in platform mode.
func WithFlowStore(ctx context.Context, fs FlowStore) context.Context {
	return context.WithValue(ctx, flowStoreKey, fs)
}

// FlowStoreFromContext retrieves the FlowStore from a context.
// Returns nil if no FlowStore is present (personal mode or tests).
func FlowStoreFromContext(ctx context.Context) FlowStore {
	fs, _ := ctx.Value(flowStoreKey).(FlowStore)
	return fs
}
