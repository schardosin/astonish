// Package streaming provides payload-free provider progress notifications.
package streaming

import "context"

// Phase describes work that may not yet produce visible model output.
type Phase string

const (
	PhaseThinking      Phase = "thinking"
	PhaseToolArguments Phase = "tool_arguments"
)

// Progress deliberately carries no reasoning, tool names, or arguments.
type Progress struct{ Phase Phase }

type progressKey struct{}

// WithProgress binds a synchronous callback to ctx. A nil callback disables it.
// Callers of Report are responsible for bounding emission; callbacks should not block.
func WithProgress(ctx context.Context, callback func(Progress)) context.Context {
	return context.WithValue(ctx, progressKey{}, callback)
}

// Report delivers progress synchronously to the callback bound to ctx, if any.
func Report(ctx context.Context, progress Progress) {
	if callback, ok := ctx.Value(progressKey{}).(func(Progress)); ok && callback != nil {
		callback(progress)
	}
}
