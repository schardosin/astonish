package provider

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/SAP/astonish/pkg/config"
)

func TestPool_GetCachesInstances(t *testing.T) {
	pool := NewPool()

	cfg := &config.AppConfig{
		Providers: map[string]config.ProviderConfig{
			"test_provider": {"type": "openai_compat", "base_url": "http://localhost:8080", "api_key": "test-key"},
		},
	}

	llm1, err := pool.Get(context.Background(), "test_provider", "test-model", cfg)
	if err != nil {
		t.Fatalf("first Get failed: %v", err)
	}

	llm2, err := pool.Get(context.Background(), "test_provider", "test-model", cfg)
	if err != nil {
		t.Fatalf("second Get failed: %v", err)
	}

	// Same instance should be returned (cached)
	if llm1 != llm2 {
		t.Error("expected same LLM instance from cache, got different instances")
	}
}

func TestPool_DifferentModelsGetDifferentInstances(t *testing.T) {
	pool := NewPool()

	cfg := &config.AppConfig{
		Providers: map[string]config.ProviderConfig{
			"test_provider": {"type": "openai_compat", "base_url": "http://localhost:8080", "api_key": "test-key"},
		},
	}

	llm1, err := pool.Get(context.Background(), "test_provider", "model-a", cfg)
	if err != nil {
		t.Fatalf("Get model-a failed: %v", err)
	}

	llm2, err := pool.Get(context.Background(), "test_provider", "model-b", cfg)
	if err != nil {
		t.Fatalf("Get model-b failed: %v", err)
	}

	if llm1 == llm2 {
		t.Error("expected different LLM instances for different models")
	}
}

func TestPool_InvalidateDropsCache(t *testing.T) {
	pool := NewPool()

	cfg := &config.AppConfig{
		Providers: map[string]config.ProviderConfig{
			"test_provider": {"type": "openai_compat", "base_url": "http://localhost:8080", "api_key": "test-key"},
		},
	}

	llm1, err := pool.Get(context.Background(), "test_provider", "test-model", cfg)
	if err != nil {
		t.Fatalf("first Get failed: %v", err)
	}

	pool.Invalidate()

	llm2, err := pool.Get(context.Background(), "test_provider", "test-model", cfg)
	if err != nil {
		t.Fatalf("Get after invalidate failed: %v", err)
	}

	// After invalidation, a fresh instance should be created
	if llm1 == llm2 {
		t.Error("expected fresh LLM instance after Invalidate()")
	}
}

func TestPool_EmptyProviderReturnsError(t *testing.T) {
	pool := NewPool()

	_, err := pool.Get(context.Background(), "", "model", &config.AppConfig{})
	if err == nil {
		t.Error("expected error for empty provider name")
	}
}

func TestPool_UnknownProviderReturnsError(t *testing.T) {
	pool := NewPool()

	_, err := pool.Get(context.Background(), "nonexistent", "model", &config.AppConfig{
		Providers: map[string]config.ProviderConfig{},
	})
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestPool_ConcurrentAccess(t *testing.T) {
	pool := NewPool()

	cfg := &config.AppConfig{
		Providers: map[string]config.ProviderConfig{
			"test_provider": {"type": "openai_compat", "base_url": "http://localhost:8080", "api_key": "test-key"},
		},
	}

	var wg sync.WaitGroup
	var errCount atomic.Int32

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := pool.Get(context.Background(), "test_provider", "test-model", cfg)
			if err != nil {
				errCount.Add(1)
			}
		}()
	}
	wg.Wait()

	if errCount.Load() > 0 {
		t.Errorf("concurrent Gets failed %d times", errCount.Load())
	}
}

func TestPool_InvalidateProvider(t *testing.T) {
	pool := NewPool()

	cfg := &config.AppConfig{
		Providers: map[string]config.ProviderConfig{
			"provA": {"type": "openai_compat", "base_url": "http://localhost:8080", "api_key": "key-a"},
			"provB": {"type": "openai_compat", "base_url": "http://localhost:8081", "api_key": "key-b"},
		},
	}

	llmA, _ := pool.Get(context.Background(), "provA", "model", cfg)
	llmB, _ := pool.Get(context.Background(), "provB", "model", cfg)

	// Invalidate only provA
	pool.InvalidateProvider("provA")

	llmA2, _ := pool.Get(context.Background(), "provA", "model", cfg)
	llmB2, _ := pool.Get(context.Background(), "provB", "model", cfg)

	if llmA == llmA2 {
		t.Error("expected fresh instance for provA after InvalidateProvider")
	}
	if llmB != llmB2 {
		t.Error("expected cached instance for provB (should not be affected)")
	}
}
