package litellm

import (
	"context"
	"fmt"
	"strings"

	"github.com/sashabaranov/go-openai"
)

const modelsDefaultBaseURL = "http://localhost:4000/v1"

// ListModels fetches available models from LiteLLM.
func ListModels(ctx context.Context, apiKey, baseURL string) ([]string, error) {
	if baseURL == "" {
		baseURL = modelsDefaultBaseURL
	}

	// Ensure baseURL has /v1 suffix
	if !strings.HasSuffix(baseURL, "/v1") {
		if strings.HasSuffix(baseURL, "/") {
			baseURL = baseURL + "v1"
		} else {
			baseURL = baseURL + "/v1"
		}
	}

	if apiKey == "" {
		apiKey = "litellm"
	}

	config := openai.DefaultConfig(apiKey)
	config.BaseURL = baseURL
	client := openai.NewClientWithConfig(config)

	models, err := client.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch LiteLLM models: %w", err)
	}

	var modelNames []string
	for _, m := range models.Models {
		modelNames = append(modelNames, m.ID)
	}
	return modelNames, nil
}
