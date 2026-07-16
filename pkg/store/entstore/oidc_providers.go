package entstore

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	platforment "github.com/SAP/astonish/ent/platform"
	"github.com/SAP/astonish/ent/platform/oidcprovider"
	"github.com/SAP/astonish/pkg/store"
)

// oidcProviderStore implements store.OIDCProviderStore using the Ent platform client.
type oidcProviderStore struct {
	client *platforment.Client
}

func (s *Store) OIDCProviders() store.OIDCProviderStore {
	return &oidcProviderStore{client: s.platformClient}
}

func (ps *oidcProviderStore) Create(ctx context.Context, provider *store.OIDCProvider) error {
	create := ps.client.OIDCProvider.Create().
		SetIssuerURL(provider.IssuerURL).
		SetClientID(provider.ClientID).
		SetClientSecret(provider.ClientSecret).
		SetEnabled(provider.Enabled)

	if provider.ID != "" {
		uid, err := uuid.Parse(provider.ID)
		if err != nil {
			return fmt.Errorf("invalid provider ID: %w", err)
		}
		create.SetID(uid)
	}
	if provider.OrgID != "" {
		oid, err := uuid.Parse(provider.OrgID)
		if err != nil {
			return fmt.Errorf("invalid org ID: %w", err)
		}
		create.SetOrgID(oid)
	}
	if provider.Name != "" {
		create.SetName(provider.Name)
	}
	if provider.DiscoveryURL != "" {
		create.SetDiscoveryURL(provider.DiscoveryURL)
	}
	if provider.Scopes != nil {
		create.SetScopes(provider.Scopes)
	}
	if provider.TeamClaim != "" {
		create.SetTeamClaim(provider.TeamClaim)
	}
	if !provider.CreatedAt.IsZero() {
		create.SetCreatedAt(provider.CreatedAt)
	}

	saved, err := create.Save(ctx)
	if err != nil {
		return err
	}
	provider.ID = saved.ID.String()
	return nil
}

func (ps *oidcProviderStore) GetByID(ctx context.Context, id string) (*store.OIDCProvider, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid provider ID: %w", err)
	}
	ent, err := ps.client.OIDCProvider.Get(ctx, uid)
	if err != nil {
		if platforment.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return entOIDCProviderToStore(ent), nil
}

func (ps *oidcProviderStore) Update(ctx context.Context, provider *store.OIDCProvider) error {
	uid, err := uuid.Parse(provider.ID)
	if err != nil {
		return fmt.Errorf("invalid provider ID: %w", err)
	}

	update := ps.client.OIDCProvider.UpdateOneID(uid).
		SetName(provider.Name).
		SetIssuerURL(provider.IssuerURL).
		SetDiscoveryURL(provider.DiscoveryURL).
		SetClientID(provider.ClientID).
		SetClientSecret(provider.ClientSecret).
		SetEnabled(provider.Enabled)

	if provider.OrgID != "" {
		oid, err := uuid.Parse(provider.OrgID)
		if err != nil {
			return fmt.Errorf("invalid org ID: %w", err)
		}
		update.SetOrgID(oid)
	} else {
		update.ClearOrgID()
	}
	if provider.Scopes != nil {
		update.SetScopes(provider.Scopes)
	} else {
		update.ClearScopes()
	}
	if provider.TeamClaim != "" {
		update.SetTeamClaim(provider.TeamClaim)
	} else {
		update.ClearTeamClaim()
	}

	return update.Exec(ctx)
}

func (ps *oidcProviderStore) Delete(ctx context.Context, id string) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid provider ID: %w", err)
	}
	return ps.client.OIDCProvider.DeleteOneID(uid).Exec(ctx)
}

func (ps *oidcProviderStore) List(ctx context.Context, orgID string) ([]*store.OIDCProvider, error) {
	query := ps.client.OIDCProvider.Query()

	switch orgID {
	case "":
		// Platform-wide only: org_id is nil.
		query.Where(oidcprovider.OrgIDIsNil())
	case "*":
		// All providers, no filter.
	default:
		oid, err := uuid.Parse(orgID)
		if err != nil {
			return nil, fmt.Errorf("invalid org ID: %w", err)
		}
		query.Where(oidcprovider.OrgIDEQ(oid))
	}

	ents, err := query.Order(oidcprovider.ByCreatedAt()).All(ctx)
	if err != nil {
		return nil, err
	}

	providers := make([]*store.OIDCProvider, len(ents))
	for i, e := range ents {
		providers[i] = entOIDCProviderToStore(e)
	}
	return providers, nil
}

func (ps *oidcProviderStore) ListEnabled(ctx context.Context, orgID string) ([]*store.OIDCProvider, error) {
	query := ps.client.OIDCProvider.Query().
		Where(oidcprovider.EnabledEQ(true))

	if orgID != "" {
		oid, err := uuid.Parse(orgID)
		if err != nil {
			return nil, fmt.Errorf("invalid org ID: %w", err)
		}
		// Return platform-wide (org_id IS NULL) OR matching org.
		query.Where(
			oidcprovider.Or(
				oidcprovider.OrgIDIsNil(),
				oidcprovider.OrgIDEQ(oid),
			),
		)
	} else {
		// Only platform-wide providers.
		query.Where(oidcprovider.OrgIDIsNil())
	}

	ents, err := query.Order(oidcprovider.ByCreatedAt()).All(ctx)
	if err != nil {
		return nil, err
	}

	providers := make([]*store.OIDCProvider, len(ents))
	for i, e := range ents {
		providers[i] = entOIDCProviderToStore(e)
	}
	return providers, nil
}

func (ps *oidcProviderStore) GetByIssuer(ctx context.Context, issuerURL string) (*store.OIDCProvider, error) {
	ent, err := ps.client.OIDCProvider.Query().
		Where(
			oidcprovider.IssuerURLEQ(issuerURL),
			oidcprovider.EnabledEQ(true),
		).
		Only(ctx)
	if err != nil {
		if platforment.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return entOIDCProviderToStore(ent), nil
}

// entOIDCProviderToStore converts an Ent OIDCProvider entity to the store.OIDCProvider DTO.
func entOIDCProviderToStore(e *platforment.OIDCProvider) *store.OIDCProvider {
	p := &store.OIDCProvider{
		ID:           e.ID.String(),
		Name:         e.Name,
		IssuerURL:    e.IssuerURL,
		DiscoveryURL: e.DiscoveryURL,
		ClientID:     e.ClientID,
		ClientSecret: e.ClientSecret,
		Scopes:       e.Scopes,
		Enabled:      e.Enabled,
		CreatedAt:    e.CreatedAt,
	}
	if e.OrgID != nil {
		p.OrgID = e.OrgID.String()
	}
	if e.TeamClaim != nil {
		p.TeamClaim = *e.TeamClaim
	}
	return p
}

// Compile-time assertion.
var _ store.OIDCProviderStore = (*oidcProviderStore)(nil)
