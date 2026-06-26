package provider

import (
	"context"
	"iter"
	"sync"

	"google.golang.org/adk/model"
)

// SwappableLLM wraps a model.LLM and allows hot-swapping the underlying
// implementation without recreating all consumers (closures, sub-agents,
// compactor, reflector, etc.). All method calls are forwarded to the current
// inner LLM under a read lock; Swap() acquires a write lock to replace it.
//
// This is used by the ChatManager to support model changes without tearing
// down the entire chat agent (tools, MCP, sandbox, ToolIndex all survive).
type SwappableLLM struct {
	mu    sync.RWMutex
	inner model.LLM
}

// NewSwappableLLM creates a new SwappableLLM wrapping the given LLM.
func NewSwappableLLM(llm model.LLM) *SwappableLLM {
	return &SwappableLLM{inner: llm}
}

// Swap atomically replaces the underlying LLM. All subsequent calls to
// Name() and GenerateContent() will use the new LLM.
func (s *SwappableLLM) Swap(newLLM model.LLM) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner = newLLM
}

// Inner returns the current underlying LLM (for inspection/testing).
func (s *SwappableLLM) Inner() model.LLM {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.inner
}

// Name implements model.LLM.
func (s *SwappableLLM) Name() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.inner.Name()
}

// GenerateContent implements model.LLM.
func (s *SwappableLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	s.mu.RLock()
	llm := s.inner
	s.mu.RUnlock()
	// Release the read lock before the potentially long-running generation.
	// This allows Swap() to proceed without waiting for generation to finish.
	// The generation uses the LLM that was current at call time.
	return llm.GenerateContent(ctx, req, stream)
}

// Verify SwappableLLM implements model.LLM at compile time.
var _ model.LLM = (*SwappableLLM)(nil)
