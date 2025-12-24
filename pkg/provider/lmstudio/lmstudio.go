package lmstudio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Cache for models metadata
var (
	modelCacheMu   sync.RWMutex
	modelCache     []ModelInfo
	modelCacheTime time.Time
	modelCacheTTL  = 1 * time.Hour
)

// ModelInfo represents enhanced model metadata for LM Studio
type ModelInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	OwnedBy   string `json:"owned_by,omitempty"`
	CreatedAt int64  `json:"created_at,omitempty"`
}

// lmstudioModelResponse represents a single model from LM Studio API
type lmstudioModelResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// lmstudioModelsResponse represents the API response
type lmstudioModelsResponse struct {
	Object string                  `json:"object"`
	Data   []lmstudioModelResponse `json:"data"`
}

// fetchModels fetches models from LM Studio API
func fetchModels(ctx context.Context, baseURL string) ([]ModelInfo, error) {
	if baseURL == "" {
		baseURL = "http://localhost:1234/v1"
	}
	// Ensure no trailing slash
	baseURL = strings.TrimRight(baseURL, "/")

	url := fmt.Sprintf("%s/models", baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

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

	var result lmstudioModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var models []ModelInfo
	for _, m := range result.Data {
		models = append(models, ModelInfo{
			ID:        m.ID,
			Name:      m.ID, // LM Studio uses ID as display name
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
func ListModelsWithMetadata(ctx context.Context, baseURL string) ([]ModelInfo, error) {
	// Check cache first
	modelCacheMu.RLock()
	if len(modelCache) > 0 && time.Since(modelCacheTime) < modelCacheTTL {
		cached := make([]ModelInfo, len(modelCache))
		copy(cached, modelCache)
		modelCacheMu.RUnlock()
		return cached, nil
	}
	modelCacheMu.RUnlock()

	models, err := fetchModels(ctx, baseURL)
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

// ListModels fetches available models from LM Studio API (backward compatible).
func ListModels(ctx context.Context, baseURL string) ([]string, error) {
	models, err := ListModelsWithMetadata(ctx, baseURL)
	if err != nil {
		return nil, err
	}

	var ids []string
	for _, m := range models {
		ids = append(ids, m.ID)
	}

	if len(ids) == 0 {
		return nil, fmt.Errorf("no models found from LM Studio API")
	}

	sort.Strings(ids)
	return ids, nil
}
