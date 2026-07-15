package entstore

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	orgent "github.com/SAP/astonish/ent/org"
	"github.com/SAP/astonish/ent/org/team"
	"github.com/SAP/astonish/ent/org/teammembership"
	"github.com/SAP/astonish/pkg/store"
)

// orgTeamStore implements store.TeamManagementStore using the org Ent client.
type orgTeamStore struct {
	client *orgent.Client
}

var _ store.TeamManagementStore = (*orgTeamStore)(nil)

func (ts *orgTeamStore) CreateTeam(ctx context.Context, t *store.Team) error {
	tid, err := uuid.Parse(t.ID)
	if err != nil {
		tid = uuid.New()
	}

	created, err := ts.client.Team.Create().
		SetID(tid).
		SetName(t.Name).
		SetSlug(t.Slug).
		SetSchemaName(t.SchemaName).
		Save(ctx)
	if err != nil {
		return err
	}
	t.ID = created.ID.String()
	t.CreatedAt = created.CreatedAt
	return nil
}

func (ts *orgTeamStore) GetTeam(ctx context.Context, id string) (*store.Team, error) {
	tid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid team ID: %w", err)
	}
	ent, err := ts.client.Team.Get(ctx, tid)
	if err != nil {
		if orgent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return entOrgTeamToStore(ent), nil
}

func (ts *orgTeamStore) GetTeamBySlug(ctx context.Context, slug string) (*store.Team, error) {
	ent, err := ts.client.Team.Query().
		Where(team.SlugEQ(slug)).
		Only(ctx)
	if err != nil {
		if orgent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return entOrgTeamToStore(ent), nil
}

func (ts *orgTeamStore) ListTeams(ctx context.Context) ([]*store.Team, error) {
	ents, err := ts.client.Team.Query().
		Order(team.ByName()).
		All(ctx)
	if err != nil {
		return nil, err
	}
	teams := make([]*store.Team, len(ents))
	for i, e := range ents {
		teams[i] = entOrgTeamToStore(e)
	}
	return teams, nil
}

func (ts *orgTeamStore) ListTeamsForUser(ctx context.Context, userID string) ([]*store.Team, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID: %w", err)
	}

	memberships, err := ts.client.TeamMembership.Query().
		Where(teammembership.UserIDEQ(uid)).
		WithTeam().
		All(ctx)
	if err != nil {
		return nil, err
	}

	teams := make([]*store.Team, 0, len(memberships))
	for _, m := range memberships {
		if m.Edges.Team != nil {
			teams = append(teams, entOrgTeamToStore(m.Edges.Team))
		}
	}
	return teams, nil
}

func (ts *orgTeamStore) DeleteTeam(ctx context.Context, id string) error {
	tid, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid team ID: %w", err)
	}
	// Delete all memberships for this team first (foreign key constraint).
	_, _ = ts.client.TeamMembership.Delete().
		Where(teammembership.TeamIDEQ(tid)).
		Exec(ctx)
	return ts.client.Team.DeleteOneID(tid).Exec(ctx)
}

func (ts *orgTeamStore) RenameTeam(ctx context.Context, id string, name string) error {
	tid, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid team ID: %w", err)
	}
	return ts.client.Team.UpdateOneID(tid).SetName(name).Exec(ctx)
}

func (ts *orgTeamStore) CountTeams(ctx context.Context) (int, error) {
	return ts.client.Team.Query().Count(ctx)
}

func (ts *orgTeamStore) CountMembers(ctx context.Context, teamID string) (int, error) {
	tid, err := uuid.Parse(teamID)
	if err != nil {
		return 0, fmt.Errorf("invalid team ID: %w", err)
	}
	return ts.client.TeamMembership.Query().
		Where(teammembership.TeamIDEQ(tid)).
		Count(ctx)
}

func (ts *orgTeamStore) AddMember(ctx context.Context, m *store.TeamMembership) error {
	uid, err := uuid.Parse(m.UserID)
	if err != nil {
		return fmt.Errorf("invalid user ID: %w", err)
	}
	tid, err := uuid.Parse(m.TeamID)
	if err != nil {
		return fmt.Errorf("invalid team ID: %w", err)
	}

	role := teammembership.Role(m.Role)

	_, err = ts.client.TeamMembership.Create().
		SetUserID(uid).
		SetTeamID(tid).
		SetRole(role).
		Save(ctx)
	return err
}

func (ts *orgTeamStore) RemoveMember(ctx context.Context, userID, teamID string) error {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return fmt.Errorf("invalid user ID: %w", err)
	}
	tid, err := uuid.Parse(teamID)
	if err != nil {
		return fmt.Errorf("invalid team ID: %w", err)
	}

	_, err = ts.client.TeamMembership.Delete().
		Where(
			teammembership.UserIDEQ(uid),
			teammembership.TeamIDEQ(tid),
		).Exec(ctx)
	return err
}

func (ts *orgTeamStore) SetRole(ctx context.Context, userID, teamID, role string) error {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return fmt.Errorf("invalid user ID: %w", err)
	}
	tid, err := uuid.Parse(teamID)
	if err != nil {
		return fmt.Errorf("invalid team ID: %w", err)
	}

	_, err = ts.client.TeamMembership.Update().
		Where(
			teammembership.UserIDEQ(uid),
			teammembership.TeamIDEQ(tid),
		).
		SetRole(teammembership.Role(role)).
		Save(ctx)
	return err
}

func (ts *orgTeamStore) ListMembers(ctx context.Context, teamID string) ([]*store.TeamMembership, error) {
	tid, err := uuid.Parse(teamID)
	if err != nil {
		return nil, fmt.Errorf("invalid team ID: %w", err)
	}

	ents, err := ts.client.TeamMembership.Query().
		Where(teammembership.TeamIDEQ(tid)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	members := make([]*store.TeamMembership, len(ents))
	for i, e := range ents {
		members[i] = &store.TeamMembership{
			UserID:   e.UserID.String(),
			TeamID:   e.TeamID.String(),
			Role:     string(e.Role),
			JoinedAt: e.JoinedAt,
		}
	}
	return members, nil
}

func (ts *orgTeamStore) GetUserTeams(ctx context.Context, userID string) ([]*store.TeamMembership, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID: %w", err)
	}

	ents, err := ts.client.TeamMembership.Query().
		Where(teammembership.UserIDEQ(uid)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	memberships := make([]*store.TeamMembership, len(ents))
	for i, e := range ents {
		memberships[i] = &store.TeamMembership{
			UserID:   e.UserID.String(),
			TeamID:   e.TeamID.String(),
			Role:     string(e.Role),
			JoinedAt: e.JoinedAt,
		}
	}
	return memberships, nil
}

func (ts *orgTeamStore) IsTeamMember(ctx context.Context, userID, teamSlug string) (bool, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return false, fmt.Errorf("invalid user ID: %w", err)
	}

	// Look up team by slug first.
	t, err := ts.client.Team.Query().
		Where(team.SlugEQ(teamSlug)).
		Only(ctx)
	if err != nil {
		if orgent.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	count, err := ts.client.TeamMembership.Query().
		Where(
			teammembership.UserIDEQ(uid),
			teammembership.TeamIDEQ(t.ID),
		).
		Count(ctx)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (ts *orgTeamStore) GetMemberRole(ctx context.Context, userID, teamID string) (string, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return "", fmt.Errorf("invalid user ID: %w", err)
	}
	tid, err := uuid.Parse(teamID)
	if err != nil {
		return "", fmt.Errorf("invalid team ID: %w", err)
	}

	m, err := ts.client.TeamMembership.Query().
		Where(
			teammembership.UserIDEQ(uid),
			teammembership.TeamIDEQ(tid),
		).
		Only(ctx)
	if err != nil {
		if orgent.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return string(m.Role), nil
}

func entOrgTeamToStore(e *orgent.Team) *store.Team {
	return &store.Team{
		ID:         e.ID.String(),
		Name:       e.Name,
		Slug:       e.Slug,
		SchemaName: e.SchemaName,
		CreatedAt:  e.CreatedAt,
	}
}
