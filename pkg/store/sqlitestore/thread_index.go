package sqlitestore

import (
	"context"
	"database/sql"

	"github.com/schardosin/astonish/pkg/session"
)

// SQLiteThreadIndex implements session.ThreadIndexer backed by SQLite.
// Used for email thread-to-session mapping in platform mode.
type SQLiteThreadIndex struct {
	db *sql.DB
}

// NewSQLiteThreadIndex creates a DB-backed thread index.
func NewSQLiteThreadIndex(db *sql.DB) *SQLiteThreadIndex {
	return &SQLiteThreadIndex{db: db}
}

func (t *SQLiteThreadIndex) LookupChain(messageIDs []string) (string, bool) {
	if len(messageIDs) == 0 {
		return "", false
	}

	// Build query with placeholders
	query := `SELECT session_key FROM email_thread_index WHERE message_id IN (`
	args := make([]any, len(messageIDs))
	for i, id := range messageIDs {
		if i > 0 {
			query += ","
		}
		query += "?"
		args[i] = id
	}
	query += `) LIMIT 1`

	var sessionKey string
	err := t.db.QueryRowContext(context.Background(), query, args...).Scan(&sessionKey)
	if err != nil {
		return "", false
	}
	return sessionKey, true
}

func (t *SQLiteThreadIndex) Associate(messageIDs []string, sessionKey string) error {
	if len(messageIDs) == 0 {
		return nil
	}

	tx, err := t.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		`INSERT OR IGNORE INTO email_thread_index (message_id, session_key) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, id := range messageIDs {
		if _, err := stmt.Exec(id, sessionKey); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (t *SQLiteThreadIndex) RemoveSession(sessionKey string) error {
	_, err := t.db.ExecContext(context.Background(),
		`DELETE FROM email_thread_index WHERE session_key = ?`, sessionKey)
	return err
}

// Compile-time interface check
var _ session.ThreadIndexer = (*SQLiteThreadIndex)(nil)
