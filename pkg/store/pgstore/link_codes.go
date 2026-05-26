package pgstore

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/store"
)

// PGLinkCodeStore manages pending link codes in the platform database.
type PGLinkCodeStore struct {
	pool *pgxpool.Pool
}

// NewPGLinkCodeStore creates a new PG-backed link code store.
func NewPGLinkCodeStore(pool *pgxpool.Pool) *PGLinkCodeStore {
	return &PGLinkCodeStore{pool: pool}
}

// Generate creates a new link code, removing any existing code for the same user+channel.
func (s *PGLinkCodeStore) Generate(ctx context.Context, code, userID, email, channel string) error {
	// Remove any existing pending code for this user+channel combo
	_, _ = s.pool.Exec(ctx,
		`DELETE FROM pending_link_codes WHERE user_id = $1 AND channel = $2`,
		userID, channel,
	)

	_, err := s.pool.Exec(ctx,
		`INSERT INTO pending_link_codes (code, user_id, email, channel, created_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		code, userID, email, channel, time.Now(), time.Now().Add(5*time.Minute),
	)
	return err
}

// Consume looks up a code, returns the pending link if valid, and removes it.
// Returns nil if the code is invalid or expired.
func (s *PGLinkCodeStore) Consume(ctx context.Context, code string) (*store.LinkCode, error) {
	var link store.LinkCode
	err := s.pool.QueryRow(ctx,
		`DELETE FROM pending_link_codes WHERE code = $1 AND expires_at > now()
		 RETURNING code, user_id, email, channel, created_at, expires_at`, code,
	).Scan(&link.Code, &link.UserID, &link.Email, &link.Channel, &link.CreatedAt, &link.ExpiresAt)
	if err != nil {
		return nil, nil // not found or expired
	}
	return &link, nil
}

// Cleanup removes expired link codes.
func (s *PGLinkCodeStore) Cleanup(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM pending_link_codes WHERE expires_at < now()`)
	return err
}

// Compile-time interface check
var _ store.LinkCodeStore = (*PGLinkCodeStore)(nil)
