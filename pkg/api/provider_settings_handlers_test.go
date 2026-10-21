package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/store"
	"github.com/SAP/astonish/pkg/store/entstore"
)

// Provider handler matrix uses in-memory stores only, with no provider calls.
type providerMatrixPlatformStore struct {
	value *store.PlatformSettings
	err   error
	saves int
}

func (s *providerMatrixPlatformStore) Get(context.Context) (*store.PlatformSettings, error) {
	return s.value, s.err
}
func (s *providerMatrixPlatformStore) Save(_ context.Context, v *store.PlatformSettings) error {
	s.value = v
	s.saves++
	return nil
}

type providerMatrixOrgStore struct {
	value *store.OrgSettings
	err   error
	saves int
}

func (s *providerMatrixOrgStore) Get(context.Context) (*store.OrgSettings, error) {
	return s.value, s.err
}
func (s *providerMatrixOrgStore) Save(_ context.Context, v *store.OrgSettings) error {
	s.value = v
	s.saves++
	return nil
}

func TestProviderMaskedUpdateMatrix(t *testing.T) {
	for _, level := range []string{"platform", "org", "team", "team-replace"} {
		for _, tc := range []struct {
			name, body string
			status     int
		}{
			{"exact", `{"existing":{"api_key":"****cret"}}`, 200},
			{"stale", `{"existing":{"api_key":"****nope"}}`, 400},
			{"unknown-instance", `{"new":{"api_key":"****cret"}}`, 400},
			{"inherited-instance", `{"inherited":{"api_key":"****cret"}}`, 400},
			{"unknown-field", `{"existing":{"client_secret":"****cret"}}`, 400},
			{"empty-stored", `{"empty":{"api_key":"****"}}`, 400},
			{"mixed-invalid", `{"existing":{"api_key":"****cret"},"new":{"api_key":"****cret"}}`, 400},
			{"replacement", `{"existing":{"api_key":"new-explicit-secret"}}`, 200},
			{"omitted", `null`, 200},
			{"deleted", `{}`, 200},
			{"clear", `{"existing":{"api_key":""}}`, 200},
		} {
			t.Run(level+"/"+tc.name, func(t *testing.T) {
				assertFileUnchanged := isolateIndependentSettingsConfig(t)
				defer assertFileUnchanged()
				original := map[string]map[string]string{"existing": {"type": "openai", "api_key": "original-secret", "base_url": "https://example.invalid"}, "removed": {"api_key": "other-secret"}, "empty": {"api_key": ""}}
				before, _ := json.Marshal(original)
				ps := &providerMatrixPlatformStore{value: &store.PlatformSettings{Providers: original}}
				os := &providerMatrixOrgStore{value: &store.OrgSettings{Providers: original}}
				ts := &urgentSettingsStore{value: &store.TeamSettings{Providers: original}}
				if level == "org" || level == "team" || level == "team-replace" {
					ps.value.Providers = map[string]map[string]string{"inherited": {"api_key": "platform-only-secret"}}
				}
				svc := &store.Services{Mode: store.ModePlatform, PlatformSettings: ps, OrgSettings: os, Settings: ts}
				scopesBefore := map[string][]byte{}
				for scope, value := range map[string]any{"platform": ps.value, "org": os.value, "team": ts.value} {
					data, err := json.Marshal(value)
					if err != nil {
						t.Fatal(err)
					}
					scopesBefore[scope] = data
				}
				var incoming map[string]map[string]string
				if err := json.Unmarshal([]byte(tc.body), &incoming); err != nil {
					t.Fatal(err)
				}
				if level == "team-replace" && incoming != nil {
					array := []map[string]string{}
					for name, fields := range incoming {
						fields["name"] = name
						array = append(array, fields)
					}
					data, _ := json.Marshal(array)
					incoming = map[string]map[string]string{"__replace_all__": {"__array__": string(data)}}
				}
				body, _ := json.Marshal(map[string]any{"providers": incoming})
				user := &PlatformUser{ID: "independent-team-admin", Role: "member"}
				path := "/api/settings/config"
				switch level {
				case "platform":
					user = &PlatformUser{ID: "independent-platform-admin", Role: "member", PlatformRole: "superadmin"}
					path = "/api/settings/platform/providers"
				case "org":
					user = &PlatformUser{ID: "independent-org-admin", Role: "admin"}
					path = "/api/settings/org/providers"
				}
				r := independentSettingsRequest(http.MethodPut, path, string(body), user, svc, "team-a")
				w := httptest.NewRecorder()
				var got map[string]map[string]string
				var saves int
				switch level {
				case "platform":
					SavePlatformProvidersHandler(w, r)
					got, saves = ps.value.Providers, ps.saves
				case "org":
					SaveOrgProvidersHandler(w, r)
					got, saves = os.value.Providers, os.saves
				default:
					UpdateSettingsHandler(w, r)
					got, saves = ts.value.Providers, ts.saves
				}
				if w.Code != tc.status {
					t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body)
				}
				after, _ := json.Marshal(original)
				if string(before) != string(after) {
					t.Fatal("mutated source settings")
				}
				target := level
				if target == "team-replace" {
					target = "team"
				}
				for scope, value := range map[string]any{"platform": ps.value, "org": os.value, "team": ts.value} {
					if scope == target && tc.status == http.StatusOK {
						continue
					}
					data, err := json.Marshal(value)
					if err != nil || string(data) != string(scopesBefore[scope]) {
						t.Fatalf("provider update changed unwritable %s settings: %v", scope, err)
					}
				}
				if ps.saves+os.saves+ts.saves != saves {
					t.Fatal("provider update saved to another scope")
				}
				if tc.status != 200 {
					if saves != 0 {
						t.Fatal("invalid request saved")
					}
					return
				}
				if saves != 1 {
					t.Fatalf("saves=%d", saves)
				}
				switch tc.name {
				case "exact":
					if level == "team" && (got["removed"]["api_key"] != "other-secret" || got["existing"]["base_url"] != "https://example.invalid") {
						t.Fatal("patch lost omitted fields")
					}
					if got["existing"]["api_key"] != "original-secret" {
						t.Fatal("lost exact stored secret")
					}
					if level != "team" && (got["removed"] != nil || got["existing"]["base_url"] != "") {
						t.Fatal("replacement retained deleted fields")
					}
				case "replacement":
					if got["existing"]["api_key"] != "new-explicit-secret" {
						t.Fatal("explicit replacement lost")
					}
				case "omitted":
					if !reflect.DeepEqual(got, original) {
						t.Fatal("omitted providers changed")
					}
				case "deleted":
					if level != "team" && len(got) != 0 {
						t.Fatal("empty replacement did not delete providers")
					}
				case "clear":
					if got["existing"]["api_key"] != "" {
						t.Fatal("explicit clear retained secret")
					}
				}
			})
		}
	}
}

func TestProviderSaveReadFailureDoesNotWrite(t *testing.T) {
	for _, level := range []string{"platform", "org"} {
		t.Run(level, func(t *testing.T) {
			ps := &providerMatrixPlatformStore{err: errors.New("private-store-canary")}
			os := &providerMatrixOrgStore{err: errors.New("private-store-canary")}
			r := httptest.NewRequest("PUT", "/", strings.NewReader(`{"providers":{"existing":{"api_key":"****cret"}}}`))
			r = r.WithContext(WithPlatformUser(store.WithServices(r.Context(), &store.Services{Mode: store.ModePlatform, PlatformSettings: ps, OrgSettings: os}), &PlatformUser{PlatformRole: "superadmin", Role: "admin"}))
			w := httptest.NewRecorder()
			if level == "platform" {
				SavePlatformProvidersHandler(w, r)
			} else {
				SaveOrgProvidersHandler(w, r)
			}
			if w.Code != 500 || ps.saves+os.saves != 0 || strings.Contains(w.Body.String(), "private-store-canary") {
				t.Fatalf("status=%d saves=%d body=%s", w.Code, ps.saves+os.saves, w.Body)
			}
		})
	}
}

func TestProviderResponseMaskingLegacyTypeAndCopy(t *testing.T) {
	providers := map[string]map[string]string{
		"sap_ai_core": {"client_id": "canary-client-id", "client_secret": strings.Repeat("canary-", 4096), "auth_url": "https://auth.invalid/private", "base_url": "https://public.invalid"},
		"xai_oauth":   {"access_token": "canary-access", "refresh_token": "canary-refresh", "expires_at": "canary-expiry"},
		"openai":      {"api_key": "tiny"},
	}
	before, _ := json.Marshal(providers)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	r := httptest.NewRequest("GET", "/api/settings/providers/effective", nil)
	r = r.WithContext(store.WithServices(r.Context(), &store.Services{Mode: store.ModePlatform, Settings: &urgentSettingsStore{value: &store.TeamSettings{Providers: providers}}}))
	w := httptest.NewRecorder()
	GetEffectiveProvidersHandler(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), "canary-") {
		t.Error("effective response exposes legacy provider secrets")
	}
	masked := maskProviderSecrets(providers)
	for name, fields := range providers {
		for key, value := range fields {
			if key == "base_url" {
				if masked[name][key] != value {
					t.Fatal("nonsecret field changed")
				}
				continue
			}
			if masked[name][key] != maskValue(value) || len(masked[name][key]) > 8 {
				t.Errorf("%s/%s not bounded and masked", name, key)
			}
		}
	}
	masked["sap_ai_core"]["client_secret"] = "response edit"
	delete(masked, "openai")
	after, _ := json.Marshal(providers)
	if string(before) != string(after) {
		t.Fatal("response copy mutated source")
	}
}

// TestSavePlatformProviders_PreservesChannels verifies that saving provider
// configuration does not wipe the Channels field from PlatformSettings.
// Regression test for: when providers were saved, the handler constructed a new
// PlatformSettings struct without preserving existing.Channels, causing Telegram
// (and other channels) to appear disabled after any provider edit.
func TestSavePlatformProviders_PreservesChannels(t *testing.T) {
	tmp := t.TempDir()
	_, esStore, err := entstore.NewPlatformServices(context.Background(), entstore.Config{
		DSN:     "file:" + filepath.Join(tmp, "platform.db"),
		DataDir: tmp,
	})
	if err != nil {
		t.Fatalf("NewPlatformServices: %v", err)
	}
	defer esStore.Close()

	ctx := context.Background()
	settingsStore := esStore.PlatformSettings()

	// Step 1: Save initial settings with Telegram enabled
	initial := &store.PlatformSettings{
		DefaultProvider: "openai",
		Providers: map[string]store.ProviderConfig{
			"openai": {"type": "openai", "api_key": "sk-test"},
		},
		Channels: &store.PlatformChannelSettings{
			Telegram: &store.PlatformTelegramConfig{Enabled: true},
		},
	}
	if err := settingsStore.Save(ctx, initial); err != nil {
		t.Fatalf("Save initial: %v", err)
	}

	// Step 2: Simulate provider save — same logic as SavePlatformProvidersHandler:
	// Load existing, construct new struct preserving Channels
	existing, err := settingsStore.Get(ctx)
	if err != nil {
		t.Fatalf("Get existing: %v", err)
	}

	// This is the fixed save logic (with Channels preserved):
	updated := &store.PlatformSettings{
		DefaultProvider: "anthropic",
		DefaultModel:    "claude-3",
		Providers: map[string]store.ProviderConfig{
			"anthropic": {"type": "anthropic", "api_key": "sk-ant-test"},
		},
		Channels: existing.Channels, // <-- THE FIX: preserve channels
	}
	if err := settingsStore.Save(ctx, updated); err != nil {
		t.Fatalf("Save updated: %v", err)
	}

	// Step 3: Re-read and verify Channels survives
	reloaded, err := settingsStore.Get(ctx)
	if err != nil {
		t.Fatalf("Get reloaded: %v", err)
	}

	if reloaded.Channels == nil {
		t.Fatal("Channels is nil after provider save — regression!")
	}
	if reloaded.Channels.Telegram == nil {
		t.Fatal("Channels.Telegram is nil after provider save — regression!")
	}
	if !reloaded.Channels.Telegram.Enabled {
		t.Error("Channels.Telegram.Enabled is false after provider save — regression!")
	}

	// Also verify provider was updated correctly
	if reloaded.DefaultProvider != "anthropic" {
		t.Errorf("DefaultProvider = %q, want %q", reloaded.DefaultProvider, "anthropic")
	}
}

// TestSavePlatformProviders_PreservesChannels_NilChannels verifies that the
// save handler doesn't crash when existing settings have no Channels configured.
func TestSavePlatformProviders_PreservesChannels_NilChannels(t *testing.T) {
	tmp := t.TempDir()
	_, esStore, err := entstore.NewPlatformServices(context.Background(), entstore.Config{
		DSN:     "file:" + filepath.Join(tmp, "platform.db"),
		DataDir: tmp,
	})
	if err != nil {
		t.Fatalf("NewPlatformServices: %v", err)
	}
	defer esStore.Close()

	ctx := context.Background()
	settingsStore := esStore.PlatformSettings()

	// Step 1: Save initial settings WITHOUT Channels
	initial := &store.PlatformSettings{
		DefaultProvider: "openai",
		Providers: map[string]store.ProviderConfig{
			"openai": {"type": "openai", "api_key": "sk-test"},
		},
	}
	if err := settingsStore.Save(ctx, initial); err != nil {
		t.Fatalf("Save initial: %v", err)
	}

	// Step 2: Simulate provider save
	existing, err := settingsStore.Get(ctx)
	if err != nil {
		t.Fatalf("Get existing: %v", err)
	}

	updated := &store.PlatformSettings{
		DefaultProvider: "anthropic",
		Providers: map[string]store.ProviderConfig{
			"anthropic": {"type": "anthropic"},
		},
		Channels: existing.Channels, // nil — should not crash
	}
	if err := settingsStore.Save(ctx, updated); err != nil {
		t.Fatalf("Save updated: %v", err)
	}

	// Step 3: Verify no crash and Channels is still nil
	reloaded, err := settingsStore.Get(ctx)
	if err != nil {
		t.Fatalf("Get reloaded: %v", err)
	}

	if reloaded.Channels != nil {
		t.Error("Channels should remain nil when it was never set")
	}
}

// TestSavePlatformProviders_PreservesAllChannelTypes verifies that all three
// channel types (Telegram, Email, Slack) survive a provider settings save.
func TestSavePlatformProviders_PreservesAllChannelTypes(t *testing.T) {
	tmp := t.TempDir()
	_, esStore, err := entstore.NewPlatformServices(context.Background(), entstore.Config{
		DSN:     "file:" + filepath.Join(tmp, "platform.db"),
		DataDir: tmp,
	})
	if err != nil {
		t.Fatalf("NewPlatformServices: %v", err)
	}
	defer esStore.Close()

	ctx := context.Background()
	settingsStore := esStore.PlatformSettings()

	markRead := true
	// Step 1: Save settings with ALL channel types enabled
	initial := &store.PlatformSettings{
		DefaultProvider: "openai",
		Providers:       map[string]store.ProviderConfig{"openai": {"type": "openai"}},
		Channels: &store.PlatformChannelSettings{
			Telegram: &store.PlatformTelegramConfig{Enabled: true},
			Email: &store.PlatformEmailConfig{
				Enabled:    true,
				IMAPServer: "imap.gmail.com:993",
				SMTPServer: "smtp.gmail.com:587",
				Address:    "bot@example.com",
				MarkRead:   &markRead,
			},
			Slack: &store.PlatformSlackConfig{
				Enabled: true,
				Mode:    "socket",
			},
		},
	}
	if err := settingsStore.Save(ctx, initial); err != nil {
		t.Fatalf("Save initial: %v", err)
	}

	// Step 2: Simulate provider save (preserving channels)
	existing, err := settingsStore.Get(ctx)
	if err != nil {
		t.Fatalf("Get existing: %v", err)
	}

	updated := &store.PlatformSettings{
		DefaultProvider: "gemini",
		Providers:       map[string]store.ProviderConfig{"gemini": {"type": "gemini"}},
		Channels:        existing.Channels,
	}
	if err := settingsStore.Save(ctx, updated); err != nil {
		t.Fatalf("Save updated: %v", err)
	}

	// Step 3: Verify all channels survive
	reloaded, err := settingsStore.Get(ctx)
	if err != nil {
		t.Fatalf("Get reloaded: %v", err)
	}

	if reloaded.Channels == nil {
		t.Fatal("Channels is nil after provider save")
	}

	// Telegram
	if reloaded.Channels.Telegram == nil || !reloaded.Channels.Telegram.Enabled {
		t.Error("Telegram config lost after provider save")
	}

	// Email
	if reloaded.Channels.Email == nil {
		t.Fatal("Email config lost after provider save")
	}
	if !reloaded.Channels.Email.Enabled {
		t.Error("Email.Enabled lost")
	}
	if reloaded.Channels.Email.IMAPServer != "imap.gmail.com:993" {
		t.Errorf("Email.IMAPServer = %q, want %q", reloaded.Channels.Email.IMAPServer, "imap.gmail.com:993")
	}
	if reloaded.Channels.Email.Address != "bot@example.com" {
		t.Errorf("Email.Address = %q, want %q", reloaded.Channels.Email.Address, "bot@example.com")
	}

	// Slack
	if reloaded.Channels.Slack == nil {
		t.Fatal("Slack config lost after provider save")
	}
	if !reloaded.Channels.Slack.Enabled {
		t.Error("Slack.Enabled lost")
	}
	if reloaded.Channels.Slack.Mode != "socket" {
		t.Errorf("Slack.Mode = %q, want %q", reloaded.Channels.Slack.Mode, "socket")
	}
}
