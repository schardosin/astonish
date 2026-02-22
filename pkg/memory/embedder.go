package memory

import (
	"fmt"
	"os"

	"github.com/schardosin/astonish/pkg/config"

	chromem "github.com/philippgille/chromem-go"
)

// EmbedderResult holds the resolved embedding function and an optional cleanup
// function that must be called when the embedder is no longer needed.
type EmbedderResult struct {
	EmbeddingFunc chromem.EmbeddingFunc
	Cleanup       func() error // nil if no cleanup is needed (e.g., cloud API providers)
}

// ResolveEmbeddingFunc determines the embedding function to use for the vector store.
//
// Default behavior (no explicit provider configured):
//
//	Uses a local, in-process embedding model via Hugot (pure Go, no external
//	dependencies). The model (all-MiniLM-L6-v2, ~23 MB) is auto-downloaded on
//	first use and cached in ~/.config/astonish/models/.
//
// Explicit provider (memory.embedding.provider in config.yaml):
//
//	Users can opt in to cloud-based embeddings by setting the provider to
//	"openai", "ollama", or "openai-compat". This is the only way to use
//	cloud APIs for embeddings — auto-detection of cloud providers is intentionally
//	not performed to avoid unexpected costs.
func ResolveEmbeddingFunc(appCfg *config.AppConfig, memoryCfg *config.MemoryConfig, debugMode bool) (*EmbedderResult, error) {
	// If memory config explicitly specifies an embedding provider, use that
	if memoryCfg != nil && memoryCfg.Embedding.Provider != "" && memoryCfg.Embedding.Provider != "auto" && memoryCfg.Embedding.Provider != "local" {
		ef, err := resolveExplicitProvider(memoryCfg)
		if err != nil {
			return nil, err
		}
		return &EmbedderResult{EmbeddingFunc: ef}, nil
	}

	// Default: use local Hugot-based embeddings (pure Go, zero cost, no API calls)
	// resolveLocalEmbedder is defined in hugot_embedder.go (build tag: GO || ALL)
	// or hugot_embedder_stub.go (no build tags — returns error)
	return resolveLocalEmbedder(debugMode)
}

// resolveExplicitProvider creates an embedding function from explicit memory config.
// This is the opt-in path for users who want to use cloud-based embedding APIs.
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
		return nil, fmt.Errorf("unsupported embedding provider: %s (supported: openai, ollama, openai-compat)", cfg.Embedding.Provider)
	}
}
