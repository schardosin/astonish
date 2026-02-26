package config

import (
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type AppConfig struct {
	General    GeneralConfig              `yaml:"general"`
	WebServers map[string]WebServerConfig `yaml:"web_servers,omitempty" json:"web_servers,omitempty"`
	Providers  map[string]ProviderConfig  `yaml:"providers"`
	Chat       ChatConfig                 `yaml:"chat,omitempty"`
	Sessions   SessionConfig              `yaml:"sessions,omitempty"`
	Memory     MemoryConfig               `yaml:"memory,omitempty"`
	Daemon     DaemonConfig               `yaml:"daemon,omitempty"`
	Channels   ChannelsConfig             `yaml:"channels,omitempty"`
	Scheduler  SchedulerConfig            `yaml:"scheduler,omitempty"`
	Browser    BrowserAppConfig           `yaml:"browser,omitempty"`
	SubAgents  SubAgentAppConfig          `yaml:"sub_agents,omitempty"`
	Skills     SkillsConfig               `yaml:"skills,omitempty"`
}

// MemoryConfig controls the semantic memory / RAG system.
type MemoryConfig struct {
	Enabled   *bool           `yaml:"enabled,omitempty" json:"enabled,omitempty"` // Default: true (nil means true)
	MemoryDir string          `yaml:"memory_dir,omitempty" json:"memory_dir,omitempty"`
	VectorDir string          `yaml:"vector_dir,omitempty" json:"vector_dir,omitempty"`
	Embedding EmbeddingConfig `yaml:"embedding,omitempty" json:"embedding,omitempty"`
	Chunking  ChunkingConfig  `yaml:"chunking,omitempty" json:"chunking,omitempty"`
	Search    SearchConfig    `yaml:"search,omitempty" json:"search,omitempty"`
	Sync      SyncConfig      `yaml:"sync,omitempty" json:"sync,omitempty"`
}

// EmbeddingConfig controls the embedding provider for memory search.
type EmbeddingConfig struct {
	Provider string `yaml:"provider,omitempty" json:"provider,omitempty"` // "auto", "openai", "ollama", "openai-compat"
	Model    string `yaml:"model,omitempty" json:"model,omitempty"`
	BaseURL  string `yaml:"base_url,omitempty" json:"base_url,omitempty"`
	APIKey   string `yaml:"api_key,omitempty" json:"api_key,omitempty"`
}

// ChunkingConfig controls how memory files are split into chunks.
type ChunkingConfig struct {
	MaxChars int `yaml:"max_chars,omitempty" json:"max_chars,omitempty"` // Default: 1600
	Overlap  int `yaml:"overlap,omitempty" json:"overlap,omitempty"`     // Default: 320
}

// SearchConfig controls memory search defaults.
type SearchConfig struct {
	MaxResults int     `yaml:"max_results,omitempty" json:"max_results,omitempty"` // Default: 6
	MinScore   float64 `yaml:"min_score,omitempty" json:"min_score,omitempty"`     // Default: 0.35
}

// SyncConfig controls the file watcher for live reindexing.
type SyncConfig struct {
	Watch      *bool `yaml:"watch,omitempty" json:"watch,omitempty"`             // Default: true (nil means true)
	DebounceMs int   `yaml:"debounce_ms,omitempty" json:"debounce_ms,omitempty"` // Default: 1500
}

// IsMemoryEnabled returns whether the memory system is enabled.
// Defaults to true if not explicitly set.
func (c *MemoryConfig) IsMemoryEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// IsWatchEnabled returns whether the file watcher is enabled.
// Defaults to true if not explicitly set.
func (c *MemoryConfig) IsWatchEnabled() bool {
	if c.Sync.Watch == nil {
		return true
	}
	return *c.Sync.Watch
}

// GetMemoryDir returns the memory directory, defaulting to ~/.config/astonish/memory/.
func GetMemoryDir(cfg *MemoryConfig) (string, error) {
	if cfg != nil && cfg.MemoryDir != "" {
		return cfg.MemoryDir, nil
	}
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "memory"), nil
}

// GetVectorDir returns the vector storage directory, defaulting to ~/.config/astonish/memory/vectors/.
func GetVectorDir(cfg *MemoryConfig) (string, error) {
	if cfg != nil && cfg.VectorDir != "" {
		return cfg.VectorDir, nil
	}
	memDir, err := GetMemoryDir(cfg)
	if err != nil {
		return "", err
	}
	return filepath.Join(memDir, "vectors"), nil
}

// WebServerConfig stores credentials and installation state for a standard MCP server.
// The server command, args, and env var names are defined in standard_servers.go.
// For servers that require an API key (Tavily, Brave, Firecrawl), the key is stored here.
// For keyless servers, only the Installed flag is set.
type WebServerConfig struct {
	APIKey    string `yaml:"api_key,omitempty" json:"api_key,omitempty"`
	Installed bool   `yaml:"installed,omitempty" json:"installed,omitempty"`
}

// SessionConfig controls session persistence behavior.
type SessionConfig struct {
	// Storage type: "memory" (default) or "file"
	Storage string `yaml:"storage,omitempty" json:"storage,omitempty"`
	// BaseDir overrides the default session storage directory.
	// Empty means ~/.config/astonish/sessions/
	BaseDir string `yaml:"base_dir,omitempty" json:"base_dir,omitempty"`
	// Compaction controls automatic context window compaction.
	Compaction CompactionConfig `yaml:"compaction,omitempty" json:"compaction,omitempty"`
}

// CompactionConfig controls how and when context is compacted.
type CompactionConfig struct {
	// Enabled turns compaction on/off. Default true (nil means true).
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// Threshold is the fraction of the context window that triggers compaction (0.0-1.0).
	// Default 0.8 (compact when 80% full).
	Threshold float64 `yaml:"threshold,omitempty" json:"threshold,omitempty"`
	// PreserveRecent is the number of most recent messages to keep intact (not summarized).
	// Default 4.
	PreserveRecent int `yaml:"preserve_recent,omitempty" json:"preserve_recent,omitempty"`
}

// IsCompactionEnabled returns whether compaction is enabled. Defaults to true.
func (c *CompactionConfig) IsCompactionEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// GetThreshold returns the compaction threshold (default 0.8).
func (c *CompactionConfig) GetThreshold() float64 {
	if c.Threshold <= 0 || c.Threshold > 1.0 {
		return 0.8
	}
	return c.Threshold
}

// GetPreserveRecent returns the number of recent messages to preserve (default 4).
func (c *CompactionConfig) GetPreserveRecent() int {
	if c.PreserveRecent <= 0 {
		return 4
	}
	return c.PreserveRecent
}

type ChatConfig struct {
	SystemPrompt string `yaml:"system_prompt,omitempty" json:"system_prompt,omitempty"`
	MaxToolCalls int    `yaml:"max_tool_calls,omitempty" json:"max_tool_calls,omitempty"`
	MaxTools     int    `yaml:"max_tools,omitempty" json:"max_tools,omitempty"`
	AutoApprove  bool   `yaml:"auto_approve,omitempty" json:"auto_approve,omitempty"`
	WorkspaceDir string `yaml:"workspace_dir,omitempty" json:"workspace_dir,omitempty"`
	FlowSaveDir  string `yaml:"flow_save_dir,omitempty" json:"flow_save_dir,omitempty"`
}

type GeneralConfig struct {
	DefaultProvider string `yaml:"default_provider" json:"default_provider"`
	DefaultModel    string `yaml:"default_model" json:"default_model"`
	WebSearchTool   string `yaml:"web_search_tool" json:"web_search_tool"`
	WebExtractTool  string `yaml:"web_extract_tool" json:"web_extract_tool"`
	ContextLength   int    `yaml:"context_length,omitempty" json:"context_length,omitempty"` // Override context window size (tokens)
}

// DaemonConfig controls the background daemon service.
type DaemonConfig struct {
	// Port for the HTTP server. Default: 9393.
	Port int `yaml:"port,omitempty" json:"port,omitempty"`
	// LogDir overrides the default log directory.
	// Empty means ~/.config/astonish/logs/
	LogDir string `yaml:"log_dir,omitempty" json:"log_dir,omitempty"`
	// Auth controls device-based authentication for the Studio web UI.
	// Auth is enabled by default in daemon mode.
	Auth StudioAuthConfig `yaml:"auth,omitempty" json:"auth,omitempty"`
}

// GetPort returns the daemon port, defaulting to 9393.
func (c *DaemonConfig) GetPort() int {
	if c.Port <= 0 {
		return 9393
	}
	return c.Port
}

// GetLogDir returns the log directory, defaulting to ~/.config/astonish/logs/.
func (c *DaemonConfig) GetLogDir() string {
	if c.LogDir != "" {
		return c.LogDir
	}
	configDir, err := GetConfigDir()
	if err != nil {
		return "logs"
	}
	return filepath.Join(configDir, "logs")
}

// StudioAuthConfig controls device-based authentication for the Studio web UI.
type StudioAuthConfig struct {
	// Disabled turns off authentication entirely. Default: false (auth is on).
	Disabled bool `yaml:"disabled,omitempty" json:"disabled,omitempty"`
	// SessionTTLDays controls how long an authorized session lasts.
	// Default: 90 days. Set to 0 to use the default.
	SessionTTLDays int `yaml:"session_ttl_days,omitempty" json:"session_ttl_days,omitempty"`
}

// IsAuthEnabled returns true if Studio authentication is enabled (default: true).
func (c *StudioAuthConfig) IsAuthEnabled() bool {
	return !c.Disabled
}

// GetSessionTTL returns the session TTL as a time.Duration.
func (c *StudioAuthConfig) GetSessionTTL() time.Duration {
	if c.SessionTTLDays > 0 {
		return time.Duration(c.SessionTTLDays) * 24 * time.Hour
	}
	return 90 * 24 * time.Hour
}

// ChannelsConfig controls communication channel integrations.
type ChannelsConfig struct {
	// Enabled controls whether channels are active. Default: false (nil means false).
	Enabled  *bool          `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Telegram TelegramConfig `yaml:"telegram,omitempty" json:"telegram,omitempty"`
	Email    EmailConfig    `yaml:"email,omitempty" json:"email,omitempty"`
}

// IsChannelsEnabled returns true if channels are explicitly enabled.
func (c *ChannelsConfig) IsChannelsEnabled() bool {
	return c.Enabled != nil && *c.Enabled
}

// SchedulerConfig controls the job scheduler.
type SchedulerConfig struct {
	// Enabled controls whether the scheduler is active. Default: true (nil means true).
	// Set to false to explicitly disable the scheduler.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

// IsSchedulerEnabled returns true if the scheduler is enabled.
// Defaults to true if not explicitly set (nil means true).
func (c *SchedulerConfig) IsSchedulerEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// BrowserAppConfig controls the built-in browser automation module.
// All fields are optional — defaults are applied by browser.DefaultConfig().
type BrowserAppConfig struct {
	// Headless controls whether the browser runs in headless mode. Default: true.
	Headless *bool `yaml:"headless,omitempty" json:"headless,omitempty"`
	// ViewportWidth is the default viewport width in pixels. Default: 1280.
	ViewportWidth int `yaml:"viewport_width,omitempty" json:"viewport_width,omitempty"`
	// ViewportHeight is the default viewport height in pixels. Default: 720.
	ViewportHeight int `yaml:"viewport_height,omitempty" json:"viewport_height,omitempty"`
	// NoSandbox disables Chrome's sandbox. Auto-detected (true when running as root).
	NoSandbox *bool `yaml:"no_sandbox,omitempty" json:"no_sandbox,omitempty"`
	// ChromePath overrides the Chromium binary path. Empty = auto-download via rod.
	ChromePath string `yaml:"chrome_path,omitempty" json:"chrome_path,omitempty"`
	// UserDataDir overrides the persistent browser profile directory.
	// Empty = ~/.config/astonish/browser/ (persistent profile).
	UserDataDir string `yaml:"user_data_dir,omitempty" json:"user_data_dir,omitempty"`
	// NavigationTimeout is the max seconds to wait for page loads. Default: 30.
	NavigationTimeout int `yaml:"navigation_timeout,omitempty" json:"navigation_timeout,omitempty"`
}

// SubAgentAppConfig holds configuration for the sub-agent delegation system.
type SubAgentAppConfig struct {
	// Enabled controls whether task delegation is available. Default: true (nil means true).
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// MaxDepth is the maximum delegation nesting depth. Default: 2.
	MaxDepth int `yaml:"max_depth,omitempty" json:"max_depth,omitempty"`
	// MaxConcurrent is the maximum number of parallel sub-agents. Default: 5.
	MaxConcurrent int `yaml:"max_concurrent,omitempty" json:"max_concurrent,omitempty"`
	// TaskTimeoutSec is the per-task timeout in seconds. Default: 300 (5 minutes).
	TaskTimeoutSec int `yaml:"task_timeout_sec,omitempty" json:"task_timeout_sec,omitempty"`
}

// IsSubAgentsEnabled returns whether the sub-agent system is enabled.
// Defaults to true if not explicitly set.
func (c *SubAgentAppConfig) IsSubAgentsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// TaskTimeout returns the per-task timeout as a time.Duration.
func (c *SubAgentAppConfig) TaskTimeout() time.Duration {
	if c.TaskTimeoutSec <= 0 {
		return 5 * time.Minute
	}
	return time.Duration(c.TaskTimeoutSec) * time.Second
}

// SkillsConfig controls the skills system.
type SkillsConfig struct {
	// Enabled controls whether skills are loaded. Default: true (nil means true).
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// UserDir is the directory for user-defined skills. Default: ~/.config/astonish/skills/
	UserDir string `yaml:"user_dir,omitempty" json:"user_dir,omitempty"`
	// ExtraDirs are additional directories to search for skills.
	ExtraDirs []string `yaml:"extra_dirs,omitempty" json:"extra_dirs,omitempty"`
	// Allowlist restricts which skills are loaded. Empty means all eligible skills.
	Allowlist []string `yaml:"allowlist,omitempty" json:"allowlist,omitempty"`
}

// IsSkillsEnabled returns whether the skills system is enabled.
// Defaults to true if not explicitly set.
func (c *SkillsConfig) IsSkillsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// GetUserSkillsDir returns the user skills directory, defaulting to ~/.config/astonish/skills/
func (c *SkillsConfig) GetUserSkillsDir() string {
	if c.UserDir != "" {
		return c.UserDir
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(configDir, "astonish", "skills")
}

// TelegramConfig holds configuration for the Telegram channel adapter.
type TelegramConfig struct {
	// Enabled controls whether the Telegram adapter is active. Default: false (nil means false).
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// BotToken is the Telegram bot token from BotFather.
	BotToken string `yaml:"bot_token,omitempty" json:"bot_token,omitempty"`
	// AllowFrom is a list of allowed Telegram user IDs. Required — at least one
	// user ID must be specified. An empty list blocks all messages (safe default).
	AllowFrom []string `yaml:"allow_from,omitempty" json:"allow_from,omitempty"`
}

// IsTelegramEnabled returns true if Telegram is explicitly enabled.
// Note: After credential migration, the bot token may be in the encrypted
// credential store rather than in BotToken. Callers should resolve the token
// separately if this returns true.
func (c *TelegramConfig) IsTelegramEnabled() bool {
	return c.Enabled != nil && *c.Enabled
}

// EmailConfig holds configuration for the Email channel adapter.
type EmailConfig struct {
	// Enabled controls whether the Email adapter is active. Default: false (nil means false).
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// Provider selects the implementation: "imap" or "gmail". Default: "imap".
	Provider string `yaml:"provider,omitempty" json:"provider,omitempty"`
	// IMAPServer is the IMAP server address (e.g., "imap.gmail.com:993").
	IMAPServer string `yaml:"imap_server,omitempty" json:"imap_server,omitempty"`
	// SMTPServer is the SMTP server address (e.g., "smtp.gmail.com:587").
	SMTPServer string `yaml:"smtp_server,omitempty" json:"smtp_server,omitempty"`
	// Address is the agent's email address.
	Address string `yaml:"address,omitempty" json:"address,omitempty"`
	// Username is the IMAP/SMTP login username. Often same as Address.
	Username string `yaml:"username,omitempty" json:"username,omitempty"`
	// Password is the IMAP/SMTP password or app password.
	// After credential migration, this will be empty (stored in encrypted store).
	Password string `yaml:"password,omitempty" json:"password,omitempty"`
	// PollInterval is seconds between inbox checks. Default: 30.
	PollInterval int `yaml:"poll_interval,omitempty" json:"poll_interval,omitempty"`
	// AllowFrom is a list of email addresses allowed to trigger the agent.
	// Use ["*"] to allow anyone. An empty list blocks all inbound messages.
	AllowFrom []string `yaml:"allow_from,omitempty" json:"allow_from,omitempty"`
	// Folder is the IMAP folder to monitor. Default: "INBOX".
	Folder string `yaml:"folder,omitempty" json:"folder,omitempty"`
	// MarkRead marks processed emails as read. Default: true.
	MarkRead *bool `yaml:"mark_read,omitempty" json:"mark_read,omitempty"`
	// MaxBodyChars truncates long email bodies. Default: 50000.
	MaxBodyChars int `yaml:"max_body_chars,omitempty" json:"max_body_chars,omitempty"`
}

// IsEmailEnabled returns true if the Email channel is explicitly enabled.
func (c *EmailConfig) IsEmailEnabled() bool {
	return c.Enabled != nil && *c.Enabled
}

// IsMarkRead returns whether processed emails should be marked as read.
// Defaults to true if not set.
func (c *EmailConfig) IsMarkRead() bool {
	if c.MarkRead == nil {
		return true
	}
	return *c.MarkRead
}

// GetPollInterval returns the poll interval in seconds, defaulting to 30.
func (c *EmailConfig) GetPollInterval() int {
	if c.PollInterval <= 0 {
		return 30
	}
	return c.PollInterval
}

type ProviderConfig map[string]string

// GetProviderType returns the provider type for a given provider instance.
// For new format: checks explicit "type" field
// For old format (backward compatible): uses instance name if it matches known provider type
// Returns empty string if neither is valid (caller should handle error)
func GetProviderType(instanceName string, instance ProviderConfig) string {
	if instance == nil {
		return ""
	}

	if providerType, ok := instance["type"]; ok && providerType != "" {
		return providerType
	}

	knownTypes := []string{
		"anthropic", "gemini", "groq", "litellm", "lm_studio",
		"ollama", "openai", "openrouter", "poe", "sap_ai_core", "xai",
	}

	for _, knownType := range knownTypes {
		if instanceName == knownType {
			return instanceName
		}
	}

	return ""
}

func GetConfigDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "astonish"), nil
}

func GetConfigPath() (string, error) {
	dir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

func GetAgentsDir() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "agents"), nil
}

// GetModelsDir returns the directory for locally downloaded ML models (e.g., embedding models).
// Defaults to ~/.config/astonish/models/.
func GetModelsDir() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "models"), nil
}

// GetSessionsDir returns the session storage directory.
// If the config specifies a custom base_dir, that is used; otherwise
// it defaults to ~/.config/astonish/sessions/.
func GetSessionsDir(cfg *SessionConfig) (string, error) {
	if cfg != nil && cfg.BaseDir != "" {
		return cfg.BaseDir, nil
	}
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "sessions"), nil
}

func LoadAppConfig() (*AppConfig, error) {
	path, err := GetConfigPath()
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &AppConfig{
			Providers: make(map[string]ProviderConfig),
		}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg AppConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Providers == nil {
		cfg.Providers = make(map[string]ProviderConfig)
	}

	return &cfg, nil
}

func SaveAppConfig(cfg *AppConfig) error {
	path, err := GetConfigPath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}
