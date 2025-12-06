package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// ListModels fetches available models from Ollama API.
func ListModels(ctx context.Context, baseURL string) ([]string, error) {
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

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var models []string
	for _, m := range result.Models {
		models = append(models, m.Name)
	}

	if len(models) == 0 {
		return nil, fmt.Errorf("no models found from Ollama API")
	}

	sort.Strings(models)
	return models, nil
}
