package pgstore

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/store"
	adksession "google.golang.org/adk/session"
)

// pgSessionStore implements store.SessionStore for PostgreSQL.
//
// It maintains an in-memory sessions map so that the ADK runner's
// session objects are properly mutated by AppendEvent (the runner
// reads back Events() from the same object after appending).
// The map is scoped to the pgSessionStore instance, which is
// created per-request by TenantMiddleware but lives for the full
// lifetime of any background goroutine (e.g., ChatRunner) that
// captures the sessionService.
type pgSessionStore struct {
	pool     *pgxpool.Pool
	schema   string
	redactFn func(string) string

	mu       sync.RWMutex
	sessions map[string]*pgSession
}

func (s *pgSessionStore) sessionsTable() string {
	return pgx.Identifier{s.schema, "sessions"}.Sanitize()
}

func (s *pgSessionStore) eventsTable() string {
	return pgx.Identifier{s.schema, "session_events"}.Sanitize()
}

// =========================================================================
// pgSession — custom session type implementing adksession.Session
// =========================================================================

// pgSession holds an in-memory representation of a session whose events
// are persisted in PostgreSQL. It mirrors fileSession in pkg/session.
type pgSession struct {
	id        string
	appName   string
	userID    string
	mu        sync.RWMutex
	events    []*adksession.Event
	state     map[string]any
	updatedAt time.Time
}

func (ps *pgSession) ID() string              { return ps.id }
func (ps *pgSession) AppName() string         { return ps.appName }
func (ps *pgSession) UserID() string          { return ps.userID }
func (ps *pgSession) State() adksession.State { return &pgState{mu: &ps.mu, state: ps.state} }
func (ps *pgSession) Events() adksession.Events {
	return pgEvents(ps.events)
}
func (ps *pgSession) LastUpdateTime() time.Time {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.updatedAt
}

// appendEvent updates the in-memory session with the event.
// It applies non-temp state deltas and appends the event to the list.
func (ps *pgSession) appendEvent(event *adksession.Event) error {
	if event.Partial {
		return nil
	}

	// Apply non-temp state deltas
	if event.Actions.StateDelta != nil {
		if ps.state == nil {
			ps.state = make(map[string]any)
		}
		for k, v := range event.Actions.StateDelta {
			if !strings.HasPrefix(k, adksession.KeyPrefixTemp) {
				ps.state[k] = v
			}
		}
	}

	ps.events = append(ps.events, event)
	ps.updatedAt = event.Timestamp
	return nil
}

// copy creates a deep copy of the session for returning from Create/Get.
func (ps *pgSession) copy() *pgSession {
	cp := &pgSession{
		id:        ps.id,
		appName:   ps.appName,
		userID:    ps.userID,
		state:     maps.Clone(ps.state),
		updatedAt: ps.updatedAt,
	}
	cp.events = make([]*adksession.Event, len(ps.events))
	copy(cp.events, ps.events)
	return cp
}

// =========================================================================
// pgState — implements adksession.State
// =========================================================================

type pgState struct {
	mu    *sync.RWMutex
	state map[string]any
}

func (s *pgState) Get(key string) (any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.state[key]
	if !ok {
		return nil, adksession.ErrStateKeyNotExist
	}
	return val, nil
}

func (s *pgState) Set(key string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state[key] = value
	return nil
}

func (s *pgState) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		s.mu.RLock()
		for k, v := range s.state {
			s.mu.RUnlock()
			if !yield(k, v) {
				return
			}
			s.mu.RLock()
		}
		s.mu.RUnlock()
	}
}

// =========================================================================
// pgEvents — implements adksession.Events
// =========================================================================

type pgEvents []*adksession.Event

func (e pgEvents) All() iter.Seq[*adksession.Event] {
	return func(yield func(*adksession.Event) bool) {
		for _, evt := range e {
			if !yield(evt) {
				return
			}
		}
	}
}

func (e pgEvents) At(i int) *adksession.Event { return e[i] }
func (e pgEvents) Len() int                   { return len(e) }

// =========================================================================
// ADK session.Service interface
// =========================================================================

func (s *pgSessionStore) Create(ctx context.Context, req *adksession.CreateRequest) (*adksession.CreateResponse, error) {
	if req.AppName == "" || req.UserID == "" {
		return nil, fmt.Errorf("app_name and user_id are required")
	}

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = uuid.NewString()
	}

	now := time.Now()

	// Initialize state from request
	state := req.State
	if state == nil {
		state = make(map[string]any)
	}

	// Create in-memory session
	sess := &pgSession{
		id:        sessionID,
		appName:   req.AppName,
		userID:    req.UserID,
		state:     state,
		events:    nil,
		updatedAt: now,
	}

	s.mu.Lock()
	s.sessions[sessionID] = sess
	s.mu.Unlock()

	// Persist metadata in PG
	_, pgErr := s.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (id, user_id, title, created_at, updated_at)
		 VALUES ($1, $2, '', $3, $3)
		 ON CONFLICT (id) DO NOTHING`, s.sessionsTable()),
		sessionID, req.UserID, now,
	)
	if pgErr != nil {
		return nil, fmt.Errorf("failed to persist session %s: %w", sessionID, pgErr)
	}

	return &adksession.CreateResponse{Session: sess.copy()}, nil
}

func (s *pgSessionStore) Get(ctx context.Context, req *adksession.GetRequest) (*adksession.GetResponse, error) {
	if req.SessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}

	// Check if session exists in PG
	var exists bool
	err := s.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT EXISTS(SELECT 1 FROM %s WHERE id = $1)`, s.sessionsTable()),
		req.SessionID,
	).Scan(&exists)
	if err != nil || !exists {
		return nil, fmt.Errorf("session %s not found", req.SessionID)
	}

	// Load events from PG
	events, err := s.loadEvents(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}

	// Build in-memory session with replayed events
	sess := &pgSession{
		id:        req.SessionID,
		appName:   req.AppName,
		userID:    req.UserID,
		state:     make(map[string]any),
		events:    nil,
		updatedAt: time.Now(),
	}
	for _, evt := range events {
		_ = sess.appendEvent(evt)
	}

	s.mu.Lock()
	s.sessions[req.SessionID] = sess
	s.mu.Unlock()

	return &adksession.GetResponse{Session: sess.copy()}, nil
}

func (s *pgSessionStore) List(ctx context.Context, req *adksession.ListRequest) (*adksession.ListResponse, error) {
	rows, err := s.pool.Query(ctx, fmt.Sprintf(
		`SELECT id FROM %s ORDER BY updated_at DESC`, s.sessionsTable()),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []adksession.Session
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		sessions = append(sessions, &pgSession{
			id:      id,
			appName: req.AppName,
			userID:  req.UserID,
			state:   make(map[string]any),
		})
	}
	return &adksession.ListResponse{Sessions: sessions}, rows.Err()
}

func (s *pgSessionStore) Delete(ctx context.Context, req *adksession.DeleteRequest) error {
	s.mu.Lock()
	delete(s.sessions, req.SessionID)
	s.mu.Unlock()

	_, err := s.pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE id = $1`, s.sessionsTable()),
		req.SessionID,
	)
	return err
}

func (s *pgSessionStore) AppendEvent(ctx context.Context, curSession adksession.Session, event *adksession.Event) error {
	if curSession == nil {
		return fmt.Errorf("session is nil")
	}
	if event == nil {
		return fmt.Errorf("event is nil")
	}
	if event.Partial {
		return nil
	}

	sess, ok := curSession.(*pgSession)
	if !ok {
		return fmt.Errorf("unexpected session type %T (expected *pgSession)", curSession)
	}

	// Update the caller's in-memory session (the runner reads Events() back)
	if err := sess.appendEvent(event); err != nil {
		return fmt.Errorf("failed to update session: %w", err)
	}

	// Also update the stored session so subsequent Get() calls see the event
	s.mu.Lock()
	if stored, ok := s.sessions[sess.id]; ok && stored != sess {
		stored.events = append(stored.events, event)
		stored.updatedAt = event.Timestamp
	}
	s.mu.Unlock()

	// Persist event to PG
	eventData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	_, err = s.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (session_id, event_data, created_at) VALUES ($1, $2, now())`,
		s.eventsTable()),
		sess.id, eventData,
	)
	if err != nil {
		return fmt.Errorf("failed to append event: %w", err)
	}

	// Update session metadata
	_, _ = s.pool.Exec(ctx, fmt.Sprintf(
		`UPDATE %s SET message_count = message_count + 1, updated_at = now() WHERE id = $1`,
		s.sessionsTable()),
		sess.id,
	)

	return nil
}

// =========================================================================
// Astonish-specific SessionStore methods
// =========================================================================

func (s *pgSessionStore) ListSessionMetas(appName, userID string) ([]store.SessionMeta, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, fmt.Sprintf(
		`SELECT id, title, message_count, parent_id, fleet_key, fleet_name, workspace_dir, created_at, updated_at
		 FROM %s WHERE (parent_id IS NULL OR parent_id = '') ORDER BY updated_at DESC`, s.sessionsTable()),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metas []store.SessionMeta
	for rows.Next() {
		m, err := scanSessionMeta(rows)
		if err != nil {
			return nil, err
		}
		m.AppName = appName
		m.UserID = userID
		metas = append(metas, m)
	}
	return metas, rows.Err()
}

func (s *pgSessionStore) GetSessionMeta(sessionID string) (*store.SessionMeta, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT id, title, message_count, parent_id, fleet_key, fleet_name, workspace_dir, created_at, updated_at
		 FROM %s WHERE id = $1`, s.sessionsTable()),
		sessionID,
	)
	m, err := scanSessionMeta(row)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *pgSessionStore) SetSessionTitle(sessionID, title string) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx, fmt.Sprintf(
		`UPDATE %s SET title = $2, updated_at = now() WHERE id = $1`, s.sessionsTable()),
		sessionID, title,
	)
	return err
}

func (s *pgSessionStore) ListChildren(parentID string) ([]store.SessionMeta, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, fmt.Sprintf(
		`SELECT id, title, message_count, parent_id, fleet_key, fleet_name, workspace_dir, created_at, updated_at
		 FROM %s WHERE parent_id = $1 ORDER BY created_at`, s.sessionsTable()),
		parentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var children []store.SessionMeta
	for rows.Next() {
		m, err := scanSessionMeta(rows)
		if err != nil {
			return nil, err
		}
		children = append(children, m)
	}
	return children, rows.Err()
}

func (s *pgSessionStore) AddSessionMeta(meta store.SessionMeta) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (id, user_id, title, message_count, parent_id, fleet_key, fleet_name, workspace_dir, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (id) DO UPDATE SET
		   title = EXCLUDED.title, message_count = EXCLUDED.message_count,
		   parent_id = EXCLUDED.parent_id, fleet_key = EXCLUDED.fleet_key,
		   fleet_name = EXCLUDED.fleet_name, workspace_dir = EXCLUDED.workspace_dir,
		   updated_at = EXCLUDED.updated_at`,
		s.sessionsTable()),
		meta.ID, meta.UserID, meta.Title, meta.MessageCount,
		nilIfEmpty(meta.ParentID), nilIfEmpty(meta.FleetKey),
		nilIfEmpty(meta.FleetName), nilIfEmpty(meta.WorkspaceDir),
		meta.CreatedAt, meta.UpdatedAt,
	)
	return err
}

func (s *pgSessionStore) UpdateSessionMeta(sessionID string, fn func(*store.SessionMeta)) error {
	meta, err := s.GetSessionMeta(sessionID)
	if err != nil {
		return err
	}
	fn(meta)
	return s.AddSessionMeta(*meta)
}

func (s *pgSessionStore) RemoveSessionMeta(sessionID string) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE id = $1`, s.sessionsTable()),
		sessionID,
	)
	return err
}

func (s *pgSessionStore) ReadTranscriptEvents(appName, userID, sessionID string) ([]*adksession.Event, error) {
	return s.loadEvents(context.Background(), sessionID)
}

func (s *pgSessionStore) ResolveSessionID(partial string) (string, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, fmt.Sprintf(
		`SELECT id FROM %s WHERE id LIKE $1 || '%%' LIMIT 2`, s.sessionsTable()),
		partial,
	)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var matches []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		matches = append(matches, id)
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no session found matching %q", partial)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous session ID %q: matches %s", partial, strings.Join(matches, ", "))
	}
}

func (s *pgSessionStore) AllSessionIDs() map[string]bool {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`SELECT id FROM %s`, s.sessionsTable()))
	if err != nil {
		return nil
	}
	defer rows.Close()

	ids := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids[id] = true
		}
	}
	return ids
}

func (s *pgSessionStore) CleanupExpiredSessions(maxAgeDays int) []string {
	ctx := context.Background()
	cutoff := time.Now().AddDate(0, 0, -maxAgeDays)

	rows, err := s.pool.Query(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE updated_at < $1 RETURNING id`, s.sessionsTable()),
		cutoff,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var deleted []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			deleted = append(deleted, id)
		}
	}
	return deleted
}

func (s *pgSessionStore) RedactSession(appName, userID, sessionID string) error {
	if s.redactFn == nil {
		return nil
	}
	ctx := context.Background()

	rows, err := s.pool.Query(ctx, fmt.Sprintf(
		`SELECT id, event_data FROM %s WHERE session_id = $1`, s.eventsTable()),
		sessionID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	type eventRow struct {
		id   int64
		data string
	}
	var toRedact []eventRow
	for rows.Next() {
		var r eventRow
		if err := rows.Scan(&r.id, &r.data); err != nil {
			return err
		}
		toRedact = append(toRedact, r)
	}

	for _, r := range toRedact {
		redacted := s.redactFn(r.data)
		if redacted != r.data {
			_, _ = s.pool.Exec(ctx, fmt.Sprintf(
				`UPDATE %s SET event_data = $2 WHERE id = $1`, s.eventsTable()),
				r.id, redacted,
			)
		}
	}
	return nil
}

func (s *pgSessionStore) SetRedactFunc(fn func(string) string) {
	s.redactFn = fn
}

// =========================================================================
// helpers
// =========================================================================

func (s *pgSessionStore) loadEvents(ctx context.Context, sessionID string) ([]*adksession.Event, error) {
	rows, err := s.pool.Query(ctx, fmt.Sprintf(
		`SELECT event_data FROM %s WHERE session_id = $1 ORDER BY id`, s.eventsTable()),
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*adksession.Event
	for rows.Next() {
		var eventData []byte
		if err := rows.Scan(&eventData); err != nil {
			return nil, err
		}
		var event adksession.Event
		if err := json.Unmarshal(eventData, &event); err != nil {
			continue
		}
		events = append(events, &event)
	}
	return events, rows.Err()
}

func scanSessionMeta(row scannable) (store.SessionMeta, error) {
	var m store.SessionMeta
	var title, parentID, fleetKey, fleetName, wsDir *string
	err := row.Scan(&m.ID, &title, &m.MessageCount, &parentID, &fleetKey, &fleetName, &wsDir, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return m, err
	}
	if title != nil {
		m.Title = *title
	}
	if parentID != nil {
		m.ParentID = *parentID
	}
	if fleetKey != nil {
		m.FleetKey = *fleetKey
	}
	if fleetName != nil {
		m.FleetName = *fleetName
	}
	if wsDir != nil {
		m.WorkspaceDir = *wsDir
	}
	return m, nil
}
