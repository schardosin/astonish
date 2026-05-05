package pgstore

import (
	"context"
	"fmt"

	"github.com/schardosin/astonish/pkg/store"
)

// pgUserStore implements store.UserStore using the platform database.
type pgUserStore struct {
	poolMgr *PoolManager
}

func (s *pgUserStore) Create(ctx context.Context, user *store.User) error {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO users (id, email, display_name, password_hash, oidc_subject, oidc_issuer, platform_role, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		user.ID, user.Email, user.DisplayName, user.PasswordHash,
		nilIfEmpty(user.OIDCSubject), nilIfEmpty(user.OIDCIssuer),
		nilIfEmpty(user.PlatformRole),
		user.Status, user.CreatedAt,
	)
	return err
}

func (s *pgUserStore) GetByID(ctx context.Context, id string) (*store.User, error) {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return nil, err
	}
	return scanUser(pool.QueryRow(ctx,
		`SELECT id, email, display_name, password_hash, oidc_subject, oidc_issuer, platform_role, status, created_at, last_login_at
		 FROM users WHERE id = $1`, id,
	))
}

func (s *pgUserStore) GetByEmail(ctx context.Context, email string) (*store.User, error) {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return nil, err
	}
	return scanUser(pool.QueryRow(ctx,
		`SELECT id, email, display_name, password_hash, oidc_subject, oidc_issuer, platform_role, status, created_at, last_login_at
		 FROM users WHERE email = $1`, email,
	))
}

func (s *pgUserStore) GetByOIDC(ctx context.Context, issuer, subject string) (*store.User, error) {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return nil, err
	}
	return scanUser(pool.QueryRow(ctx,
		`SELECT id, email, display_name, password_hash, oidc_subject, oidc_issuer, platform_role, status, created_at, last_login_at
		 FROM users WHERE oidc_issuer = $1 AND oidc_subject = $2`, issuer, subject,
	))
}

func (s *pgUserStore) Update(ctx context.Context, user *store.User) error {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`UPDATE users SET email = $2, display_name = $3, password_hash = $4,
		 oidc_subject = $5, oidc_issuer = $6, platform_role = $7, status = $8, last_login_at = $9
		 WHERE id = $1`,
		user.ID, user.Email, user.DisplayName, user.PasswordHash,
		nilIfEmpty(user.OIDCSubject), nilIfEmpty(user.OIDCIssuer),
		nilIfEmpty(user.PlatformRole),
		user.Status, user.LastLoginAt,
	)
	return err
}

func (s *pgUserStore) Delete(ctx context.Context, id string) error {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	return err
}

func (s *pgUserStore) List(ctx context.Context) ([]*store.User, error) {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx,
		`SELECT id, email, display_name, password_hash, oidc_subject, oidc_issuer, platform_role, status, created_at, last_login_at
		 FROM users ORDER BY email`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*store.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *pgUserStore) ListByOrg(ctx context.Context, orgID string) ([]*store.UserWithRole, error) {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx,
		`SELECT u.id, u.email, u.display_name, u.password_hash, u.oidc_subject, u.oidc_issuer,
		        u.platform_role, u.status, u.created_at, u.last_login_at, om.role, om.joined_at
		 FROM users u
		 JOIN org_memberships om ON om.user_id = u.id
		 WHERE om.org_id = $1
		 ORDER BY u.email`, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*store.UserWithRole
	for rows.Next() {
		uw := &store.UserWithRole{}
		var pwHash, oidcSub, oidcIss, platformRole *string
		var lastLogin *interface{}
		err := rows.Scan(
			&uw.ID, &uw.Email, &uw.DisplayName, &pwHash, &oidcSub, &oidcIss,
			&platformRole, &uw.Status, &uw.CreatedAt, &lastLogin, &uw.Role, &uw.JoinedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user with role: %w", err)
		}
		if pwHash != nil {
			uw.PasswordHash = *pwHash
		}
		if oidcSub != nil {
			uw.OIDCSubject = *oidcSub
		}
		if oidcIss != nil {
			uw.OIDCIssuer = *oidcIss
		}
		if platformRole != nil {
			uw.PlatformRole = *platformRole
		}
		users = append(users, uw)
	}
	return users, rows.Err()
}

func (s *pgUserStore) SetPlatformRole(ctx context.Context, userID, role string) error {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`UPDATE users SET platform_role = $2 WHERE id = $1`,
		userID, nilIfEmpty(role),
	)
	return err
}

func (s *pgUserStore) CountByPlatformRole(ctx context.Context, role string) (int, error) {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return 0, err
	}
	var count int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM users WHERE platform_role = $1`, role,
	).Scan(&count)
	return count, err
}

// --- pgOrgStore implements store.OrganizationStore ---

type pgOrgStore struct {
	poolMgr *PoolManager
}

func (s *pgOrgStore) Create(ctx context.Context, org *store.Organization) error {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO organizations (id, name, slug, db_name, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		org.ID, org.Name, org.Slug, org.DBName, org.Status, org.CreatedAt,
	)
	return err
}

func (s *pgOrgStore) GetByID(ctx context.Context, id string) (*store.Organization, error) {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return nil, err
	}
	return scanOrg(pool.QueryRow(ctx,
		`SELECT id, name, slug, db_name, status, created_at
		 FROM organizations WHERE id = $1`, id,
	))
}

func (s *pgOrgStore) GetBySlug(ctx context.Context, slug string) (*store.Organization, error) {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return nil, err
	}
	return scanOrg(pool.QueryRow(ctx,
		`SELECT id, name, slug, db_name, status, created_at
		 FROM organizations WHERE slug = $1`, slug,
	))
}

func (s *pgOrgStore) List(ctx context.Context) ([]*store.Organization, error) {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx,
		`SELECT id, name, slug, db_name, status, created_at
		 FROM organizations ORDER BY name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orgs []*store.Organization
	for rows.Next() {
		org, err := scanOrg(rows)
		if err != nil {
			return nil, err
		}
		orgs = append(orgs, org)
	}
	return orgs, rows.Err()
}

func (s *pgOrgStore) Update(ctx context.Context, org *store.Organization) error {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`UPDATE organizations SET name = $2, slug = $3, status = $4
		 WHERE id = $1`,
		org.ID, org.Name, org.Slug, org.Status,
	)
	return err
}

func (s *pgOrgStore) Count(ctx context.Context) (int, error) {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return 0, err
	}
	var count int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM organizations`).Scan(&count)
	return count, err
}

func (s *pgOrgStore) AddMember(ctx context.Context, userID, orgID, role string) error {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO org_memberships (user_id, org_id, role, joined_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (user_id, org_id) DO UPDATE SET role = EXCLUDED.role`,
		userID, orgID, role,
	)
	return err
}

func (s *pgOrgStore) RemoveMember(ctx context.Context, userID, orgID string) error {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`DELETE FROM org_memberships WHERE user_id = $1 AND org_id = $2`,
		userID, orgID,
	)
	return err
}

func (s *pgOrgStore) GetUserOrgs(ctx context.Context, userID string) ([]*store.OrgMembership, error) {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx,
		`SELECT om.user_id, om.org_id, o.slug, o.name, om.role, om.joined_at
		 FROM org_memberships om
		 JOIN organizations o ON o.id = om.org_id
		 WHERE om.user_id = $1
		 ORDER BY om.joined_at`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memberships []*store.OrgMembership
	for rows.Next() {
		m := &store.OrgMembership{}
		if err := rows.Scan(&m.UserID, &m.OrgID, &m.OrgSlug, &m.OrgName, &m.Role, &m.JoinedAt); err != nil {
			return nil, err
		}
		memberships = append(memberships, m)
	}
	return memberships, rows.Err()
}

func (s *pgOrgStore) GetMemberRole(ctx context.Context, userID, orgID string) (string, error) {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return "", err
	}
	var role string
	err = pool.QueryRow(ctx,
		`SELECT role FROM org_memberships WHERE user_id = $1 AND org_id = $2`,
		userID, orgID,
	).Scan(&role)
	if err != nil {
		return "", fmt.Errorf("user %s is not a member of org %s", userID, orgID)
	}
	return role, nil
}

func (s *pgOrgStore) ListMembers(ctx context.Context, orgID string) ([]*store.UserWithRole, error) {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx,
		`SELECT u.id, u.email, u.display_name, u.password_hash, u.oidc_subject, u.oidc_issuer,
		        u.status, u.created_at, u.last_login_at, om.role, om.joined_at
		 FROM users u
		 JOIN org_memberships om ON om.user_id = u.id
		 WHERE om.org_id = $1
		 ORDER BY u.email`, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*store.UserWithRole
	for rows.Next() {
		uw := &store.UserWithRole{}
		var pwHash, oidcSub, oidcIss *string
		var lastLogin *interface{}
		err := rows.Scan(
			&uw.ID, &uw.Email, &uw.DisplayName, &pwHash, &oidcSub, &oidcIss,
			&uw.Status, &uw.CreatedAt, &lastLogin, &uw.Role, &uw.JoinedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan org member: %w", err)
		}
		if pwHash != nil {
			uw.PasswordHash = *pwHash
		}
		if oidcSub != nil {
			uw.OIDCSubject = *oidcSub
		}
		if oidcIss != nil {
			uw.OIDCIssuer = *oidcIss
		}
		users = append(users, uw)
	}
	return users, rows.Err()
}

// --- pgLoginSessionStore implements store.LoginSessionStore ---

type pgLoginSessionStore struct {
	poolMgr *PoolManager
}

func (s *pgLoginSessionStore) Create(ctx context.Context, sess *store.LoginSession) error {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO login_sessions (token_hash, user_id, org_id, created_at, expires_at, user_agent, ip_address)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		sess.TokenHash, sess.UserID, sess.OrgID, sess.CreatedAt, sess.ExpiresAt,
		nilIfEmpty(sess.UserAgent), nilIfEmpty(sess.IPAddress),
	)
	return err
}

func (s *pgLoginSessionStore) Validate(ctx context.Context, tokenHash string) (*store.LoginSession, error) {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return nil, err
	}
	row := pool.QueryRow(ctx,
		`SELECT token_hash, user_id, org_id, created_at, expires_at, user_agent, ip_address
		 FROM login_sessions WHERE token_hash = $1 AND expires_at > now()`, tokenHash,
	)
	sess := &store.LoginSession{}
	var ua, ip *string
	err = row.Scan(&sess.TokenHash, &sess.UserID, &sess.OrgID, &sess.CreatedAt, &sess.ExpiresAt, &ua, &ip)
	if err != nil {
		return nil, err
	}
	if ua != nil {
		sess.UserAgent = *ua
	}
	if ip != nil {
		sess.IPAddress = *ip
	}
	return sess, nil
}

func (s *pgLoginSessionStore) Delete(ctx context.Context, tokenHash string) error {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `DELETE FROM login_sessions WHERE token_hash = $1`, tokenHash)
	return err
}

func (s *pgLoginSessionStore) DeleteExpired(ctx context.Context) error {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `DELETE FROM login_sessions WHERE expires_at < now()`)
	return err
}

// --- scan helpers ---

// scannable is an interface satisfied by both pgx.Row and pgx.Rows.
type scannable interface {
	Scan(dest ...any) error
}

func scanUser(row scannable) (*store.User, error) {
	u := &store.User{}
	var pwHash, oidcSub, oidcIss, platformRole *string
	var lastLogin *interface{}
	err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &pwHash, &oidcSub, &oidcIss, &platformRole, &u.Status, &u.CreatedAt, &lastLogin)
	if err != nil {
		return nil, fmt.Errorf("failed to scan user: %w", err)
	}
	if pwHash != nil {
		u.PasswordHash = *pwHash
	}
	if oidcSub != nil {
		u.OIDCSubject = *oidcSub
	}
	if oidcIss != nil {
		u.OIDCIssuer = *oidcIss
	}
	if platformRole != nil {
		u.PlatformRole = *platformRole
	}
	return u, nil
}

func scanOrg(row scannable) (*store.Organization, error) {
	o := &store.Organization{}
	err := row.Scan(&o.ID, &o.Name, &o.Slug, &o.DBName, &o.Status, &o.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to scan organization: %w", err)
	}
	return o, nil
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
