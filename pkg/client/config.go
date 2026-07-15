// Package client provides an HTTP client for connecting to a remote Astonish
// platform server. It handles authentication (JWT tokens), automatic token
// refresh, and SSE streaming for chat and flow execution.
package client

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/SAP/astonish/pkg/config"
)

// RemoteConfig holds the connection settings for a remote Astonish server.
// Stored in ~/.config/astonish/remote.yaml.
type RemoteConfig struct {
	URL       string `yaml:"url"`
	Org       string `yaml:"org"`
	Team      string `yaml:"team"`
	UserEmail string `yaml:"user_email,omitempty"`
}

const remoteConfigFile = "remote.yaml"

// configPath returns the full path to remote.yaml.
func configPath() (string, error) {
	dir, err := config.GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, remoteConfigFile), nil
}

// LoadRemoteConfig loads the remote configuration from disk.
// Returns nil, nil if the file does not exist (meaning local/personal mode).
func LoadRemoteConfig() (*RemoteConfig, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read remote config: %w", err)
	}

	var cfg RemoteConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse remote config: %w", err)
	}
	return &cfg, nil
}

// SaveRemoteConfig writes the remote configuration to disk.
func SaveRemoteConfig(cfg *RemoteConfig) error {
	path, err := configPath()
	if err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal remote config: %w", err)
	}

	return os.WriteFile(path, data, 0600)
}

// RemoveRemoteConfig deletes the remote configuration file (logout/disconnect).
func RemoveRemoteConfig() error {
	path, err := configPath()
	if err != nil {
		return err
	}

	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove remote config: %w", err)
	}
	return nil
}

// IsRemoteMode returns true if a remote configuration exists and has a URL set.
func IsRemoteMode() bool {
	cfg, err := LoadRemoteConfig()
	return err == nil && cfg != nil && cfg.URL != ""
}
