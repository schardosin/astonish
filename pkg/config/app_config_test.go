package config

import (
	"testing"
)

func TestGetProviderType(t *testing.T) {
	tests := []struct {
		name         string
		instanceName string
		instance     ProviderConfig
		expectedType string
	}{
		{
			name:         "explicit type field takes precedence",
			instanceName: "my-openai",
			instance: ProviderConfig{
				"type":    "openai",
				"api_key": "sk-...",
			},
			expectedType: "openai",
		},
		{
			name:         "old format - instance name matches known type",
			instanceName: "openai",
			instance: ProviderConfig{
				"api_key": "sk-...",
			},
			expectedType: "openai",
		},
		{
			name:         "old format - anthropic instance name",
			instanceName: "anthropic",
			instance: ProviderConfig{
				"api_key": "sk-ant-...",
			},
			expectedType: "anthropic",
		},
		{
			name:         "old format - gemini instance name",
			instanceName: "gemini",
			instance: ProviderConfig{
				"api_key": "AIza...",
			},
			expectedType: "gemini",
		},
		{
			name:         "new format - custom instance name with type",
			instanceName: "litellm-prod",
			instance: ProviderConfig{
				"type":     "litellm",
				"api_key":  "sk-...",
				"base_url": "http://localhost:4000/v1",
			},
			expectedType: "litellm",
		},
		{
			name:         "new format - anthropic dev instance",
			instanceName: "anthropic-dev",
			instance: ProviderConfig{
				"type":    "anthropic",
				"api_key": "sk-ant-...",
			},
			expectedType: "anthropic",
		},
		{
			name:         "nil instance returns empty",
			instanceName: "openai",
			instance:     nil,
			expectedType: "",
		},
		{
			name:         "empty type and unknown instance name",
			instanceName: "my-custom-name",
			instance: ProviderConfig{
				"api_key": "sk-...",
			},
			expectedType: "",
		},
		{
			name:         "empty type field returns empty",
			instanceName: "my-openai",
			instance: ProviderConfig{
				"type":    "",
				"api_key": "sk-...",
			},
			expectedType: "",
		},
		{
			name:         "xai provider type",
			instanceName: "xai-prod",
			instance: ProviderConfig{
				"type":    "xai",
				"api_key": "xai-...",
			},
			expectedType: "xai",
		},
		{
			name:         "ollama local provider",
			instanceName: "ollama-local",
			instance: ProviderConfig{
				"type":     "ollama",
				"base_url": "http://localhost:11434",
			},
			expectedType: "ollama",
		},
		{
			name:         "sap_ai_core provider",
			instanceName: "sap-prod",
			instance: ProviderConfig{
				"type":          "sap_ai_core",
				"client_id":     "sb-xxx",
				"client_secret": "secret",
			},
			expectedType: "sap_ai_core",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetProviderType(tt.instanceName, tt.instance)
			if result != tt.expectedType {
				t.Errorf("GetProviderType(%q, %v) = %q, expected %q",
					tt.instanceName, tt.instance, result, tt.expectedType)
			}
		})
	}
}

func TestGetProviderType_KnownProviderTypes(t *testing.T) {
	knownTypes := []string{
		"anthropic", "gemini", "groq", "litellm", "lm_studio",
		"ollama", "openai", "openrouter", "poe", "sap_ai_core", "xai",
	}

	for _, providerType := range knownTypes {
		t.Run("old format "+providerType, func(t *testing.T) {
			instance := ProviderConfig{"api_key": "test-key"}
			result := GetProviderType(providerType, instance)
			if result != providerType {
				t.Errorf("GetProviderType(%q, instance) = %q, expected %q",
					providerType, result, providerType)
			}
		})
	}
}
