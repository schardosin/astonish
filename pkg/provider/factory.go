package provider

import (
	"context"
	"fmt"
	"os"

	"github.com/sashabaranov/go-openai"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/provider/anthropic"
	"github.com/schardosin/astonish/pkg/provider/google"
	openai_provider "github.com/schardosin/astonish/pkg/provider/openai"
	"github.com/schardosin/astonish/pkg/provider/sap"
	"google.golang.org/adk/model"
)

// ProviderDisplayNames maps provider IDs to their proper display names.
// This is the centralized source of truth for how provider names should be displayed
// in both the CLI and UI.
var ProviderDisplayNames = map[string]string{
	"anthropic":   "Anthropic",
	"gemini":      "Google GenAI",
	"groq":        "Groq",
	"lm_studio":   "LM Studio",
	"ollama":      "Ollama",
	"openai":      "OpenAI",
	"openrouter":  "Openrouter",
	"sap_ai_core": "SAP AI Core",
	"xai":         "xAI",
}

// GetProviderDisplayName returns the proper display name for a provider ID.
// If the provider ID is not found, it returns the ID as-is.
func GetProviderDisplayName(providerID string) string {
	if name, ok := ProviderDisplayNames[providerID]; ok {
		return name
	}
	return providerID
}

// GetProviderIDs returns a list of all known provider IDs.
func GetProviderIDs() []string {
	ids := make([]string, 0, len(ProviderDisplayNames))
	for id := range ProviderDisplayNames {
		ids = append(ids, id)
	}
	return ids
}

// GetProvider returns an LLM model based on the provider name.
func GetProvider(ctx context.Context, name string, modelName string, cfg *config.AppConfig) (model.LLM, error) {
	switch name {
	case "anthropic":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" && cfg != nil {
			apiKey = cfg.Providers["anthropic"]["api_key"]
		}
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
		}
		if modelName == "" {
			modelName = "claude-3-opus-20240229"
		}
		return anthropic.NewProvider(apiKey, modelName), nil

	case "google_genai", "gemini":
		apiKey := os.Getenv("GOOGLE_API_KEY")
		if apiKey == "" && cfg != nil {
			apiKey = cfg.Providers["gemini"]["api_key"]
		}
		if apiKey == "" {
			return nil, fmt.Errorf("GOOGLE_API_KEY not set")
		}
		if modelName == "" {
			modelName = "gemini-1.5-flash"
		}
		return google.NewProvider(ctx, modelName, apiKey)

	case "openai":
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" && cfg != nil {
			apiKey = cfg.Providers["openai"]["api_key"]
		}
		if apiKey == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY not set")
		}
		if modelName == "" {
			modelName = "gpt-4"
		}
		client := openai.NewClient(apiKey)
		return openai_provider.NewProvider(client, modelName, true), nil

	case "openrouter":
		apiKey := ""
		if cfg != nil {
			apiKey = cfg.Providers["openrouter"]["api_key"]
		}
		if apiKey == "" {
			return nil, fmt.Errorf("OpenRouter API Key not configured")
		}
		if modelName == "" {
			return nil, fmt.Errorf("model name required for openrouter")
		}
		
		config := openai.DefaultConfig(apiKey)
		config.BaseURL = "https://openrouter.ai/api/v1"
		client := openai.NewClientWithConfig(config)
		return openai_provider.NewProvider(client, modelName, true), nil

	case "ollama":
		baseURL := "http://localhost:11434"
		if cfg != nil && cfg.Providers["ollama"] != nil {
			if val, ok := cfg.Providers["ollama"]["base_url"]; ok && val != "" {
				baseURL = val
			}
		}
		if modelName == "" {
			return nil, fmt.Errorf("model name required for ollama")
		}

		// Use OpenAI client with Ollama base URL
		// Note: Ollama's OpenAI compatible endpoint is at /v1
		config := openai.DefaultConfig("ollama") // API key not required but must be non-empty string
		config.BaseURL = fmt.Sprintf("%s/v1", baseURL)
		client := openai.NewClientWithConfig(config)
		return openai_provider.NewProvider(client, modelName, true), nil

	case "groq":
		apiKey := ""
		if cfg != nil && cfg.Providers["groq"] != nil {
			apiKey = cfg.Providers["groq"]["api_key"]
		}
		if apiKey == "" {
			apiKey = os.Getenv("GROQ_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("GROQ_API_KEY not set")
		}
		if modelName == "" {
			modelName = "llama3-70b-8192"
		}

		config := openai.DefaultConfig(apiKey)
		config.BaseURL = "https://api.groq.com/openai/v1"
		client := openai.NewClientWithConfig(config)
		return openai_provider.NewProvider(client, modelName, true), nil

	case "lm_studio":
		baseURL := "http://localhost:1234/v1"
		if cfg != nil && cfg.Providers["lm_studio"] != nil {
			if val, ok := cfg.Providers["lm_studio"]["base_url"]; ok && val != "" {
				baseURL = val
			}
		}
		if modelName == "" {
			return nil, fmt.Errorf("model name required for lm_studio")
		}

		config := openai.DefaultConfig("lm-studio")
		config.BaseURL = baseURL
		client := openai.NewClientWithConfig(config)
		return openai_provider.NewProvider(client, modelName, false), nil

	case "sap_ai_core":
		if modelName == "" {
			return nil, fmt.Errorf("model name required for sap_ai_core")
		}
		return sap.NewProvider(ctx, modelName)

	case "xai", "grok":
		apiKey := os.Getenv("XAI_API_KEY")
		if apiKey == "" && cfg != nil {
			apiKey = cfg.Providers["xai"]["api_key"]
		}
		if apiKey == "" {
			return nil, fmt.Errorf("XAI_API_KEY not set")
		}
		if modelName == "" {
			modelName = "grok-beta"
		}
		
		config := openai.DefaultConfig(apiKey)
		config.BaseURL = "https://api.x.ai/v1"
		client := openai.NewClientWithConfig(config)
		return openai_provider.NewProvider(client, modelName, true), nil

	default:
		return nil, fmt.Errorf("unsupported provider: %s", name)
	}
}
