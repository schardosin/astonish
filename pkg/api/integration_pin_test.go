package api

import (
	"context"
	"testing"

	"github.com/schardosin/astonish/pkg/provider"
	"github.com/schardosin/astonish/pkg/store"
)

// --- Model Pin + Resume + Fallback Integration Tests ---
//
// These tests exercise the FULL overlay chain end-to-end at the api→provider
// package boundary, as it appears at real call sites:
//
//     cfg := provider.ResolveEffectiveConfig(ctx, platform, org, team)
//     cfg = provider.ApplyUserDefault(cfg, personalSettings)
//     cfg = provider.ApplyProviderOverride(cfg, pinnedProvider, pinnedModel)
//
// Individual overlay functions are covered in pkg/provider/resolve_test.go;
// these tests verify the composed chain behaves as consumers rely on it.
//
// No external dependencies: no Postgres DSN, no LLM API key. Mock stores are
// shared with integration_multitenant_test.go (mockPlatformSettingsStore,
// mockOrgSettingsStore, mockTeamSettingsStore).

// fakeUserDefault implements provider.UserDefaultSettings for tests.
// Empty strings mean "inherit from the cascade below" (no override).
type fakeUserDefault struct {
	provider string
	model    string
}

func (f *fakeUserDefault) GetDefaultProvider() string { return f.provider }
func (f *fakeUserDefault) GetDefaultModel() string    { return f.model }

// TestIntegration_Pin_SetPinOverridesCascade verifies that a session pin
// (the innermost overlay layer) beats a fully-populated Platform→Org→Team
// cascade when resolving the effective model.
func TestIntegration_Pin_SetPinOverridesCascade(t *testing.T) {
	ctx := context.Background()

	platformStore := &mockPlatformSettingsStore{settings: &store.PlatformSettings{
		DefaultProvider: "Bifrost",
		DefaultModel:    "gpt-4",
	}}
	orgStore := &mockOrgSettingsStore{settings: &store.OrgSettings{
		DefaultProvider: "Anthropic",
		DefaultModel:    "claude-sonnet",
	}}
	teamStore := &mockTeamSettingsStore{settings: &store.TeamSettings{
		DefaultProvider: "SAP AI Core",
		DefaultModel:    "sapaicore/claude-opus",
	}}

	cfg := provider.ResolveEffectiveConfig(ctx, platformStore, orgStore, teamStore)
	cfg = provider.ApplyUserDefault(cfg, nil)
	cfg = provider.ApplyProviderOverride(cfg, "OpenAI", "gpt-4o-mini")

	if got, want := cfg.General.DefaultProvider, "OpenAI"; got != want {
		t.Errorf("effective provider: got %q, want %q (session pin should win)", got, want)
	}
	if got, want := cfg.General.DefaultModel, "gpt-4o-mini"; got != want {
		t.Errorf("effective model: got %q, want %q (session pin should win)", got, want)
	}
}

// TestIntegration_Pin_ClearPinFallsBackToCascade verifies that setting an
// empty-string pin (the "unpin" contract) restores the cascade default.
// Simulates the resume-after-clear code path.
func TestIntegration_Pin_ClearPinFallsBackToCascade(t *testing.T) {
	ctx := context.Background()

	platformStore := &mockPlatformSettingsStore{settings: &store.PlatformSettings{
		DefaultProvider: "Bifrost",
		DefaultModel:    "gpt-4",
	}}
	teamStore := &mockTeamSettingsStore{settings: &store.TeamSettings{
		DefaultProvider: "SAP AI Core",
		DefaultModel:    "sapaicore/claude-opus",
	}}

	// Step 1: pin the session to a specific provider/model.
	cfg := provider.ResolveEffectiveConfig(ctx, platformStore, nil, teamStore)
	cfg = provider.ApplyProviderOverride(cfg, "OpenAI", "gpt-4o")
	if cfg.General.DefaultProvider != "OpenAI" || cfg.General.DefaultModel != "gpt-4o" {
		t.Fatalf("pre-clear: expected pin to apply, got %q/%q",
			cfg.General.DefaultProvider, cfg.General.DefaultModel)
	}

	// Step 2: re-resolve (simulating a resume where pin was cleared).
	cfg2 := provider.ResolveEffectiveConfig(ctx, platformStore, nil, teamStore)
	cfg2 = provider.ApplyProviderOverride(cfg2, "", "")

	if got, want := cfg2.General.DefaultProvider, "SAP AI Core"; got != want {
		t.Errorf("after clear: provider got %q, want %q (cascade default)", got, want)
	}
	if got, want := cfg2.General.DefaultModel, "sapaicore/claude-opus"; got != want {
		t.Errorf("after clear: model got %q, want %q (cascade default)", got, want)
	}
}

// TestIntegration_Pin_AppPinAppliesLikeSessionPin verifies that an app pin
// (which flows through the same ApplyProviderOverride overlay) beats the
// cascade in the same way a session pin does. Apps and sessions share the
// same overlay function — this test locks that equivalence.
func TestIntegration_Pin_AppPinAppliesLikeSessionPin(t *testing.T) {
	ctx := context.Background()

	teamStore := &mockTeamSettingsStore{settings: &store.TeamSettings{
		DefaultProvider: "Anthropic",
		DefaultModel:    "claude-sonnet",
	}}

	// Simulate an app that was created and later assigned a pin.
	appPinnedProvider := "OpenAI"
	appPinnedModel := "gpt-4o"

	cfg := provider.ResolveEffectiveConfig(ctx, nil, nil, teamStore)
	cfg = provider.ApplyProviderOverride(cfg, appPinnedProvider, appPinnedModel)

	if got, want := cfg.General.DefaultProvider, "OpenAI"; got != want {
		t.Errorf("app pin provider: got %q, want %q", got, want)
	}
	if got, want := cfg.General.DefaultModel, "gpt-4o"; got != want {
		t.Errorf("app pin model: got %q, want %q", got, want)
	}
}

// TestIntegration_Pin_UserDefaultBeatsTeamDefault verifies that with no
// session/app pin, a user's personal default (ApplyUserDefault layer)
// overrides the team default from the cascade.
func TestIntegration_Pin_UserDefaultBeatsTeamDefault(t *testing.T) {
	ctx := context.Background()

	teamStore := &mockTeamSettingsStore{settings: &store.TeamSettings{
		DefaultProvider: "Anthropic",
		DefaultModel:    "claude-sonnet",
	}}

	userDefault := &fakeUserDefault{
		provider: "OpenAI",
		model:    "gpt-4o",
	}

	cfg := provider.ResolveEffectiveConfig(ctx, nil, nil, teamStore)
	cfg = provider.ApplyUserDefault(cfg, userDefault)
	cfg = provider.ApplyProviderOverride(cfg, "", "") // no session pin

	if got, want := cfg.General.DefaultProvider, "OpenAI"; got != want {
		t.Errorf("provider: got %q, want %q (user default should beat team)", got, want)
	}
	if got, want := cfg.General.DefaultModel, "gpt-4o"; got != want {
		t.Errorf("model: got %q, want %q (user default should beat team)", got, want)
	}
}

// TestIntegration_Pin_ChainedCascadeEachLayerOverrides walks the full
// cascade (Platform → Org → Team → UserDefault → SessionPin) with a
// table-driven set of scenarios where each successive layer overrides the
// previous. Locks the priority order at the call-site level.
func TestIntegration_Pin_ChainedCascadeEachLayerOverrides(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		platform     *store.PlatformSettings
		org          *store.OrgSettings
		team         *store.TeamSettings
		userDefault  provider.UserDefaultSettings
		pinProvider  string
		pinModel     string
		wantProvider string
		wantModel    string
	}{
		{
			name:         "only_platform",
			platform:     &store.PlatformSettings{DefaultProvider: "Bifrost", DefaultModel: "gpt-4"},
			wantProvider: "Bifrost",
			wantModel:    "gpt-4",
		},
		{
			name:         "org_overrides_platform",
			platform:     &store.PlatformSettings{DefaultProvider: "Bifrost", DefaultModel: "gpt-4"},
			org:          &store.OrgSettings{DefaultProvider: "Anthropic", DefaultModel: "claude-sonnet"},
			wantProvider: "Anthropic",
			wantModel:    "claude-sonnet",
		},
		{
			name:         "team_overrides_org",
			platform:     &store.PlatformSettings{DefaultProvider: "Bifrost", DefaultModel: "gpt-4"},
			org:          &store.OrgSettings{DefaultProvider: "Anthropic", DefaultModel: "claude-sonnet"},
			team:         &store.TeamSettings{DefaultProvider: "SAP AI Core", DefaultModel: "sapaicore/claude-opus"},
			wantProvider: "SAP AI Core",
			wantModel:    "sapaicore/claude-opus",
		},
		{
			name:         "user_default_overrides_team",
			platform:     &store.PlatformSettings{DefaultProvider: "Bifrost", DefaultModel: "gpt-4"},
			org:          &store.OrgSettings{DefaultProvider: "Anthropic", DefaultModel: "claude-sonnet"},
			team:         &store.TeamSettings{DefaultProvider: "SAP AI Core", DefaultModel: "sapaicore/claude-opus"},
			userDefault:  &fakeUserDefault{provider: "OpenAI", model: "gpt-4o"},
			wantProvider: "OpenAI",
			wantModel:    "gpt-4o",
		},
		{
			name:         "session_pin_overrides_user_default",
			platform:     &store.PlatformSettings{DefaultProvider: "Bifrost", DefaultModel: "gpt-4"},
			org:          &store.OrgSettings{DefaultProvider: "Anthropic", DefaultModel: "claude-sonnet"},
			team:         &store.TeamSettings{DefaultProvider: "SAP AI Core", DefaultModel: "sapaicore/claude-opus"},
			userDefault:  &fakeUserDefault{provider: "OpenAI", model: "gpt-4o"},
			pinProvider:  "Groq",
			pinModel:     "llama-3.3-70b",
			wantProvider: "Groq",
			wantModel:    "llama-3.3-70b",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var platformStore store.PlatformSettingsStore
			if tc.platform != nil {
				platformStore = &mockPlatformSettingsStore{settings: tc.platform}
			}
			var orgStore store.OrgSettingsStore
			if tc.org != nil {
				orgStore = &mockOrgSettingsStore{settings: tc.org}
			}
			var teamStore store.SettingsStore
			if tc.team != nil {
				teamStore = &mockTeamSettingsStore{settings: tc.team}
			}

			cfg := provider.ResolveEffectiveConfig(ctx, platformStore, orgStore, teamStore)
			cfg = provider.ApplyUserDefault(cfg, tc.userDefault)
			cfg = provider.ApplyProviderOverride(cfg, tc.pinProvider, tc.pinModel)

			if got := cfg.General.DefaultProvider; got != tc.wantProvider {
				t.Errorf("provider: got %q, want %q", got, tc.wantProvider)
			}
			if got := cfg.General.DefaultModel; got != tc.wantModel {
				t.Errorf("model: got %q, want %q", got, tc.wantModel)
			}
		})
	}
}

// TestIntegration_Pin_MissingCredentialOverlayStillApplies verifies that
// when a session is pinned to a provider that is NOT present in the
// resolved cfg.Providers credentials map, the overlay STILL applies
// (mutates cfg.General.DefaultProvider/DefaultModel unconditionally).
//
// The "missing credential" fallback is a call-site concern (an slog.Warn +
// substitution before instantiating the LLM), NOT part of the overlay
// function. This test locks that contract so nobody "helpfully" adds a
// credential-existence check inside ApplyProviderOverride.
func TestIntegration_Pin_MissingCredentialOverlayStillApplies(t *testing.T) {
	ctx := context.Background()

	// Team configures ONLY "SAP AI Core" credentials.
	teamStore := &mockTeamSettingsStore{settings: &store.TeamSettings{
		DefaultProvider: "SAP AI Core",
		DefaultModel:    "sapaicore/claude-opus",
		Providers: map[string]store.ProviderConfig{
			"SAP AI Core": {"type": "openai_compat", "base_url": "https://sap.example.com", "api_key": "team-key"},
		},
	}}

	cfg := provider.ResolveEffectiveConfig(ctx, nil, nil, teamStore)

	// Sanity: cascade produced SAP AI Core as default with its credentials.
	if _, ok := cfg.Providers["SAP AI Core"]; !ok {
		t.Fatalf("expected 'SAP AI Core' credentials in resolved cfg.Providers, got: %v", cfg.Providers)
	}

	// Pin to a provider with NO credential entry in cfg.Providers.
	missingProvider := "OpenAI"
	missingModel := "gpt-4o"
	if _, ok := cfg.Providers[missingProvider]; ok {
		t.Fatalf("test setup broken: %q should NOT be in cfg.Providers", missingProvider)
	}

	cfg = provider.ApplyProviderOverride(cfg, missingProvider, missingModel)

	// The overlay MUST mutate General regardless of credential presence.
	if got, want := cfg.General.DefaultProvider, missingProvider; got != want {
		t.Errorf("provider: got %q, want %q (overlay must apply even without credentials)", got, want)
	}
	if got, want := cfg.General.DefaultModel, missingModel; got != want {
		t.Errorf("model: got %q, want %q (overlay must apply even without credentials)", got, want)
	}

	// And cfg.Providers must remain untouched (the overlay does not touch
	// the additive credentials map — only General).
	if _, ok := cfg.Providers[missingProvider]; ok {
		t.Errorf("cfg.Providers[%q] should NOT be auto-populated by the overlay", missingProvider)
	}
	if _, ok := cfg.Providers["SAP AI Core"]; !ok {
		t.Errorf("cfg.Providers['SAP AI Core'] must remain after overlay (additive map is untouched)")
	}
}
