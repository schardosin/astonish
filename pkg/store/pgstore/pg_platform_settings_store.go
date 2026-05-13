package pgstore

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/schardosin/astonish/pkg/store"
)

// pgPlatformSettingsStore implements store.PlatformSettingsStore using the
// platform_settings table in the platform database. Provider secrets (api_key,
// client_secret, etc.) are stored encrypted in the platform_secrets table
// rather than in the JSONB value.
type pgPlatformSettingsStore struct {
	pool    *pgxpool.Pool
	secrets *PlatformSecretStore
}

const platformSettingsKeyProviders = "providers"

// NewPGPlatformSettingsStore creates a new platform settings store.
// The secrets store is used to encrypt/decrypt provider API keys.
func NewPGPlatformSettingsStore(pool *pgxpool.Pool, secrets *PlatformSecretStore) *pgPlatformSettingsStore {
	return &pgPlatformSettingsStore{pool: pool, secrets: secrets}
}

func (s *pgPlatformSettingsStore) Get(ctx context.Context) (*store.PlatformSettings, error) {
	var data json.RawMessage
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(value, '{}'::jsonb) FROM platform_settings WHERE key = $1`,
		platformSettingsKeyProviders,
	).Scan(&data)
	if err != nil {
		// No row means no settings yet — return empty.
		return &store.PlatformSettings{}, nil
	}

	var settings store.PlatformSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}

	// Inject secrets from platform_secrets into provider configs.
	if s.secrets != nil {
		injectProviderSecrets(settings.Providers, "platform", s.secrets.GetSecret)
	}

	return &settings, nil
}

func (s *pgPlatformSettingsStore) Save(ctx context.Context, settings *store.PlatformSettings) error {
	// Extract secrets from provider configs and store them encrypted.
	// Then scrub from the settings before saving to JSONB.
	if s.secrets != nil && settings.Providers != nil {
		extractProviderSecrets(settings.Providers, "platform", s.secrets.SetSecret)
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx,
		`INSERT INTO platform_settings (key, value, updated_at)
		 VALUES ($1, $2, now())
		 ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = now()`,
		platformSettingsKeyProviders, data,
	)
	return err
}

// extractProviderSecrets removes secret fields from provider configs and
// persists them encrypted. Keys follow the pattern "provider.<level>.<instance>.<field>".
func extractProviderSecrets(providers map[string]store.ProviderConfig, level string, setSecret func(string, string) error) {
	for instanceName, pCfg := range providers {
		for _, key := range providerSecretFields(pCfg) {
			val, has := pCfg[key]
			if !has || val == "" || isMasked(val) {
				continue
			}
			storeKey := "provider." + level + "." + instanceName + "." + key
			if err := setSecret(storeKey, val); err == nil {
				// Scrub from the config that goes into JSONB
				delete(pCfg, key)
			}
		}
	}
}

// injectProviderSecrets re-injects secret values from the encrypted store
// into provider configs for runtime use.
func injectProviderSecrets(providers map[string]store.ProviderConfig, level string, getSecret func(string) string) {
	for instanceName, pCfg := range providers {
		for _, key := range providerSecretFields(pCfg) {
			storeKey := "provider." + level + "." + instanceName + "." + key
			if val := getSecret(storeKey); val != "" {
				if pCfg == nil {
					pCfg = make(store.ProviderConfig)
					providers[instanceName] = pCfg
				}
				pCfg[key] = val
			}
		}
	}
}

// providerSecretFields returns the field names that should be treated as secrets
// for a given provider config. Based on provider type.
func providerSecretFields(pCfg store.ProviderConfig) []string {
	switch pCfg["type"] {
	case "sap_ai_core":
		return []string{"client_id", "client_secret", "auth_url"}
	default:
		return []string{"api_key"}
	}
}

func isMasked(val string) bool {
	return len(val) >= 4 && val[:4] == "****"
}
