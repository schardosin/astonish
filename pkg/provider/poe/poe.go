package poe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"
)

const baseURL = "https://api.poe.com/v1"

// Cache for models metadata
var (
	modelCacheMu   sync.RWMutex
	modelCache     []ModelInfo
	modelCacheTime time.Time
	modelCacheTTL  = 1 * time.Hour
)

// ModelInfo represents enhanced model metadata for Poe
type ModelInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	OwnedBy   string `json:"owned_by,omitempty"`
	CreatedAt int64  `json:"created_at,omitempty"`
}

// poeModelResponse represents a single model from Poe API
type poeModelResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// poeModelsResponse represents the API response
type poeModelsResponse struct {
	Object string             `json:"object"`
	Data   []poeModelResponse `json:"data"`
}

// fetchModels fetches models from Poe API
func fetchModels(ctx context.Context, apiKey string) ([]ModelInfo, error) {
	if apiKey == "" {
		apiKey = os.Getenv("POE_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("POE_API_KEY not set")
	}

	url := fmt.Sprintf("%s/models", baseURL)
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

	var result poeModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var models []ModelInfo
	for _, m := range result.Data {
		models = append(models, ModelInfo{
			ID:        m.ID,
			Name:      m.ID, // Poe uses ID as display name
			OwnedBy:   m.OwnedBy,
			CreatedAt: m.Created,
		})
	}

	// Sort by ID
	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})

	return models, nil
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

// ListModels fetches available models from Poe API (backward compatible).
func ListModels(ctx context.Context, apiKey string) ([]string, error) {
	models, err := ListModelsWithMetadata(ctx, apiKey)
	if err != nil {
		return nil, err
	}

	var ids []string
	for _, m := range models {
		ids = append(ids, m.ID)
	}

	sort.Strings(ids)
	return ids, nil
}

// GetBaseURL returns the base URL for the Poe API.
func GetBaseURL() string {
	return baseURL
}
