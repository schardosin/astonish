package store

import (
	"context"
	"fmt"
	"time"
)

// ModelLimitEntry is a learned or configured per-model capability/limit.
type ModelLimitEntry struct {
	MaxOutputTokens int       `json:"max_output_tokens,omitempty"`
	ContextWindow   int       `json:"context_window,omitempty"`
	SupportsTools   *bool     `json:"supports_tools,omitempty"`
	Source          string    `json:"source,omitempty"` // e.g. "learned_400"
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
}

// ModelLimitsStore persists per-provider/model token limits and capabilities
// learned from provider error responses (or set explicitly). Platform-scoped.
type ModelLimitsStore interface {
	// Get returns the learned limits for provider/model, or nil if none.
	Get(ctx context.Context, provider, model string) (*ModelLimitEntry, error)

	// UpsertMaxOutput stores maxOutputTokens for provider/model.
	// Only decreases an existing value (or sets if missing).
	UpsertMaxOutput(ctx context.Context, provider, model string, max int, source string) error

	// UpsertSupportsTools stores whether the model supports function calling.
	// Only persists supports == false (we only learn the negative from errors).
	UpsertSupportsTools(ctx context.Context, provider, model string, supports bool, source string) error
}

// ModelLimitsKey builds the composite map key used in platform_settings.
func ModelLimitsKey(provider, model string) string {
	return fmt.Sprintf("%s/%s", provider, model)
}
