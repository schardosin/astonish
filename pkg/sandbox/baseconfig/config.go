// Package baseconfig provides a declarative description of what to install
// inside the @base sandbox template. It renders a BaseConfig into an ordered
// list of shell steps suitable for K8sBackend.BuildTemplate.
//
// This package is backend-neutral — it has no dependency on Incus, K8s, or
// any database. It reuses the existing tool/browser install-command catalogs
// from pkg/sandbox.
package baseconfig

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// BaseConfig describes what should be installed in the @base template layer.
// It is persisted as JSONB in platform.sandbox_templates.base_config.
type BaseConfig struct {
	// Core: install the core tool set (git, curl, node, python, uv, docker, etc.).
	Core bool `json:"core" yaml:"core"`

	// OptionalTools: IDs from the sandbox.OptionalTools() catalog to install.
	OptionalTools []string `json:"optional_tools" yaml:"optional_tools"`

	// Browser configures browser engine installation.
	Browser BrowserConfig `json:"browser" yaml:"browser"`

	// ExtraSteps: raw shell commands appended after all standard installs.
	// Each entry is executed as `/bin/sh -c <entry>`.
	// Use sparingly — these are opaque and non-introspectable.
	ExtraSteps []string `json:"extra_steps,omitempty" yaml:"extra_steps,omitempty"`

	// Architecture: target arch for arch-aware installs (e.g., KasmVNC deb).
	// Typically "amd64" or "arm64". Mapped to Incus-style "x86_64" / "aarch64"
	// internally for BrowserContainerInstallCommands.
	Architecture string `json:"architecture" yaml:"architecture"`
}

// BrowserConfig controls browser installation in the base template.
type BrowserConfig struct {
	// Engine: "none" | "default" | "cloakbrowser".
	// "default" installs Chromium via apt; "cloakbrowser" installs the
	// CloakBrowser stealth binary + KasmVNC.
	Engine string `json:"engine" yaml:"engine"`

	// FingerprintPlatform: OS platform for CloakBrowser fingerprint.
	// "linux" | "macos" | "windows". Only relevant when Engine="cloakbrowser".
	FingerprintPlatform string `json:"fingerprint_platform,omitempty" yaml:"fingerprint_platform,omitempty"`

	// FingerprintSeed: "auto" or a fixed hex string for deterministic
	// fingerprint generation. Only relevant when Engine="cloakbrowser".
	FingerprintSeed string `json:"fingerprint_seed,omitempty" yaml:"fingerprint_seed,omitempty"`
}

// DefaultBaseConfig returns a sensible starting point for platform admins.
// Core tools + OpenCode + CloakBrowser with linux/auto fingerprint.
func DefaultBaseConfig() BaseConfig {
	return BaseConfig{
		Core:          true,
		OptionalTools: []string{"opencode"},
		Browser: BrowserConfig{
			Engine:              "cloakbrowser",
			FingerprintPlatform: "linux",
			FingerprintSeed:     "auto",
		},
		Architecture: "amd64",
	}
}

// LoadFromJSON parses a BaseConfig from JSON bytes.
func LoadFromJSON(data []byte) (*BaseConfig, error) {
	var cfg BaseConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("baseconfig: parse JSON: %w", err)
	}
	return &cfg, nil
}

// LoadFromYAML parses a BaseConfig from YAML bytes.
func LoadFromYAML(data []byte) (*BaseConfig, error) {
	var cfg BaseConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("baseconfig: parse YAML: %w", err)
	}
	return &cfg, nil
}

// ToJSON serializes the config to JSON (for persistence in base_config JSONB).
func (c *BaseConfig) ToJSON() ([]byte, error) {
	return json.Marshal(c)
}
