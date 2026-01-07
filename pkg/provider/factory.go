package provider

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/sashabaranov/go-openai"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/provider/anthropic"
	"github.com/schardosin/astonish/pkg/provider/google"
	"github.com/schardosin/astonish/pkg/provider/groq"
	"github.com/schardosin/astonish/pkg/provider/litellm"
	"github.com/schardosin/astonish/pkg/provider/lmstudio"
	"github.com/schardosin/astonish/pkg/provider/ollama"
	openai_provider "github.com/schardosin/astonish/pkg/provider/openai"
	openai_compat "github.com/schardosin/astonish/pkg/provider/openai_compat"
	"github.com/schardosin/astonish/pkg/provider/openrouter"
	"github.com/schardosin/astonish/pkg/provider/poe"
	"github.com/schardosin/astonish/pkg/provider/sap"
	"github.com/schardosin/astonish/pkg/provider/xai"
	"google.golang.org/adk/model"
)

// ProviderDisplayNames maps provider IDs to their proper display names.
// This is the centralized source of truth for how provider names should be displayed
// in both the CLI and UI.
var ProviderDisplayNames = map[string]string{
	"anthropic":     "Anthropic",
	"gemini":        "Google GenAI",
	"groq":          "Groq",
	"litellm":       "LiteLLM",
	"lm_studio":     "LM Studio",
	"ollama":        "Ollama",
	"openai":        "OpenAI",
	"openai_compat": "OpenAI Compatible",
	"openrouter":    "Openrouter",
	"poe":           "Poe",
	"sap_ai_core":   "SAP AI Core",
	"xai":           "xAI",
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

// GetProvider returns an LLM model based on a provider instance name.
func GetProvider(ctx context.Context, instanceName string, modelName string, cfg *config.AppConfig) (model.LLM, error) {
	instance, exists := cfg.Providers[instanceName]
	if !exists {
		return nil, fmt.Errorf("provider instance '%s' not found", instanceName)
	}

	providerType := config.GetProviderType(instanceName, instance)
	if providerType == "" {
		return nil, fmt.Errorf("provider instance '%s' has no 'type' field and name is not a known provider type", instanceName)
	}

	switch providerType {
	case "anthropic":
		apiKey := instance["api_key"]
		if apiKey == "" {
			apiKey = os.Getenv("ANTHROPIC_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
		}
		if modelName == "" {
			modelName = "claude-3-opus-20240229"
		}
		return anthropic.NewProvider(apiKey, modelName), nil

	case "google_genai", "gemini":
		apiKey := instance["api_key"]
		if apiKey == "" {
			apiKey = os.Getenv("GOOGLE_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("GOOGLE_API_KEY not set")
		}
		if modelName == "" {
			modelName = "gemini-1.5-flash"
		}
		return google.NewProvider(ctx, modelName, apiKey)

	case "openai":
		apiKey := instance["api_key"]
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
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
		apiKey := instance["api_key"]
		if apiKey == "" {
			apiKey = os.Getenv("OPENROUTER_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("OPENROUTER_API_KEY not set")
		}
		if modelName == "" {
			return nil, fmt.Errorf("model name required for openrouter")
		}

		config := openai.DefaultConfig(apiKey)
		config.BaseURL = "https://openrouter.ai/api/v1"
		client := openai.NewClientWithConfig(config)

		maxTokens := openrouter.GetMaxCompletionTokens(ctx, apiKey, modelName)
		if maxTokens > 0 {
			log.Printf("[OpenRouter] Model %s: setting max_completion_tokens=%d", modelName, maxTokens)
			return openai_provider.NewProviderWithMaxTokens(client, modelName, true, maxTokens), nil
		}
		return openai_provider.NewProvider(client, modelName, true), nil

	case "poe":
		apiKey := instance["api_key"]
		if apiKey == "" {
			apiKey = os.Getenv("POE_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("POE_API_KEY not set")
		}
		if modelName == "" {
			modelName = "gpt-4o"
		}

		config := openai.DefaultConfig(apiKey)
		config.BaseURL = poe.GetBaseURL()
		client := openai.NewClientWithConfig(config)
		return openai_provider.NewProvider(client, modelName, true), nil

	case "ollama":
		baseURL := "http://localhost:11434"
		if instance["base_url"] != "" {
			baseURL = instance["base_url"]
		}
		if modelName == "" {
			return nil, fmt.Errorf("model name required for ollama")
		}

		config := openai.DefaultConfig("ollama")
		config.BaseURL = fmt.Sprintf("%s/v1", baseURL)
		client := openai.NewClientWithConfig(config)
		return openai_provider.NewProvider(client, modelName, true), nil

	case "groq":
		apiKey := instance["api_key"]
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
		if instance["base_url"] != "" {
			baseURL = instance["base_url"]
		}
		if modelName == "" {
			return nil, fmt.Errorf("model name required for lm_studio")
		}

		config := openai.DefaultConfig("lm-studio")
		config.BaseURL = baseURL
		client := openai.NewClientWithConfig(config)
		return openai_provider.NewProvider(client, modelName, false), nil

	case "litellm":
		apiKey := instance["api_key"]
		if apiKey == "" {
			apiKey = os.Getenv("LITELLM_API_KEY")
		}

		baseURL := "http://localhost:4000"
		if instance["base_url"] != "" {
			baseURL = strings.TrimSuffix(instance["base_url"], "/v1")
		}
		if modelName == "" {
			return nil, fmt.Errorf("model name required for litellm")
		}
		return litellm.NewProvider(apiKey, baseURL, modelName), nil

	case "sap_ai_core":
		if modelName == "" {
			return nil, fmt.Errorf("model name required for sap_ai_core")
		}

		clientID := instance["client_id"]
		if clientID == "" {
			clientID = os.Getenv("AICORE_CLIENT_ID")
		}
		clientSecret := instance["client_secret"]
		if clientSecret == "" {
			clientSecret = os.Getenv("AICORE_CLIENT_SECRET")
		}
		authURL := instance["auth_url"]
		if authURL == "" {
			authURL = os.Getenv("AICORE_AUTH_URL")
		}
		baseURL := instance["base_url"]
		if baseURL == "" {
			baseURL = os.Getenv("AICORE_BASE_URL")
		}
		resourceGroup := instance["resource_group"]
		if resourceGroup == "" {
			resourceGroup = os.Getenv("AICORE_RESOURCE_GROUP")
		}

		if clientID == "" || clientSecret == "" || authURL == "" || baseURL == "" {
			return nil, fmt.Errorf("SAP AI Core configuration incomplete")
		}
		return sap.NewProviderWithConfig(ctx, modelName, clientID, clientSecret, authURL, baseURL, resourceGroup)

	case "xai", "grok":
		apiKey := instance["api_key"]
		if apiKey == "" {
			apiKey = os.Getenv("XAI_API_KEY")
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

	case "openai_compat":
		apiKey := instance["api_key"]
		if apiKey == "" {
			return nil, fmt.Errorf("API key not set for OpenAI Compatible provider")
		}
		baseURL := instance["base_url"]
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		if modelName == "" {
			modelName = "gpt-4o"
		}
		return openai_compat.NewProvider(apiKey, baseURL, modelName), nil

	default:
		return nil, fmt.Errorf("unsupported provider type: %s", providerType)
	}
}

// ListModelsForProvider fetches available models for a given provider instance.
// This is used by the API to provide model lists to the UI.
func ListModelsForProvider(ctx context.Context, providerID string, cfg *config.AppConfig) ([]string, error) {
	instance, exists := cfg.Providers[providerID]
	if !exists {
		return nil, fmt.Errorf("provider instance '%s' not found", providerID)
	}

	providerType := config.GetProviderType(providerID, instance)
	if providerType == "" {
		return nil, fmt.Errorf("provider instance '%s' has no 'type' field and name is not a known provider type", providerID)
	}

	instanceConfig := instance

	switch providerType {
	case "anthropic":
		apiKey := instanceConfig["api_key"]
		if apiKey == "" {
			apiKey = os.Getenv("ANTHROPIC_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("Anthropic API key not configured")
		}
		return anthropic.ListModels(ctx, apiKey)

	case "google_genai", "gemini":
		apiKey := instanceConfig["api_key"]
		if apiKey == "" {
			apiKey = os.Getenv("GOOGLE_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("Google API key not configured")
		}
		return google.ListModels(ctx, apiKey)

	case "openai":
		apiKey := instanceConfig["api_key"]
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("OpenAI API key not configured")
		}
		return openai_provider.ListModels(ctx, apiKey)

	case "groq":
		apiKey := instanceConfig["api_key"]
		if apiKey == "" {
			apiKey = os.Getenv("GROQ_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("Groq API key not configured")
		}
		return groq.ListModels(ctx, apiKey)

	case "xai":
		apiKey := instanceConfig["api_key"]
		if apiKey == "" {
			apiKey = os.Getenv("XAI_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("xAI API key not configured")
		}
		return xai.ListModels(ctx, apiKey)

	case "ollama":
		baseURL := "http://localhost:11434"
		if instanceConfig["base_url"] != "" {
			baseURL = instanceConfig["base_url"]
		}
		return ollama.ListModels(ctx, baseURL)

	case "lm_studio":
		baseURL := "http://localhost:1234/v1"
		if instanceConfig["base_url"] != "" {
			baseURL = instanceConfig["base_url"]
		}
		return lmstudio.ListModels(ctx, baseURL)

	case "litellm":
		apiKey := instanceConfig["api_key"]
		if apiKey == "" {
			apiKey = os.Getenv("LITELLM_API_KEY")
		}

		baseURL := "http://localhost:4000/v1"
		if instanceConfig["base_url"] != "" {
			baseURL = instanceConfig["base_url"]
		}
		return litellm.ListModels(ctx, apiKey, baseURL)

	case "openrouter":
		apiKey := instanceConfig["api_key"]
		if apiKey == "" {
			apiKey = os.Getenv("OPENROUTER_API_KEY")
		}
		models, err := openrouter.ListModels(apiKey)
		if err != nil {
			return nil, err
		}
		var modelNames []string
		for _, m := range models {
			modelNames = append(modelNames, m.ID)
		}
		return modelNames, nil

	case "poe":
		apiKey := instanceConfig["api_key"]
		if apiKey == "" {
			apiKey = os.Getenv("POE_API_KEY")
		}
		return poe.ListModels(ctx, apiKey)

	case "sap_ai_core":
		clientID := instanceConfig["client_id"]
		if clientID == "" {
			clientID = os.Getenv("AICORE_CLIENT_ID")
		}
		clientSecret := instanceConfig["client_secret"]
		if clientSecret == "" {
			clientSecret = os.Getenv("AICORE_CLIENT_SECRET")
		}
		authURL := instanceConfig["auth_url"]
		if authURL == "" {
			authURL = os.Getenv("AICORE_AUTH_URL")
		}
		baseURL := instanceConfig["base_url"]
		if baseURL == "" {
			baseURL = os.Getenv("AICORE_BASE_URL")
		}
		resourceGroup := instanceConfig["resource_group"]
		if resourceGroup == "" {
			resourceGroup = os.Getenv("AICORE_RESOURCE_GROUP")
		}

		if clientID == "" || clientSecret == "" || authURL == "" || baseURL == "" {
			return nil, fmt.Errorf("SAP AI Core configuration incomplete")
		}
		return sap.ListModels(ctx, clientID, clientSecret, authURL, baseURL, resourceGroup)

	case "openai_compat":
		apiKey := instanceConfig["api_key"]
		if apiKey == "" {
			return nil, fmt.Errorf("API key not configured for OpenAI Compatible provider")
		}
		baseURL := instanceConfig["base_url"]
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		return openai_compat.ListModels(ctx, apiKey, baseURL)

	default:
		return nil, fmt.Errorf("unsupported provider type: %s", providerType)
	}
}
