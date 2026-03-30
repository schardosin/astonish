package agent

import (
	"context"
	"iter"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/adk/session"
)

// sessionEventTracking stores event filtering state per session
type sessionEventTracking struct {
	lastSeenEventIndex  int
	nodeEventStartIndex int
	trackingNodeName    string
}

// Package-level map for session event tracking (persists across LiveSession instances)
var (
	sessionTrackingMu  sync.RWMutex
	sessionTrackingMap = make(map[string]*sessionEventTracking)

	// EnableEventFiltering controls whether LLM nodes see filtered events (per-node history)
	// or full session history. Set to true for node isolation, false for full context.
	// TODO: Make this configurable per agent flow in the future
	EnableEventFiltering = true
)

// LiveSession wraps a session and fetches fresh data from the service on access
// It also tracks node boundaries for filtering events by current node
type LiveSession struct {
	service session.Service
	ctx     context.Context
	base    session.Session
	agent   *AstonishAgent // Reference for tracking node boundaries
}

func (s *LiveSession) ID() string {
	return s.base.ID()
}

func (s *LiveSession) AppName() string {
	return s.base.AppName()
}

func (s *LiveSession) UserID() string {
	return s.base.UserID()
}

func (s *LiveSession) getAppAndUser() (string, string) {
	appName := s.base.AppName()
	if appName == "" {
		appName = "astonish"
	}
	userID := s.base.UserID()
	if userID == "" {
		userID = "console_user"
	}
	return appName, userID
}

func (s *LiveSession) LastUpdateTime() time.Time {
	appName, userID := s.getAppAndUser()
	resp, err := s.service.Get(s.ctx, &session.GetRequest{
		SessionID: s.base.ID(),
		AppName:   appName,
		UserID:    userID,
	})
	if err != nil || resp == nil || resp.Session == nil {
		return s.base.LastUpdateTime()
	}
	return resp.Session.LastUpdateTime()
}

func (s *LiveSession) State() session.State {
	appName, userID := s.getAppAndUser()
	resp, err := s.service.Get(s.ctx, &session.GetRequest{
		SessionID: s.base.ID(),
		AppName:   appName,
		UserID:    userID,
	})
	if err != nil || resp == nil || resp.Session == nil {
		return s.base.State()
	}
	return resp.Session.State()
}

func (s *LiveSession) Events() session.Events {
	appName, userID := s.getAppAndUser()
	resp, err := s.service.Get(s.ctx, &session.GetRequest{
		SessionID: s.base.ID(),
		AppName:   appName,
		UserID:    userID,
	})
	if err != nil || resp == nil || resp.Session == nil {
		return s.base.Events()
	}

	allEvents := resp.Session.Events()
	totalLen := allEvents.Len()

	// If event filtering is disabled, return all events (full context mode)
	if !EnableEventFiltering {
		return allEvents
	}

	// If no events, return all events
	if totalLen == 0 {
		return allEvents
	}

	// Get session ID for tracking key
	sessionID := s.base.ID()

	// Find the current node by scanning backwards for Actions.StateDelta["current_node"]
	// Also detect boundaries: input nodes or different current_node (node transitions)
	currentNode := ""
	nodeBoundaryIndex := -1 // Index where we found a boundary (input node or different node)
	for i := totalLen - 1; i >= 0; i-- {
		ev := allEvents.At(i)
		if ev != nil && ev.Actions.StateDelta != nil {
			// Check for input node boundary (marks a reset point)
			if nodeType, ok := ev.Actions.StateDelta["node_type"]; ok {
				if nodeTypeStr, ok := nodeType.(string); ok && nodeTypeStr == "input" {
					nodeBoundaryIndex = i
					break // Found input boundary, stop scanning
				}
			}
			// Check current_node
			if nodeVal, ok := ev.Actions.StateDelta["current_node"]; ok {
				if nodeName, ok := nodeVal.(string); ok && nodeName != "" {
					if currentNode == "" {
						// First node found - this is our current node
						currentNode = nodeName
					} else if nodeName != currentNode {
						// Found a different node - this is a boundary!
						nodeBoundaryIndex = i
						break
					}
				}
			}
		}
	}

	// Fallback: if no current_node found in StateDelta, check Author field
	if currentNode == "" {
		for i := totalLen - 1; i >= 0; i-- {
			ev := allEvents.At(i)
			if ev != nil && ev.Author != "" && ev.Author != "user" && ev.Author != "astonish_agent" {
				currentNode = ev.Author
				break
			}
		}
	}

	// If still no current node identified, return all events
	if currentNode == "" {
		return allEvents
	}

	// Get or create tracking for this session (using package-level map)
	sessionTrackingMu.Lock()
	tracking := sessionTrackingMap[sessionID]
	if tracking == nil {
		tracking = &sessionEventTracking{}
		sessionTrackingMap[sessionID] = tracking
	}

	// Determine the start index for this node's events
	startIndex := tracking.nodeEventStartIndex // Default to existing start index

	// If we found a boundary (input node or different node), use that as the start
	if nodeBoundaryIndex >= 0 {
		startIndex = nodeBoundaryIndex + 1 // Start after the boundary event
	} else if totalLen > tracking.lastSeenEventIndex {
		// New events have been added since last call
		if currentNode != tracking.trackingNodeName && tracking.trackingNodeName != "" {
			// Different node - start fresh from where new events begin
			startIndex = tracking.lastSeenEventIndex + 2 // +2 to skip the last answer event
		}
		// If same node, keep existing startIndex (already set above)
	}
	// If no new events and no input boundary, keep existing startIndex (already set above)

	// Update tracking state
	tracking.lastSeenEventIndex = totalLen
	tracking.trackingNodeName = currentNode
	if startIndex > tracking.nodeEventStartIndex || tracking.nodeEventStartIndex == 0 {
		tracking.nodeEventStartIndex = startIndex
	}
	sessionTrackingMu.Unlock()

	// Debug output
	if s.agent != nil && s.agent.DebugMode {
		slog.Debug("live session events filtered", "total", totalLen, "currentNode", currentNode, "startIndex", startIndex, "lastSeen", tracking.lastSeenEventIndex)
	}

	// Return filtered events from startIndex onwards
	return &sliceFilteredEvents{
		source:     allEvents,
		startIndex: startIndex,
	}
}

// sliceFilteredEvents returns events from startIndex onwards
type sliceFilteredEvents struct {
	source     session.Events
	startIndex int
}

func (e *sliceFilteredEvents) Len() int {
	totalLen := e.source.Len()
	if totalLen <= e.startIndex {
		return 0
	}
	return totalLen - e.startIndex
}

func (e *sliceFilteredEvents) At(i int) *session.Event {
	return e.source.At(e.startIndex + i)
}

func (e *sliceFilteredEvents) All() iter.Seq[*session.Event] {
	return func(yield func(*session.Event) bool) {
		for i := e.startIndex; i < e.source.Len(); i++ {
			if !yield(e.source.At(i)) {
				return
			}
		}
	}
}
