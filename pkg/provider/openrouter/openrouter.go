package openrouter

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// Model represents an OpenRouter model
type Model struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Pricing struct {
		Prompt    string `json:"prompt"`
		Completion string `json:"completion"`
	} `json:"pricing"`
}

// ModelResponse represents the API response for models
type ModelResponse struct {
	Data []Model `json:"data"`
}

// DisplayModel holds processed model info for UI
type DisplayModel struct {
	ID          string
	DisplayName string
	Group       string
	IsFree      bool
}

// ListModels fetches and processes models from OpenRouter
func ListModels(apiKey string) ([]DisplayModel, error) {
	req, err := http.NewRequest("GET", "https://openrouter.ai/api/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// OpenRouter models endpoint is public, but providing key might help with rate limits
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status: %s", resp.Status)
	}

	var modelResp ModelResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var displayModels []DisplayModel
	for _, m := range modelResp.Data {
		// Parse Group and Name
		// ID format is usually "vendor/model-name"
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
