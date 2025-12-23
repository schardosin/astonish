package provider

// ModelInfo represents enhanced model information for UI display
type ModelInfo struct {
	ID                  string        `json:"id"`
	Name                string        `json:"name"`
	ContextLength       int           `json:"context_length,omitempty"`
	MaxCompletionTokens int           `json:"max_completion_tokens,omitempty"`
	Pricing             *ModelPricing `json:"pricing,omitempty"`
}

// ModelPricing contains the pricing information for a model
type ModelPricing struct {
	Prompt     string `json:"prompt"`     // Cost per token for input
	Completion string `json:"completion"` // Cost per token for output
}
