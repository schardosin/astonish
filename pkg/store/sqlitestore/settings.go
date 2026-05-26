package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/schardosin/astonish/pkg/store"
)

// sqlitePlatformSettingsStore implements store.PlatformSettingsStore.
type sqlitePlatformSettingsStore struct {
	db      *sql.DB
	secrets *SQLitePlatformSecretStore
}

func (s *sqlitePlatformSettingsStore) Get(ctx context.Context) (*store.PlatformSettings, error) {
	row := s.db.QueryRowContext(ctx, `SELECT value FROM platform_settings WHERE key = 'settings'`)
	var val string
	if err := row.Scan(&val); err == sql.ErrNoRows {
		return &store.PlatformSettings{}, nil
	} else if err != nil {
		return nil, err
	}
	settings := &store.PlatformSettings{}
	if err := json.Unmarshal([]byte(val), settings); err != nil {
		return nil, err
	}
	// Inject secrets from platform_secrets store.
	if s.secrets != nil && settings.Providers != nil {
		injectProviderSecrets(settings.Providers, "platform", s.secrets.GetSecret)
	}
	return settings, nil
}

func (s *sqlitePlatformSettingsStore) Save(ctx context.Context, settings *store.PlatformSettings) error {
	// Extract and store secrets separately (encrypted).
	if s.secrets != nil && settings.Providers != nil {
		extractProviderSecrets(settings.Providers, "platform", s.secrets.SetSecret)
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO platform_settings (key, value, updated_at) VALUES ('settings', ?, datetime('now'))
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
		string(data))
	return err
}

// sqliteSettingsStore implements store.SettingsStore (team-level).
type sqliteSettingsStore struct {
	db       *sql.DB
	orgSlug  string
	teamSlug string
	secrets  *SQLitePlatformSecretStore
}

func (s *sqliteSettingsStore) Get(ctx context.Context) (*store.TeamSettings, error) {
	row := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'team_settings'`)
	var val string
	if err := row.Scan(&val); err == sql.ErrNoRows {
		return &store.TeamSettings{}, nil
	} else if err != nil {
		return nil, err
	}
	settings := &store.TeamSettings{}
	if err := json.Unmarshal([]byte(val), settings); err != nil {
		return nil, err
	}
	// Inject secrets from platform_secrets (team-scoped keys).
	if s.secrets != nil && settings.Providers != nil {
		secretLevel := "team." + s.orgSlug + "." + s.teamSlug
		injectProviderSecrets(settings.Providers, secretLevel, s.secrets.GetSecret)
	}
	return settings, nil
}

func (s *sqliteSettingsStore) Save(ctx context.Context, settings *store.TeamSettings) error {
	// Extract secrets from provider configs and store encrypted.
	if s.secrets != nil && settings.Providers != nil {
		secretLevel := "team." + s.orgSlug + "." + s.teamSlug
		extractProviderSecrets(settings.Providers, secretLevel, s.secrets.SetSecret)
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value, updated_at) VALUES ('team_settings', ?, datetime('now'))
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
		string(data))
	return err
}

// --- OrgSettingsStore ---

// sqliteOrgSettingsStore implements store.OrgSettingsStore using the
// organizations.settings TEXT column in the platform database.
type sqliteOrgSettingsStore struct {
	db      *sql.DB
	orgSlug string
	secrets *SQLitePlatformSecretStore
}

func (s *sqliteOrgSettingsStore) Get(ctx context.Context) (*store.OrgSettings, error) {
	var val sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT settings FROM organizations WHERE slug = ?`, s.orgSlug,
	).Scan(&val)
	if err != nil {
		return &store.OrgSettings{}, nil
	}

	data := val.String
	if data == "" || data == "{}" {
		return &store.OrgSettings{}, nil
	}

	var settings store.OrgSettings
	if err := json.Unmarshal([]byte(data), &settings); err != nil {
		return nil, err
	}

	// Inject secrets from platform_secrets (org-scoped keys).
	if s.secrets != nil && settings.Providers != nil {
		secretLevel := "org." + s.orgSlug
		injectProviderSecrets(settings.Providers, secretLevel, s.secrets.GetSecret)
	}

	return &settings, nil
}

func (s *sqliteOrgSettingsStore) Save(ctx context.Context, settings *store.OrgSettings) error {
	// Extract secrets from provider configs and store encrypted.
	if s.secrets != nil && settings.Providers != nil {
		secretLevel := "org." + s.orgSlug
		extractProviderSecrets(settings.Providers, secretLevel, s.secrets.SetSecret)
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE organizations SET settings = ? WHERE slug = ?`,
		string(data), s.orgSlug,
	)
	return err
}

// --- Provider secret helpers ---

// extractProviderSecrets removes secret fields from provider configs and
// persists them encrypted. Keys follow the pattern "provider.<level>.<instance>.<field>".
func extractProviderSecrets(providers map[string]store.ProviderConfig, level string, setSecret func(string, string) error) {
	for instanceName, pCfg := range providers {
		for _, key := range providerSecretFields(pCfg) {
			val, has := pCfg[key]
			if !has || val == "" || isMaskedValue(val) {
				continue
			}
			storeKey := "provider." + level + "." + instanceName + "." + key
			if err := setSecret(storeKey, val); err == nil {
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

// providerSecretFields returns the field names that should be treated as secrets.
func providerSecretFields(pCfg store.ProviderConfig) []string {
	switch pCfg["type"] {
	case "sap_ai_core":
		return []string{"client_id", "client_secret", "auth_url"}
	default:
		return []string{"api_key"}
	}
}

func isMaskedValue(val string) bool {
	return len(val) >= 4 && val[:4] == "****"
}
