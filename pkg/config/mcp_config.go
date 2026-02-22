package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// MCPServerConfig represents the configuration for a single MCP server
type MCPServerConfig struct {
	Command   string            `json:"command" yaml:"command"`
	Args      []string          `json:"args" yaml:"args,omitempty"`
	Env       map[string]string `json:"env" yaml:"env,omitempty"`
	Transport string            `json:"transport" yaml:"transport,omitempty"`       // "stdio" or "sse"
	URL       string            `json:"url,omitempty" yaml:"url,omitempty"`         // For SSE transport
	Enabled   *bool             `json:"enabled,omitempty" yaml:"enabled,omitempty"` // nil defaults to true
}

// IsEnabled returns true if the server is enabled (defaults to true if not set)
func (c *MCPServerConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// MCPConfig represents the entire MCP configuration
type MCPConfig struct {
	MCPServers map[string]MCPServerConfig `json:"mcpServers"`
}

// LoadMCPConfig loads the MCP configuration from the config directory.
// It merges standard web servers from config.yaml into the result so that
// the MCP manager can start them alongside custom servers. Standard server
// entries from config.yaml take precedence over same-name entries in mcp_config.json.
func LoadMCPConfig() (*MCPConfig, error) {
	cfg, err := LoadMCPConfigRaw()
	if err != nil {
		return nil, err
	}

	// Merge standard web servers from config.yaml.
	// These are stored separately so they survive mcp_config.json resets.
	mergeStandardServers(cfg)

	return cfg, nil
}

// LoadMCPConfigRaw loads the MCP configuration from mcp_config.json only,
// without merging standard web servers from config.yaml.
// Use this when you need the raw file contents (e.g. for the Settings UI Source view).
func LoadMCPConfigRaw() (*MCPConfig, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get config directory: %w", err)
	}

	mcpConfigPath := filepath.Join(configDir, "mcp_config.json")

	var config MCPConfig

	if _, err := os.Stat(mcpConfigPath); os.IsNotExist(err) {
		config.MCPServers = make(map[string]MCPServerConfig)
	} else {
		data, err := os.ReadFile(mcpConfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read MCP config file: %w", err)
		}

		if err := json.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("failed to parse MCP config: %w", err)
		}

		if config.MCPServers == nil {
			config.MCPServers = make(map[string]MCPServerConfig)
		}
	}

	return &config, nil
}

// mergeStandardServers injects configured standard servers from config.yaml
// into the MCPConfig. Entries from config.yaml override same-name entries in mcp_config.json
// but preserve any explicit Enabled flag the user has set (e.g., disabling Playwright).
// Key-based servers (Tavily, Brave, Firecrawl) require an API key in config.yaml.
// Keyless servers (Playwright) are always injected — they need no setup.
func mergeStandardServers(cfg *MCPConfig) {
	appCfg, _ := LoadAppConfig()

	// First: inject key-based servers that have credentials in config.yaml
	if appCfg != nil && appCfg.WebServers != nil {
		for id, ws := range appCfg.WebServers {
			srv := GetStandardServer(id)
			if srv == nil || len(srv.EnvVars) == 0 {
				continue // keyless handled below
			}
			if ws.APIKey == "" {
				continue
			}
			newCfg := BuildMCPServerConfig(srv, ws.APIKey)
			// Preserve explicit Enabled flag from existing entry (user may have disabled it)
			if existing, ok := cfg.MCPServers[id]; ok && existing.Enabled != nil {
				newCfg.Enabled = existing.Enabled
			}
			cfg.MCPServers[id] = newCfg
		}
	}

	// Second: always inject keyless servers (e.g. Playwright) — no config entry needed.
	// Refresh command/args but preserve any explicit Enabled flag.
	for _, srv := range GetStandardServers() {
		if len(srv.EnvVars) > 0 {
			continue // needs API key, handled above
		}
		newCfg := BuildMCPServerConfig(&srv, "")
		if existing, ok := cfg.MCPServers[srv.ID]; ok && existing.Enabled != nil {
			newCfg.Enabled = existing.Enabled
		}
		cfg.MCPServers[srv.ID] = newCfg
	}
}

// SaveMCPConfig saves the MCP configuration to the config directory.
// Standard web servers (from config.yaml) are stripped before writing
// since they are managed separately and merged at load time.
// Exception: disabled standard servers are preserved — the user explicitly
// chose to disable them, and that choice must survive save/load cycles.
func SaveMCPConfig(config *MCPConfig) error {
	configDir, err := GetConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	mcpConfigPath := filepath.Join(configDir, "mcp_config.json")

	// Strip standard server entries — they live in config.yaml, not here.
	// But keep any that were explicitly disabled (Enabled == false) since
	// that's a deliberate user choice we need to persist.
	filtered := &MCPConfig{
		MCPServers: make(map[string]MCPServerConfig, len(config.MCPServers)),
	}
	standardIDs := GetStandardServerIDs()
	for name, srv := range config.MCPServers {
		if !standardIDs[name] || (srv.Enabled != nil && !*srv.Enabled) {
			filtered.MCPServers[name] = srv
		}
	}

	// Marshal to JSON with indentation
	data, err := json.MarshalIndent(filtered, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal MCP config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(mcpConfigPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write MCP config file: %w", err)
	}

	return nil
}

// GetMCPConfigPath returns the path to the MCP config file
func GetMCPConfigPath() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "mcp_config.json"), nil
}

// SetMCPServerEnabled sets the enabled status for a specific MCP server
func SetMCPServerEnabled(serverName string, enabled bool) error {
	config, err := LoadMCPConfig()
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	server, exists := config.MCPServers[serverName]
	if !exists {
		return fmt.Errorf("server '%s' not found", serverName)
	}

	server.Enabled = &enabled
	config.MCPServers[serverName] = server

	if err := SaveMCPConfig(config); err != nil {
		return fmt.Errorf("failed to save MCP config: %w", err)
	}

	return nil
}

// GetMCPServerNames returns all server names from the config
func GetMCPServerNames() ([]string, error) {
	config, err := LoadMCPConfig()
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(config.MCPServers))
	for name := range config.MCPServers {
		names = append(names, name)
	}
	return names, nil
}
