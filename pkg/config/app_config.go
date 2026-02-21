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
}

// WebServerConfig stores credentials for a standard web MCP server (Tavily, Brave, Firecrawl).
// The server command, args, and env var names are defined in standard_servers.go.
// Only the API key is stored here, keeping config.yaml as the single source of truth
// for web server credentials (separate from mcp_config.json which holds custom servers).
type WebServerConfig struct {
	APIKey string `yaml:"api_key" json:"api_key"`
}

// SessionConfig controls session persistence behavior.
type SessionConfig struct {
	// Storage type: "memory" (default) or "file"
	Storage string `yaml:"storage,omitempty" json:"storage,omitempty"`
	// BaseDir overrides the default session storage directory.
	// Empty means ~/.config/astonish/sessions/
	BaseDir string `yaml:"base_dir,omitempty" json:"base_dir,omitempty"`
}

type ChatConfig struct {
	SystemPrompt      string `yaml:"system_prompt,omitempty" json:"system_prompt,omitempty"`
	MaxToolCalls      int    `yaml:"max_tool_calls,omitempty" json:"max_tool_calls,omitempty"`
	MaxTools          int    `yaml:"max_tools,omitempty" json:"max_tools,omitempty"`
	AutoApprove       bool   `yaml:"auto_approve,omitempty" json:"auto_approve,omitempty"`
	WorkspaceDir      string `yaml:"workspace_dir,omitempty" json:"workspace_dir,omitempty"`
	FlowSaveThreshold int    `yaml:"flow_save_threshold,omitempty" json:"flow_save_threshold,omitempty"`
	FlowSaveDir       string `yaml:"flow_save_dir,omitempty" json:"flow_save_dir,omitempty"`
}

type GeneralConfig struct {
	DefaultProvider string `yaml:"default_provider" json:"default_provider"`
	DefaultModel    string `yaml:"default_model" json:"default_model"`
	WebSearchTool   string `yaml:"web_search_tool" json:"web_search_tool"`
	WebExtractTool  string `yaml:"web_extract_tool" json:"web_extract_tool"`
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
