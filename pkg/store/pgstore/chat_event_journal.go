package pgstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/store"
)

// pgChatEventJournal backs store.ChatEventJournal with team-schema tables:
// {schema}.chat_session_events (append-only log) and {schema}.sessions.last_seq
// (monotonic counter per chat session).
//
// Sequence allocation is serialized by a per-chat PG advisory lock held by
// the producer at a higher layer (see docs/architecture/sandbox-backends.md
// §5.14). This store assumes that serialization: it does a single
// UPDATE ... RETURNING to advance last_seq and an INSERT per event in the
// same transaction.
type pgChatEventJournal struct {
	pool   *pgxpool.Pool
	schema string
}

// NewPGChatEventJournal constructs a journal bound to a team schema.
func NewPGChatEventJournal(pool *pgxpool.Pool, schema string) store.ChatEventJournal {
	return &pgChatEventJournal{pool: pool, schema: schema}
}

func (j *pgChatEventJournal) sessionsTable() string {
	return pgx.Identifier{j.schema, "sessions"}.Sanitize()
}

func (j *pgChatEventJournal) eventsTable() string {
	return pgx.Identifier{j.schema, "chat_session_events"}.Sanitize()
}

// Append allocates a contiguous range of seq values for the chat session and
// inserts each event with its assigned seq. The whole operation runs in one
// transaction; if any INSERT fails the counter advance is rolled back.
func (j *pgChatEventJournal) Append(ctx context.Context, events []*store.ChatEvent) error {
	if len(events) == 0 {
		return nil
	}
	// All events must target the same chat session (this is the unit
	// serialized by the producer's advisory lock).
	chatID := events[0].ChatSessionID
	if chatID == "" {
		return errors.New("chat_session_id is required")
	}
	for i, e := range events[1:] {
		if e.ChatSessionID != chatID {
			return fmt.Errorf("mixed chat_session_id in Append batch at index %d: %q != %q",
				i+1, e.ChatSessionID, chatID)
		}
	}

	tx, err := j.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Advance last_seq by N and get the new high-water mark.
	var newLast int64
	err = tx.QueryRow(ctx,
		fmt.Sprintf(
			`UPDATE %s SET last_seq = last_seq + $2 WHERE id = $1 RETURNING last_seq`,
			j.sessionsTable(),
		),
		chatID, int64(len(events)),
	).Scan(&newLast)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("chat session %s not found", chatID)
		}
		return fmt.Errorf("advance last_seq: %w", err)
	}

	startSeq := newLast - int64(len(events)) + 1

	for i, e := range events {
		seq := startSeq + int64(i)
		e.Seq = seq
		if _, err := tx.Exec(ctx,
			fmt.Sprintf(
				`INSERT INTO %s (chat_session_id, seq, event_type, payload, producer_pod)
				 VALUES ($1, $2, $3, $4, $5)`,
				j.eventsTable(),
			),
			chatID, seq, e.EventType, e.Payload, e.ProducerPod,
		); err != nil {
			return fmt.Errorf("insert event at seq %d: %w", seq, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (j *pgChatEventJournal) ReadSince(ctx context.Context, chatSessionID string, afterSeq int64, limit int) ([]*store.ChatEvent, error) {
	if chatSessionID == "" {
		return nil, errors.New("chat_session_id is required")
	}
	query := fmt.Sprintf(
		`SELECT chat_session_id, seq, event_type, payload, producer_pod, created_at
		   FROM %s
		  WHERE chat_session_id = $1 AND seq > $2
		  ORDER BY seq ASC`,
		j.eventsTable(),
	)
	args := []any{chatSessionID, afterSeq}
	if limit > 0 {
		query += " LIMIT $3"
		args = append(args, limit)
	}
	rows, err := j.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*store.ChatEvent
	for rows.Next() {
		e := &store.ChatEvent{}
		if err := rows.Scan(
			&e.ChatSessionID, &e.Seq, &e.EventType, &e.Payload, &e.ProducerPod, &e.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (j *pgChatEventJournal) LastSeq(ctx context.Context, chatSessionID string) (int64, error) {
	var seq int64
	err := j.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT last_seq FROM %s WHERE id = $1`, j.sessionsTable()),
		chatSessionID,
	).Scan(&seq)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return seq, nil
}
