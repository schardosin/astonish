package openai

import (
	"context"
	"strings"

	"github.com/sashabaranov/go-openai"
)

// ListModels fetches available models from OpenAI.
func ListModels(ctx context.Context, apiKey string) ([]string, error) {
	client := openai.NewClient(apiKey)
	models, err := client.ListModels(ctx)
	if err != nil {
		return nil, err
	}

	var modelNames []string
	for _, m := range models.Models {
		// Filter for chat models (gpt-*)
		// Python implementation filters for "gpt" start
		if strings.HasPrefix(m.ID, "gpt") {
			modelNames = append(modelNames, m.ID)
		}
	}
	return modelNames, nil
}
