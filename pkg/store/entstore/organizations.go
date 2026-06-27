package entstore

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	platforment "github.com/schardosin/astonish/ent/platform"
	"github.com/schardosin/astonish/ent/platform/loginsession"
	"github.com/schardosin/astonish/ent/platform/organization"
	"github.com/schardosin/astonish/ent/platform/orgmembership"
	"github.com/schardosin/astonish/pkg/store"
)

// orgStore implements store.OrganizationStore using the Ent platform client.
type orgStore struct {
	client *platforment.Client
}

func (s *Store) Organizations() store.OrganizationStore {
	return &orgStore{client: s.platformClient}
}

func (os *orgStore) Create(ctx context.Context, org *store.Organization) error {
	oid, err := uuid.Parse(org.ID)
	if err != nil {
		return fmt.Errorf("invalid org ID: %w", err)
	}

	create := os.client.Organization.Create().
		SetID(oid).
		SetName(org.Name).
		SetSlug(org.Slug).
		SetStatus(organization.Status(org.Status))

	if org.DBName != "" {
		create.SetDbName(org.DBName)
	}
	if !org.CreatedAt.IsZero() {
		create.SetCreatedAt(org.CreatedAt)
	}

	_, err = create.Save(ctx)
	return err
}

func (os *orgStore) GetByID(ctx context.Context, id string) (*store.Organization, error) {
	oid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid org ID: %w", err)
	}
	ent, err := os.client.Organization.Get(ctx, oid)
	if err != nil {
		if platforment.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return entOrgToStore(ent), nil
}

func (os *orgStore) GetBySlug(ctx context.Context, slug string) (*store.Organization, error) {
	ent, err := os.client.Organization.Query().
		Where(organization.SlugEQ(slug)).
		Only(ctx)
	if err != nil {
		if platforment.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return entOrgToStore(ent), nil
}

func (os *orgStore) List(ctx context.Context) ([]*store.Organization, error) {
	ents, err := os.client.Organization.Query().
		Order(organization.ByName()).
		All(ctx)
	if err != nil {
		return nil, err
	}

	orgs := make([]*store.Organization, len(ents))
	for i, e := range ents {
		orgs[i] = entOrgToStore(e)
	}
	return orgs, nil
}

func (os *orgStore) Update(ctx context.Context, org *store.Organization) error {
	oid, err := uuid.Parse(org.ID)
	if err != nil {
		return fmt.Errorf("invalid org ID: %w", err)
	}

	return os.client.Organization.UpdateOneID(oid).
		SetName(org.Name).
		SetSlug(org.Slug).
		SetDbName(org.DBName).
		SetStatus(organization.Status(org.Status)).
		Exec(ctx)
}

func (os *orgStore) Delete(ctx context.Context, id string) error {
	oid, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid org ID: %w", err)
	}

	// Delete all login sessions for this org first (FK: NO ACTION).
	if _, err := os.client.LoginSession.Delete().
		Where(loginsession.OrgIDEQ(oid)).
		Exec(ctx); err != nil {
		return fmt.Errorf("failed to delete login sessions: %w", err)
	}

	// Delete all memberships for this org (FK: NO ACTION).
	if _, err := os.client.OrgMembership.Delete().
		Where(orgmembership.OrgIDEQ(oid)).
		Exec(ctx); err != nil {
		return fmt.Errorf("failed to delete memberships: %w", err)
	}

	// Delete the org record itself.
	return os.client.Organization.DeleteOneID(oid).Exec(ctx)
}

func (os *orgStore) Count(ctx context.Context) (int, error) {
	return os.client.Organization.Query().Count(ctx)
}

func (os *orgStore) AddMember(ctx context.Context, userID, orgID, role string) error {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return fmt.Errorf("invalid user ID: %w", err)
	}
	oid, err := uuid.Parse(orgID)
	if err != nil {
		return fmt.Errorf("invalid org ID: %w", err)
	}

	_, err = os.client.OrgMembership.Create().
		SetUserID(uid).
		SetOrgID(oid).
		SetRole(orgmembership.Role(role)).
		Save(ctx)
	return err
}

func (os *orgStore) RemoveMember(ctx context.Context, userID, orgID string) error {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return fmt.Errorf("invalid user ID: %w", err)
	}
	oid, err := uuid.Parse(orgID)
	if err != nil {
		return fmt.Errorf("invalid org ID: %w", err)
	}

	// Delete by composite key fields.
	_, err = os.client.OrgMembership.Delete().
		Where(
			orgmembership.UserIDEQ(uid),
			orgmembership.OrgIDEQ(oid),
		).
		Exec(ctx)
	return err
}

func (os *orgStore) GetUserOrgs(ctx context.Context, userID string) ([]*store.OrgMembership, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID: %w", err)
	}

	memberships, err := os.client.OrgMembership.Query().
		Where(orgmembership.UserIDEQ(uid)).
		WithOrganization().
		All(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]*store.OrgMembership, 0, len(memberships))
	for _, m := range memberships {
		om := &store.OrgMembership{
			UserID:   m.UserID.String(),
			OrgID:    m.OrgID.String(),
			Role:     string(m.Role),
			JoinedAt: m.JoinedAt,
		}
		if m.Edges.Organization != nil {
			om.OrgSlug = m.Edges.Organization.Slug
			om.OrgName = m.Edges.Organization.Name
			om.OrgStatus = string(m.Edges.Organization.Status)
		}
		result = append(result, om)
	}
	return result, nil
}

func (os *orgStore) GetMemberRole(ctx context.Context, userID, orgID string) (string, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return "", fmt.Errorf("invalid user ID: %w", err)
	}
	oid, err := uuid.Parse(orgID)
	if err != nil {
		return "", fmt.Errorf("invalid org ID: %w", err)
	}

	m, err := os.client.OrgMembership.Query().
		Where(
			orgmembership.UserIDEQ(uid),
			orgmembership.OrgIDEQ(oid),
		).
		Only(ctx)
	if err != nil {
		if platforment.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return string(m.Role), nil
}

func (os *orgStore) ListMembers(ctx context.Context, orgID string) ([]*store.UserWithRole, error) {
	oid, err := uuid.Parse(orgID)
	if err != nil {
		return nil, fmt.Errorf("invalid org ID: %w", err)
	}

	memberships, err := os.client.OrgMembership.Query().
		Where(orgmembership.OrgIDEQ(oid)).
		WithUser().
		All(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]*store.UserWithRole, 0, len(memberships))
	for _, m := range memberships {
		if m.Edges.User == nil {
			continue
		}
		u := entUserToStore(m.Edges.User)
		result = append(result, &store.UserWithRole{
			User:     *u,
			Role:     string(m.Role),
			JoinedAt: m.JoinedAt,
		})
	}
	return result, nil
}

// entOrgToStore converts an Ent Organization entity to the store.Organization DTO.
func entOrgToStore(e *platforment.Organization) *store.Organization {
	return &store.Organization{
		ID:        e.ID.String(),
		Name:      e.Name,
		Slug:      e.Slug,
		DBName:    e.DbName,
		Status:    string(e.Status),
		CreatedAt: e.CreatedAt,
	}
}

// Compile-time assertion.
var _ store.OrganizationStore = (*orgStore)(nil)
