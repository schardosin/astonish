package groq

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
)

// ListModels fetches available models from Groq API.
func ListModels(ctx context.Context, apiKey string) ([]string, error) {
	if apiKey == "" {
		apiKey = os.Getenv("GROQ_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("GROQ_API_KEY not set")
	}

	url := "https://api.groq.com/openai/v1/models"
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
		return nil, fmt.Errorf("no models found from Groq API")
	}

	sort.Strings(models)
	return models, nil
}
