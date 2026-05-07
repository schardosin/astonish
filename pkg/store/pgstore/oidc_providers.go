package pgstore

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/schardosin/astonish/pkg/store"
)

// pgOIDCProviderStore implements store.OIDCProviderStore using the platform database.
type pgOIDCProviderStore struct {
	poolMgr *PoolManager
}

func (s *pgOIDCProviderStore) Create(ctx context.Context, provider *store.OIDCProvider) error {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO oidc_providers (id, org_id, name, issuer_url, client_id, client_secret, scopes, team_claim, enabled, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		provider.ID, nilIfEmpty(provider.OrgID), provider.Name, provider.IssuerURL,
		provider.ClientID, provider.ClientSecret, provider.Scopes,
		nilIfEmpty(provider.TeamClaim), provider.Enabled, provider.CreatedAt,
	)
	return err
}

func (s *pgOIDCProviderStore) GetByID(ctx context.Context, id string) (*store.OIDCProvider, error) {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return nil, err
	}
	return scanOIDCProvider(pool.QueryRow(ctx,
		`SELECT id, org_id, name, issuer_url, client_id, client_secret, scopes, team_claim, enabled, created_at
		 FROM oidc_providers WHERE id = $1`, id,
	))
}

func (s *pgOIDCProviderStore) Update(ctx context.Context, provider *store.OIDCProvider) error {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`UPDATE oidc_providers SET org_id = $2, name = $3, issuer_url = $4, client_id = $5,
		 client_secret = $6, scopes = $7, team_claim = $8, enabled = $9
		 WHERE id = $1`,
		provider.ID, nilIfEmpty(provider.OrgID), provider.Name, provider.IssuerURL,
		provider.ClientID, provider.ClientSecret, provider.Scopes,
		nilIfEmpty(provider.TeamClaim), provider.Enabled,
	)
	return err
}

func (s *pgOIDCProviderStore) Delete(ctx context.Context, id string) error {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `DELETE FROM oidc_providers WHERE id = $1`, id)
	return err
}

func (s *pgOIDCProviderStore) List(ctx context.Context, orgID string) ([]*store.OIDCProvider, error) {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return nil, err
	}

	var rows pgx.Rows
	switch orgID {
	case "*":
		// All providers across all orgs
		rows, err = pool.Query(ctx,
			`SELECT id, org_id, name, issuer_url, client_id, client_secret, scopes, team_claim, enabled, created_at
			 FROM oidc_providers ORDER BY created_at`)
	case "":
		// Platform-wide only (org_id IS NULL)
		rows, err = pool.Query(ctx,
			`SELECT id, org_id, name, issuer_url, client_id, client_secret, scopes, team_claim, enabled, created_at
			 FROM oidc_providers WHERE org_id IS NULL ORDER BY created_at`)
	default:
		// Specific org + platform-wide
		rows, err = pool.Query(ctx,
			`SELECT id, org_id, name, issuer_url, client_id, client_secret, scopes, team_claim, enabled, created_at
			 FROM oidc_providers WHERE org_id IS NULL OR org_id = $1 ORDER BY created_at`, orgID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []*store.OIDCProvider
	for rows.Next() {
		p, err := scanOIDCProvider(rows)
		if err != nil {
			return nil, err
		}
		providers = append(providers, p)
	}
	return providers, rows.Err()
}

func (s *pgOIDCProviderStore) ListEnabled(ctx context.Context, orgID string) ([]*store.OIDCProvider, error) {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return nil, err
	}

	var rows pgx.Rows
	if orgID == "" {
		rows, err = pool.Query(ctx,
			`SELECT id, org_id, name, issuer_url, client_id, client_secret, scopes, team_claim, enabled, created_at
			 FROM oidc_providers WHERE enabled = true AND org_id IS NULL ORDER BY created_at`)
	} else {
		rows, err = pool.Query(ctx,
			`SELECT id, org_id, name, issuer_url, client_id, client_secret, scopes, team_claim, enabled, created_at
			 FROM oidc_providers WHERE enabled = true AND (org_id IS NULL OR org_id = $1) ORDER BY created_at`, orgID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []*store.OIDCProvider
	for rows.Next() {
		p, err := scanOIDCProvider(rows)
		if err != nil {
			return nil, err
		}
		providers = append(providers, p)
	}
	return providers, rows.Err()
}

func (s *pgOIDCProviderStore) GetByIssuer(ctx context.Context, issuerURL string) (*store.OIDCProvider, error) {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return nil, err
	}
	return scanOIDCProvider(pool.QueryRow(ctx,
		`SELECT id, org_id, name, issuer_url, client_id, client_secret, scopes, team_claim, enabled, created_at
		 FROM oidc_providers WHERE issuer_url = $1 AND enabled = true LIMIT 1`, issuerURL,
	))
}

// scanOIDCProvider scans a single OIDC provider row.
func scanOIDCProvider(row scannable) (*store.OIDCProvider, error) {
	p := &store.OIDCProvider{}
	var orgID, teamClaim *string
	err := row.Scan(&p.ID, &orgID, &p.Name, &p.IssuerURL, &p.ClientID, &p.ClientSecret,
		&p.Scopes, &teamClaim, &p.Enabled, &p.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to scan oidc_provider: %w", err)
	}
	if orgID != nil {
		p.OrgID = *orgID
	}
	if teamClaim != nil {
		p.TeamClaim = *teamClaim
	}
	return p, nil
}
