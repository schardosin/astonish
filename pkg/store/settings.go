package store

import "context"

// TeamSettings represents the team-level subset of application configuration
// that can be independently configured per-team in platform mode.
// These settings are stored in the teams.settings JSONB column.
type TeamSettings struct {
	// Providers maps provider instance names to their configuration (API keys, base URLs, models).
	Providers map[string]map[string]string `json:"providers,omitempty"`

	// DefaultProvider is the default provider instance name.
	DefaultProvider string `json:"default_provider,omitempty"`

	// DefaultModel is the default model ID.
	DefaultModel string `json:"default_model,omitempty"`

	// WebSearchTool is the configured web search tool.
	WebSearchTool string `json:"web_search_tool,omitempty"`

	// WebExtractTool is the configured web extraction tool.
	WebExtractTool string `json:"web_extract_tool,omitempty"`

	// ContextLength overrides context window size (tokens).
	ContextLength int `json:"context_length,omitempty"`

	// WebServers holds web tool configurations (Tavily, Brave, Firecrawl keys and state).
	WebServers map[string]map[string]string `json:"web_servers,omitempty"`

	// MemoryProvider is the embedding provider for memory indexing.
	MemoryProvider string `json:"memory_provider,omitempty"`

	// MemoryModel is the embedding model for memory indexing.
	MemoryModel string `json:"memory_model,omitempty"`

	// TemplateName is the sandbox container template name for this team.
	// When set, all fleet sessions for this team use this template instead of @base.
	// Format: "team-<slug>" (e.g., "team-general").
	TemplateName string `json:"template_name,omitempty"`
}

// SettingsStore provides read/write access to team-level settings.
type SettingsStore interface {
	// Get returns the current team settings.
	Get(ctx context.Context) (*TeamSettings, error)

	// Save persists the team settings.
	Save(ctx context.Context, settings *TeamSettings) error
}
