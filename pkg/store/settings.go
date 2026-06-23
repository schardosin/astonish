package store

import "context"

// ProviderConfig holds a single provider's configuration fields.
// Keys include: type, base_url, api_key, client_id, client_secret, auth_url,
// resource_group, etc. Secret fields are stored encrypted and scrubbed from
// the JSONB on save; they are re-injected at read time for runtime use.
type ProviderConfig = map[string]string

// PlatformSettings represents platform-wide configuration visible to ALL
// organizations and teams. Stored in the platform_settings table.
type PlatformSettings struct {
	// Providers maps provider instance names to their configuration.
	Providers map[string]ProviderConfig `json:"providers,omitempty"`

	// DefaultProvider is the platform-wide default provider instance name.
	DefaultProvider string `json:"default_provider,omitempty"`

	// DefaultModel is the platform-wide default model ID.
	DefaultModel string `json:"default_model,omitempty"`

	// Channels holds per-channel-type configuration (non-secret fields).
	// Secrets (tokens, passwords) are stored separately in platform_secrets.
	Channels *PlatformChannelSettings `json:"channels,omitempty"`

	// Auth holds platform-wide authentication policy settings.
	// These override the corresponding YAML config values when set.
	Auth *PlatformAuthSettings `json:"auth,omitempty"`
}

// PlatformAuthSettings holds platform-wide authentication policy that can be
// changed at runtime via the Platform Admin UI. When a field is non-nil it
// overrides the corresponding value from the YAML config file.
type PlatformAuthSettings struct {
	// AllowRegistration controls whether new users can self-register.
	// nil means "use YAML config default" (which itself defaults to true).
	AllowRegistration *bool `json:"allow_registration,omitempty"`

	// RequireEmailVerification controls whether self-registered users must
	// verify their email before their account becomes active.
	// nil means "use YAML config default" (which itself defaults to true).
	RequireEmailVerification *bool `json:"require_email_verification,omitempty"`
}

// PlatformChannelSettings groups configuration for all supported channel adapters.
type PlatformChannelSettings struct {
	Telegram *PlatformTelegramConfig `json:"telegram,omitempty"`
	Email    *PlatformEmailConfig    `json:"email,omitempty"`
	Slack    *PlatformSlackConfig    `json:"slack,omitempty"`
}

// PlatformTelegramConfig holds non-secret Telegram channel settings.
type PlatformTelegramConfig struct {
	Enabled bool `json:"enabled"`
}

// PlatformEmailConfig holds non-secret Email channel settings.
type PlatformEmailConfig struct {
	Enabled      bool   `json:"enabled"`
	Provider     string `json:"provider,omitempty"`      // "imap" (default) or "gmail"
	IMAPServer   string `json:"imap_server"`             // e.g. "imap.gmail.com:993"
	SMTPServer   string `json:"smtp_server"`             // e.g. "smtp.gmail.com:587"
	Address      string `json:"address"`                 // agent's email address
	Username     string `json:"username,omitempty"`      // login username (defaults to address)
	PollInterval int    `json:"poll_interval,omitempty"` // seconds, default 30
	Folder       string `json:"folder,omitempty"`        // default "INBOX"
	MarkRead     *bool  `json:"mark_read,omitempty"`     // default true
	MaxBodyChars int    `json:"max_body_chars,omitempty"` // default 50000
}

// PlatformSlackConfig holds non-secret Slack channel settings.
type PlatformSlackConfig struct {
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode,omitempty"` // "socket" (default) or "events"
}

// OrgSettings represents organization-wide configuration visible to all
// teams within the org. Stored in organizations.settings JSONB column.
type OrgSettings struct {
	// Providers maps provider instance names to their configuration.
	Providers map[string]ProviderConfig `json:"providers,omitempty"`

	// DefaultProvider overrides the platform default for this org.
	DefaultProvider string `json:"default_provider,omitempty"`

	// DefaultModel overrides the platform default for this org.
	DefaultModel string `json:"default_model,omitempty"`
}

// TeamSettings represents the team-level subset of application configuration
// that can be independently configured per-team in platform mode.
// These settings are stored in the teams.settings JSONB column.
type TeamSettings struct {
	// Providers maps provider instance names to their configuration.
	Providers map[string]ProviderConfig `json:"providers,omitempty"`

	// DefaultProvider overrides the org/platform default for this team.
	DefaultProvider string `json:"default_provider,omitempty"`

	// DefaultModel overrides the org/platform default for this team.
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

	// DisabledTools is a list of built-in tool names that are disabled for this team.
	// Tools in this list will not be available to the agent when serving this team's requests.
	// Only applies in platform mode. Empty = all tools available (default).
	DisabledTools []string `json:"disabled_tools,omitempty"`
}

// PlatformSettingsStore provides read/write access to platform-level settings.
// These are visible to all organizations and teams.
type PlatformSettingsStore interface {
	// Get returns the current platform settings.
	Get(ctx context.Context) (*PlatformSettings, error)

	// Save persists the platform settings.
	Save(ctx context.Context, settings *PlatformSettings) error
}

// OrgSettingsStore provides read/write access to org-level settings.
// These are visible to all teams within the organization.
type OrgSettingsStore interface {
	// Get returns the current org settings.
	Get(ctx context.Context) (*OrgSettings, error)

	// Save persists the org settings.
	Save(ctx context.Context, settings *OrgSettings) error
}

// SettingsStore provides read/write access to team-level settings.
type SettingsStore interface {
	// Get returns the current team settings.
	Get(ctx context.Context) (*TeamSettings, error)

	// Save persists the team settings.
	Save(ctx context.Context, settings *TeamSettings) error
}
