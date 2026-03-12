package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateOpenCodeConfig_Anthropic(t *testing.T) {
	appCfg := &AppConfig{
		General: GeneralConfig{
			DefaultProvider: "anthropic",
			DefaultModel:    "claude-4.6-opus",
		},
		Providers: map[string]ProviderConfig{
			"anthropic": {"api_key": "sk-ant-test123"},
		},
	}

	tmpDir := t.TempDir()
	origConfigDir := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer func() {
		if origConfigDir != "" {
			os.Setenv("XDG_CONFIG_HOME", origConfigDir)
		} else {
			os.Unsetenv("XDG_CONFIG_HOME")
		}
	}()

	// Create the astonish dir
	os.MkdirAll(filepath.Join(tmpDir, "astonish"), 0755)

	result, err := GenerateOpenCodeConfig(appCfg, nil)
	if err != nil {
		t.Fatalf("GenerateOpenCodeConfig() error = %v", err)
		return
	}

	if result.ProviderID != "anthropic" {
		t.Errorf("ProviderID = %q, want %q", result.ProviderID, "anthropic")
	}
	if result.ModelID != "claude-4.6-opus" {
		t.Errorf("ModelID = %q, want %q", result.ModelID, "claude-4.6-opus")
	}
	if result.FullModelID() != "anthropic/claude-4.6-opus" {
		t.Errorf("FullModelID() = %q, want %q", result.FullModelID(), "anthropic/claude-4.6-opus")
	}

	// Verify the file was written and is valid JSON
	data, err := os.ReadFile(result.ConfigPath)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
		return
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Config file is not valid JSON: %v", err)
		return
	}

	// Check enabled_providers
	enabled, ok := parsed["enabled_providers"].([]any)
	if !ok {
		t.Fatal("enabled_providers not found or wrong type")
		return
	}
	if len(enabled) != 1 || enabled[0] != "anthropic" {
		t.Errorf("enabled_providers = %v, want [\"anthropic\"]", enabled)
	}

	// Native provider should not need extra env
	if len(result.ExtraEnv) != 0 {
		t.Errorf("ExtraEnv = %v, want empty", result.ExtraEnv)
	}
}

func TestGenerateOpenCodeConfig_OpenAI(t *testing.T) {
	appCfg := &AppConfig{
		General: GeneralConfig{
			DefaultProvider: "openai",
			DefaultModel:    "gpt-5",
		},
		Providers: map[string]ProviderConfig{
			"openai": {"api_key": "sk-test123"},
		},
	}

	tmpDir := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Unsetenv("XDG_CONFIG_HOME")
	os.MkdirAll(filepath.Join(tmpDir, "astonish"), 0755)

	result, err := GenerateOpenCodeConfig(appCfg, nil)
	if err != nil {
		t.Fatalf("GenerateOpenCodeConfig() error = %v", err)
		return
	}

	if result.ProviderID != "openai" {
		t.Errorf("ProviderID = %q, want %q", result.ProviderID, "openai")
	}
	if result.ModelID != "gpt-5" {
		t.Errorf("ModelID = %q, want %q", result.ModelID, "gpt-5")
	}
}

func TestGenerateOpenCodeConfig_Gemini(t *testing.T) {
	appCfg := &AppConfig{
		General: GeneralConfig{
			DefaultProvider: "gemini",
			DefaultModel:    "gemini-3-pro",
		},
		Providers: map[string]ProviderConfig{
			"gemini": {"api_key": "AIza-test"},
		},
	}

	tmpDir := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Unsetenv("XDG_CONFIG_HOME")
	os.MkdirAll(filepath.Join(tmpDir, "astonish"), 0755)

	result, err := GenerateOpenCodeConfig(appCfg, nil)
	if err != nil {
		t.Fatalf("GenerateOpenCodeConfig() error = %v", err)
		return
	}

	if result.ProviderID != "google" {
		t.Errorf("ProviderID = %q, want %q", result.ProviderID, "google")
	}
}

func TestGenerateOpenCodeConfig_OpenAICompat(t *testing.T) {
	appCfg := &AppConfig{
		General: GeneralConfig{
			DefaultProvider: "my-bifrost",
			DefaultModel:    "sapaicore/anthropic--claude-4.6-opus",
		},
		Providers: map[string]ProviderConfig{
			"my-bifrost": {
				"type":     "openai_compat",
				"api_key":  "sk-bf-test",
				"base_url": "https://bifrost.example.com/v1",
			},
		},
	}

	tmpDir := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Unsetenv("XDG_CONFIG_HOME")
	os.MkdirAll(filepath.Join(tmpDir, "astonish"), 0755)

	result, err := GenerateOpenCodeConfig(appCfg, nil)
	if err != nil {
		t.Fatalf("GenerateOpenCodeConfig() error = %v", err)
		return
	}

	if result.ProviderID != "astonish" {
		t.Errorf("ProviderID = %q, want %q", result.ProviderID, "astonish")
	}
	if result.ModelID != "sapaicore/anthropic--claude-4.6-opus" {
		t.Errorf("ModelID = %q, want %q", result.ModelID, "sapaicore/anthropic--claude-4.6-opus")
	}

	// Should have ASTONISH_OC_API_KEY in extra env
	if result.ExtraEnv["ASTONISH_OC_API_KEY"] != "sk-bf-test" {
		t.Errorf("ExtraEnv[ASTONISH_OC_API_KEY] = %q, want %q", result.ExtraEnv["ASTONISH_OC_API_KEY"], "sk-bf-test")
	}

	// Verify config file has the right structure
	data, err := os.ReadFile(result.ConfigPath)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
		return
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Config file is not valid JSON: %v", err)
		return
	}

	providerMap, ok := parsed["provider"].(map[string]any)
	if !ok {
		t.Fatal("provider section not found")
		return
	}

	astonishProvider, ok := providerMap["astonish"].(map[string]any)
	if !ok {
		t.Fatal("astonish provider entry not found")
		return
	}

	if astonishProvider["npm"] != "@ai-sdk/openai-compatible" {
		t.Errorf("npm = %q, want %q", astonishProvider["npm"], "@ai-sdk/openai-compatible")
	}

	opts, ok := astonishProvider["options"].(map[string]any)
	if !ok {
		t.Fatal("options not found")
		return
	}

	// Already had /v1, should remain unchanged
	if opts["baseURL"] != "https://bifrost.example.com/v1" {
		t.Errorf("baseURL = %q, want %q", opts["baseURL"], "https://bifrost.example.com/v1")
	}
	if opts["apiKey"] != "{env:ASTONISH_OC_API_KEY}" {
		t.Errorf("apiKey = %q, want %q", opts["apiKey"], "{env:ASTONISH_OC_API_KEY}")
	}
}

func TestGenerateOpenCodeConfig_OpenAICompat_AutoAppendV1(t *testing.T) {
	// Base URL without /v1 should get it auto-appended, since the
	// @ai-sdk/openai-compatible adapter only appends /chat/completions.
	appCfg := &AppConfig{
		General: GeneralConfig{
			DefaultProvider: "my-bifrost",
			DefaultModel:    "sapaicore/anthropic--claude-4.6-opus",
		},
		Providers: map[string]ProviderConfig{
			"my-bifrost": {
				"type":     "openai_compat",
				"api_key":  "sk-bf-test",
				"base_url": "https://bifrost.example.com",
			},
		},
	}

	tmpDir := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Unsetenv("XDG_CONFIG_HOME")
	os.MkdirAll(filepath.Join(tmpDir, "astonish"), 0755)

	result, err := GenerateOpenCodeConfig(appCfg, nil)
	if err != nil {
		t.Fatalf("GenerateOpenCodeConfig() error = %v", err)
		return
	}

	data, err := os.ReadFile(result.ConfigPath)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
		return
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Config file is not valid JSON: %v", err)
		return
	}

	providerMap := parsed["provider"].(map[string]any)
	astonish := providerMap["astonish"].(map[string]any)
	opts := astonish["options"].(map[string]any)

	// Should have /v1 auto-appended
	if opts["baseURL"] != "https://bifrost.example.com/v1" {
		t.Errorf("baseURL = %q, want %q", opts["baseURL"], "https://bifrost.example.com/v1")
	}
}

func TestGenerateOpenCodeConfig_OpenAICompat_TrailingSlash(t *testing.T) {
	// Base URL with trailing slash should be trimmed before /v1 is appended
	appCfg := &AppConfig{
		General: GeneralConfig{
			DefaultProvider: "my-bifrost",
			DefaultModel:    "model-1",
		},
		Providers: map[string]ProviderConfig{
			"my-bifrost": {
				"type":     "openai_compat",
				"base_url": "https://bifrost.example.com/",
			},
		},
	}

	tmpDir := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Unsetenv("XDG_CONFIG_HOME")
	os.MkdirAll(filepath.Join(tmpDir, "astonish"), 0755)

	result, err := GenerateOpenCodeConfig(appCfg, nil)
	if err != nil {
		t.Fatalf("GenerateOpenCodeConfig() error = %v", err)
		return
	}

	data, err := os.ReadFile(result.ConfigPath)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
		return
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Config file is not valid JSON: %v", err)
		return
	}

	providerMap := parsed["provider"].(map[string]any)
	astonish := providerMap["astonish"].(map[string]any)
	opts := astonish["options"].(map[string]any)

	// Should be cleaned up: no double slash, /v1 appended
	if opts["baseURL"] != "https://bifrost.example.com/v1" {
		t.Errorf("baseURL = %q, want %q", opts["baseURL"], "https://bifrost.example.com/v1")
	}
}

func TestGenerateOpenCodeConfig_SAPAICore(t *testing.T) {
	appCfg := &AppConfig{
		General: GeneralConfig{
			DefaultProvider: "sap_ai_core",
			DefaultModel:    "anthropic--claude-sonnet-4.5",
		},
		Providers: map[string]ProviderConfig{
			"sap_ai_core": {
				"client_id":      "test-client-id",
				"client_secret":  "test-secret",
				"auth_url":       "https://auth.example.com",
				"base_url":       "https://api.ai.example.com",
				"resource_group": "my-rg",
			},
		},
	}

	tmpDir := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Unsetenv("XDG_CONFIG_HOME")
	os.MkdirAll(filepath.Join(tmpDir, "astonish"), 0755)

	result, err := GenerateOpenCodeConfig(appCfg, nil)
	if err != nil {
		t.Fatalf("GenerateOpenCodeConfig() error = %v", err)
		return
	}

	if result.ProviderID != "sap-ai-core" {
		t.Errorf("ProviderID = %q, want %q", result.ProviderID, "sap-ai-core")
	}

	// Should have AICORE_SERVICE_KEY in extra env
	serviceKey := result.ExtraEnv["AICORE_SERVICE_KEY"]
	if serviceKey == "" {
		t.Fatal("AICORE_SERVICE_KEY not set in ExtraEnv")
		return
	}

	// Parse the service key JSON
	var sk map[string]any
	if err := json.Unmarshal([]byte(serviceKey), &sk); err != nil {
		t.Fatalf("AICORE_SERVICE_KEY is not valid JSON: %v", err)
		return
	}

	if sk["clientid"] != "test-client-id" {
		t.Errorf("clientid = %q, want %q", sk["clientid"], "test-client-id")
	}
	if sk["clientsecret"] != "test-secret" {
		t.Errorf("clientsecret = %q, want %q", sk["clientsecret"], "test-secret")
	}
	if sk["url"] != "https://auth.example.com" {
		t.Errorf("url = %q, want %q", sk["url"], "https://auth.example.com")
	}

	serviceURLs, ok := sk["serviceurls"].(map[string]any)
	if !ok {
		t.Fatal("serviceurls not found")
		return
	}
	if serviceURLs["AI_API_URL"] != "https://api.ai.example.com" {
		t.Errorf("AI_API_URL = %q, want %q", serviceURLs["AI_API_URL"], "https://api.ai.example.com")
	}

	// Should also have resource group
	if result.ExtraEnv["AICORE_RESOURCE_GROUP"] != "my-rg" {
		t.Errorf("AICORE_RESOURCE_GROUP = %q, want %q", result.ExtraEnv["AICORE_RESOURCE_GROUP"], "my-rg")
	}
}

func TestGenerateOpenCodeConfig_OpenRouter(t *testing.T) {
	appCfg := &AppConfig{
		General: GeneralConfig{
			DefaultProvider: "openrouter",
			DefaultModel:    "anthropic/claude-4.5-sonnet",
		},
		Providers: map[string]ProviderConfig{
			"openrouter": {"api_key": "sk-or-test"},
		},
	}

	tmpDir := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Unsetenv("XDG_CONFIG_HOME")
	os.MkdirAll(filepath.Join(tmpDir, "astonish"), 0755)

	result, err := GenerateOpenCodeConfig(appCfg, nil)
	if err != nil {
		t.Fatalf("GenerateOpenCodeConfig() error = %v", err)
		return
	}

	// OpenRouter is not a native provider, should use "astonish"
	if result.ProviderID != "astonish" {
		t.Errorf("ProviderID = %q, want %q", result.ProviderID, "astonish")
	}

	// Verify config file has the right base URL
	data, err := os.ReadFile(result.ConfigPath)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
		return
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Config file is not valid JSON: %v", err)
		return
	}

	providerMap := parsed["provider"].(map[string]any)
	astonish := providerMap["astonish"].(map[string]any)
	opts := astonish["options"].(map[string]any)

	if opts["baseURL"] != "https://openrouter.ai/api/v1" {
		t.Errorf("baseURL = %q, want %q", opts["baseURL"], "https://openrouter.ai/api/v1")
	}
}

func TestGenerateOpenCodeConfig_Ollama(t *testing.T) {
	appCfg := &AppConfig{
		General: GeneralConfig{
			DefaultProvider: "ollama",
			DefaultModel:    "llama3",
		},
		Providers: map[string]ProviderConfig{
			"ollama": {"base_url": "http://localhost:11434"},
		},
	}

	tmpDir := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Unsetenv("XDG_CONFIG_HOME")
	os.MkdirAll(filepath.Join(tmpDir, "astonish"), 0755)

	result, err := GenerateOpenCodeConfig(appCfg, nil)
	if err != nil {
		t.Fatalf("GenerateOpenCodeConfig() error = %v", err)
		return
	}

	if result.ProviderID != "astonish" {
		t.Errorf("ProviderID = %q, want %q", result.ProviderID, "astonish")
	}

	// Ollama should NOT have an API key in extra env
	if _, hasKey := result.ExtraEnv["ASTONISH_OC_API_KEY"]; hasKey {
		t.Error("Ollama should not have ASTONISH_OC_API_KEY")
	}
}

func TestGenerateOpenCodeConfig_ModelOverride(t *testing.T) {
	appCfg := &AppConfig{
		General: GeneralConfig{
			DefaultProvider: "anthropic",
			DefaultModel:    "claude-4.6-opus",
		},
		Providers: map[string]ProviderConfig{
			"anthropic": {"api_key": "sk-ant-test"},
		},
		OpenCode: OpenCodeConfig{
			Model: "claude-4.5-sonnet",
		},
	}

	tmpDir := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Unsetenv("XDG_CONFIG_HOME")
	os.MkdirAll(filepath.Join(tmpDir, "astonish"), 0755)

	result, err := GenerateOpenCodeConfig(appCfg, nil)
	if err != nil {
		t.Fatalf("GenerateOpenCodeConfig() error = %v", err)
		return
	}

	// Should use the override model, not the default
	if result.ModelID != "claude-4.5-sonnet" {
		t.Errorf("ModelID = %q, want %q", result.ModelID, "claude-4.5-sonnet")
	}
}

func TestGenerateOpenCodeConfig_CredentialStore(t *testing.T) {
	appCfg := &AppConfig{
		General: GeneralConfig{
			DefaultProvider: "anthropic",
			DefaultModel:    "claude-4.6-opus",
		},
		Providers: map[string]ProviderConfig{
			"anthropic": {}, // api_key scrubbed from config
		},
	}

	// Mock credential store that returns the API key
	getSecret := func(key string) string {
		if key == "provider.anthropic.api_key" {
			return "sk-ant-from-store"
		}
		return ""
	}

	tmpDir := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Unsetenv("XDG_CONFIG_HOME")
	os.MkdirAll(filepath.Join(tmpDir, "astonish"), 0755)

	result, err := GenerateOpenCodeConfig(appCfg, getSecret)
	if err != nil {
		t.Fatalf("GenerateOpenCodeConfig() error = %v", err)
		return
	}

	// Native provider should work even with credential store
	if result.ProviderID != "anthropic" {
		t.Errorf("ProviderID = %q, want %q", result.ProviderID, "anthropic")
	}
}

func TestGenerateOpenCodeConfig_NoProvider(t *testing.T) {
	appCfg := &AppConfig{
		General: GeneralConfig{
			DefaultProvider: "",
		},
		Providers: map[string]ProviderConfig{},
	}

	tmpDir := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Unsetenv("XDG_CONFIG_HOME")
	os.MkdirAll(filepath.Join(tmpDir, "astonish"), 0755)

	_, err := GenerateOpenCodeConfig(appCfg, nil)
	if err == nil {
		t.Error("Expected error for missing provider, got nil")
	}
}

func TestBuildAICoreServiceKey(t *testing.T) {
	providerCfg := ProviderConfig{
		"client_id":     "cid",
		"client_secret": "csec",
		"auth_url":      "https://auth.example.com",
		"base_url":      "https://api.example.com",
	}

	result := buildAICoreServiceKey("sap_ai_core", providerCfg, nil)
	if result == "" {
		t.Fatal("buildAICoreServiceKey returned empty string")
		return
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("Not valid JSON: %v", err)
		return
	}

	if parsed["clientid"] != "cid" {
		t.Errorf("clientid = %q, want %q", parsed["clientid"], "cid")
	}
	if parsed["clientsecret"] != "csec" {
		t.Errorf("clientsecret = %q, want %q", parsed["clientsecret"], "csec")
	}
}

func TestBuildAICoreServiceKey_MissingFields(t *testing.T) {
	providerCfg := ProviderConfig{
		"client_id": "cid",
		// Missing client_secret, auth_url, base_url
	}

	result := buildAICoreServiceKey("sap_ai_core", providerCfg, nil)
	if result != "" {
		t.Errorf("Expected empty string for incomplete config, got %q", result)
	}
}
