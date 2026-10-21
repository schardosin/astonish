package api

import (
	"context"
	"testing"

	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/credentials"
	"github.com/SAP/astonish/pkg/provider"
)

// TestResolveProviderSecret_PlatformConfinement proves the provider base_url
// Medium vulnerability is closed: a multi-tenant (platform) request must never
// inherit the server operator's process-wide credentials — neither an
// environment variable nor an operator-owned credential-store entry keyed by a
// tenant-chosen provider name — while trusted local Code mode (unconfined)
// keeps both legitimate fallbacks.
//
// Attack this guards against: a tenant creates a `litellm`/`sap_ai_core`
// provider instance pointed at an attacker-controlled base_url and omits the
// key. Without confinement, resolveProviderSecret would hand the operator's
// OPENAI_API_KEY (env) or the `provider.openai.api_key` store entry back, and
// the model-list / health-check handler would dial it to the attacker URL.
func TestResolveProviderSecret_PlatformConfinement(t *testing.T) {
	const (
		providerID  = "openai"
		cfgKey      = "api_key"
		envVar      = "OPENAI_API_KEY"
		envSecret   = "operator-env-secret"
		storeSecret = "operator-store-secret"
	)

	// Operator env credential is present on the host process.
	t.Setenv(envVar, envSecret)

	// Operator credential-store entry keyed by the (tenant-chosen) provider name.
	store, err := credentials.Open(t.TempDir())
	if err != nil {
		t.Fatalf("credentials.Open: %v", err)
	}
	if err := store.SetSecret("provider."+providerID+"."+cfgKey, storeSecret); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}
	prev := getAPICredentialStore()
	SetAPICredentialStore(store)
	t.Cleanup(func() { SetAPICredentialStore(prev) })

	// Tenant provider instance: attacker base_url, NO key supplied.
	tenantCfg := config.ProviderConfig{"base_url": "https://attacker.example"}

	t.Run("platform request inherits no operator secret", func(t *testing.T) {
		ctx := provider.WithConfinedProviderSecrets(context.Background())
		got := resolveProviderSecret(ctx, providerID, cfgKey, envVar, tenantCfg)
		if got != "" {
			t.Fatalf("confined request leaked operator secret: got %q, want empty", got)
		}
	})

	t.Run("platform request honors tenant-supplied key", func(t *testing.T) {
		ctx := provider.WithConfinedProviderSecrets(context.Background())
		cfgWithKey := config.ProviderConfig{"base_url": "https://attacker.example", cfgKey: "tenant-own-key"}
		got := resolveProviderSecret(ctx, providerID, cfgKey, envVar, cfgWithKey)
		if got != "tenant-own-key" {
			t.Fatalf("confined request dropped tenant's own key: got %q, want %q", got, "tenant-own-key")
		}
	})

	t.Run("local Code mode keeps credential-store fallback", func(t *testing.T) {
		// Unconfined context (local Code mode never sets the flag). Store entry
		// wins (step 1) ahead of the env fallback.
		got := resolveProviderSecret(context.Background(), providerID, cfgKey, envVar, tenantCfg)
		if got != storeSecret {
			t.Fatalf("local request lost credential-store fallback: got %q, want %q", got, storeSecret)
		}
	})

	t.Run("local Code mode keeps env fallback when store empty", func(t *testing.T) {
		// Remove the store so only the env fallback remains.
		SetAPICredentialStore(nil)
		t.Cleanup(func() { SetAPICredentialStore(store) })
		got := resolveProviderSecret(context.Background(), providerID, cfgKey, envVar, tenantCfg)
		if got != envSecret {
			t.Fatalf("local request lost env fallback: got %q, want %q", got, envSecret)
		}
	})
}
