package openai

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/sashabaranov/go-openai"
)

// Cache for models metadata
var (
	modelCacheMu   sync.RWMutex
	modelCache     []ModelInfo
	modelCacheTime time.Time
	modelCacheTTL  = 1 * time.Hour
)

// ModelInfo represents enhanced model metadata for OpenAI
type ModelInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	OwnedBy   string `json:"owned_by,omitempty"`
	CreatedAt int64  `json:"created_at,omitempty"`
}

// fetchModels fetches models from OpenAI API
func fetchModels(ctx context.Context, apiKey string) ([]ModelInfo, error) {
	client := openai.NewClient(apiKey)
	models, err := client.ListModels(ctx)
	if err != nil {
		return nil, err
	}

	var result []ModelInfo
	for _, m := range models.Models {
		// Filter for chat models (gpt-*, o1-*, o3-*, o4-*, chatgpt-*)
		if strings.HasPrefix(m.ID, "gpt") || 
		   strings.HasPrefix(m.ID, "o1") || 
		   strings.HasPrefix(m.ID, "o3") || 
		   strings.HasPrefix(m.ID, "o4") ||
		   strings.HasPrefix(m.ID, "chatgpt") {
			result = append(result, ModelInfo{
				ID:        m.ID,
				Name:      m.ID, // OpenAI uses ID as display name
				OwnedBy:   m.OwnedBy,
				CreatedAt: m.CreatedAt,
			})
		}
	}

	return result, nil
}

// ListModelsWithMetadata fetches models with metadata and caching
func ListModelsWithMetadata(ctx context.Context, apiKey string) ([]ModelInfo, error) {
	// Check cache first
	modelCacheMu.RLock()
	if len(modelCache) > 0 && time.Since(modelCacheTime) < modelCacheTTL {
		cached := make([]ModelInfo, len(modelCache))
		copy(cached, modelCache)
		modelCacheMu.RUnlock()
		return cached, nil
	}
	modelCacheMu.RUnlock()

	models, err := fetchModels(ctx, apiKey)
	if err != nil {
		return nil, err
	}

	// Update cache
	modelCacheMu.Lock()
	modelCache = models
	modelCacheTime = time.Now()
	modelCacheMu.Unlock()

	return models, nil
}

// ListModels fetches available models from OpenAI (backward compatible).
func ListModels(ctx context.Context, apiKey string) ([]string, error) {
	models, err := ListModelsWithMetadata(ctx, apiKey)
	if err != nil {
		return nil, err
	}

	var modelNames []string
	for _, m := range models {
		modelNames = append(modelNames, m.ID)
	}
	return modelNames, nil
}
