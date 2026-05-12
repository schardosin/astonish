package store

import (
	"context"
	"time"

	adksession "google.golang.org/adk/session"
)

// SessionMeta contains metadata about a chat session.
// This mirrors the existing session.SessionMeta type.
type SessionMeta struct {
	ID           string    `json:"id"`
	AppName      string    `json:"appName"`
	UserID       string    `json:"userId"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	Title        string    `json:"title,omitempty"`
	MessageCount int       `json:"messageCount"`
	ParentID     string    `json:"parentId,omitempty"`
	FleetKey     string    `json:"fleetKey,omitempty"`
	FleetName    string    `json:"fleetName,omitempty"`
	IssueNumber  int       `json:"issueNumber,omitempty"`
	Repo         string    `json:"repo,omitempty"`
	WorkspaceDir string    `json:"workspaceDir,omitempty"`
}

// SessionStore manages chat sessions and their events.
//
// It embeds the ADK session.Service interface for compatibility with the
// ADK runner, and adds Astonish-specific operations for session metadata,
// transcripts, and lifecycle management.
type SessionStore interface {
	// ADK session.Service — required for the ADK runner.
	adksession.Service

	// Metadata operations.
	ListSessionMetas(ctx context.Context, appName, userID string) ([]SessionMeta, error)
	GetSessionMeta(ctx context.Context, sessionID string) (*SessionMeta, error)
	SetSessionTitle(ctx context.Context, sessionID, title string) error
	ListChildren(ctx context.Context, parentID string) ([]SessionMeta, error)
	AddSessionMeta(ctx context.Context, meta SessionMeta) error
	UpdateSessionMeta(ctx context.Context, sessionID string, fn func(*SessionMeta)) error
	RemoveSessionMeta(ctx context.Context, sessionID string) error

	// Transcript access.
	ReadTranscriptEvents(ctx context.Context, appName, userID, sessionID string) ([]*adksession.Event, error)

	// AppendFleetEvent persists a fleet message event to a session's transcript
	// without requiring a full ADK session object. Used by fleet sessions which
	// manage their own message loop outside the ADK runner.
	AppendFleetEvent(ctx context.Context, sessionID string, event *adksession.Event) error

	// Partial ID resolution.
	ResolveSessionID(ctx context.Context, partial string) (string, error)

	// Session lifecycle.
	AllSessionIDs(ctx context.Context) map[string]bool
	CleanupExpiredSessions(ctx context.Context, maxAgeDays int) []string
	RedactSession(ctx context.Context, appName, userID, sessionID string) error

	// SetRedactFunc sets the function used to redact sensitive content in session transcripts.
	SetRedactFunc(fn func(string) string)
}
