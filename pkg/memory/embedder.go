package memory

import (
	"fmt"
	"os"
	"strings"

	"github.com/schardosin/astonish/pkg/config"

	chromem "github.com/philippgille/chromem-go"
)

// ResolveEmbeddingFunc determines the best embedding function from the user's
// configured Astonish providers. It tries providers in this order:
//  1. OpenAI → text-embedding-3-small
//  2. OpenAI-compatible → configured base_url with text-embedding-3-small
//  3. Ollama → nomic-embed-text
//
// Returns an error if no suitable provider is found.
func ResolveEmbeddingFunc(appCfg *config.AppConfig, memoryCfg *config.MemoryConfig) (chromem.EmbeddingFunc, error) {
	// If memory config explicitly specifies an embedding provider, use that
	if memoryCfg != nil && memoryCfg.Embedding.Provider != "" && memoryCfg.Embedding.Provider != "auto" {
		return resolveExplicitProvider(memoryCfg)
	}

	// Auto-detect from configured providers
	if appCfg == nil || len(appCfg.Providers) == 0 {
		return nil, fmt.Errorf("no providers configured; add a provider to config.yaml or configure memory.embedding explicitly")
	}

	// 1. Try OpenAI
	for name, instance := range appCfg.Providers {
		providerType := config.GetProviderType(name, instance)
		if providerType == "openai" {
			apiKey := instance["api_key"]
			if apiKey == "" {
				apiKey = os.Getenv("OPENAI_API_KEY")
			}
			if apiKey != "" {
				return chromem.NewEmbeddingFuncOpenAI(apiKey, chromem.EmbeddingModelOpenAI3Small), nil
			}
		}
	}

	// 2. Try OpenAI-compatible providers (including SAP AI Core, LiteLLM, etc.)
	for name, instance := range appCfg.Providers {
		providerType := config.GetProviderType(name, instance)
		if providerType == "openai_compat" {
			apiKey := instance["api_key"]
			baseURL := instance["base_url"]
			if apiKey != "" && baseURL != "" {
				baseURL = strings.TrimSuffix(baseURL, "/")
				model := "text-embedding-3-small"
				return chromem.NewEmbeddingFuncOpenAICompat(baseURL, apiKey, model, nil), nil
			}
		}
	}

	// 3. Try Ollama
	for name, instance := range appCfg.Providers {
		providerType := config.GetProviderType(name, instance)
		if providerType == "ollama" {
			baseURL := instance["base_url"]
			if baseURL == "" {
				baseURL = "http://localhost:11434"
			}
			// Ollama's embedding endpoint is at /api, not /v1
			ollamaBaseURL := strings.TrimSuffix(baseURL, "/")
			if !strings.HasSuffix(ollamaBaseURL, "/api") {
				ollamaBaseURL = ollamaBaseURL + "/api"
			}
			return chromem.NewEmbeddingFuncOllama("nomic-embed-text", ollamaBaseURL), nil
		}
	}

	// 4. Try other providers with OpenAI-compat embedding endpoint
	for name, instance := range appCfg.Providers {
		providerType := config.GetProviderType(name, instance)
		switch providerType {
		case "groq":
			apiKey := instance["api_key"]
			if apiKey == "" {
				apiKey = os.Getenv("GROQ_API_KEY")
			}
			if apiKey != "" {
				// Groq doesn't have embedding models, skip
				continue
			}
		case "anthropic":
			// Anthropic doesn't have embedding models, skip
			continue
		case "xai", "grok":
			// xAI doesn't have embedding models, skip
			continue
		}
	}

	return nil, fmt.Errorf("no embedding-capable provider found; configure an OpenAI, Ollama, or OpenAI-compatible provider")
}

// resolveExplicitProvider creates an embedding function from explicit memory config.
func resolveExplicitProvider(cfg *config.MemoryConfig) (chromem.EmbeddingFunc, error) {
	switch cfg.Embedding.Provider {
	case "openai":
		apiKey := cfg.Embedding.APIKey
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("OpenAI embedding requires an API key")
		}
		model := cfg.Embedding.Model
		if model == "" {
			model = string(chromem.EmbeddingModelOpenAI3Small)
		}
		return chromem.NewEmbeddingFuncOpenAI(apiKey, chromem.EmbeddingModelOpenAI(model)), nil

	case "ollama":
		baseURL := cfg.Embedding.BaseURL
		if baseURL == "" {
			baseURL = "http://localhost:11434/api"
		}
		model := cfg.Embedding.Model
		if model == "" {
			model = "nomic-embed-text"
		}
		return chromem.NewEmbeddingFuncOllama(model, baseURL), nil

	case "openai-compat", "openai_compat":
		baseURL := cfg.Embedding.BaseURL
		if baseURL == "" {
			return nil, fmt.Errorf("OpenAI-compatible embedding requires a base_url")
		}
		apiKey := cfg.Embedding.APIKey
		model := cfg.Embedding.Model
		if model == "" {
			model = "text-embedding-3-small"
		}
		return chromem.NewEmbeddingFuncOpenAICompat(baseURL, apiKey, model, nil), nil

	default:
		return nil, fmt.Errorf("unsupported embedding provider: %s", cfg.Embedding.Provider)
	}
}
