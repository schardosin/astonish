package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/schardosin/astonish/pkg/store"
)

// sqliteAuditStore implements store.AuditStore.
type sqliteAuditStore struct {
	db    *sql.DB
	table string // "org_audit_log" or "team_audit_log"
}

func (s *sqliteAuditStore) Log(ctx context.Context, entry *store.AuditEntry) error {
	var detailJSON []byte
	if entry.Detail != nil {
		detailJSON, _ = json.Marshal(entry.Detail)
	}

	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (timestamp, user_id, team_id, action, resource, detail, ip_address, session_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, s.table),
		formatTime(time.Now()), entry.UserID, nilStr(entry.TeamID),
		entry.Action, entry.Resource, nilStr(string(detailJSON)),
		nilStr(entry.IPAddress), nilStr(entry.SessionID))
	return err
}

func (s *sqliteAuditStore) Query(ctx context.Context, filter store.AuditFilter) ([]*store.AuditEntry, error) {
	query := fmt.Sprintf(`SELECT id, timestamp, user_id, action, resource, detail, ip_address, session_id FROM %s WHERE 1=1`, s.table)
	var args []interface{}

	if filter.UserID != "" {
		query += " AND user_id = ?"
		args = append(args, filter.UserID)
	}
	if filter.Action != "" {
		query += " AND action = ?"
		args = append(args, filter.Action)
	}
	if filter.Resource != "" {
		query += " AND resource = ?"
		args = append(args, filter.Resource)
	}
	if !filter.Since.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, formatTime(filter.Since))
	}
	if !filter.Until.IsZero() {
		query += " AND timestamp <= ?"
		args = append(args, formatTime(filter.Until))
	}

	query += " ORDER BY id DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*store.AuditEntry
	for rows.Next() {
		e := &store.AuditEntry{}
		var ts string
		var detail, ipAddr, sessionID sql.NullString
		if err := rows.Scan(&e.ID, &ts, &e.UserID, &e.Action, &e.Resource, &detail, &ipAddr, &sessionID); err != nil {
			return nil, err
		}
		e.Timestamp = parseTime(ts)
		e.IPAddress = ipAddr.String
		e.SessionID = sessionID.String
		if detail.Valid && detail.String != "" {
			var d interface{}
			_ = json.Unmarshal([]byte(detail.String), &d)
			e.Detail = d
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// sqliteTeamManagementStore implements store.TeamManagementStore.
type sqliteTeamManagementStore struct {
	db *sql.DB
}

func (s *sqliteTeamManagementStore) CreateTeam(ctx context.Context, team *store.Team) error {
	if team.ID == "" {
		team.ID = uuid.New().String()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO teams (id, name, slug, created_at) VALUES (?, ?, ?, ?)`,
		team.ID, team.Name, team.Slug, formatTime(team.CreatedAt))
	return err
}

func (s *sqliteTeamManagementStore) GetTeam(ctx context.Context, id string) (*store.Team, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, slug, created_at FROM teams WHERE id = ?`, id)
	return scanTeamRow(row)
}

func (s *sqliteTeamManagementStore) GetTeamBySlug(ctx context.Context, slug string) (*store.Team, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, slug, created_at FROM teams WHERE slug = ?`, slug)
	return scanTeamRow(row)
}

func (s *sqliteTeamManagementStore) ListTeams(ctx context.Context) ([]*store.Team, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, slug, created_at FROM teams ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var teams []*store.Team
	for rows.Next() {
		t := &store.Team{}
		var createdAt string
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &createdAt); err != nil {
			return nil, err
		}
		t.CreatedAt = parseTime(createdAt)
		teams = append(teams, t)
	}
	return teams, rows.Err()
}

func (s *sqliteTeamManagementStore) ListTeamsForUser(ctx context.Context, userID string) ([]*store.Team, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.id, t.name, t.slug, t.created_at
		 FROM teams t
		 JOIN team_memberships tm ON tm.team_id = t.id
		 WHERE tm.user_id = ?
		 ORDER BY t.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var teams []*store.Team
	for rows.Next() {
		t := &store.Team{}
		var createdAt string
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &createdAt); err != nil {
			return nil, err
		}
		t.CreatedAt = parseTime(createdAt)
		teams = append(teams, t)
	}
	return teams, rows.Err()
}

func (s *sqliteTeamManagementStore) DeleteTeam(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM teams WHERE id = ?`, id)
	return err
}

func (s *sqliteTeamManagementStore) AddMember(ctx context.Context, membership *store.TeamMembership) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO team_memberships (user_id, team_id, role, joined_at)
		 VALUES (?, ?, ?, ?)`,
		membership.UserID, membership.TeamID, membership.Role, formatTime(time.Now()))
	return err
}

func (s *sqliteTeamManagementStore) RemoveMember(ctx context.Context, userID, teamID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM team_memberships WHERE user_id = ? AND team_id = ?`, userID, teamID)
	return err
}

func (s *sqliteTeamManagementStore) SetRole(ctx context.Context, userID, teamID, role string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE team_memberships SET role = ? WHERE user_id = ? AND team_id = ?`,
		role, userID, teamID)
	return err
}

func (s *sqliteTeamManagementStore) ListMembers(ctx context.Context, teamID string) ([]*store.TeamMembership, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT tm.user_id, tm.team_id, tm.role, tm.joined_at
		 FROM team_memberships tm
		 WHERE tm.team_id = ?
		 ORDER BY tm.joined_at`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []*store.TeamMembership
	for rows.Next() {
		m := &store.TeamMembership{}
		var joinedAt string
		if err := rows.Scan(&m.UserID, &m.TeamID, &m.Role, &joinedAt); err != nil {
			return nil, err
		}
		m.JoinedAt = parseTime(joinedAt)
		members = append(members, m)
	}
	return members, rows.Err()
}

func (s *sqliteTeamManagementStore) GetUserTeams(ctx context.Context, userID string) ([]*store.TeamMembership, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id, team_id, role, joined_at FROM team_memberships WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memberships []*store.TeamMembership
	for rows.Next() {
		m := &store.TeamMembership{}
		var joinedAt string
		if err := rows.Scan(&m.UserID, &m.TeamID, &m.Role, &joinedAt); err != nil {
			return nil, err
		}
		m.JoinedAt = parseTime(joinedAt)
		memberships = append(memberships, m)
	}
	return memberships, rows.Err()
}

func (s *sqliteTeamManagementStore) IsTeamMember(ctx context.Context, userID, teamSlug string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM team_memberships tm
		 JOIN teams t ON t.id = tm.team_id
		 WHERE tm.user_id = ? AND t.slug = ?`, userID, teamSlug).Scan(&count)
	return count > 0, err
}

func (s *sqliteTeamManagementStore) GetMemberRole(ctx context.Context, userID, teamID string) (string, error) {
	var role string
	err := s.db.QueryRowContext(ctx,
		`SELECT role FROM team_memberships WHERE user_id = ? AND team_id = ?`,
		userID, teamID).Scan(&role)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return role, err
}

func scanTeamRow(row *sql.Row) (*store.Team, error) {
	t := &store.Team{}
	var createdAt string
	err := row.Scan(&t.ID, &t.Name, &t.Slug, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.CreatedAt = parseTime(createdAt)
	return t, nil
}
