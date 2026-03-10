package fleet

import (
	"fmt"
	"sync"
)

// SessionRegistry tracks active fleet sessions globally.
// It is the central lookup for the API layer to find running fleet sessions,
// route incoming messages, and stream SSE events.
type SessionRegistry struct {
	sessions map[string]*FleetSession // keyed by session ID
	mu       sync.RWMutex
}

// NewSessionRegistry creates a new empty session registry.
func NewSessionRegistry() *SessionRegistry {
	return &SessionRegistry{
		sessions: make(map[string]*FleetSession),
	}
}

// Register adds a fleet session to the registry.
func (r *SessionRegistry) Register(fs *FleetSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[fs.ID] = fs
}

// Unregister removes a fleet session from the registry.
func (r *SessionRegistry) Unregister(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, sessionID)
}

// Get returns a fleet session by ID, or nil if not found.
func (r *SessionRegistry) Get(sessionID string) *FleetSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sessions[sessionID]
}

// List returns metadata for all active fleet sessions.
func (r *SessionRegistry) List() []FleetSessionInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]FleetSessionInfo, 0, len(r.sessions))
	for _, fs := range r.sessions {
		state, activeAgent := fs.GetState()
		infos = append(infos, FleetSessionInfo{
			ID:          fs.ID,
			FleetKey:    fs.FleetKey,
			FleetName:   fs.FleetConfig.Name,
			State:       string(state),
			ActiveAgent: activeAgent,
		})
	}
	return infos
}

// IsFleetSession checks whether the given session ID belongs to an active fleet session.
func (r *SessionRegistry) IsFleetSession(sessionID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.sessions[sessionID]
	return ok
}

// PostHumanMessage posts a human message to the fleet session's channel.
// Returns an error if the session is not found or not running.
func (r *SessionRegistry) PostHumanMessage(sessionID, text string) error {
	fs := r.Get(sessionID)
	if fs == nil {
		return fmt.Errorf("fleet session %s not found", sessionID)
	}
	ctx := fs.ctx
	if ctx == nil {
		return fmt.Errorf("fleet session %s is not running", sessionID)
	}
	msg := Message{
		Sender: "customer",
		Text:   text,
	}
	if err := fs.Channel.PostMessage(ctx, msg); err != nil {
		return err
	}
	// Persist to transcript
	fs.notifyMessagePosted(msg)
	return nil
}

// FleetSessionInfo is a read-only view of a fleet session for API responses.
type FleetSessionInfo struct {
	ID          string `json:"id"`
	FleetKey    string `json:"fleet_key"`
	FleetName   string `json:"fleet_name"`
	State       string `json:"state"`
	ActiveAgent string `json:"active_agent,omitempty"`
}
