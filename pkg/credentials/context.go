package credentials

import "context"

// redactorContextKey is the context key for the Redactor instance.
type redactorContextKey struct{}

// WithRedactor returns a new context containing the given Redactor.
// Used to propagate the per-session Redactor into the ADK runner context
// so that tool functions (e.g., memory_save) can call Placeholderize()
// without needing a direct reference to the agent's Redactor.
func WithRedactor(ctx context.Context, r *Redactor) context.Context {
	return context.WithValue(ctx, redactorContextKey{}, r)
}

// RedactorFromContext retrieves the Redactor from a context.
// Returns nil if no Redactor is present (personal mode without platform
// injection, or tests).
func RedactorFromContext(ctx context.Context) *Redactor {
	if ctx == nil {
		return nil
	}
	r, _ := ctx.Value(redactorContextKey{}).(*Redactor)
	return r
}
