package pgstore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/store"
)

// pgAuditStore implements store.AuditStore for PostgreSQL.
// It is append-only: the app role has INSERT privilege but not UPDATE or DELETE.
type pgAuditStore struct {
	pool   *pgxpool.Pool
	schema string
	table  string // "org_audit_log" or "team_audit_log"
}

func (a *pgAuditStore) tableName() string {
	return pgx.Identifier{a.schema, a.table}.Sanitize()
}

func (a *pgAuditStore) Log(ctx context.Context, entry *store.AuditEntry) error {
	detailJSON, _ := json.Marshal(entry.Detail)

	_, err := a.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (user_id, team_id, action, resource, detail, ip_address, session_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`, a.tableName()),
		entry.UserID, nilIfEmpty(entry.TeamID), entry.Action, entry.Resource,
		detailJSON, nilIfEmpty(entry.IPAddress), nilIfEmpty(entry.SessionID),
	)
	return err
}

func (a *pgAuditStore) Query(ctx context.Context, filter store.AuditFilter) ([]*store.AuditEntry, error) {
	// Build dynamic WHERE clause
	where := "WHERE 1=1"
	args := []any{}
	argIdx := 1

	if filter.UserID != "" {
		where += fmt.Sprintf(" AND user_id = $%d", argIdx)
		args = append(args, filter.UserID)
		argIdx++
	}
	if filter.Action != "" {
		where += fmt.Sprintf(" AND action = $%d", argIdx)
		args = append(args, filter.Action)
		argIdx++
	}
	if filter.Resource != "" {
		where += fmt.Sprintf(" AND resource = $%d", argIdx)
		args = append(args, filter.Resource)
		argIdx++
	}
	if !filter.Since.IsZero() {
		where += fmt.Sprintf(" AND timestamp >= $%d", argIdx)
		args = append(args, filter.Since)
		argIdx++
	}
	if !filter.Until.IsZero() {
		where += fmt.Sprintf(" AND timestamp <= $%d", argIdx)
		args = append(args, filter.Until)
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	offset := filter.Offset

	query := fmt.Sprintf(
		`SELECT id, timestamp, user_id, team_id, action, resource, detail, ip_address, session_id
		 FROM %s %s ORDER BY id DESC LIMIT %d OFFSET %d`,
		a.tableName(), where, limit, offset,
	)

	rows, err := a.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*store.AuditEntry
	for rows.Next() {
		entry, err := scanAuditEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func scanAuditEntry(row scannable) (*store.AuditEntry, error) {
	e := &store.AuditEntry{}
	var teamID, ipAddr, sessID *string
	var detailJSON []byte

	err := row.Scan(&e.ID, &e.Timestamp, &e.UserID, &teamID, &e.Action, &e.Resource,
		&detailJSON, &ipAddr, &sessID)
	if err != nil {
		return nil, fmt.Errorf("failed to scan audit entry: %w", err)
	}

	if teamID != nil {
		e.TeamID = *teamID
	}
	if ipAddr != nil {
		e.IPAddress = *ipAddr
	}
	if sessID != nil {
		e.SessionID = *sessID
	}
	if len(detailJSON) > 0 {
		_ = json.Unmarshal(detailJSON, &e.Detail)
	}
	return e, nil
}

// --- pgTeamManagementStore implements store.TeamManagementStore ---

type pgTeamManagementStore struct {
	pool *pgxpool.Pool
}

func (t *pgTeamManagementStore) CreateTeam(ctx context.Context, team *store.Team) error {
	_, err := t.pool.Exec(ctx,
		`INSERT INTO teams (id, name, slug, schema_name, created_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		team.ID, team.Name, team.Slug, team.SchemaName, team.CreatedAt,
	)
	return err
}

func (t *pgTeamManagementStore) GetTeam(ctx context.Context, id string) (*store.Team, error) {
	return scanTeam(t.pool.QueryRow(ctx,
		`SELECT id, name, slug, schema_name, created_at FROM teams WHERE id = $1`, id,
	))
}

func (t *pgTeamManagementStore) GetTeamBySlug(ctx context.Context, slug string) (*store.Team, error) {
	return scanTeam(t.pool.QueryRow(ctx,
		`SELECT id, name, slug, schema_name, created_at FROM teams WHERE slug = $1`, slug,
	))
}

func (t *pgTeamManagementStore) ListTeams(ctx context.Context) ([]*store.Team, error) {
	rows, err := t.pool.Query(ctx,
		`SELECT id, name, slug, schema_name, created_at FROM teams ORDER BY name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var teams []*store.Team
	for rows.Next() {
		team, err := scanTeam(rows)
		if err != nil {
			return nil, err
		}
		teams = append(teams, team)
	}
	return teams, rows.Err()
}

func (t *pgTeamManagementStore) ListTeamsForUser(ctx context.Context, userID string) ([]*store.Team, error) {
	rows, err := t.pool.Query(ctx,
		`SELECT t.id, t.name, t.slug, t.schema_name, t.created_at
		 FROM teams t
		 JOIN team_memberships tm ON tm.team_id = t.id
		 WHERE tm.user_id = $1
		 ORDER BY t.name`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var teams []*store.Team
	for rows.Next() {
		team, err := scanTeam(rows)
		if err != nil {
			return nil, err
		}
		teams = append(teams, team)
	}
	return teams, rows.Err()
}

func (t *pgTeamManagementStore) DeleteTeam(ctx context.Context, id string) error {
	_, err := t.pool.Exec(ctx, `DELETE FROM teams WHERE id = $1`, id)
	return err
}

func (t *pgTeamManagementStore) AddMember(ctx context.Context, m *store.TeamMembership) error {
	_, err := t.pool.Exec(ctx,
		`INSERT INTO team_memberships (user_id, team_id, role, joined_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (user_id, team_id) DO UPDATE SET role = $3`,
		m.UserID, m.TeamID, m.Role, m.JoinedAt,
	)
	return err
}

func (t *pgTeamManagementStore) RemoveMember(ctx context.Context, userID, teamID string) error {
	_, err := t.pool.Exec(ctx,
		`DELETE FROM team_memberships WHERE user_id = $1 AND team_id = $2`,
		userID, teamID,
	)
	return err
}

func (t *pgTeamManagementStore) SetRole(ctx context.Context, userID, teamID, role string) error {
	_, err := t.pool.Exec(ctx,
		`UPDATE team_memberships SET role = $3 WHERE user_id = $1 AND team_id = $2`,
		userID, teamID, role,
	)
	return err
}

func (t *pgTeamManagementStore) ListMembers(ctx context.Context, teamID string) ([]*store.TeamMembership, error) {
	rows, err := t.pool.Query(ctx,
		`SELECT user_id, team_id, role, joined_at FROM team_memberships WHERE team_id = $1`,
		teamID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []*store.TeamMembership
	for rows.Next() {
		m := &store.TeamMembership{}
		if err := rows.Scan(&m.UserID, &m.TeamID, &m.Role, &m.JoinedAt); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

func (t *pgTeamManagementStore) GetUserTeams(ctx context.Context, userID string) ([]*store.TeamMembership, error) {
	rows, err := t.pool.Query(ctx,
		`SELECT user_id, team_id, role, joined_at FROM team_memberships WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memberships []*store.TeamMembership
	for rows.Next() {
		m := &store.TeamMembership{}
		if err := rows.Scan(&m.UserID, &m.TeamID, &m.Role, &m.JoinedAt); err != nil {
			return nil, err
		}
		memberships = append(memberships, m)
	}
	return memberships, rows.Err()
}

func (t *pgTeamManagementStore) IsTeamMember(ctx context.Context, userID, teamSlug string) (bool, error) {
	var exists bool
	err := t.pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM team_memberships tm
			JOIN teams t ON t.id = tm.team_id
			WHERE tm.user_id = $1 AND t.slug = $2
		)`, userID, teamSlug,
	).Scan(&exists)
	return exists, err
}

func (t *pgTeamManagementStore) GetMemberRole(ctx context.Context, userID, teamID string) (string, error) {
	var role string
	err := t.pool.QueryRow(ctx,
		`SELECT role FROM team_memberships WHERE user_id = $1 AND team_id = $2`,
		userID, teamID,
	).Scan(&role)
	if err != nil {
		return "", fmt.Errorf("user is not a member of this team")
	}
	return role, nil
}

func scanTeam(row scannable) (*store.Team, error) {
	team := &store.Team{}
	err := row.Scan(&team.ID, &team.Name, &team.Slug, &team.SchemaName, &team.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to scan team: %w", err)
	}
	return team, nil
}

