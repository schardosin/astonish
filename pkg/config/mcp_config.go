package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// MCPServerConfig represents the configuration for a single MCP server
type MCPServerConfig struct {
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	Transport string            `json:"transport"` // "stdio" or "sse"
	URL       string            `json:"url,omitempty"` // For SSE transport
}

// MCPConfig represents the entire MCP configuration
type MCPConfig struct {
	MCPServers map[string]MCPServerConfig `json:"mcpServers"`
}

// LoadMCPConfig loads the MCP configuration from the config directory
func LoadMCPConfig() (*MCPConfig, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get config directory: %w", err)
	}

	mcpConfigPath := filepath.Join(configDir, "mcp_config.json")

	// Check if file exists
	if _, err := os.Stat(mcpConfigPath); os.IsNotExist(err) {
		// Return empty config if file doesn't exist
		return &MCPConfig{
			MCPServers: make(map[string]MCPServerConfig),
		}, nil
	}

	// Read file
	data, err := os.ReadFile(mcpConfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read MCP config file: %w", err)
	}

	// Parse JSON
	var config MCPConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse MCP config: %w", err)
	}

	// Initialize map if nil
	if config.MCPServers == nil {
		config.MCPServers = make(map[string]MCPServerConfig)
	}

	return &config, nil
}

// SaveMCPConfig saves the MCP configuration to the config directory
func SaveMCPConfig(config *MCPConfig) error {
	configDir, err := GetConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	mcpConfigPath := filepath.Join(configDir, "mcp_config.json")

	// Marshal to JSON with indentation
	data, err := json.MarshalIndent(config, "", "  ")
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
