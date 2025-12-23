package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const modelsURL = "https://openrouter.ai/api/v1/models"

// ModelMetadata represents the full model metadata from OpenRouter API
type ModelMetadata struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextLength int    `json:"context_length"`
	TopProvider   struct {
		ContextLength       int  `json:"context_length"`
		MaxCompletionTokens int  `json:"max_completion_tokens"`
		IsModerated         bool `json:"is_moderated"`
	} `json:"top_provider"`
	Pricing struct {
		Prompt     string `json:"prompt"`
		Completion string `json:"completion"`
	} `json:"pricing"`
}

// ModelsResponse represents the API response for models
type ModelsResponse struct {
	Data []ModelMetadata `json:"data"`
}

// DisplayModel holds processed model info for UI
type DisplayModel struct {
	ID          string
	DisplayName string
	Group       string
	IsFree      bool
}

// modelCache caches the model metadata to avoid repeated API calls
var (
	modelCacheMu    sync.RWMutex
	modelCache      map[string]ModelMetadata
	modelCacheTime  time.Time
	modelCacheTTL   = 1 * time.Hour // Cache TTL
)

// FetchModelsMetadata fetches all model metadata from OpenRouter API.
// Results are cached for 1 hour to avoid excessive API calls.
func FetchModelsMetadata(ctx context.Context, apiKey string) (map[string]ModelMetadata, error) {
	modelCacheMu.RLock()
	if modelCache != nil && time.Since(modelCacheTime) < modelCacheTTL {
		result := modelCache
		modelCacheMu.RUnlock()
		return result, nil
	}
	modelCacheMu.RUnlock()

	// Fetch fresh data
	modelCacheMu.Lock()
	defer modelCacheMu.Unlock()

	// Double-check after acquiring write lock
	if modelCache != nil && time.Since(modelCacheTime) < modelCacheTTL {
		return modelCache, nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status: %s", resp.Status)
	}

	var modelsResp ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Build cache map
	cache := make(map[string]ModelMetadata, len(modelsResp.Data))
	for _, m := range modelsResp.Data {
		cache[m.ID] = m
	}

	modelCache = cache
	modelCacheTime = time.Now()

	return cache, nil
}

// GetMaxCompletionTokens returns the max_completion_tokens for a model.
// Returns 0 if model is not found or metadata fetch fails.
func GetMaxCompletionTokens(ctx context.Context, apiKey, modelID string) int {
	cache, err := FetchModelsMetadata(ctx, apiKey)
	if err != nil {
		return 0
	}

	if model, ok := cache[modelID]; ok {
		return model.TopProvider.MaxCompletionTokens
	}
	return 0
}

// GetModelMetadata returns the full metadata for a specific model.
func GetModelMetadata(ctx context.Context, apiKey, modelID string) (ModelMetadata, bool) {
	cache, err := FetchModelsMetadata(ctx, apiKey)
	if err != nil {
		return ModelMetadata{}, false
	}

	model, ok := cache[modelID]
	return model, ok
}

// ListModels fetches and processes models from OpenRouter for UI display
func ListModels(apiKey string) ([]DisplayModel, error) {
	cache, err := FetchModelsMetadata(context.Background(), apiKey)
	if err != nil {
		return nil, err
	}

	var displayModels []DisplayModel
	for _, m := range cache {
		// Parse Group and Name
		parts := strings.SplitN(m.ID, "/", 2)
		group := "unknown"
		if len(parts) == 2 {
			group = parts[0]
		}

		// Check if free
		isFree := strings.Contains(m.ID, ":free")

		// Format Display Name
		displayName := m.Name
		if isFree {
			displayName = fmt.Sprintf("[FREE] %s", displayName)
		}

		displayModels = append(displayModels, DisplayModel{
			ID:          m.ID,
			DisplayName: displayName,
			Group:       group,
			IsFree:      isFree,
		})
	}

	// Sort by Group then Name
	sort.Slice(displayModels, func(i, j int) bool {
		if displayModels[i].Group != displayModels[j].Group {
			return displayModels[i].Group < displayModels[j].Group
		}
		return displayModels[i].DisplayName < displayModels[j].DisplayName
	})

	return displayModels, nil
}

// ModelInfoResult contains enhanced model information for UI display
type ModelInfoResult struct {
	ID                  string              `json:"id"`
	Name                string              `json:"name"`
	ContextLength       int                 `json:"context_length,omitempty"`
	MaxCompletionTokens int                 `json:"max_completion_tokens,omitempty"`
	Pricing             *ModelPricingResult `json:"pricing,omitempty"`
}

// ModelPricingResult contains pricing information
type ModelPricingResult struct {
	Prompt     string `json:"prompt"`     // Cost per token for input
	Completion string `json:"completion"` // Cost per token for output
}

// ListModelsWithMetadata returns all models with full metadata including pricing.
// Uses cached data from FetchModelsMetadata.
func ListModelsWithMetadata(ctx context.Context, apiKey string) ([]ModelInfoResult, error) {
	cache, err := FetchModelsMetadata(ctx, apiKey)
	if err != nil {
		return nil, err
	}

	var models []ModelInfoResult
	for _, m := range cache {
		model := ModelInfoResult{
			ID:                  m.ID,
			Name:                m.Name,
			ContextLength:       m.ContextLength,
			MaxCompletionTokens: m.TopProvider.MaxCompletionTokens,
		}

		// Add pricing info if available
		if m.Pricing.Prompt != "" || m.Pricing.Completion != "" {
			model.Pricing = &ModelPricingResult{
				Prompt:     m.Pricing.Prompt,
				Completion: m.Pricing.Completion,
			}
		}

		models = append(models, model)
	}

	// Sort by name for consistent ordering
	sort.Slice(models, func(i, j int) bool {
		return models[i].Name < models[j].Name
	})

	return models, nil
}
