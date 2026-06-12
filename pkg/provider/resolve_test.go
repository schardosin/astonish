package provider

import (
	"context"
	"testing"

	"github.com/schardosin/astonish/pkg/store"
)

// mockPlatformSettings implements store.PlatformSettingsStore for testing.
type mockPlatformSettings struct {
	settings *store.PlatformSettings
	err      error
}

func (m *mockPlatformSettings) Get(_ context.Context) (*store.PlatformSettings, error) {
	return m.settings, m.err
}
func (m *mockPlatformSettings) Save(_ context.Context, _ *store.PlatformSettings) error {
	return nil
}

// mockOrgSettings implements store.OrgSettingsStore for testing.
type mockOrgSettings struct {
	settings *store.OrgSettings
	err      error
}

func (m *mockOrgSettings) Get(_ context.Context) (*store.OrgSettings, error) {
	return m.settings, m.err
}
func (m *mockOrgSettings) Save(_ context.Context, _ *store.OrgSettings) error { return nil }

// mockTeamSettings implements store.SettingsStore for testing.
type mockTeamSettings struct {
	settings *store.TeamSettings
	err      error
}

func (m *mockTeamSettings) Get(_ context.Context) (*store.TeamSettings, error) {
	return m.settings, m.err
}
func (m *mockTeamSettings) Save(_ context.Context, _ *store.TeamSettings) error { return nil }

func TestResolveEffectiveConfig_AllThreeLayers(t *testing.T) {
	ctx := context.Background()

	platform := &mockPlatformSettings{settings: &store.PlatformSettings{
		DefaultProvider: "Bifrost",
		DefaultModel:    "gpt-4",
		Providers: map[string]store.ProviderConfig{
			"Bifrost": {"type": "openai_compat", "base_url": "https://bifrost.example.com"},
		},
	}}
	org := &mockOrgSettings{settings: &store.OrgSettings{
		DefaultProvider: "OrgProvider",
		Providers: map[string]store.ProviderConfig{
			"OrgProvider": {"type": "anthropic", "api_key": "org-key"},
		},
	}}
	team := &mockTeamSettings{settings: &store.TeamSettings{
		DefaultProvider: "SAP AI Core",
		DefaultModel:    "sapaicore/claude-opus",
		Providers: map[string]store.ProviderConfig{
			"SAP AI Core": {"type": "sap_ai_core", "base_url": "https://sap.example.com"},
		},
	}}

	result := ResolveEffectiveConfig(ctx, platform, org, team)

	// Team should win
	if result.General.DefaultProvider != "SAP AI Core" {
		t.Errorf("expected DefaultProvider='SAP AI Core', got %q", result.General.DefaultProvider)
	}
	if result.General.DefaultModel != "sapaicore/claude-opus" {
		t.Errorf("expected DefaultModel='sapaicore/claude-opus', got %q", result.General.DefaultModel)
	}

	// All providers should be merged (additive by name)
	if len(result.Providers) != 3 {
		t.Errorf("expected 3 merged providers, got %d: %v", len(result.Providers), result.Providers)
	}
	if result.Providers["Bifrost"]["base_url"] != "https://bifrost.example.com" {
		t.Error("Bifrost provider config not preserved")
	}
	if result.Providers["OrgProvider"]["type"] != "anthropic" {
		t.Error("OrgProvider config not preserved")
	}
	if result.Providers["SAP AI Core"]["type"] != "sap_ai_core" {
		t.Error("SAP AI Core config not preserved")
	}
}

func TestResolveEffectiveConfig_TeamOverridesPlatform(t *testing.T) {
	ctx := context.Background()

	platform := &mockPlatformSettings{settings: &store.PlatformSettings{
		DefaultProvider: "Bifrost",
		DefaultModel:    "gpt-4",
		Providers: map[string]store.ProviderConfig{
			"Bifrost": {"type": "openai_compat", "base_url": "https://bifrost.example.com"},
		},
	}}
	team := &mockTeamSettings{settings: &store.TeamSettings{
		DefaultProvider: "SAP AI Core",
		DefaultModel:    "sapaicore/claude-opus",
		Providers: map[string]store.ProviderConfig{
			"SAP AI Core": {"type": "sap_ai_core", "base_url": "https://sap.example.com"},
		},
	}}

	// No org settings
	result := ResolveEffectiveConfig(ctx, platform, nil, team)

	if result.General.DefaultProvider != "SAP AI Core" {
		t.Errorf("expected team provider to override platform, got %q", result.General.DefaultProvider)
	}
}

func TestResolveEffectiveConfig_NilStores(t *testing.T) {
	ctx := context.Background()

	result := ResolveEffectiveConfig(ctx, nil, nil, nil)

	if result.General.DefaultProvider != "" {
		t.Errorf("expected empty provider with nil stores, got %q", result.General.DefaultProvider)
	}
	if result.Providers != nil {
		t.Errorf("expected nil providers with nil stores, got %v", result.Providers)
	}
}

func TestResolveEffectiveConfig_OnlyPlatform(t *testing.T) {
	ctx := context.Background()

	platform := &mockPlatformSettings{settings: &store.PlatformSettings{
		DefaultProvider: "Bifrost",
		DefaultModel:    "gpt-4",
		Providers: map[string]store.ProviderConfig{
			"Bifrost": {"type": "openai_compat"},
		},
	}}

	result := ResolveEffectiveConfig(ctx, platform, nil, nil)

	if result.General.DefaultProvider != "Bifrost" {
		t.Errorf("expected 'Bifrost', got %q", result.General.DefaultProvider)
	}
	if result.General.DefaultModel != "gpt-4" {
		t.Errorf("expected 'gpt-4', got %q", result.General.DefaultModel)
	}
}

func TestResolveEffectiveConfig_OrgOverridesPlatform(t *testing.T) {
	ctx := context.Background()

	platform := &mockPlatformSettings{settings: &store.PlatformSettings{
		DefaultProvider: "Bifrost",
		DefaultModel:    "gpt-4",
		Providers: map[string]store.ProviderConfig{
			"Bifrost": {"type": "openai_compat"},
		},
	}}
	org := &mockOrgSettings{settings: &store.OrgSettings{
		DefaultProvider: "Anthropic",
		// No model override — should keep platform model
		Providers: map[string]store.ProviderConfig{
			"Anthropic": {"type": "anthropic"},
		},
	}}

	result := ResolveEffectiveConfig(ctx, platform, org, nil)

	if result.General.DefaultProvider != "Anthropic" {
		t.Errorf("expected org provider to override platform, got %q", result.General.DefaultProvider)
	}
	// Model should remain from platform since org didn't override it
	if result.General.DefaultModel != "gpt-4" {
		t.Errorf("expected platform model to persist, got %q", result.General.DefaultModel)
	}
}

func TestResolveEffectiveConfig_TeamModelOnly(t *testing.T) {
	ctx := context.Background()

	platform := &mockPlatformSettings{settings: &store.PlatformSettings{
		DefaultProvider: "Bifrost",
		DefaultModel:    "gpt-4",
		Providers: map[string]store.ProviderConfig{
			"Bifrost": {"type": "openai_compat"},
		},
	}}
	// Team only overrides model, not provider
	team := &mockTeamSettings{settings: &store.TeamSettings{
		DefaultModel: "gpt-4-turbo",
	}}

	result := ResolveEffectiveConfig(ctx, platform, nil, team)

	if result.General.DefaultProvider != "Bifrost" {
		t.Errorf("expected platform provider since team didn't override it, got %q", result.General.DefaultProvider)
	}
	if result.General.DefaultModel != "gpt-4-turbo" {
		t.Errorf("expected team model override, got %q", result.General.DefaultModel)
	}
}
