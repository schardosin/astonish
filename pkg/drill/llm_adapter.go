package drill

import (
	"context"
	"strings"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// llmProviderAdapter wraps a model.LLM (from Google's ADK) to implement
// the LLMProvider interface used by semantic assertions.
type llmProviderAdapter struct {
	llm model.LLM
}

// NewLLMProviderFromModel creates an LLMProvider from an ADK model.LLM.
// This allows semantic assertions to use whatever provider the user has
// configured (Anthropic, OpenAI, Gemini, etc.).
func NewLLMProviderFromModel(llm model.LLM) LLMProvider {
	if llm == nil {
		return nil
	}
	return &llmProviderAdapter{llm: llm}
}

func (a *llmProviderAdapter) EvaluateText(ctx context.Context, prompt string) (string, error) {
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{
				Role: "user",
				Parts: []*genai.Part{
					genai.NewPartFromText(prompt),
				},
			},
		},
	}

	var result strings.Builder
	for resp, err := range a.llm.GenerateContent(ctx, req, false) {
		if err != nil {
			return "", err
		}
		if resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if part.Text != "" {
					result.WriteString(part.Text)
				}
			}
		}
	}
	return result.String(), nil
}
