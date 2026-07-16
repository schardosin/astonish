package provider

import (
	"context"
	"testing"

	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/store"
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

// -----------------------------------------------------------------------------
// TDD RED-state tests for ApplyUserDefault + ApplyProviderOverride overlays.
//
// These tests specify the two new overlay helpers BEFORE the implementation
// exists. `go test` will fail with "undefined: ApplyUserDefault" and
// "undefined: ApplyProviderOverride" until Todo 2 lands.
//
// Semantics (mirroring applyProviderLayer at pkg/provider/resolve.go:62):
//   - Empty-string defaults are no-ops (inherit from cascade below).
//   - Non-empty defaults override cfg.General.DefaultProvider / DefaultModel.
//   - Neither overlay touches the additive cfg.Providers map — overrides are
//     default-only, not provider-config-only. Providers from lower layers
//     survive verbatim.
//   - Both overlays return the same *config.AppConfig pointer they receive
//     (mutate-in-place; return-self for chainable call sites).
//   - Nil / zero inputs are safe no-ops.
//   - Idempotent: applying the same overlay twice equals applying it once.
// -----------------------------------------------------------------------------

// fakePersonalSettings is the test's inline stand-in for the eventual
// store.PersonalSettings type (added in Todo 3). It satisfies whatever
// contract ApplyUserDefault demands — Todo 2 chooses the concrete interface
// or struct shape and this fake conforms. Two accessor methods keep the
// door open for interface-based dispatch without requiring the test to
// import the store package (per Todo 1 MUST-NOT constraint).
type fakePersonalSettings struct {
	DefaultProvider string
	DefaultModel    string
}

func (f *fakePersonalSettings) GetDefaultProvider() string {
	if f == nil {
		return ""
	}
	return f.DefaultProvider
}

func (f *fakePersonalSettings) GetDefaultModel() string {
	if f == nil {
		return ""
	}
	return f.DefaultModel
}

func TestApplyUserDefault(t *testing.T) {
	tests := []struct {
		name             string
		cfg              *config.AppConfig
		userSettings     *fakePersonalSettings
		wantProvider     string
		wantModel        string
		wantProvidersLen int
	}{
		{
			name:             "nil userSettings is a safe no-op",
			cfg:              &config.AppConfig{General: config.GeneralConfig{DefaultProvider: "TeamProv", DefaultModel: "team-model"}},
			userSettings:     nil,
			wantProvider:     "TeamProv",
			wantModel:        "team-model",
			wantProvidersLen: 0,
		},
		{
			name:             "empty userSettings leaves cfg unchanged",
			cfg:              &config.AppConfig{General: config.GeneralConfig{DefaultProvider: "TeamProv", DefaultModel: "team-model"}},
			userSettings:     &fakePersonalSettings{},
			wantProvider:     "TeamProv",
			wantModel:        "team-model",
			wantProvidersLen: 0,
		},
		{
			name:             "user default overrides team default (both fields)",
			cfg:              &config.AppConfig{General: config.GeneralConfig{DefaultProvider: "TeamProv", DefaultModel: "team-model"}},
			userSettings:     &fakePersonalSettings{DefaultProvider: "UserProv", DefaultModel: "user-model"},
			wantProvider:     "UserProv",
			wantModel:        "user-model",
			wantProvidersLen: 0,
		},
		{
			name: "partial override: provider set, model empty inherits cascade model",
			cfg: &config.AppConfig{
				General: config.GeneralConfig{DefaultProvider: "TeamProv", DefaultModel: "team-model"},
			},
			userSettings:     &fakePersonalSettings{DefaultProvider: "UserProv"},
			wantProvider:     "UserProv",
			wantModel:        "team-model",
			wantProvidersLen: 0,
		},
		{
			name: "partial override: model set, provider empty inherits cascade provider",
			cfg: &config.AppConfig{
				General: config.GeneralConfig{DefaultProvider: "TeamProv", DefaultModel: "team-model"},
			},
			userSettings:     &fakePersonalSettings{DefaultModel: "user-model"},
			wantProvider:     "TeamProv",
			wantModel:        "user-model",
			wantProvidersLen: 0,
		},
		{
			name: "providers map from lower layers survives (additive, not replaced)",
			cfg: &config.AppConfig{
				General: config.GeneralConfig{DefaultProvider: "TeamProv", DefaultModel: "team-model"},
				Providers: map[string]config.ProviderConfig{
					"TeamProv":     {"type": "openai_compat", "base_url": "https://team.example.com"},
					"PlatformProv": {"type": "anthropic"},
				},
			},
			userSettings:     &fakePersonalSettings{DefaultProvider: "TeamProv", DefaultModel: "user-model"},
			wantProvider:     "TeamProv",
			wantModel:        "user-model",
			wantProvidersLen: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ApplyUserDefault(tc.cfg, tc.userSettings)

			if result != tc.cfg {
				t.Errorf("expected ApplyUserDefault to return same *AppConfig pointer (mutate-in-place)")
			}
			if result.General.DefaultProvider != tc.wantProvider {
				t.Errorf("DefaultProvider: got %q, want %q", result.General.DefaultProvider, tc.wantProvider)
			}
			if result.General.DefaultModel != tc.wantModel {
				t.Errorf("DefaultModel: got %q, want %q", result.General.DefaultModel, tc.wantModel)
			}
			if len(result.Providers) != tc.wantProvidersLen {
				t.Errorf("Providers map size: got %d, want %d (overlays must not touch providers map)", len(result.Providers), tc.wantProvidersLen)
			}
		})
	}
}

func TestApplyUserDefault_Idempotent(t *testing.T) {
	cfg := &config.AppConfig{
		General: config.GeneralConfig{DefaultProvider: "TeamProv", DefaultModel: "team-model"},
		Providers: map[string]config.ProviderConfig{
			"TeamProv": {"type": "openai_compat"},
		},
	}
	us := &fakePersonalSettings{DefaultProvider: "UserProv", DefaultModel: "user-model"}

	first := ApplyUserDefault(cfg, us)
	firstProvider := first.General.DefaultProvider
	firstModel := first.General.DefaultModel
	firstProvidersLen := len(first.Providers)

	second := ApplyUserDefault(cfg, us)
	if second.General.DefaultProvider != firstProvider {
		t.Errorf("idempotent: DefaultProvider drifted between applications: first=%q second=%q", firstProvider, second.General.DefaultProvider)
	}
	if second.General.DefaultModel != firstModel {
		t.Errorf("idempotent: DefaultModel drifted: first=%q second=%q", firstModel, second.General.DefaultModel)
	}
	if len(second.Providers) != firstProvidersLen {
		t.Errorf("idempotent: Providers map size drifted: first=%d second=%d", firstProvidersLen, len(second.Providers))
	}
}

func TestApplyProviderOverride(t *testing.T) {
	tests := []struct {
		name             string
		cfg              *config.AppConfig
		provider         string
		model            string
		wantProvider     string
		wantModel        string
		wantProvidersLen int
	}{
		{
			name:             "empty override (both) leaves cfg unchanged (full clear = restore cascade)",
			cfg:              &config.AppConfig{General: config.GeneralConfig{DefaultProvider: "CascadeProv", DefaultModel: "cascade-model"}},
			provider:         "",
			model:            "",
			wantProvider:     "CascadeProv",
			wantModel:        "cascade-model",
			wantProvidersLen: 0,
		},
		{
			name:             "override both fields pinned",
			cfg:              &config.AppConfig{General: config.GeneralConfig{DefaultProvider: "CascadeProv", DefaultModel: "cascade-model"}},
			provider:         "PinnedProv",
			model:            "pinned-model",
			wantProvider:     "PinnedProv",
			wantModel:        "pinned-model",
			wantProvidersLen: 0,
		},
		{
			name:             "empty provider only = inherit provider (no-op for that field)",
			cfg:              &config.AppConfig{General: config.GeneralConfig{DefaultProvider: "CascadeProv", DefaultModel: "cascade-model"}},
			provider:         "",
			model:            "pinned-model",
			wantProvider:     "CascadeProv",
			wantModel:        "pinned-model",
			wantProvidersLen: 0,
		},
		{
			name:             "empty model only = inherit model (partial override, provider pinned)",
			cfg:              &config.AppConfig{General: config.GeneralConfig{DefaultProvider: "CascadeProv", DefaultModel: "cascade-model"}},
			provider:         "PinnedProv",
			model:            "",
			wantProvider:     "PinnedProv",
			wantModel:        "cascade-model",
			wantProvidersLen: 0,
		},
		{
			name: "providers map from lower layers survives (additive, not replaced)",
			cfg: &config.AppConfig{
				General: config.GeneralConfig{DefaultProvider: "CascadeProv", DefaultModel: "cascade-model"},
				Providers: map[string]config.ProviderConfig{
					"CascadeProv":  {"type": "openai_compat", "base_url": "https://cascade.example.com"},
					"PlatformProv": {"type": "anthropic"},
					"OrgProv":      {"type": "sap_ai_core"},
				},
			},
			provider:         "PinnedProv",
			model:            "pinned-model",
			wantProvider:     "PinnedProv",
			wantModel:        "pinned-model",
			wantProvidersLen: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ApplyProviderOverride(tc.cfg, tc.provider, tc.model)

			if result != tc.cfg {
				t.Errorf("expected ApplyProviderOverride to return same *AppConfig pointer (mutate-in-place)")
			}
			if result.General.DefaultProvider != tc.wantProvider {
				t.Errorf("DefaultProvider: got %q, want %q", result.General.DefaultProvider, tc.wantProvider)
			}
			if result.General.DefaultModel != tc.wantModel {
				t.Errorf("DefaultModel: got %q, want %q", result.General.DefaultModel, tc.wantModel)
			}
			if len(result.Providers) != tc.wantProvidersLen {
				t.Errorf("Providers map size: got %d, want %d (overlays must not touch providers map)", len(result.Providers), tc.wantProvidersLen)
			}
		})
	}
}

func TestApplyProviderOverride_Idempotent(t *testing.T) {
	cfg := &config.AppConfig{
		General: config.GeneralConfig{DefaultProvider: "CascadeProv", DefaultModel: "cascade-model"},
		Providers: map[string]config.ProviderConfig{
			"CascadeProv": {"type": "openai_compat"},
		},
	}

	first := ApplyProviderOverride(cfg, "PinnedProv", "pinned-model")
	firstProvider := first.General.DefaultProvider
	firstModel := first.General.DefaultModel
	firstProvidersLen := len(first.Providers)

	second := ApplyProviderOverride(cfg, "PinnedProv", "pinned-model")
	if second.General.DefaultProvider != firstProvider {
		t.Errorf("idempotent: DefaultProvider drifted: first=%q second=%q", firstProvider, second.General.DefaultProvider)
	}
	if second.General.DefaultModel != firstModel {
		t.Errorf("idempotent: DefaultModel drifted: first=%q second=%q", firstModel, second.General.DefaultModel)
	}
	if len(second.Providers) != firstProvidersLen {
		t.Errorf("idempotent: Providers map size drifted: first=%d second=%d", firstProvidersLen, len(second.Providers))
	}
}

// TestApplyUserDefault_ChainedCascadeOrder verifies the full 5-layer cascade
// composition order: Platform → Org → Team → UserDefault → ProviderOverride.
// Each subsequent layer overrides the previous when non-empty, and the
// per-session ProviderOverride is the innermost (highest-precedence) layer.
func TestApplyUserDefault_ChainedCascadeOrder(t *testing.T) {
	ctx := context.Background()

	platform := &mockPlatformSettings{settings: &store.PlatformSettings{
		DefaultProvider: "PlatformProv",
		DefaultModel:    "platform-model",
		Providers: map[string]store.ProviderConfig{
			"PlatformProv": {"type": "openai_compat"},
		},
	}}
	org := &mockOrgSettings{settings: &store.OrgSettings{
		DefaultProvider: "OrgProv",
		DefaultModel:    "org-model",
		Providers: map[string]store.ProviderConfig{
			"OrgProv": {"type": "anthropic"},
		},
	}}
	team := &mockTeamSettings{settings: &store.TeamSettings{
		DefaultProvider: "TeamProv",
		DefaultModel:    "team-model",
		Providers: map[string]store.ProviderConfig{
			"TeamProv": {"type": "sap_ai_core"},
		},
	}}

	cfg := ResolveEffectiveConfig(ctx, platform, org, team)

	cfg = ApplyUserDefault(cfg, &fakePersonalSettings{
		DefaultProvider: "UserProv",
		DefaultModel:    "user-model",
	})
	if cfg.General.DefaultProvider != "UserProv" {
		t.Errorf("after ApplyUserDefault: got provider %q, want %q", cfg.General.DefaultProvider, "UserProv")
	}
	if cfg.General.DefaultModel != "user-model" {
		t.Errorf("after ApplyUserDefault: got model %q, want %q", cfg.General.DefaultModel, "user-model")
	}

	cfg = ApplyProviderOverride(cfg, "PinnedProv", "pinned-model")
	if cfg.General.DefaultProvider != "PinnedProv" {
		t.Errorf("after ApplyProviderOverride: got provider %q, want %q", cfg.General.DefaultProvider, "PinnedProv")
	}
	if cfg.General.DefaultModel != "pinned-model" {
		t.Errorf("after ApplyProviderOverride: got model %q, want %q", cfg.General.DefaultModel, "pinned-model")
	}

	if len(cfg.Providers) != 3 {
		t.Errorf("expected 3 merged providers from lower layers, got %d: %v", len(cfg.Providers), cfg.Providers)
	}
	for _, name := range []string{"PlatformProv", "OrgProv", "TeamProv"} {
		if _, ok := cfg.Providers[name]; !ok {
			t.Errorf("provider %q from lower layer was dropped by overlays", name)
		}
	}
}

// TestApplyProviderOverride_ClearRestoresCascade verifies the "full clear"
// case: empty strings for both provider and model must restore whatever the
// cascade below already produced. This is the "unpin" contract used when a
// user clears their session/app pin.
func TestApplyProviderOverride_ClearRestoresCascade(t *testing.T) {
	ctx := context.Background()

	platform := &mockPlatformSettings{settings: &store.PlatformSettings{
		DefaultProvider: "PlatformProv",
		DefaultModel:    "platform-model",
	}}
	team := &mockTeamSettings{settings: &store.TeamSettings{
		DefaultProvider: "TeamProv",
		DefaultModel:    "team-model",
	}}

	cfg := ResolveEffectiveConfig(ctx, platform, nil, team)
	if cfg.General.DefaultProvider != "TeamProv" || cfg.General.DefaultModel != "team-model" {
		t.Fatalf("precondition: cascade must resolve to team; got provider=%q model=%q",
			cfg.General.DefaultProvider, cfg.General.DefaultModel)
	}

	cfg = ApplyProviderOverride(cfg, "", "")
	if cfg.General.DefaultProvider != "TeamProv" {
		t.Errorf("empty override must inherit cascade provider; got %q, want %q", cfg.General.DefaultProvider, "TeamProv")
	}
	if cfg.General.DefaultModel != "team-model" {
		t.Errorf("empty override must inherit cascade model; got %q, want %q", cfg.General.DefaultModel, "team-model")
	}
}
