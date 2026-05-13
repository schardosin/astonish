package pgstore

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/schardosin/astonish/pkg/store"
)

// pgSettingsStore implements store.SettingsStore using the teams.settings JSONB column.
// Provider secrets are stored in platform_secrets with team-scoped keys.
type pgSettingsStore struct {
	pool     *pgxpool.Pool
	teamSlug string
	orgSlug  string
	secrets  *PlatformSecretStore
}

func (s *pgSettingsStore) Get(ctx context.Context) (*store.TeamSettings, error) {
	var data json.RawMessage
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(settings, '{}'::jsonb) FROM public.teams WHERE slug = $1`,
		s.teamSlug,
	).Scan(&data)
	if err != nil {
		return nil, err
	}

	var settings store.TeamSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}

	// Inject secrets from platform_secrets (team-scoped keys).
	if s.secrets != nil && settings.Providers != nil {
		secretLevel := "team." + s.orgSlug + "." + s.teamSlug
		injectProviderSecrets(settings.Providers, secretLevel, s.secrets.GetSecret)
	}

	return &settings, nil
}

func (s *pgSettingsStore) Save(ctx context.Context, settings *store.TeamSettings) error {
	// Extract secrets from provider configs and store encrypted.
	if s.secrets != nil && settings.Providers != nil {
		secretLevel := "team." + s.orgSlug + "." + s.teamSlug
		extractProviderSecrets(settings.Providers, secretLevel, s.secrets.SetSecret)
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx,
		`UPDATE public.teams SET settings = $1 WHERE slug = $2`,
		data, s.teamSlug,
	)
	return err
}
