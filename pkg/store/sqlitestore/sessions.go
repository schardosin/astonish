package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"iter"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	adksession "google.golang.org/adk/session"

	"github.com/schardosin/astonish/pkg/store"
)

// sqliteSessionStore implements store.SessionStore for SQLite.
type sqliteSessionStore struct {
	db *sql.DB

	mu       sync.RWMutex
	sessions map[string]*sqliteSession // id → session (in-memory cache)

	redactFn func(string) string
}

// --- ADK Session interface implementation ---

// sqliteSession implements adksession.Session.
type sqliteSession struct {
	id      string
	appName string
	userID  string

	mu        sync.RWMutex
	events    []*adksession.Event
	state     map[string]any
	updatedAt time.Time
}

func (s *sqliteSession) ID() string      { return s.id }
func (s *sqliteSession) AppName() string  { return s.appName }
func (s *sqliteSession) UserID() string   { return s.userID }
func (s *sqliteSession) State() adksession.State {
	return &sqliteSessionState{mu: &s.mu, state: s.state}
}
func (s *sqliteSession) Events() adksession.Events {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sqliteSessionEvents(s.events)
}
func (s *sqliteSession) LastUpdateTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.updatedAt
}

// appendEvent applies state delta and appends the event (caller must hold mu lock externally or use internal lock).
func (s *sqliteSession) appendEvent(event *adksession.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Apply state delta
	if event.Actions.StateDelta != nil {
		if s.state == nil {
			s.state = make(map[string]any)
		}
		maps.Copy(s.state, event.Actions.StateDelta)
	}

	// Remove temp keys from event's StateDelta before storing
	if len(event.Actions.StateDelta) > 0 {
		filtered := make(map[string]any)
		for k, v := range event.Actions.StateDelta {
			if !strings.HasPrefix(k, adksession.KeyPrefixTemp) {
				filtered[k] = v
			}
		}
		event.Actions.StateDelta = filtered
	}

	s.events = append(s.events, event)
	s.updatedAt = event.Timestamp
}

// --- State wrapper ---

type sqliteSessionState struct {
	mu    *sync.RWMutex
	state map[string]any
}

func (ss *sqliteSessionState) Get(key string) (any, error) {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	val, ok := ss.state[key]
	if !ok {
		return nil, adksession.ErrStateKeyNotExist
	}
	return val, nil
}

func (ss *sqliteSessionState) Set(key string, value any) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.state[key] = value
	return nil
}

func (ss *sqliteSessionState) All() iter.Seq2[string, any] {
	ss.mu.RLock()
	stateCopy := maps.Clone(ss.state)
	ss.mu.RUnlock()

	return func(yield func(string, any) bool) {
		for k, v := range stateCopy {
			if !yield(k, v) {
				return
			}
		}
	}
}

// --- Events wrapper ---

type sqliteSessionEvents []*adksession.Event

func (e sqliteSessionEvents) All() iter.Seq[*adksession.Event] {
	return func(yield func(*adksession.Event) bool) {
		for _, ev := range e {
			if !yield(ev) {
				return
			}
		}
	}
}
func (e sqliteSessionEvents) Len() int                    { return len(e) }
func (e sqliteSessionEvents) At(i int) *adksession.Event {
	if i >= 0 && i < len(e) {
		return e[i]
	}
	return nil
}

// --- ADK Service interface (Create, Get, List, Delete, AppendEvent) ---

func (ss *sqliteSessionStore) Create(_ context.Context, req *adksession.CreateRequest) (*adksession.CreateResponse, error) {
	if req.AppName == "" || req.UserID == "" {
		return nil, fmt.Errorf("app_name and user_id are required")
	}

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = uuid.NewString()
	}

	state := req.State
	if state == nil {
		state = make(map[string]any)
	}

	// Extract parent_id from state if set (Astonish convention).
	parentID, _ := state["_astonish_parent_id"].(string)

	sess := &sqliteSession{
		id:        sessionID,
		appName:   req.AppName,
		userID:    req.UserID,
		state:     state,
		updatedAt: time.Now(),
	}

	// Persist to SQLite.
	now := formatTime(time.Now())
	_, err := ss.db.Exec(
		`INSERT INTO sessions (id, user_id, parent_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (id) DO NOTHING`,
		sessionID, req.UserID, nilStr(parentID), now, now)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	// Cache in memory.
	ss.mu.Lock()
	if ss.sessions == nil {
		ss.sessions = make(map[string]*sqliteSession)
	}
	ss.sessions[sessionID] = sess
	ss.mu.Unlock()

	return &adksession.CreateResponse{Session: sess}, nil
}

func (ss *sqliteSessionStore) Get(ctx context.Context, req *adksession.GetRequest) (*adksession.GetResponse, error) {
	if req.AppName == "" || req.UserID == "" || req.SessionID == "" {
		return nil, fmt.Errorf("app_name, user_id, session_id are required")
	}

	// Check existence in DB.
	var exists int
	err := ss.db.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id = ?`, req.SessionID).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("session %q not found", req.SessionID)
	}

	// Load events from DB and rebuild session.
	events, err := ss.loadEvents(ctx, req.SessionID)
	if err != nil {
		return nil, fmt.Errorf("load events: %w", err)
	}

	sess := &sqliteSession{
		id:        req.SessionID,
		appName:   req.AppName,
		userID:    req.UserID,
		state:     make(map[string]any),
		updatedAt: time.Now(),
	}

	// Replay events to rebuild state.
	for _, ev := range events {
		if ev.Actions.StateDelta != nil {
			maps.Copy(sess.state, ev.Actions.StateDelta)
		}
	}
	sess.events = events
	if len(events) > 0 {
		sess.updatedAt = events[len(events)-1].Timestamp
	}

	// Apply filters.
	filtered := sess.events
	if req.NumRecentEvents > 0 {
		start := max(len(filtered)-req.NumRecentEvents, 0)
		filtered = filtered[start:]
	}
	if !req.After.IsZero() && len(filtered) > 0 {
		for i, ev := range filtered {
			if !ev.Timestamp.Before(req.After) {
				filtered = filtered[i:]
				break
			}
		}
	}

	// Return a copy with filtered events.
	result := &sqliteSession{
		id:        sess.id,
		appName:   sess.appName,
		userID:    sess.userID,
		state:     maps.Clone(sess.state),
		events:    filtered,
		updatedAt: sess.updatedAt,
	}

	// Update cache.
	ss.mu.Lock()
	if ss.sessions == nil {
		ss.sessions = make(map[string]*sqliteSession)
	}
	ss.sessions[req.SessionID] = sess
	ss.mu.Unlock()

	return &adksession.GetResponse{Session: result}, nil
}

func (ss *sqliteSessionStore) List(ctx context.Context, req *adksession.ListRequest) (*adksession.ListResponse, error) {
	if req.AppName == "" {
		return nil, fmt.Errorf("app_name is required")
	}

	query := `SELECT id, user_id, updated_at FROM sessions ORDER BY updated_at DESC`
	args := []any{}

	if req.UserID != "" {
		query = `SELECT id, user_id, updated_at FROM sessions WHERE user_id = ? ORDER BY updated_at DESC`
		args = append(args, req.UserID)
	}

	rows, err := ss.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []adksession.Session
	for rows.Next() {
		var id, userID, updatedAt string
		if err := rows.Scan(&id, &userID, &updatedAt); err != nil {
			continue
		}
		sessions = append(sessions, &sqliteSession{
			id:        id,
			appName:   req.AppName,
			userID:    userID,
			state:     make(map[string]any),
			updatedAt: parseTime(updatedAt),
		})
	}

	return &adksession.ListResponse{Sessions: sessions}, nil
}

func (ss *sqliteSessionStore) Delete(ctx context.Context, req *adksession.DeleteRequest) error {
	if req.SessionID == "" {
		return fmt.Errorf("session_id is required")
	}

	ss.mu.Lock()
	delete(ss.sessions, req.SessionID)
	ss.mu.Unlock()

	_, err := ss.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, req.SessionID)
	return err
}

func (ss *sqliteSessionStore) AppendEvent(ctx context.Context, curSession adksession.Session, event *adksession.Event) error {
	if curSession == nil {
		return fmt.Errorf("session is nil")
	}
	if event == nil {
		return fmt.Errorf("event is nil")
	}
	if event.Partial {
		return nil
	}

	sess, ok := curSession.(*sqliteSession)
	if !ok {
		return fmt.Errorf("unexpected session type %T", curSession)
	}

	// Update in-memory session.
	sess.appendEvent(event)

	// Also update cached copy if different from the caller's.
	ss.mu.RLock()
	cached := ss.sessions[sess.id]
	ss.mu.RUnlock()
	if cached != nil && cached != sess {
		cached.appendEvent(event)
	}

	// Persist event to DB.
	eventData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	now := formatTime(time.Now())
	_, err = ss.db.ExecContext(ctx,
		`INSERT INTO session_events (session_id, event_data, created_at) VALUES (?, ?, ?)`,
		sess.id, string(eventData), now)
	if err != nil {
		return fmt.Errorf("persist event: %w", err)
	}

	// Update session metadata (message_count, updated_at).
	_, _ = ss.db.ExecContext(ctx,
		`UPDATE sessions SET message_count = message_count + 1, updated_at = ? WHERE id = ?`,
		now, sess.id)

	return nil
}

// --- Astonish-specific SessionStore methods ---

func (ss *sqliteSessionStore) ListSessionMetas(ctx context.Context, _, _ string) ([]store.SessionMeta, error) {
	rows, err := ss.db.QueryContext(ctx,
		`SELECT id, user_id, title, message_count, parent_id, fleet_key, fleet_name,
		        issue_number, repo, workspace_dir, created_at, updated_at
		 FROM sessions
		 WHERE parent_id IS NULL OR parent_id = ''
		 ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metas []store.SessionMeta
	for rows.Next() {
		m, err := scanSessionMeta(rows)
		if err != nil {
			continue
		}
		metas = append(metas, m)
	}
	return metas, rows.Err()
}

func (ss *sqliteSessionStore) GetSessionMeta(ctx context.Context, sessionID string) (*store.SessionMeta, error) {
	row := ss.db.QueryRowContext(ctx,
		`SELECT id, user_id, title, message_count, parent_id, fleet_key, fleet_name,
		        issue_number, repo, workspace_dir, created_at, updated_at
		 FROM sessions WHERE id = ?`, sessionID)

	m, err := scanSessionMeta(row)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (ss *sqliteSessionStore) SetSessionTitle(ctx context.Context, sessionID, title string) error {
	_, err := ss.db.ExecContext(ctx,
		`UPDATE sessions SET title = ?, updated_at = ? WHERE id = ?`,
		title, formatTime(time.Now()), sessionID)
	return err
}

func (ss *sqliteSessionStore) ListChildren(ctx context.Context, parentID string) ([]store.SessionMeta, error) {
	rows, err := ss.db.QueryContext(ctx,
		`SELECT id, user_id, title, message_count, parent_id, fleet_key, fleet_name,
		        issue_number, repo, workspace_dir, created_at, updated_at
		 FROM sessions WHERE parent_id = ? ORDER BY created_at`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metas []store.SessionMeta
	for rows.Next() {
		m, err := scanSessionMeta(rows)
		if err != nil {
			continue
		}
		metas = append(metas, m)
	}
	return metas, rows.Err()
}

func (ss *sqliteSessionStore) AddSessionMeta(ctx context.Context, meta store.SessionMeta) error {
	now := formatTime(time.Now())
	createdAt := formatTime(meta.CreatedAt)
	if createdAt == "" {
		createdAt = now
	}
	_, err := ss.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, title, message_count, parent_id, fleet_key, fleet_name,
		                       issue_number, repo, workspace_dir, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET
		   title = excluded.title,
		   message_count = excluded.message_count,
		   parent_id = excluded.parent_id,
		   fleet_key = excluded.fleet_key,
		   fleet_name = excluded.fleet_name,
		   issue_number = excluded.issue_number,
		   repo = excluded.repo,
		   workspace_dir = excluded.workspace_dir,
		   updated_at = excluded.updated_at`,
		meta.ID, meta.UserID, meta.Title, meta.MessageCount,
		nilStr(meta.ParentID), meta.FleetKey, meta.FleetName,
		meta.IssueNumber, meta.Repo, meta.WorkspaceDir,
		createdAt, now)
	return err
}

func (ss *sqliteSessionStore) UpdateSessionMeta(ctx context.Context, sessionID string, fn func(*store.SessionMeta)) error {
	meta, err := ss.GetSessionMeta(ctx, sessionID)
	if err != nil {
		return err
	}
	fn(meta)
	return ss.AddSessionMeta(ctx, *meta)
}

func (ss *sqliteSessionStore) RemoveSessionMeta(ctx context.Context, sessionID string) error {
	ss.mu.Lock()
	delete(ss.sessions, sessionID)
	ss.mu.Unlock()

	_, err := ss.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID)
	return err
}

func (ss *sqliteSessionStore) ReadTranscriptEvents(ctx context.Context, _, _, sessionID string) ([]*adksession.Event, error) {
	return ss.loadEvents(ctx, sessionID)
}

func (ss *sqliteSessionStore) AppendFleetEvent(ctx context.Context, sessionID string, event *adksession.Event) error {
	eventData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal fleet event: %w", err)
	}

	now := formatTime(time.Now())
	_, err = ss.db.ExecContext(ctx,
		`INSERT INTO session_events (session_id, event_data, created_at) VALUES (?, ?, ?)`,
		sessionID, string(eventData), now)
	if err != nil {
		return err
	}

	_, _ = ss.db.ExecContext(ctx,
		`UPDATE sessions SET message_count = message_count + 1, updated_at = ? WHERE id = ?`,
		now, sessionID)
	return nil
}

func (ss *sqliteSessionStore) ResolveSessionID(ctx context.Context, partial string) (string, error) {
	rows, err := ss.db.QueryContext(ctx,
		`SELECT id FROM sessions WHERE id LIKE ? LIMIT 2`, partial+"%")
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	switch len(ids) {
	case 0:
		return "", fmt.Errorf("no session matching %q", partial)
	case 1:
		return ids[0], nil
	default:
		return "", fmt.Errorf("ambiguous session ID %q matches %d sessions", partial, len(ids))
	}
}

func (ss *sqliteSessionStore) AllSessionIDs(ctx context.Context) map[string]bool {
	rows, err := ss.db.QueryContext(ctx, `SELECT id FROM sessions`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		result[id] = true
	}
	return result
}

func (ss *sqliteSessionStore) CleanupExpiredSessions(ctx context.Context, maxAgeDays int) []string {
	cutoff := formatTime(time.Now().AddDate(0, 0, -maxAgeDays))

	rows, err := ss.db.QueryContext(ctx,
		`DELETE FROM sessions WHERE updated_at < ? RETURNING id`, cutoff)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var deleted []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		deleted = append(deleted, id)
		ss.mu.Lock()
		delete(ss.sessions, id)
		ss.mu.Unlock()
	}
	return deleted
}

func (ss *sqliteSessionStore) RedactSession(ctx context.Context, _, _, sessionID string) error {
	if ss.redactFn == nil {
		return nil
	}

	rows, err := ss.db.QueryContext(ctx,
		`SELECT id, event_data FROM session_events WHERE session_id = ?`, sessionID)
	if err != nil {
		return err
	}
	defer rows.Close()

	type eventRow struct {
		id   int64
		data string
	}
	var toUpdate []eventRow

	for rows.Next() {
		var r eventRow
		if err := rows.Scan(&r.id, &r.data); err != nil {
			continue
		}
		redacted := ss.redactFn(r.data)
		if redacted != r.data {
			r.data = redacted
			toUpdate = append(toUpdate, r)
		}
	}

	for _, r := range toUpdate {
		_, _ = ss.db.ExecContext(ctx,
			`UPDATE session_events SET event_data = ? WHERE id = ?`, r.data, r.id)
	}

	// Invalidate cache.
	ss.mu.Lock()
	delete(ss.sessions, sessionID)
	ss.mu.Unlock()

	return nil
}

func (ss *sqliteSessionStore) SetRedactFunc(fn func(string) string) {
	ss.redactFn = fn
}

// --- Internal helpers ---

func (ss *sqliteSessionStore) loadEvents(ctx context.Context, sessionID string) ([]*adksession.Event, error) {
	rows, err := ss.db.QueryContext(ctx,
		`SELECT event_data FROM session_events WHERE session_id = ? ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*adksession.Event
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			continue
		}
		var event adksession.Event
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		events = append(events, &event)
	}
	return events, rows.Err()
}

// scanSessionMeta scans a session metadata row.
func scanSessionMeta(row scannable) (store.SessionMeta, error) {
	var m store.SessionMeta
	var title, parentID, fleetKey, fleetName, repo, workspaceDir sql.NullString
	var issueNumber sql.NullInt64
	var createdAt, updatedAt string

	err := row.Scan(&m.ID, &m.UserID, &title, &m.MessageCount, &parentID,
		&fleetKey, &fleetName, &issueNumber, &repo, &workspaceDir,
		&createdAt, &updatedAt)
	if err != nil {
		return m, err
	}

	m.Title = title.String
	m.ParentID = parentID.String
	m.FleetKey = fleetKey.String
	m.FleetName = fleetName.String
	m.Repo = repo.String
	m.WorkspaceDir = workspaceDir.String
	if issueNumber.Valid {
		m.IssueNumber = int(issueNumber.Int64)
	}
	m.CreatedAt = parseTime(createdAt)
	m.UpdatedAt = parseTime(updatedAt)

	return m, nil
}
