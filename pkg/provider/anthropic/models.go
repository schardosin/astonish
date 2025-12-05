package anthropic

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// ListModels fetches available models from Anthropic.
func ListModels(apiKey string) ([]string, error) {
	url := "https://api.anthropic.com/v1/models"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
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
		return nil, err
	}

	var modelNames []string
	for _, m := range result.Data {
		modelNames = append(modelNames, m.ID)
	}

	return modelNames, nil
}
