package provider

import (
	"context"
	"fmt"
	"sync"

	"github.com/schardosin/astonish/pkg/config"
	"google.golang.org/adk/model"
)

// Pool caches LLM instances keyed by (providerName, modelName).
//
// The pool is designed for use cases where multiple callers (channels, chat)
// may resolve to the same underlying provider+model combination. Rather than
// creating a new HTTP client for every message, the pool lazily creates and
// reuses instances.
//
// Thread-safe for concurrent access. Call Invalidate() when provider settings
// change to force fresh instances on the next Get().
type Pool struct {
	mu    sync.RWMutex
	cache map[string]model.LLM
}

// NewPool creates a new empty LLM provider pool.
func NewPool() *Pool {
	return &Pool{
		cache: make(map[string]model.LLM),
	}
}

// poolKey creates a cache key from provider name and model name.
func poolKey(providerName, modelName string) string {
	return providerName + "\x00" + modelName
}

// Get returns a cached LLM instance for the given provider+model, or creates
// one if not yet cached. The appCfg must contain the provider configuration
// (including credentials) as resolved by ResolveEffectiveConfig.
//
// If the provider+model combination changes configuration (e.g., new API key),
// call Invalidate() first to drop stale entries.
func (p *Pool) Get(ctx context.Context, providerName, modelName string, appCfg *config.AppConfig) (model.LLM, error) {
	if providerName == "" {
		return nil, fmt.Errorf("no provider configured")
	}

	key := poolKey(providerName, modelName)

	// Fast path: read lock check
	p.mu.RLock()
	if llm, ok := p.cache[key]; ok {
		p.mu.RUnlock()
		return llm, nil
	}
	p.mu.RUnlock()

	// Slow path: create under write lock (double-check after acquiring)
	p.mu.Lock()
	defer p.mu.Unlock()

	if llm, ok := p.cache[key]; ok {
		return llm, nil
	}

	llm, err := GetProvider(ctx, providerName, modelName, appCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider '%s' model '%s': %w", providerName, modelName, err)
	}

	p.cache[key] = llm
	return llm, nil
}

// Invalidate drops all cached LLM instances. The next Get() call for each
// provider+model will create a fresh instance. This should be called when
// provider settings change (API keys, base URLs, etc.).
func (p *Pool) Invalidate() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cache = make(map[string]model.LLM)
}

// InvalidateProvider drops cached entries for a specific provider name.
// All models for that provider are invalidated.
func (p *Pool) InvalidateProvider(providerName string) {
	prefix := providerName + "\x00"
	p.mu.Lock()
	defer p.mu.Unlock()
	for key := range p.cache {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			delete(p.cache, key)
		}
	}
}
