package google

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"os"
	"sort"

	"google.golang.org/adk/model"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/genai"
)

// Provider implements the model.LLM interface for Google GenAI.
type Provider struct {
	model model.LLM
}

func (p *Provider) Name() string {
	return p.model.Name()
}

func (p *Provider) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	// WORKAROUND: Google GenAI (Gemini) does not support function calling with response_mime_type: application/json
	// If tools are present, we must unset the response MIME type and schema to avoid 400 error.
	// The prompt instructions will still guide the model to produce JSON, and the agent will parse it manually.
	if req.Config != nil && len(req.Config.Tools) > 0 {
		req.Config.ResponseMIMEType = ""
		req.Config.ResponseSchema = nil
	}
	return p.model.GenerateContent(ctx, req, stream)
}

// NewProvider creates a new Google GenAI provider.
func NewProvider(ctx context.Context, modelName string, apiKey string) (model.LLM, error) {
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("GOOGLE_API_KEY not set")
	}

	// Use ADK's gemini package to create the model
	// This handles the model.LLM interface implementation
	m, err := gemini.NewModel(ctx, modelName, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		return nil, err
	}

	return &Provider{model: m}, nil
}

// ListModels fetches available models from Google GenAI API using the OpenAI-compatible endpoint.
func ListModels(ctx context.Context, apiKey string) ([]string, error) {
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("GOOGLE_API_KEY not set")
	}

	url := "https://generativelanguage.googleapis.com/v1beta/openai/models"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch models: %s - %s", resp.Status, string(body))
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var models []string
	for _, m := range result.Data {
		models = append(models, m.ID)
	}

	if len(models) == 0 {
		return nil, fmt.Errorf("no models found from Google AI API")
	}

	sort.Strings(models)
	return models, nil
}
