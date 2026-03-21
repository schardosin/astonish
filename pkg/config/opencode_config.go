package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// OpenCodeConfigResult holds the generated OpenCode configuration details
// needed by the OpenCode tool at invocation time.
type OpenCodeConfigResult struct {
	// ConfigPath is the path to the generated opencode.json file.
	ConfigPath string
	// ProviderID is the OpenCode provider identifier (e.g., "anthropic", "astonish").
	ProviderID string
	// ModelID is the model identifier within the provider (e.g., "claude-4.6-opus").
	ModelID string
	// ExtraEnv contains environment variables that must be set before invoking
	// OpenCode (e.g., ASTONISH_OC_API_KEY, AICORE_SERVICE_KEY).
	ExtraEnv map[string]string
}

// FullModelID returns the provider/model string for the --model flag.
func (r *OpenCodeConfigResult) FullModelID() string {
	if r.ProviderID == "" || r.ModelID == "" {
		return ""
	}
	return r.ProviderID + "/" + r.ModelID
}

// openCodeJSON is the top-level OpenCode config structure.
type openCodeJSON struct {
	Schema            string                `json:"$schema"`
	Provider          map[string]ocProvider `json:"provider,omitempty"`
	EnabledProviders  []string              `json:"enabled_providers,omitempty"`
	DisabledProviders []string              `json:"disabled_providers,omitempty"`
	Share             string                `json:"share,omitempty"`
	Autoupdate        bool                  `json:"autoupdate"`
	Permission        map[string]string     `json:"permission,omitempty"`
}

// ocProvider is an OpenCode provider entry.
type ocProvider struct {
	ID      string             `json:"id,omitempty"`
	NPM     string             `json:"npm,omitempty"`
	Name    string             `json:"name,omitempty"`
	Options map[string]any     `json:"options,omitempty"`
	Models  map[string]ocModel `json:"models,omitempty"`
}

// ocModel is an OpenCode model entry.
type ocModel struct {
	Name  string   `json:"name,omitempty"`
	Limit *ocLimit `json:"limit,omitempty"`
}

// ocLimit defines context/output token limits.
type ocLimit struct {
	Context int `json:"context,omitempty"`
	Output  int `json:"output,omitempty"`
}

// Known base URLs for providers that use OpenAI-compatible endpoints.
var openCodeProviderBaseURLs = map[string]string{
	"openrouter": "https://openrouter.ai/api/v1",
	"groq":       "https://api.groq.com/openai/v1",
	"xai":        "https://api.x.ai/v1",
	"grok":       "https://api.x.ai/v1",
	"poe":        "https://api.poe.com/bot/v1",
	"ollama":     "http://localhost:11434/v1",
	"lm_studio":  "http://localhost:1234/v1",
}

// nativeOpenCodeProviders lists Astonish provider types that map to built-in
// OpenCode providers (no custom npm adapter needed).
var nativeOpenCodeProviders = map[string]string{
	"anthropic":   "anthropic",
	"openai":      "openai",
	"gemini":      "google",
	"sap_ai_core": "sap-ai-core",
}

// GenerateOpenCodeConfig generates an OpenCode config JSON file and returns
// the result with all information needed to invoke OpenCode.
//
// Parameters:
//   - appCfg: the current Astonish app config
//   - getSecret: resolves secrets from the credential store (may be nil)
//
// The generated config is written to ~/.config/astonish/opencode.json.
func GenerateOpenCodeConfig(appCfg *AppConfig, getSecret SecretGetter) (*OpenCodeConfigResult, error) {
	if appCfg == nil {
		return nil, fmt.Errorf("app config is nil")
	}

	providerName := appCfg.General.DefaultProvider
	if providerName == "" {
		return nil, fmt.Errorf("no default provider configured")
	}

	providerCfg, hasCfg := appCfg.Providers[providerName]
	provType := GetProviderType(providerName, providerCfg)
	if provType == "" {
		provType = providerName
	}

	// Determine the model to use
	modelName := appCfg.OpenCode.Model
	if modelName == "" {
		modelName = appCfg.General.DefaultModel
	}
	if modelName == "" {
		return nil, fmt.Errorf("no model configured")
	}

	result := &OpenCodeConfigResult{
		ExtraEnv: make(map[string]string),
	}

	ocCfg := openCodeJSON{
		Schema:     "https://opencode.ai/config.json",
		Share:      "disabled",
		Autoupdate: false,
		Permission: map[string]string{
			"edit":  "allow",
			"write": "allow",
			"bash":  "allow",
		},
	}

	// Check if this is a native OpenCode provider
	if nativeID, isNative := nativeOpenCodeProviders[provType]; isNative {
		result.ProviderID = nativeID
		result.ModelID = modelName
		ocCfg.EnabledProviders = []string{nativeID}

		if provType == "sap_ai_core" {
			// Build AICORE_SERVICE_KEY JSON blob from individual fields
			serviceKey := buildAICoreServiceKey(providerName, providerCfg, getSecret)
			if serviceKey != "" {
				result.ExtraEnv["AICORE_SERVICE_KEY"] = serviceKey
			}
			// Also forward resource group if set
			rg := resolveProviderField(providerName, "resource_group", providerCfg, getSecret)
			if rg != "" {
				result.ExtraEnv["AICORE_RESOURCE_GROUP"] = rg
			}
		}

		// For native providers, we still create a minimal provider entry
		// to set options if needed (e.g., custom base URL)
		if hasCfg {
			entry := ocProvider{}
			needsEntry := false

			if baseURL := providerCfg["base_url"]; baseURL != "" && provType != "sap_ai_core" {
				entry.Options = map[string]any{"baseURL": baseURL}
				needsEntry = true
			}

			if needsEntry {
				ocCfg.Provider = map[string]ocProvider{nativeID: entry}
			}
		}
	} else {
		// Non-native provider: use @ai-sdk/openai-compatible
		result.ProviderID = "astonish"
		result.ModelID = modelName

		// Resolve base URL
		baseURL := ""
		if hasCfg {
			baseURL = providerCfg["base_url"]
		}
		if baseURL == "" {
			if knownURL, ok := openCodeProviderBaseURLs[provType]; ok {
				baseURL = knownURL
			}
		}

		// The @ai-sdk/openai-compatible adapter appends only /chat/completions
		// to the baseURL (it does NOT add /v1). Most OpenAI-compatible endpoints
		// expect /v1/chat/completions, so append /v1 if not already present.
		// This mirrors the Go openai_compat provider behavior.
		if baseURL != "" && !strings.HasSuffix(baseURL, "/v1") {
			baseURL = strings.TrimRight(baseURL, "/") + "/v1"
		}

		// Resolve API key
		apiKey := resolveProviderField(providerName, "api_key", providerCfg, getSecret)

		// Build the provider entry
		entry := ocProvider{
			NPM:  "@ai-sdk/openai-compatible",
			Name: "Astonish",
			Models: map[string]ocModel{
				modelName: {Name: modelName},
			},
		}

		opts := map[string]any{}
		if baseURL != "" {
			opts["baseURL"] = baseURL
		}

		// For API key, we use an env var reference to avoid writing secrets to disk
		if apiKey != "" {
			result.ExtraEnv["ASTONISH_OC_API_KEY"] = apiKey
			opts["apiKey"] = "{env:ASTONISH_OC_API_KEY}"
		}

		if len(opts) > 0 {
			entry.Options = opts
		}

		ocCfg.Provider = map[string]ocProvider{"astonish": entry}
		ocCfg.EnabledProviders = []string{"astonish"}
	}

	// Write the config file
	configDir, err := GetConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get config dir: %w", err)
	}

	configPath := filepath.Join(configDir, "opencode.json")
	result.ConfigPath = configPath

	data, err := json.MarshalIndent(ocCfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal opencode config: %w", err)
	}

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config dir: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to write opencode config: %w", err)
	}

	return result, nil
}

// buildAICoreServiceKey assembles the AICORE_SERVICE_KEY JSON blob from
// individual Astonish config fields.
func buildAICoreServiceKey(instanceName string, providerCfg ProviderConfig, getSecret SecretGetter) string {
	clientID := resolveProviderField(instanceName, "client_id", providerCfg, getSecret)
	clientSecret := resolveProviderField(instanceName, "client_secret", providerCfg, getSecret)
	authURL := resolveProviderField(instanceName, "auth_url", providerCfg, getSecret)
	baseURL := resolveProviderField(instanceName, "base_url", providerCfg, getSecret)

	if clientID == "" || clientSecret == "" || authURL == "" || baseURL == "" {
		return ""
	}

	serviceKey := map[string]any{
		"clientid":     clientID,
		"clientsecret": clientSecret,
		"url":          authURL,
		"serviceurls": map[string]string{
			"AI_API_URL": baseURL,
		},
	}

	data, err := json.Marshal(serviceKey)
	if err != nil {
		return ""
	}
	return string(data)
}

// resolveProviderField resolves a provider config field, checking the
// credential store first, then the config map.
func resolveProviderField(instanceName, fieldKey string, providerCfg ProviderConfig, getSecret SecretGetter) string {
	// 1. Try credential store
	if getSecret != nil {
		storeKey := "provider." + instanceName + "." + fieldKey
		if val := getSecret(storeKey); val != "" {
			return val
		}
	}

	// 2. Try config map
	if providerCfg != nil {
		if val, ok := providerCfg[fieldKey]; ok && val != "" {
			return val
		}
	}

	// 3. Try environment variable
	provType := ""
	if providerCfg != nil {
		provType = GetProviderType(instanceName, providerCfg)
	}
	if provType == "" {
		provType = instanceName
	}
	if mapping, ok := ProviderEnvMapping[provType]; ok {
		if envVar, ok := mapping[fieldKey]; ok {
			if val := os.Getenv(envVar); val != "" {
				return val
			}
		}
	}

	return ""
}

// GetOpenCodeConfigPath returns the path to the Astonish-managed OpenCode
// config file. This can be used to check if the file exists.
func GetOpenCodeConfigPath() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "opencode.json"), nil
}
