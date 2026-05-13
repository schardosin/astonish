package pgstore

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/schardosin/astonish/pkg/store"
)

// pgUserChannelStore implements store.UserChannelStore using the platform database.
type pgUserChannelStore struct {
	poolMgr *PoolManager
}

func (s *pgUserChannelStore) Link(ctx context.Context, ch *store.UserChannel) error {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO user_channels (id, user_id, channel_type, external_id, display_name, enabled, verified, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		ch.ID, ch.UserID, ch.ChannelType, ch.ExternalID, ch.DisplayName,
		ch.Enabled, ch.Verified, ch.CreatedAt,
	)
	return err
}

func (s *pgUserChannelStore) Unlink(ctx context.Context, id string) error {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `DELETE FROM user_channels WHERE id = $1`, id)
	return err
}

func (s *pgUserChannelStore) GetByID(ctx context.Context, id string) (*store.UserChannel, error) {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return nil, err
	}
	return scanUserChannel(pool.QueryRow(ctx,
		`SELECT id, user_id, channel_type, external_id, display_name, enabled, verified, verified_at, created_at
		 FROM user_channels WHERE id = $1`, id,
	))
}

func (s *pgUserChannelStore) GetByExternalID(ctx context.Context, channelType, externalID string) (*store.UserChannel, error) {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return nil, err
	}
	ch, err := scanUserChannel(pool.QueryRow(ctx,
		`SELECT id, user_id, channel_type, external_id, display_name, enabled, verified, verified_at, created_at
		 FROM user_channels WHERE channel_type = $1 AND external_id = $2`, channelType, externalID,
	))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return ch, err
}

func (s *pgUserChannelStore) ListByUser(ctx context.Context, userID string) ([]*store.UserChannel, error) {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx,
		`SELECT id, user_id, channel_type, external_id, display_name, enabled, verified, verified_at, created_at
		 FROM user_channels WHERE user_id = $1 ORDER BY created_at`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectUserChannels(rows)
}

func (s *pgUserChannelStore) ListByChannelType(ctx context.Context, channelType string) ([]*store.UserChannel, error) {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx,
		`SELECT id, user_id, channel_type, external_id, display_name, enabled, verified, verified_at, created_at
		 FROM user_channels WHERE channel_type = $1 AND enabled = true AND verified = true
		 ORDER BY created_at`, channelType,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectUserChannels(rows)
}

func (s *pgUserChannelStore) ListByUsers(ctx context.Context, userIDs []string, channelType string) ([]*store.UserChannel, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx,
		`SELECT id, user_id, channel_type, external_id, display_name, enabled, verified, verified_at, created_at
		 FROM user_channels WHERE user_id = ANY($1) AND channel_type = $2 AND enabled = true AND verified = true
		 ORDER BY created_at`, userIDs, channelType,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectUserChannels(rows)
}

func (s *pgUserChannelStore) Update(ctx context.Context, ch *store.UserChannel) error {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`UPDATE user_channels SET display_name = $2, enabled = $3
		 WHERE id = $1`,
		ch.ID, ch.DisplayName, ch.Enabled,
	)
	return err
}

func (s *pgUserChannelStore) Verify(ctx context.Context, id string) error {
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	_, err = pool.Exec(ctx,
		`UPDATE user_channels SET verified = true, verified_at = $2 WHERE id = $1`,
		id, now,
	)
	return err
}

// scanUserChannel scans a single row into a UserChannel.
func scanUserChannel(row pgx.Row) (*store.UserChannel, error) {
	var ch store.UserChannel
	var verifiedAt *time.Time
	err := row.Scan(
		&ch.ID, &ch.UserID, &ch.ChannelType, &ch.ExternalID, &ch.DisplayName,
		&ch.Enabled, &ch.Verified, &verifiedAt, &ch.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	ch.VerifiedAt = verifiedAt
	return &ch, nil
}

// collectUserChannels collects rows into a slice of UserChannel.
func collectUserChannels(rows pgx.Rows) ([]*store.UserChannel, error) {
	var result []*store.UserChannel
	for rows.Next() {
		var ch store.UserChannel
		var verifiedAt *time.Time
		if err := rows.Scan(
			&ch.ID, &ch.UserID, &ch.ChannelType, &ch.ExternalID, &ch.DisplayName,
			&ch.Enabled, &ch.Verified, &verifiedAt, &ch.CreatedAt,
		); err != nil {
			return nil, err
		}
		ch.VerifiedAt = verifiedAt
		result = append(result, &ch)
	}
	return result, rows.Err()
}
