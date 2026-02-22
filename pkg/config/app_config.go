package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type AppConfig struct {
	General    GeneralConfig              `yaml:"general"`
	WebServers map[string]WebServerConfig `yaml:"web_servers,omitempty" json:"web_servers,omitempty"`
	Providers  map[string]ProviderConfig  `yaml:"providers"`
	Chat       ChatConfig                 `yaml:"chat,omitempty"`
	Sessions   SessionConfig              `yaml:"sessions,omitempty"`
	Memory     MemoryConfig               `yaml:"memory,omitempty"`
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
// For keyless servers (Playwright), only the Installed flag is set.
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
