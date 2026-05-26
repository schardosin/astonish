package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/schardosin/astonish/pkg/store"
)

// sqliteOIDCProviderStore implements store.OIDCProviderStore.
type sqliteOIDCProviderStore struct {
	db *sql.DB
}

func (s *sqliteOIDCProviderStore) Create(ctx context.Context, provider *store.OIDCProvider) error {
	if provider.ID == "" {
		provider.ID = uuid.New().String()
	}
	scopesJSON, _ := json.Marshal(provider.Scopes)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO oidc_providers (id, org_id, name, issuer_url, discovery_url, client_id, client_secret, scopes, team_claim, enabled, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		provider.ID, nilStr(provider.OrgID), provider.Name, provider.IssuerURL,
		nilStr(provider.DiscoveryURL), provider.ClientID, provider.ClientSecret,
		string(scopesJSON), nilStr(provider.TeamClaim), boolToInt(provider.Enabled),
		formatTime(time.Now()))
	return err
}

func (s *sqliteOIDCProviderStore) GetByID(ctx context.Context, id string) (*store.OIDCProvider, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, name, issuer_url, discovery_url, client_id, client_secret, scopes, team_claim, enabled, created_at
		 FROM oidc_providers WHERE id = ?`, id)
	return scanOIDCRow(row)
}

func (s *sqliteOIDCProviderStore) Update(ctx context.Context, provider *store.OIDCProvider) error {
	scopesJSON, _ := json.Marshal(provider.Scopes)
	_, err := s.db.ExecContext(ctx,
		`UPDATE oidc_providers SET org_id = ?, name = ?, issuer_url = ?, discovery_url = ?,
		 client_id = ?, client_secret = ?, scopes = ?, team_claim = ?, enabled = ?
		 WHERE id = ?`,
		nilStr(provider.OrgID), provider.Name, provider.IssuerURL,
		nilStr(provider.DiscoveryURL), provider.ClientID, provider.ClientSecret,
		string(scopesJSON), nilStr(provider.TeamClaim), boolToInt(provider.Enabled),
		provider.ID)
	return err
}

func (s *sqliteOIDCProviderStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM oidc_providers WHERE id = ?`, id)
	return err
}

func (s *sqliteOIDCProviderStore) List(ctx context.Context, orgID string) ([]*store.OIDCProvider, error) {
	var rows *sql.Rows
	var err error
	if orgID == "*" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, org_id, name, issuer_url, discovery_url, client_id, client_secret, scopes, team_claim, enabled, created_at
			 FROM oidc_providers ORDER BY name`)
	} else if orgID == "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, org_id, name, issuer_url, discovery_url, client_id, client_secret, scopes, team_claim, enabled, created_at
			 FROM oidc_providers WHERE org_id IS NULL ORDER BY name`)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, org_id, name, issuer_url, discovery_url, client_id, client_secret, scopes, team_claim, enabled, created_at
			 FROM oidc_providers WHERE org_id = ? OR org_id IS NULL ORDER BY name`, orgID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []*store.OIDCProvider
	for rows.Next() {
		p, err := scanOIDCRows(rows)
		if err != nil {
			return nil, err
		}
		providers = append(providers, p)
	}
	return providers, rows.Err()
}

func (s *sqliteOIDCProviderStore) ListEnabled(ctx context.Context, orgID string) ([]*store.OIDCProvider, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, name, issuer_url, discovery_url, client_id, client_secret, scopes, team_claim, enabled, created_at
		 FROM oidc_providers WHERE enabled = 1 AND (org_id = ? OR org_id IS NULL) ORDER BY name`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []*store.OIDCProvider
	for rows.Next() {
		p, err := scanOIDCRows(rows)
		if err != nil {
			return nil, err
		}
		providers = append(providers, p)
	}
	return providers, rows.Err()
}

func (s *sqliteOIDCProviderStore) GetByIssuer(ctx context.Context, issuerURL string) (*store.OIDCProvider, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, name, issuer_url, discovery_url, client_id, client_secret, scopes, team_claim, enabled, created_at
		 FROM oidc_providers WHERE issuer_url = ? AND enabled = 1 LIMIT 1`, issuerURL)
	return scanOIDCRow(row)
}

func scanOIDCRow(row *sql.Row) (*store.OIDCProvider, error) {
	p := &store.OIDCProvider{}
	var orgID, discoveryURL, scopes, teamClaim sql.NullString
	var enabled int
	var createdAt string
	err := row.Scan(&p.ID, &orgID, &p.Name, &p.IssuerURL, &discoveryURL,
		&p.ClientID, &p.ClientSecret, &scopes, &teamClaim, &enabled, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.OrgID = orgID.String
	p.DiscoveryURL = discoveryURL.String
	p.TeamClaim = teamClaim.String
	p.Enabled = enabled != 0
	p.CreatedAt = parseTime(createdAt)
	if scopes.Valid && scopes.String != "" {
		_ = json.Unmarshal([]byte(scopes.String), &p.Scopes)
	}
	return p, nil
}

func scanOIDCRows(rows *sql.Rows) (*store.OIDCProvider, error) {
	p := &store.OIDCProvider{}
	var orgID, discoveryURL, scopes, teamClaim sql.NullString
	var enabled int
	var createdAt string
	err := rows.Scan(&p.ID, &orgID, &p.Name, &p.IssuerURL, &discoveryURL,
		&p.ClientID, &p.ClientSecret, &scopes, &teamClaim, &enabled, &createdAt)
	if err != nil {
		return nil, err
	}
	p.OrgID = orgID.String
	p.DiscoveryURL = discoveryURL.String
	p.TeamClaim = teamClaim.String
	p.Enabled = enabled != 0
	p.CreatedAt = parseTime(createdAt)
	if scopes.Valid && scopes.String != "" {
		_ = json.Unmarshal([]byte(scopes.String), &p.Scopes)
	}
	return p, nil
}

// sqliteUserChannelStore implements store.UserChannelStore.
type sqliteUserChannelStore struct {
	db *sql.DB
}

func (s *sqliteUserChannelStore) Link(ctx context.Context, ch *store.UserChannel) error {
	if ch.ID == "" {
		ch.ID = uuid.New().String()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_channels (id, user_id, channel_type, external_id, display_name, enabled, verified, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ch.ID, ch.UserID, ch.ChannelType, ch.ExternalID, nilStr(ch.DisplayName),
		boolToInt(ch.Enabled), boolToInt(ch.Verified), formatTime(time.Now()))
	return err
}

func (s *sqliteUserChannelStore) Unlink(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_channels WHERE id = ?`, id)
	return err
}

func (s *sqliteUserChannelStore) GetByID(ctx context.Context, id string) (*store.UserChannel, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, channel_type, external_id, display_name, enabled, verified, verified_at, created_at
		 FROM user_channels WHERE id = ?`, id)
	return scanChannelRow(row)
}

func (s *sqliteUserChannelStore) GetByExternalID(ctx context.Context, channelType, externalID string) (*store.UserChannel, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, channel_type, external_id, display_name, enabled, verified, verified_at, created_at
		 FROM user_channels WHERE channel_type = ? AND external_id = ?`, channelType, externalID)
	return scanChannelRow(row)
}

func (s *sqliteUserChannelStore) ListByUser(ctx context.Context, userID string) ([]*store.UserChannel, error) {
	return s.queryChannels(ctx,
		`SELECT id, user_id, channel_type, external_id, display_name, enabled, verified, verified_at, created_at
		 FROM user_channels WHERE user_id = ? ORDER BY created_at`, userID)
}

func (s *sqliteUserChannelStore) ListByChannelType(ctx context.Context, channelType string) ([]*store.UserChannel, error) {
	return s.queryChannels(ctx,
		`SELECT id, user_id, channel_type, external_id, display_name, enabled, verified, verified_at, created_at
		 FROM user_channels WHERE channel_type = ? AND verified = 1 AND enabled = 1 ORDER BY created_at`, channelType)
}

func (s *sqliteUserChannelStore) ListByUsers(ctx context.Context, userIDs []string, channelType string) ([]*store.UserChannel, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	query := `SELECT id, user_id, channel_type, external_id, display_name, enabled, verified, verified_at, created_at
		 FROM user_channels WHERE channel_type = ? AND user_id IN (`
	args := []interface{}{channelType}
	for i, uid := range userIDs {
		if i > 0 {
			query += ","
		}
		query += "?"
		args = append(args, uid)
	}
	query += `) ORDER BY created_at`
	return s.queryChannels(ctx, query, args...)
}

func (s *sqliteUserChannelStore) Update(ctx context.Context, ch *store.UserChannel) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE user_channels SET display_name = ?, enabled = ? WHERE id = ?`,
		nilStr(ch.DisplayName), boolToInt(ch.Enabled), ch.ID)
	return err
}

func (s *sqliteUserChannelStore) Verify(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE user_channels SET verified = 1, verified_at = ? WHERE id = ?`,
		formatTime(time.Now()), id)
	return err
}

func (s *sqliteUserChannelStore) queryChannels(ctx context.Context, query string, args ...interface{}) ([]*store.UserChannel, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []*store.UserChannel
	for rows.Next() {
		ch := &store.UserChannel{}
		var displayName, verifiedAt sql.NullString
		var enabled, verified int
		var createdAt string
		if err := rows.Scan(&ch.ID, &ch.UserID, &ch.ChannelType, &ch.ExternalID,
			&displayName, &enabled, &verified, &verifiedAt, &createdAt); err != nil {
			return nil, err
		}
		ch.DisplayName = displayName.String
		ch.Enabled = enabled != 0
		ch.Verified = verified != 0
		if verifiedAt.Valid {
			t := parseTime(verifiedAt.String)
			ch.VerifiedAt = &t
		}
		ch.CreatedAt = parseTime(createdAt)
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

func scanChannelRow(row *sql.Row) (*store.UserChannel, error) {
	ch := &store.UserChannel{}
	var displayName, verifiedAt sql.NullString
	var enabled, verified int
	var createdAt string
	err := row.Scan(&ch.ID, &ch.UserID, &ch.ChannelType, &ch.ExternalID,
		&displayName, &enabled, &verified, &verifiedAt, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ch.DisplayName = displayName.String
	ch.Enabled = enabled != 0
	ch.Verified = verified != 0
	if verifiedAt.Valid {
		t := parseTime(verifiedAt.String)
		ch.VerifiedAt = &t
	}
	ch.CreatedAt = parseTime(createdAt)
	return ch, nil
}
