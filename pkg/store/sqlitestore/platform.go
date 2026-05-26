package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/schardosin/astonish/pkg/store"
)

// --- UserStore ---

type sqliteUserStore struct {
	db *sql.DB
}

func (s *sqliteUserStore) Create(ctx context.Context, user *store.User) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, email, display_name, password_hash, oidc_subject, oidc_issuer, platform_role, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID, user.Email, user.DisplayName, nilStr(user.PasswordHash),
		nilStr(user.OIDCSubject), nilStr(user.OIDCIssuer),
		coalesce(user.PlatformRole, "member"),
		coalesce(user.Status, "active"), formatTime(user.CreatedAt),
	)
	return err
}

func (s *sqliteUserStore) GetByID(ctx context.Context, id string) (*store.User, error) {
	return scanUserRow(s.db.QueryRowContext(ctx,
		`SELECT id, email, display_name, password_hash, oidc_subject, oidc_issuer, platform_role, status, created_at, last_login_at
		 FROM users WHERE id = ?`, id))
}

func (s *sqliteUserStore) GetByEmail(ctx context.Context, email string) (*store.User, error) {
	return scanUserRow(s.db.QueryRowContext(ctx,
		`SELECT id, email, display_name, password_hash, oidc_subject, oidc_issuer, platform_role, status, created_at, last_login_at
		 FROM users WHERE email = ?`, email))
}

func (s *sqliteUserStore) GetByOIDC(ctx context.Context, issuer, subject string) (*store.User, error) {
	return scanUserRow(s.db.QueryRowContext(ctx,
		`SELECT id, email, display_name, password_hash, oidc_subject, oidc_issuer, platform_role, status, created_at, last_login_at
		 FROM users WHERE oidc_issuer = ? AND oidc_subject = ?`, issuer, subject))
}

func (s *sqliteUserStore) Update(ctx context.Context, user *store.User) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET email = ?, display_name = ?, password_hash = ?,
		 oidc_subject = ?, oidc_issuer = ?, platform_role = ?, status = ?, last_login_at = ?
		 WHERE id = ?`,
		user.Email, user.DisplayName, nilStr(user.PasswordHash),
		nilStr(user.OIDCSubject), nilStr(user.OIDCIssuer),
		coalesce(user.PlatformRole, "member"),
		user.Status, nilTime(user.LastLoginAt),
		user.ID,
	)
	return err
}

func (s *sqliteUserStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}

func (s *sqliteUserStore) List(ctx context.Context) ([]*store.User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, email, display_name, password_hash, oidc_subject, oidc_issuer, platform_role, status, created_at, last_login_at
		 FROM users ORDER BY email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*store.User
	for rows.Next() {
		u, err := scanUserRows(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *sqliteUserStore) ListByOrg(ctx context.Context, orgID string) ([]*store.UserWithRole, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT u.id, u.email, u.display_name, u.password_hash, u.oidc_subject, u.oidc_issuer,
		        u.platform_role, u.status, u.created_at, u.last_login_at, om.role, om.joined_at
		 FROM users u
		 JOIN org_memberships om ON om.user_id = u.id
		 WHERE om.org_id = ?
		 ORDER BY u.email`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*store.UserWithRole
	for rows.Next() {
		uw := &store.UserWithRole{}
		var pwHash, oidcSub, oidcIss, platformRole, lastLogin, joinedAt sql.NullString
		var createdAt string
		err := rows.Scan(
			&uw.ID, &uw.Email, &uw.DisplayName, &pwHash, &oidcSub, &oidcIss,
			&platformRole, &uw.Status, &createdAt, &lastLogin, &uw.Role, &joinedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan user with role: %w", err)
		}
		uw.CreatedAt = parseTime(createdAt)
		uw.PasswordHash = pwHash.String
		uw.OIDCSubject = oidcSub.String
		uw.OIDCIssuer = oidcIss.String
		uw.PlatformRole = platformRole.String
		if lastLogin.Valid {
			uw.LastLoginAt = parseTime(lastLogin.String)
		}
		if joinedAt.Valid {
			uw.JoinedAt = parseTime(joinedAt.String)
		}
		users = append(users, uw)
	}
	return users, rows.Err()
}

func (s *sqliteUserStore) SetPlatformRole(ctx context.Context, userID, role string) error {
	if role == "" {
		role = "member"
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET platform_role = ? WHERE id = ?`, role, userID)
	return err
}

func (s *sqliteUserStore) CountByPlatformRole(ctx context.Context, role string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE platform_role = ?`, role).Scan(&count)
	return count, err
}

// --- OrganizationStore ---

type sqliteOrgStore struct {
	db *sql.DB
}

func (s *sqliteOrgStore) Create(ctx context.Context, org *store.Organization) error {
	status := org.Status
	if status == "" {
		status = "active"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO organizations (id, name, slug, status, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		org.ID, org.Name, org.Slug, status, formatTime(org.CreatedAt))
	return err
}

func (s *sqliteOrgStore) GetByID(ctx context.Context, id string) (*store.Organization, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, slug, status, created_at FROM organizations WHERE id = ?`, id)
	return scanOrgRow(row)
}

func (s *sqliteOrgStore) GetBySlug(ctx context.Context, slug string) (*store.Organization, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, slug, status, created_at FROM organizations WHERE slug = ?`, slug)
	return scanOrgRow(row)
}

func (s *sqliteOrgStore) List(ctx context.Context) ([]*store.Organization, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, slug, status, created_at FROM organizations ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orgs []*store.Organization
	for rows.Next() {
		o := &store.Organization{}
		var createdAt string
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.Status, &createdAt); err != nil {
			return nil, err
		}
		o.CreatedAt = parseTime(createdAt)
		orgs = append(orgs, o)
	}
	return orgs, rows.Err()
}

func (s *sqliteOrgStore) Update(ctx context.Context, org *store.Organization) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE organizations SET name = ?, slug = ?, status = ? WHERE id = ?`,
		org.Name, org.Slug, org.Status, org.ID)
	return err
}

func (s *sqliteOrgStore) Count(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM organizations`).Scan(&count)
	return count, err
}

func (s *sqliteOrgStore) AddMember(ctx context.Context, userID, orgID, role string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO org_memberships (user_id, org_id, role, joined_at)
		 VALUES (?, ?, ?, ?)`, userID, orgID, role, formatTime(time.Now()))
	return err
}

func (s *sqliteOrgStore) RemoveMember(ctx context.Context, userID, orgID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM org_memberships WHERE user_id = ? AND org_id = ?`, userID, orgID)
	return err
}

func (s *sqliteOrgStore) GetUserOrgs(ctx context.Context, userID string) ([]*store.OrgMembership, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT om.user_id, om.org_id, o.slug, o.name, om.role, om.joined_at
		 FROM org_memberships om
		 JOIN organizations o ON o.id = om.org_id
		 WHERE om.user_id = ?
		 ORDER BY o.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memberships []*store.OrgMembership
	for rows.Next() {
		m := &store.OrgMembership{}
		var joinedAt string
		if err := rows.Scan(&m.UserID, &m.OrgID, &m.OrgSlug, &m.OrgName, &m.Role, &joinedAt); err != nil {
			return nil, err
		}
		m.JoinedAt = parseTime(joinedAt)
		memberships = append(memberships, m)
	}
	return memberships, rows.Err()
}

func (s *sqliteOrgStore) GetMemberRole(ctx context.Context, userID, orgID string) (string, error) {
	var role string
	err := s.db.QueryRowContext(ctx,
		`SELECT role FROM org_memberships WHERE user_id = ? AND org_id = ?`,
		userID, orgID).Scan(&role)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return role, err
}

func (s *sqliteOrgStore) ListMembers(ctx context.Context, orgID string) ([]*store.UserWithRole, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT u.id, u.email, u.display_name, u.password_hash, u.oidc_subject, u.oidc_issuer,
		        u.platform_role, u.status, u.created_at, u.last_login_at, om.role, om.joined_at
		 FROM users u
		 JOIN org_memberships om ON om.user_id = u.id
		 WHERE om.org_id = ?
		 ORDER BY u.email`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*store.UserWithRole
	for rows.Next() {
		uw := &store.UserWithRole{}
		var pwHash, oidcSub, oidcIss, platformRole, lastLogin, joinedAt sql.NullString
		var createdAt string
		err := rows.Scan(
			&uw.ID, &uw.Email, &uw.DisplayName, &pwHash, &oidcSub, &oidcIss,
			&platformRole, &uw.Status, &createdAt, &lastLogin, &uw.Role, &joinedAt,
		)
		if err != nil {
			return nil, err
		}
		uw.CreatedAt = parseTime(createdAt)
		uw.PasswordHash = pwHash.String
		uw.OIDCSubject = oidcSub.String
		uw.OIDCIssuer = oidcIss.String
		uw.PlatformRole = platformRole.String
		if lastLogin.Valid {
			uw.LastLoginAt = parseTime(lastLogin.String)
		}
		if joinedAt.Valid {
			uw.JoinedAt = parseTime(joinedAt.String)
		}
		users = append(users, uw)
	}
	return users, rows.Err()
}

// --- LoginSessionStore ---

type sqliteLoginSessionStore struct {
	db *sql.DB
}

func (s *sqliteLoginSessionStore) Create(ctx context.Context, session *store.LoginSession) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO login_sessions (token_hash, user_id, org_id, created_at, expires_at, user_agent, ip_address)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		session.TokenHash, session.UserID, session.OrgID,
		formatTime(session.CreatedAt), formatTime(session.ExpiresAt),
		nilStr(session.UserAgent), nilStr(session.IPAddress))
	return err
}

func (s *sqliteLoginSessionStore) Validate(ctx context.Context, tokenHash string) (*store.LoginSession, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT token_hash, user_id, org_id, created_at, expires_at, user_agent, ip_address
		 FROM login_sessions WHERE token_hash = ? AND expires_at > ?`,
		tokenHash, formatTime(time.Now()))

	session := &store.LoginSession{}
	var createdAt, expiresAt string
	var userAgent, ipAddr sql.NullString
	err := row.Scan(&session.TokenHash, &session.UserID, &session.OrgID,
		&createdAt, &expiresAt, &userAgent, &ipAddr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	session.CreatedAt = parseTime(createdAt)
	session.ExpiresAt = parseTime(expiresAt)
	session.UserAgent = userAgent.String
	session.IPAddress = ipAddr.String
	return session, nil
}

func (s *sqliteLoginSessionStore) Delete(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM login_sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func (s *sqliteLoginSessionStore) DeleteExpired(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM login_sessions WHERE expires_at <= ?`, formatTime(time.Now()))
	return err
}

// --- helpers ---

func scanUserRow(row *sql.Row) (*store.User, error) {
	u := &store.User{}
	var pwHash, oidcSub, oidcIss, platformRole, lastLogin sql.NullString
	var createdAt string
	err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &pwHash, &oidcSub, &oidcIss,
		&platformRole, &u.Status, &createdAt, &lastLogin)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.PasswordHash = pwHash.String
	u.OIDCSubject = oidcSub.String
	u.OIDCIssuer = oidcIss.String
	u.PlatformRole = platformRole.String
	u.CreatedAt = parseTime(createdAt)
	if lastLogin.Valid {
		u.LastLoginAt = parseTime(lastLogin.String)
	}
	return u, nil
}

func scanUserRows(rows *sql.Rows) (*store.User, error) {
	u := &store.User{}
	var pwHash, oidcSub, oidcIss, platformRole, lastLogin sql.NullString
	var createdAt string
	err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &pwHash, &oidcSub, &oidcIss,
		&platformRole, &u.Status, &createdAt, &lastLogin)
	if err != nil {
		return nil, err
	}
	u.PasswordHash = pwHash.String
	u.OIDCSubject = oidcSub.String
	u.OIDCIssuer = oidcIss.String
	u.PlatformRole = platformRole.String
	u.CreatedAt = parseTime(createdAt)
	if lastLogin.Valid {
		u.LastLoginAt = parseTime(lastLogin.String)
	}
	return u, nil
}

func scanOrgRow(row *sql.Row) (*store.Organization, error) {
	o := &store.Organization{}
	var createdAt string
	err := row.Scan(&o.ID, &o.Name, &o.Slug, &o.Status, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	o.CreatedAt = parseTime(createdAt)
	return o, nil
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	// Try RFC3339 first, then SQLite datetime format
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, _ = time.Parse("2006-01-02 15:04:05", s)
	}
	return t
}

func nilStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nilTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return formatTime(t)
}

func coalesce(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
