package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAppSettingsResponse_JSONStructure(t *testing.T) {
	response := AppSettingsResponse{
		General: GeneralSettings{
			DefaultProvider:            "openai-prod",
			DefaultProviderDisplayName: "OpenAI",
			DefaultModel:               "gpt-4o",
			WebSearchTool:              "",
			WebExtractTool:             "",
		},
		Providers: []ProviderSettings{
			{
				Name:        "openai-prod",
				Type:        "openai",
				DisplayName: "OpenAI",
				Configured:  true,
				Fields: map[string]string{
					"api_key": "****",
				},
			},
		},
	}

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}

	var decoded AppSettingsResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if decoded.General.DefaultProvider != "openai-prod" {
		t.Errorf("DefaultProvider = %q, expected %q", decoded.General.DefaultProvider, "openai-prod")
	}

	if len(decoded.Providers) != 1 {
		t.Errorf("Providers length = %d, expected 1", len(decoded.Providers))
	}

	if decoded.Providers[0].Name != "openai-prod" {
		t.Errorf("Provider[0].Name = %q, expected %q", decoded.Providers[0].Name, "openai-prod")
	}
}

func TestProviderSettings_FieldsMasking(t *testing.T) {
	settings := ProviderSettings{
		Name:        "test-provider",
		Type:        "openai",
		DisplayName: "OpenAI",
		Configured:  true,
		Fields: map[string]string{
			"api_key":  "****",
			"base_url": "http://localhost:11434",
		},
	}

	if settings.Fields["api_key"] != "****" {
		t.Error("api_key should be masked")
	}

	if settings.Fields["base_url"] != "http://localhost:11434" {
		t.Error("base_url should not be masked")
	}
}

func TestUpdateAppSettingsRequest_ProvidersMap(t *testing.T) {
	request := UpdateAppSettingsRequest{
		General: &GeneralSettings{
			DefaultProvider: "openai-prod",
			DefaultModel:    "gpt-4o",
		},
		Providers: map[string]map[string]string{
			"openai-prod": {
				"type":    "openai",
				"api_key": "sk-...",
			},
			"anthropic-dev": {
				"type":    "anthropic",
				"api_key": "sk-ant-...",
			},
		},
	}

	data, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	var decoded UpdateAppSettingsRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	if decoded.General.DefaultProvider != "openai-prod" {
		t.Errorf("DefaultProvider = %q, expected %q", decoded.General.DefaultProvider, "openai-prod")
	}

	if len(decoded.Providers) != 2 {
		t.Errorf("Providers length = %d, expected 2", len(decoded.Providers))
	}

	if decoded.Providers["openai-prod"]["type"] != "openai" {
		t.Errorf("Providers[openai-prod][type] = %q, expected %q", decoded.Providers["openai-prod"]["type"], "openai")
	}
}

func TestProviderSettings_EmptyFields(t *testing.T) {
	settings := ProviderSettings{
		Name:        "empty-provider",
		Type:        "openai",
		DisplayName: "OpenAI",
		Configured:  false,
		Fields:      map[string]string{},
	}

	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("failed to marshal settings: %v", err)
	}

	if !strings.Contains(string(data), `"configured":false`) {
		t.Error("response should contain configured: false")
	}
}

func TestGeneralSettings_AllFields(t *testing.T) {
	settings := GeneralSettings{
		DefaultProvider:            "litellm-prod",
		DefaultProviderDisplayName: "LiteLLM",
		DefaultModel:               "gpt-4",
		WebSearchTool:              "brave",
		WebExtractTool:             "jina",
	}

	if settings.DefaultProvider != "litellm-prod" {
		t.Errorf("DefaultProvider = %q, expected %q", settings.DefaultProvider, "litellm-prod")
	}

	if settings.WebSearchTool != "brave" {
		t.Errorf("WebSearchTool = %q, expected %q", settings.WebSearchTool, "brave")
	}

	if settings.WebExtractTool != "jina" {
		t.Errorf("WebExtractTool = %q, expected %q", settings.WebExtractTool, "jina")
	}
}

func TestUpdateAppSettingsRequest_ReplaceAll(t *testing.T) {
	newProvidersJSON := json.RawMessage(`[{"name":"new-provider","type":"openai","api_key":"sk-..."}]`)

	request := UpdateAppSettingsRequest{
		Providers: map[string]map[string]string{
			"__replace_all__": {
				"__array__": string(newProvidersJSON),
			},
		},
	}

	data, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	providers, ok := decoded["providers"].(map[string]interface{})
	if !ok {
		t.Fatal("providers should be a map")
	}

	replaceAll, ok := providers["__replace_all__"].(map[string]interface{})
	if !ok {
		t.Fatal("__replace_all__ should be a map")
	}

	arrayStr, ok := replaceAll["__array__"].(string)
	if !ok {
		t.Fatal("__array__ should be a string")
	}

	var providersArray []map[string]interface{}
	if err := json.Unmarshal([]byte(arrayStr), &providersArray); err != nil {
		t.Fatalf("failed to unmarshal providers array: %v", err)
	}

	if len(providersArray) != 1 {
		t.Errorf("providers array length = %d, expected 1", len(providersArray))
	}
}

func TestAppSettingsResponse_MultipleProviders(t *testing.T) {
	response := AppSettingsResponse{
		General: GeneralSettings{
			DefaultProvider: "openai-prod",
		},
		Providers: []ProviderSettings{
			{Name: "openai-prod", Type: "openai", DisplayName: "OpenAI", Configured: true, Fields: map[string]string{"api_key": "****"}},
			{Name: "anthropic-dev", Type: "anthropic", DisplayName: "Anthropic", Configured: true, Fields: map[string]string{"api_key": "****"}},
			{Name: "litellm-prod", Type: "litellm", DisplayName: "LiteLLM", Configured: true, Fields: map[string]string{"api_key": "****", "base_url": "http://localhost:4000"}},
		},
	}

	if len(response.Providers) != 3 {
		t.Errorf("Providers length = %d, expected 3", len(response.Providers))
	}

	providerNames := make(map[string]bool)
	for _, p := range response.Providers {
		if providerNames[p.Name] {
			t.Errorf("duplicate provider name: %s", p.Name)
		}
		providerNames[p.Name] = true
	}
}

func TestAppSettingsResponse_ProviderTypes(t *testing.T) {
	tests := []struct {
		name       string
		provider   ProviderSettings
		expectType string
		expectName string
	}{
		{
			name:       "openai provider",
			provider:   ProviderSettings{Name: "openai-prod", Type: "openai", DisplayName: "OpenAI", Configured: true, Fields: map[string]string{"api_key": "****"}},
			expectType: "openai",
			expectName: "openai-prod",
		},
		{
			name:       "anthropic provider",
			provider:   ProviderSettings{Name: "anthropic-dev", Type: "anthropic", DisplayName: "Anthropic", Configured: true, Fields: map[string]string{"api_key": "****"}},
			expectType: "anthropic",
			expectName: "anthropic-dev",
		},
		{
			name:       "litellm with base_url",
			provider:   ProviderSettings{Name: "litellm-prod", Type: "litellm", DisplayName: "LiteLLM", Configured: true, Fields: map[string]string{"api_key": "****", "base_url": "http://localhost:4000"}},
			expectType: "litellm",
			expectName: "litellm-prod",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.provider.Type != tt.expectType {
				t.Errorf("Type = %q, expected %q", tt.provider.Type, tt.expectType)
			}
			if tt.provider.Name != tt.expectName {
				t.Errorf("Name = %q, expected %q", tt.provider.Name, tt.expectName)
			}
		})
	}
}

func TestUpdateAppSettingsRequest_MultipleProviderUpdates(t *testing.T) {
	request := UpdateAppSettingsRequest{
		Providers: map[string]map[string]string{
			"openai-prod": {
				"type":    "openai",
				"api_key": "sk-prod-123",
			},
			"openai-dev": {
				"type":    "openai",
				"api_key": "sk-dev-456",
			},
			"anthropic-qa": {
				"type":    "anthropic",
				"api_key": "sk-ant-qa",
			},
		},
	}

	if len(request.Providers) != 3 {
		t.Errorf("Providers length = %d, expected 3", len(request.Providers))
	}

	expectedKeys := []string{"openai-prod", "openai-dev", "anthropic-qa"}
	for _, key := range expectedKeys {
		if _, ok := request.Providers[key]; !ok {
			t.Errorf("expected provider key %q not found", key)
		}
	}
}

func TestGeneralSettings_DefaultProviderDisplayName(t *testing.T) {
	tests := []struct {
		name        string
		provider    string
		displayName string
	}{
		{"openai provider", "openai-prod", "OpenAI"},
		{"anthropic provider", "anthropic-dev", "Anthropic"},
		{"litellm provider", "litellm-prod", "LiteLLM"},
		{"gemini provider", "gemini-prod", "Google GenAI"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := GeneralSettings{
				DefaultProvider:            tt.provider,
				DefaultProviderDisplayName: tt.displayName,
			}
			if settings.DefaultProviderDisplayName != tt.displayName {
				t.Errorf("DefaultProviderDisplayName = %q, expected %q", settings.DefaultProviderDisplayName, tt.displayName)
			}
		})
	}
}
