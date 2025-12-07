package config

import "os"

// ProviderEnvMapping maps provider config keys to environment variable names
// This is the single source of truth for how config keys map to env vars
var ProviderEnvMapping = map[string]map[string]string{
	"anthropic": {
		"api_key": "ANTHROPIC_API_KEY",
	},
	"gemini": {
		"api_key": "GOOGLE_API_KEY",
	},
	"openai": {
		"api_key": "OPENAI_API_KEY",
	},
	"sap_ai_core": {
		"client_id":      "AICORE_CLIENT_ID",
		"client_secret":  "AICORE_CLIENT_SECRET",
		"auth_url":       "AICORE_AUTH_URL",
		"base_url":       "AICORE_BASE_URL",
		"resource_group": "AICORE_RESOURCE_GROUP",
	},
	"xai": {
		"api_key": "XAI_API_KEY",
	},
	"grok": {
		"api_key": "XAI_API_KEY",
	},
	"groq": {
		"api_key": "GROQ_API_KEY",
	},
}

// SetupProviderEnv sets environment variables from config for a specific provider
func SetupProviderEnv(providerName string, providerCfg ProviderConfig) {
	if mapping, ok := ProviderEnvMapping[providerName]; ok {
		for cfgKey, envKey := range mapping {
			if val, ok := providerCfg[cfgKey]; ok && val != "" {
				os.Setenv(envKey, val)
			}
		}
	}
}

// SetupAllProviderEnv sets environment variables for all configured providers
func SetupAllProviderEnv(appCfg *AppConfig) {
	if appCfg == nil || appCfg.Providers == nil {
		return
	}
	for providerName, providerCfg := range appCfg.Providers {
		SetupProviderEnv(providerName, providerCfg)
	}
}

// SetupMCPEnv sets environment variables from MCP server configs
func SetupMCPEnv(mcpCfg *MCPConfig) {
	if mcpCfg == nil {
		return
	}
	for _, server := range mcpCfg.MCPServers {
		for k, v := range server.Env {
			if v != "" {
				os.Setenv(k, v)
			}
		}
	}
}
