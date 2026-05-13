package filestore

import (
	"context"

	"github.com/schardosin/astonish/pkg/session"
	"github.com/schardosin/astonish/pkg/store"
	adksession "google.golang.org/adk/session"
)

// SessionStoreWrapper wraps the existing session.FileStore behind the
// store.SessionStore interface.
type SessionStoreWrapper struct {
	inner *session.FileStore
}

// NewSessionStore creates a SessionStore backed by the existing file-based session store.
func NewSessionStore(fs *session.FileStore) store.SessionStore {
	return &SessionStoreWrapper{inner: fs}
}

// Inner returns the underlying session.FileStore for code that still needs
// direct access during the transition period.
func (w *SessionStoreWrapper) Inner() *session.FileStore {
	return w.inner
}

// --- ADK session.Service interface ---

func (w *SessionStoreWrapper) Create(ctx context.Context, req *adksession.CreateRequest) (*adksession.CreateResponse, error) {
	return w.inner.Create(ctx, req)
}

func (w *SessionStoreWrapper) Get(ctx context.Context, req *adksession.GetRequest) (*adksession.GetResponse, error) {
	return w.inner.Get(ctx, req)
}

func (w *SessionStoreWrapper) List(ctx context.Context, req *adksession.ListRequest) (*adksession.ListResponse, error) {
	return w.inner.List(ctx, req)
}

func (w *SessionStoreWrapper) Delete(ctx context.Context, req *adksession.DeleteRequest) error {
	return w.inner.Delete(ctx, req)
}

func (w *SessionStoreWrapper) AppendEvent(ctx context.Context, curSession adksession.Session, event *adksession.Event) error {
	return w.inner.AppendEvent(ctx, curSession, event)
}

// --- Astonish-specific session operations ---

func (w *SessionStoreWrapper) ListSessionMetas(_ context.Context, appName, userID string) ([]store.SessionMeta, error) {
	metas, err := w.inner.ListSessionMetas(appName, userID)
	if err != nil {
		return nil, err
	}
	result := make([]store.SessionMeta, len(metas))
	for i, m := range metas {
		result[i] = convertSessionMeta(m)
	}
	return result, nil
}

func (w *SessionStoreWrapper) GetSessionMeta(_ context.Context, sessionID string) (*store.SessionMeta, error) {
	meta, err := w.inner.GetSessionMeta(sessionID)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, nil
	}
	sm := convertSessionMeta(*meta)
	return &sm, nil
}

func (w *SessionStoreWrapper) SetSessionTitle(ctx context.Context, sessionID, title string) error {
	return w.inner.SetSessionTitle(ctx, sessionID, title)
}

func (w *SessionStoreWrapper) ListChildren(_ context.Context, parentID string) ([]store.SessionMeta, error) {
	metas, err := w.inner.ListChildren(parentID)
	if err != nil {
		return nil, err
	}
	result := make([]store.SessionMeta, len(metas))
	for i, m := range metas {
		result[i] = convertSessionMeta(m)
	}
	return result, nil
}

func (w *SessionStoreWrapper) AddSessionMeta(_ context.Context, meta store.SessionMeta) error {
	return w.inner.AddSessionMeta(convertToInternalMeta(meta))
}

func (w *SessionStoreWrapper) UpdateSessionMeta(_ context.Context, sessionID string, fn func(*store.SessionMeta)) error {
	return w.inner.UpdateSessionMeta(sessionID, func(m *session.SessionMeta) {
		sm := convertSessionMeta(*m)
		fn(&sm)
		*m = convertToInternalMeta(sm)
	})
}

func (w *SessionStoreWrapper) RemoveSessionMeta(_ context.Context, sessionID string) error {
	return w.inner.RemoveSessionMeta(sessionID)
}

func (w *SessionStoreWrapper) ReadTranscriptEvents(_ context.Context, appName, userID, sessionID string) ([]*adksession.Event, error) {
	return w.inner.ReadTranscriptEvents(appName, userID, sessionID)
}

func (w *SessionStoreWrapper) ResolveSessionID(_ context.Context, partial string) (string, error) {
	return w.inner.ResolveSessionID(partial)
}

func (w *SessionStoreWrapper) AllSessionIDs(_ context.Context) map[string]bool {
	return w.inner.AllSessionIDs()
}

func (w *SessionStoreWrapper) CleanupExpiredSessions(_ context.Context, maxAgeDays int) []string {
	return w.inner.CleanupExpiredSessions(maxAgeDays)
}

func (w *SessionStoreWrapper) RedactSession(_ context.Context, appName, userID, sessionID string) error {
	return w.inner.RedactSession(appName, userID, sessionID)
}

func (w *SessionStoreWrapper) SetRedactFunc(fn func(string) string) {
	w.inner.RedactFunc = fn
}

// AppendFleetEvent is a no-op for the file-based store since fleet transcripts
// are managed via JSONL files in wireFleetTranscript. This method exists to
// satisfy the SessionStore interface.
func (w *SessionStoreWrapper) AppendFleetEvent(_ context.Context, _ string, _ *adksession.Event) error {
	return nil
}

// --- conversion helpers ---

func convertSessionMeta(m session.SessionMeta) store.SessionMeta {
	return store.SessionMeta{
		ID:           m.ID,
		AppName:      m.AppName,
		UserID:       m.UserID,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
		Title:        m.Title,
		MessageCount: m.MessageCount,
		ParentID:     m.ParentID,
		FleetKey:     m.FleetKey,
		FleetName:    m.FleetName,
		IssueNumber:  m.IssueNumber,
		Repo:         m.Repo,
		WorkspaceDir: m.WorkspaceDir,
	}
}

func convertToInternalMeta(m store.SessionMeta) session.SessionMeta {
	return session.SessionMeta{
		ID:           m.ID,
		AppName:      m.AppName,
		UserID:       m.UserID,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
		Title:        m.Title,
		MessageCount: m.MessageCount,
		ParentID:     m.ParentID,
		FleetKey:     m.FleetKey,
		FleetName:    m.FleetName,
		IssueNumber:  m.IssueNumber,
		Repo:         m.Repo,
		WorkspaceDir: m.WorkspaceDir,
	}
}

// Compile-time check.
var _ store.SessionStore = (*SessionStoreWrapper)(nil)
