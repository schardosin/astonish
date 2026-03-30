package groq

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

	"github.com/schardosin/astonish/pkg/provider/httpool"
)

const modelsURL = "https://api.groq.com/openai/v1/models"

// Cache for models metadata
var (
	modelCacheMu   sync.RWMutex
	modelCache     []ModelInfo
	modelCacheTime time.Time
	modelCacheTTL  = 1 * time.Hour
)

// ModelInfo represents enhanced model metadata for Groq
type ModelInfo struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	OwnedBy       string `json:"owned_by,omitempty"`
	ContextWindow int    `json:"context_length,omitempty"`
	Active        bool   `json:"active,omitempty"`
	CreatedAt     int64  `json:"created_at,omitempty"`
}

// groqModelResponse represents a single model from Groq API
type groqModelResponse struct {
	ID            string `json:"id"`
	Object        string `json:"object"`
	Created       int64  `json:"created"`
	OwnedBy       string `json:"owned_by"`
	Active        bool   `json:"active"`
	ContextWindow int    `json:"context_window"`
}

// groqModelsResponse represents the API response
type groqModelsResponse struct {
	Object string              `json:"object"`
	Data   []groqModelResponse `json:"data"`
}

// fetchModels fetches models from Groq API
func fetchModels(ctx context.Context, apiKey string) ([]ModelInfo, error) {
	if apiKey == "" {
		apiKey = os.Getenv("GROQ_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("GROQ_API_KEY not set")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := httpool.Client(30 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			body = []byte(fmt.Sprintf("<unreadable: %v>", readErr))
		}
		return nil, fmt.Errorf("failed to fetch models: %s - %s", resp.Status, string(body))
	}

	var result groqModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var models []ModelInfo
	for _, m := range result.Data {
		// Only include active models
		if !m.Active {
			continue
		}

		models = append(models, ModelInfo{
			ID:            m.ID,
			Name:          m.ID, // Groq uses ID as display name
			OwnedBy:       m.OwnedBy,
			ContextWindow: m.ContextWindow,
			Active:        m.Active,
			CreatedAt:     m.Created,
		})
	}

	// Sort by ID
	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})

	return models, nil
}

// ListModelsWithMetadata fetches models with full metadata and caching
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

// ListModels fetches available models from Groq API (backward compatible).
func ListModels(ctx context.Context, apiKey string) ([]string, error) {
	models, err := ListModelsWithMetadata(ctx, apiKey)
	if err != nil {
		return nil, err
	}

	var ids []string
	for _, m := range models {
		ids = append(ids, m.ID)
	}

	if len(ids) == 0 {
		return nil, fmt.Errorf("no models found from Groq API")
	}

	return ids, nil
}
