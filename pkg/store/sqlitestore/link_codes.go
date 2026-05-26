package sqlitestore

import (
	"context"
	"database/sql"
	"time"

	"github.com/schardosin/astonish/pkg/store"
)

// SQLiteLinkCodeStore manages pending link codes in the platform SQLite database.
type SQLiteLinkCodeStore struct {
	db *sql.DB
}

// NewSQLiteLinkCodeStore creates a new SQLite-backed link code store.
func NewSQLiteLinkCodeStore(db *sql.DB) *SQLiteLinkCodeStore {
	return &SQLiteLinkCodeStore{db: db}
}

// Generate creates a new link code, removing any existing code for the same user+channel.
func (s *SQLiteLinkCodeStore) Generate(ctx context.Context, code, userID, email, channel string) error {
	// Remove any existing pending code for this user+channel combo
	_, _ = s.db.ExecContext(ctx,
		`DELETE FROM pending_link_codes WHERE user_id = ? AND channel = ?`,
		userID, channel,
	)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pending_link_codes (code, user_id, email, channel, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		code, userID, email, channel,
		time.Now().UTC().Format(time.RFC3339),
		time.Now().Add(5*time.Minute).UTC().Format(time.RFC3339),
	)
	return err
}

// Consume looks up a code, returns the pending link if valid, and removes it.
// Returns (nil, nil) if the code is invalid or expired.
func (s *SQLiteLinkCodeStore) Consume(ctx context.Context, code string) (*store.LinkCode, error) {
	// First find the matching non-expired code
	var link store.LinkCode
	var createdStr, expiresStr string
	err := s.db.QueryRowContext(ctx,
		`SELECT code, user_id, email, channel, created_at, expires_at
		 FROM pending_link_codes
		 WHERE code = ? AND expires_at > datetime('now')`, code,
	).Scan(&link.Code, &link.UserID, &link.Email, &link.Channel, &createdStr, &expiresStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	// Parse timestamps
	if t, parseErr := time.Parse(time.RFC3339, createdStr); parseErr == nil {
		link.CreatedAt = t
	}
	if t, parseErr := time.Parse(time.RFC3339, expiresStr); parseErr == nil {
		link.ExpiresAt = t
	}

	// Delete after consuming
	_, _ = s.db.ExecContext(ctx, `DELETE FROM pending_link_codes WHERE code = ?`, code)

	return &link, nil
}

// Cleanup removes expired link codes.
func (s *SQLiteLinkCodeStore) Cleanup(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pending_link_codes WHERE expires_at < datetime('now')`)
	return err
}

// Compile-time interface check
var _ store.LinkCodeStore = (*SQLiteLinkCodeStore)(nil)
