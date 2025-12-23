package ollama

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

// ModelInfo represents enhanced model metadata for Ollama
type ModelInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	OwnedBy   string `json:"owned_by,omitempty"` // Used for Family
	CreatedAt int64  `json:"created_at,omitempty"`
}

// ollamaModelDetails represents details in the API response
type ollamaModelDetails struct {
	ParentModel       string   `json:"parent_model"`
	Format            string   `json:"format"`
	Family            string   `json:"family"`
	Families          []string `json:"families"`
	ParameterSize     string   `json:"parameter_size"`
	QuantizationLevel string   `json:"quantization_level"`
}

// ollamaModelResponse represents a single model from Ollama API
type ollamaModelResponse struct {
	Name       string             `json:"name"`
	Model      string             `json:"model"`
	ModifiedAt time.Time          `json:"modified_at"`
	Size       int64              `json:"size"`
	Digest     string             `json:"digest"`
	Details    ollamaModelDetails `json:"details"`
}

// ollamaTagsResponse represents the /api/tags response
type ollamaTagsResponse struct {
	Models []ollamaModelResponse `json:"models"`
}

// fetchModels fetches models from Ollama API
func fetchModels(ctx context.Context, baseURL string) ([]ModelInfo, error) {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	// Ensure no trailing slash
	baseURL = strings.TrimRight(baseURL, "/")

	url := fmt.Sprintf("%s/api/tags", baseURL)
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

	var result ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var models []ModelInfo
	for _, m := range result.Models {
		// Construct a descriptive "OwnedBy" string using Family, Params, and Quantization
		ownerInfo := m.Details.Family
		if m.Details.ParameterSize != "" {
			if ownerInfo != "" {
				ownerInfo += " • "
			}
			ownerInfo += m.Details.ParameterSize
		}
		if m.Details.QuantizationLevel != "" {
			if ownerInfo != "" {
				ownerInfo += " • "
			}
			ownerInfo += m.Details.QuantizationLevel
		}

		models = append(models, ModelInfo{
			ID:        m.Name,
			Name:      m.Name,
			OwnedBy:   ownerInfo,
			CreatedAt: m.ModifiedAt.Unix(),
		})
	}

	// Sort by Name
	sort.Slice(models, func(i, j int) bool {
		return models[i].Name < models[j].Name
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

// ListModels fetches available models from Ollama API (backward compatible).
func ListModels(ctx context.Context, baseURL string) ([]string, error) {
	models, err := ListModelsWithMetadata(ctx, baseURL)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, m := range models {
		names = append(names, m.Name)
	}

	if len(names) == 0 {
		return nil, fmt.Errorf("no models found from Ollama API")
	}

	sort.Strings(names)
	return names, nil
}
