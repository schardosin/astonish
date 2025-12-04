package provider

import (
	"context"
	"fmt"
	"os"

	"github.com/sashabaranov/go-openai"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/provider/google"
	openai_provider "github.com/schardosin/astonish/pkg/provider/openai"
	"github.com/schardosin/astonish/pkg/provider/sap"
	"google.golang.org/adk/model"
)

// GetProvider returns an LLM model based on the provider name.
func GetProvider(ctx context.Context, name string, modelName string, cfg *config.AppConfig) (model.LLM, error) {
	switch name {
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
		return openai_provider.NewProvider(client, modelName), nil

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
		return openai_provider.NewProvider(client, modelName), nil

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
		return openai_provider.NewProvider(client, modelName), nil

	case "sap_ai_core":
		if modelName == "" {
			return nil, fmt.Errorf("model name required for sap_ai_core")
		}
		return sap.NewProvider(ctx, modelName)

	default:
		return nil, fmt.Errorf("unsupported provider: %s", name)
	}
}
