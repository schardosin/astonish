package pgstore

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/session"
)

// PGThreadIndex implements session.ThreadIndexer backed by PostgreSQL.
// Uses the platform-level email_thread_index table.
type PGThreadIndex struct {
	pool *pgxpool.Pool
}

// NewPGThreadIndex creates a DB-backed thread index.
func NewPGThreadIndex(pool *pgxpool.Pool) *PGThreadIndex {
	return &PGThreadIndex{pool: pool}
}

func (t *PGThreadIndex) LookupChain(messageIDs []string) (string, bool) {
	if len(messageIDs) == 0 {
		return "", false
	}

	ctx := context.Background()

	// Query for any matching Message-ID, ordered by the input priority
	// (first match wins, which respects In-Reply-To before References).
	for _, id := range messageIDs {
		var sessionKey string
		err := t.pool.QueryRow(ctx,
			`SELECT session_key FROM email_thread_index WHERE message_id = $1`, id,
		).Scan(&sessionKey)
		if err == nil {
			return sessionKey, true
		}
		if err != pgx.ErrNoRows {
			return "", false // actual error
		}
	}
	return "", false
}

func (t *PGThreadIndex) Associate(messageIDs []string, sessionKey string) error {
	if len(messageIDs) == 0 || sessionKey == "" {
		return nil
	}

	ctx := context.Background()

	for _, id := range messageIDs {
		if id == "" {
			continue
		}
		_, err := t.pool.Exec(ctx,
			`INSERT INTO email_thread_index (message_id, session_key)
			 VALUES ($1, $2)
			 ON CONFLICT (message_id) DO UPDATE SET session_key = $2`,
			id, sessionKey,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (t *PGThreadIndex) RemoveSession(sessionKey string) error {
	if sessionKey == "" {
		return nil
	}

	ctx := context.Background()
	_, err := t.pool.Exec(ctx,
		`DELETE FROM email_thread_index WHERE session_key = $1`, sessionKey,
	)
	return err
}

// Compile-time check
var _ session.ThreadIndexer = (*PGThreadIndex)(nil)
