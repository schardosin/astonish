package openai_compat

import (
	"context"
	"iter"
	"strings"

	"github.com/sashabaranov/go-openai"
	openai_provider "github.com/schardosin/astonish/pkg/provider/openai"
	"google.golang.org/adk/model"
)

// Provider implements model.LLM for OpenAI Compatible endpoints.
type Provider struct {
	*openai_provider.Provider
}

// NewProvider creates a new OpenAI Compatible provider.
func NewProvider(apiKey, baseURL, modelName string) model.LLM {
	config := openai.DefaultConfig(apiKey)

	// Ensure baseURL has /v1 suffix
	if baseURL != "" {
		if !strings.HasSuffix(baseURL, "/v1") {
			if strings.HasSuffix(baseURL, "/") {
				baseURL = baseURL + "v1"
			} else {
				baseURL = baseURL + "/v1"
			}
		}
		config.BaseURL = baseURL
	}

	client := openai.NewClientWithConfig(config)

	return &Provider{
		Provider: openai_provider.NewProvider(client, modelName, true),
	}
}

// Name implements model.LLM.
func (p *Provider) Name() string {
	return p.Provider.Name()
}

// GenerateContent implements model.LLM.
func (p *Provider) GenerateContent(ctx context.Context, req *model.LLMRequest, streaming bool) iter.Seq2[*model.LLMResponse, error] {
	return p.Provider.GenerateContent(ctx, req, streaming)
}

// ListModels returns available models from an OpenAI compatible endpoint.
func ListModels(ctx context.Context, apiKey, baseURL string) ([]string, error) {
	config := openai.DefaultConfig(apiKey)

	if baseURL != "" {
		if !strings.HasSuffix(baseURL, "/v1") {
			if strings.HasSuffix(baseURL, "/") {
				baseURL = baseURL + "v1"
			} else {
				baseURL = baseURL + "/v1"
			}
		}
		config.BaseURL = baseURL
	}

	client := openai.NewClientWithConfig(config)

	resp, err := client.ListModels(ctx)
	if err != nil {
		return nil, err
	}

	var models []string
	for _, m := range resp.Models {
		models = append(models, m.ID)
	}
	return models, nil
}

// GetRequiredFields returns the required configuration fields for this provider.
func GetRequiredFields() []string {
	return []string{"api_key", "base_url"}
}
