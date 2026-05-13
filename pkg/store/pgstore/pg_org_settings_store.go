package pgstore

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/schardosin/astonish/pkg/store"
)

// pgOrgSettingsStore implements store.OrgSettingsStore using the
// organizations.settings JSONB column in the platform database.
// Provider secrets are stored in platform_secrets with org-scoped keys.
type pgOrgSettingsStore struct {
	pool    *pgxpool.Pool
	orgSlug string
	secrets *PlatformSecretStore
}

// NewPGOrgSettingsStore creates a new org settings store.
func NewPGOrgSettingsStore(pool *pgxpool.Pool, orgSlug string, secrets *PlatformSecretStore) *pgOrgSettingsStore {
	return &pgOrgSettingsStore{pool: pool, orgSlug: orgSlug, secrets: secrets}
}

func (s *pgOrgSettingsStore) Get(ctx context.Context) (*store.OrgSettings, error) {
	var data json.RawMessage
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(settings, '{}'::jsonb) FROM organizations WHERE slug = $1`,
		s.orgSlug,
	).Scan(&data)
	if err != nil {
		return &store.OrgSettings{}, nil
	}

	var settings store.OrgSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}

	// Inject secrets from platform_secrets (org-scoped keys).
	if s.secrets != nil {
		secretLevel := "org." + s.orgSlug
		injectProviderSecrets(settings.Providers, secretLevel, s.secrets.GetSecret)
	}

	return &settings, nil
}

func (s *pgOrgSettingsStore) Save(ctx context.Context, settings *store.OrgSettings) error {
	// Extract secrets from provider configs and store encrypted.
	if s.secrets != nil && settings.Providers != nil {
		secretLevel := "org." + s.orgSlug
		extractProviderSecrets(settings.Providers, secretLevel, s.secrets.SetSecret)
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx,
		`UPDATE organizations SET settings = $1 WHERE slug = $2`,
		data, s.orgSlug,
	)
	return err
}
