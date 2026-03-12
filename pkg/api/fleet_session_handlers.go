package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/session"
	"github.com/schardosin/astonish/pkg/tools"
	adkmodel "google.golang.org/adk/model"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
)

// fleetSessionRegistry is the global registry for active fleet sessions.
var (
	fleetSessionRegistry     *fleet.SessionRegistry
	fleetSessionRegistryOnce sync.Once

	// fleetFileStore is a standalone FileStore for fleet session persistence.
	// Set during daemon startup so fleet sessions can persist transcripts and
	// session metadata even when the ChatManager hasn't been lazily initialized
	// (which only happens when someone opens the Studio UI).
	fleetFileStore *session.FileStore
)

// SetFleetSessionStore sets the FileStore used for fleet session persistence.
// Must be called during daemon startup before any fleet sessions are created.
func SetFleetSessionStore(fs *session.FileStore) {
	fleetFileStore = fs
}

// getFleetFileStore returns the FileStore for fleet persistence, trying the
// dedicated fleet store first, then falling back to the ChatManager's store.
func getFleetFileStore() *session.FileStore {
	if fleetFileStore != nil {
		return fleetFileStore
	}
	// Fallback: try the ChatManager (works when Studio UI has been opened)
	cm := GetChatManager()
	if cm != nil {
		return cm.fileStore()
	}
	return nil
}

// getFleetSessionRegistry returns the global fleet session registry (singleton).
func getFleetSessionRegistry() *fleet.SessionRegistry {
	fleetSessionRegistryOnce.Do(func() {
		fleetSessionRegistry = fleet.NewSessionRegistry()
	})
	return fleetSessionRegistry
}

// GetFleetSessionRegistry returns the global fleet session registry (exported for cross-package use).
func GetFleetSessionRegistry() *fleet.SessionRegistry {
	return getFleetSessionRegistry()
}

// FleetStartRequest is the request body for POST /api/studio/fleet/start.
type FleetStartRequest struct {
	FleetKey string `json:"fleet_key"`
	PlanKey  string `json:"plan_key,omitempty"` // alternative to fleet_key: start from a fleet plan
	Message  string `json:"message,omitempty"`  // optional initial message from user
}

// FleetMessageRequest is the request body for POST /api/studio/fleet/sessions/{id}/message.
type FleetMessageRequest struct {
	Message string `json:"message"`
}

// FleetSessionResult contains the result of creating a fleet session.
// Used by both the HTTP handler and channel commands (e.g., Telegram /fleet).
type FleetSessionResult struct {
	Session   *fleet.FleetSession
	FleetKey  string
	FleetName string
	Agents    []map[string]interface{}

	// SetOnMessagePosted allows the caller to compose an additional callback
	// on top of the existing transcript callback. The provided function receives
	// every message posted to the fleet channel (agent, customer, and system).
	SetOnMessagePosted func(fn func(msg fleet.Message))

	// SetOnSessionDone allows the caller to register a callback for session completion.
	// Composes with any existing done callback (e.g., plan activator).
	SetOnSessionDone func(fn func(sessionID string, sessionErr error))
}

// StartFleetSessionFromPlan creates, registers, and starts a fleet session from a plan key.
// The session is started in a background goroutine. The returned result includes
// SetOnMessagePosted and SetOnSessionDone functions that compose with the existing
// transcript callbacks (rather than replacing them), allowing callers (e.g., Telegram)
// to add forwarding logic on top.
// If initialMessage is non-empty, it is posted as the first customer message.
func StartFleetSessionFromPlan(planKey, initialMessage string) (*FleetSessionResult, error) {
	if fleetPlanRegistryVar == nil {
		return nil, fmt.Errorf("fleet plan system not initialized")
	}
	plan, ok := fleetPlanRegistryVar.GetPlan(planKey)
	if !ok {
		return nil, fmt.Errorf("fleet plan %q not found", planKey)
	}

	subAgentMgr := tools.GetSubAgentManager()
	if subAgentMgr == nil {
		return nil, fmt.Errorf("sub-agent system not initialized")
	}

	fleetCfg := &plan.FleetConfig
	channel := fleet.NewChatChannel(planKey)
	fleetSession := fleet.NewFleetSession(planKey, fleetCfg, channel, subAgentMgr)
	fleetSession.Plan = plan

	// Generate project context if configured
	if fleetCfg.ProjectContext != nil {
		workspaceDir := plan.ResolveWorkspaceDir()
		if workspaceDir != "" {
			if err := os.MkdirAll(workspaceDir, 0755); err != nil {
				log.Printf("[fleet] Warning: could not create workspace %s: %v", workspaceDir, err)
			} else {
				fleetSession.ProjectContext = fleet.GenerateProjectContext(
					context.Background(), workspaceDir, fleetCfg.ProjectContext)
			}
		}
	}

	// Register in session registry and persist metadata
	registry := getFleetSessionRegistry()
	registry.Register(fleetSession)
	persistFleetSessionMeta(fleetSession, fleetCfg, 0, "")
	wireFleetTranscript(fleetSession)

	// Capture the transcript callback so we can compose additional callbacks on top.
	transcriptCallback := fleetSession.OnMessagePosted
	existingDoneCallback := fleetSession.OnSessionDone

	// Start in background
	go func() {
		defer func() {
			registry.Unregister(fleetSession.ID)
			log.Printf("[fleet] Session %s removed from registry", fleetSession.ID)
		}()
		if err := fleetSession.Run(context.Background()); err != nil {
			log.Printf("[fleet] Session %s error: %v", fleetSession.ID, err)
		}
	}()

	// Post initial message if provided
	if initialMessage != "" {
		msg := fleet.Message{
			Sender: "customer",
			Text:   initialMessage,
		}
		if err := channel.PostMessage(context.Background(), msg); err != nil {
			log.Printf("[fleet] Error posting initial message: %v", err)
		}
		if fleetSession.OnMessagePosted != nil {
			fleetSession.OnMessagePosted(msg)
		}
	}

	return &FleetSessionResult{
		Session:   fleetSession,
		FleetKey:  planKey,
		FleetName: plan.Name,
		Agents:    buildAgentList(fleetCfg),
		// SetOnMessagePosted composes the caller's callback with the existing
		// transcript callback. Both are called for every message.
		SetOnMessagePosted: func(fn func(msg fleet.Message)) {
			fleetSession.OnMessagePosted = func(msg fleet.Message) {
				if transcriptCallback != nil {
					transcriptCallback(msg)
				}
				if fn != nil {
					fn(msg)
				}
			}
		},
		// SetOnSessionDone composes the caller's callback with any existing done callback.
		SetOnSessionDone: func(fn func(sessionID string, sessionErr error)) {
			fleetSession.OnSessionDone = func(sessionID string, sessionErr error) {
				if existingDoneCallback != nil {
					existingDoneCallback(sessionID, sessionErr)
				}
				if fn != nil {
					fn(sessionID, sessionErr)
				}
			}
		},
	}, nil
}

// FleetStartHandler handles POST /api/studio/fleet/start.
// It creates a new fleet session, starts the message loop in a background goroutine,
// and returns the session info as JSON. The client should then connect to the
// SSE stream endpoint to receive real-time events.
func FleetStartHandler(w http.ResponseWriter, r *http.Request) {
	var req FleetStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.FleetKey == "" && req.PlanKey == "" {
		http.Error(w, "fleet_key or plan_key is required", http.StatusBadRequest)
		return
	}

	// If starting from a plan, use the shared helper
	if req.PlanKey != "" {
		result, err := StartFleetSessionFromPlan(req.PlanKey, req.Message)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"session_id": result.Session.ID,
			"fleet_key":  result.FleetKey,
			"fleet_name": result.FleetName,
			"agents":     result.Agents,
		})
		return
	}

	// Start from a regular fleet template
	fleetReg := GetFleetRegistry()
	if fleetReg == nil {
		http.Error(w, "Fleet system not initialized", http.StatusServiceUnavailable)
		return
	}
	cfg, ok := fleetReg.GetFleet(req.FleetKey)
	if !ok {
		http.Error(w, fmt.Sprintf("Fleet %q not found", req.FleetKey), http.StatusNotFound)
		return
	}

	subAgentMgr := tools.GetSubAgentManager()
	if subAgentMgr == nil {
		http.Error(w, "Sub-agent system not initialized (sub-agents must be enabled)", http.StatusServiceUnavailable)
		return
	}

	channel := fleet.NewChatChannel(req.FleetKey)
	fleetSession := fleet.NewFleetSession(req.FleetKey, cfg, channel, subAgentMgr)

	registry := getFleetSessionRegistry()
	registry.Register(fleetSession)
	persistFleetSessionMeta(fleetSession, cfg, 0, "")
	wireFleetTranscript(fleetSession)

	go func() {
		defer func() {
			registry.Unregister(fleetSession.ID)
			log.Printf("[fleet] Session %s removed from registry", fleetSession.ID)
		}()
		if err := fleetSession.Run(context.Background()); err != nil {
			log.Printf("[fleet] Session %s error: %v", fleetSession.ID, err)
		}
	}()

	if req.Message != "" {
		initialMsg := fleet.Message{
			Sender: "customer",
			Text:   req.Message,
		}
		if err := channel.PostMessage(context.Background(), initialMsg); err != nil {
			log.Printf("[fleet] Error posting initial message: %v", err)
		}
		if fleetSession.OnMessagePosted != nil {
			fleetSession.OnMessagePosted(initialMsg)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"session_id": fleetSession.ID,
		"fleet_key":  req.FleetKey,
		"fleet_name": cfg.Name,
		"agents":     buildAgentList(cfg),
	})
}

// persistFleetSessionMeta adds the fleet session to the persistent session index
// so it appears in the sidebar alongside regular chat sessions.
func persistFleetSessionMeta(fs *fleet.FleetSession, fleetCfg *fleet.FleetConfig, issueNumber int, repo string) {
	fileStore := getFleetFileStore()
	if fileStore == nil {
		log.Printf("[fleet] Warning: no file store available, cannot persist fleet session meta for %s", fs.ID)
		return
	}

	now := time.Now()
	meta := session.SessionMeta{
		ID:          fs.ID,
		AppName:     studioChatAppName,
		UserID:      studioChatUserID,
		CreatedAt:   now,
		UpdatedAt:   now,
		Title:       fmt.Sprintf("Fleet: %s", fleetCfg.Name),
		FleetKey:    fs.FleetKey,
		FleetName:   fleetCfg.Name,
		IssueNumber: issueNumber,
		Repo:        repo,
	}

	if err := fileStore.AddSessionMeta(meta); err != nil {
		log.Printf("[fleet] Warning: could not persist fleet session meta: %v", err)
	}
}

// updateFleetSessionMeta updates the message count and timestamp for a fleet session in the index.
func updateFleetSessionMeta(sessionID string, messageCount int) {
	fileStore := getFleetFileStore()
	if fileStore == nil {
		return
	}

	_ = fileStore.UpdateSessionMeta(sessionID, func(meta *session.SessionMeta) {
		meta.MessageCount = messageCount
		meta.UpdatedAt = time.Now()
	})
}

// wireFleetTranscript creates a JSONL transcript file for a fleet session
// and wires up the OnMessagePosted callback to persist messages to it.
// This makes fleet sessions visible via `astonish sessions show <id>`.
func wireFleetTranscript(fs *fleet.FleetSession) {
	fileStore := getFleetFileStore()
	if fileStore == nil {
		log.Printf("[fleet] Warning: no file store available, cannot create transcript for %s", fs.ID)
		return
	}

	// Create the transcript file in the same location as regular session transcripts
	transcriptPath := filepath.Join(fileStore.BaseDir(), studioChatAppName, studioChatUserID, fs.ID+".jsonl")
	transcript := session.NewTranscript(transcriptPath)

	if err := transcript.WriteHeader(fs.ID); err != nil {
		log.Printf("[fleet] Warning: could not create fleet transcript: %v", err)
		return
	}

	// Wire the callback so every message posted to the channel gets persisted
	var invocationCounter int
	fs.OnMessagePosted = func(msg fleet.Message) {
		invocationCounter++
		event := fleetMessageToEvent(msg, invocationCounter)
		if err := transcript.AppendEvent(event); err != nil {
			log.Printf("[fleet] Warning: could not persist fleet message: %v", err)
		}
	}
}

// fleetMessageToEvent converts a fleet message to an ADK session event
// so it can be stored in the JSONL transcript and read by `sessions show`.
func fleetMessageToEvent(msg fleet.Message, invocationNum int) *adksession.Event {
	// Map fleet sender to ADK role
	role := genai.RoleModel
	author := msg.Sender
	if msg.Sender == "customer" {
		role = genai.RoleUser
		author = "user"
	}

	content := &genai.Content{
		Role: role,
		Parts: []*genai.Part{
			genai.NewPartFromText(msg.Text),
		},
	}

	return &adksession.Event{
		LLMResponse: adkmodel.LLMResponse{
			Content: content,
		},
		ID:           msg.ID,
		Timestamp:    msg.Timestamp,
		InvocationID: fmt.Sprintf("fleet-turn-%d", invocationNum),
		Author:       author,
	}
}

// FleetMessageHandler handles POST /api/studio/fleet/sessions/{id}/message.
// It posts a human message to an active fleet session.
func FleetMessageHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["id"]

	var req FleetMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	registry := getFleetSessionRegistry()
	if err := registry.PostHumanMessage(sessionID, req.Message); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}

// FleetSessionStatusHandler handles GET /api/studio/fleet/sessions/{id}.
// Returns the current state of a fleet session.
func FleetSessionStatusHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["id"]

	registry := getFleetSessionRegistry()
	fs := registry.Get(sessionID)
	if fs == nil {
		http.Error(w, "Fleet session not found", http.StatusNotFound)
		return
	}

	state, activeAgent := fs.GetState()

	// Get thread history
	thread, err := fs.Channel.GetThread(r.Context())
	if err != nil {
		http.Error(w, "Failed to get thread: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"session_id":   fs.ID,
		"fleet_key":    fs.FleetKey,
		"fleet_name":   fs.FleetConfig.Name,
		"state":        string(state),
		"active_agent": activeAgent,
		"messages":     thread,
		"agents":       buildAgentList(fs.FleetConfig),
	})
}

// FleetSessionsListHandler handles GET /api/studio/fleet/sessions.
// Lists all active fleet sessions.
func FleetSessionsListHandler(w http.ResponseWriter, r *http.Request) {
	registry := getFleetSessionRegistry()
	sessions := registry.List()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sessions": sessions,
	})
}

// FleetSessionStopHandler handles POST /api/studio/fleet/sessions/{id}/stop.
func FleetSessionStopHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["id"]

	registry := getFleetSessionRegistry()
	fs := registry.Get(sessionID)
	if fs == nil {
		http.Error(w, "Fleet session not found", http.StatusNotFound)
		return
	}

	fs.Stop()
	registry.Unregister(sessionID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}

// FleetSessionStreamHandler handles GET /api/studio/fleet/sessions/{id}/stream.
// It opens an SSE stream for a fleet session, sending existing thread history
// followed by real-time messages. Supports connect/reconnect independently of
// the fleet session lifecycle.
func FleetSessionStreamHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["id"]

	registry := getFleetSessionRegistry()
	fs := registry.Get(sessionID)
	if fs == nil {
		http.Error(w, "Fleet session not found", http.StatusNotFound)
		return
	}

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()

	var sseMu sync.Mutex
	safeSendSSE := func(eventType string, data interface{}) {
		sseMu.Lock()
		defer sseMu.Unlock()
		SendSSE(w, flusher, eventType, data)
	}

	// Send session info
	safeSendSSE("fleet_session", map[string]interface{}{
		"session_id": fs.ID,
		"fleet_key":  fs.FleetKey,
		"fleet_name": fs.FleetConfig.Name,
		"agents":     buildAgentList(fs.FleetConfig),
	})

	// Send current state
	state, activeAgent := fs.GetState()
	safeSendSSE("fleet_state", map[string]interface{}{
		"state":        string(state),
		"active_agent": activeAgent,
	})

	// Subscribe to new messages BEFORE reading history to avoid missing
	// messages posted between reading history and subscribing.
	subscriberID := uuid.New().String()
	subscribable, canSubscribe := fs.Channel.(fleet.Subscribable)
	var msgCh <-chan fleet.Message
	if canSubscribe {
		msgCh = subscribable.Subscribe(subscriberID)
		defer subscribable.Unsubscribe(subscriberID)
	}

	// Send existing thread history
	thread, err := fs.Channel.GetThread(ctx)
	if err == nil {
		for _, msg := range thread {
			safeSendSSE("fleet_message", map[string]interface{}{
				"id":        msg.ID,
				"sender":    msg.Sender,
				"text":      msg.Text,
				"artifacts": msg.Artifacts,
				"mentions":  msg.Mentions,
				"timestamp": msg.Timestamp,
				"metadata":  msg.Metadata,
			})
		}
	}

	// Track seen message IDs to avoid duplicating messages that arrive
	// between the history read and the subscriber channel.
	seen := make(map[string]bool, len(thread))
	for _, msg := range thread {
		seen[msg.ID] = true
	}

	// Wire up state change callback for this viewer
	prevStateCallback := fs.OnStateChange
	fs.OnStateChange = func(state fleet.SessionState, activeAgent string) {
		safeSendSSE("fleet_state", map[string]interface{}{
			"state":        string(state),
			"active_agent": activeAgent,
		})
		if prevStateCallback != nil {
			prevStateCallback(state, activeAgent)
		}
	}
	defer func() {
		fs.OnStateChange = prevStateCallback
	}()

	// Also update the persistent meta with message count on each new message
	// (debounced via the subscriber loop below)

	// Stream new messages until disconnect or session done
	if msgCh == nil {
		// Not a ChatChannel, just wait for disconnect
		<-ctx.Done()
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgCh:
			if !ok {
				// Channel closed (fleet session ended)
				donePayload := map[string]interface{}{"done": true}
				if lastErr := fs.LastError(); lastErr != nil {
					donePayload["error"] = lastErr.Error()
				}
				safeSendSSE("fleet_done", donePayload)
				return
			}
			if seen[msg.ID] {
				continue // already sent as part of history
			}
			seen[msg.ID] = true
			safeSendSSE("fleet_message", map[string]interface{}{
				"id":        msg.ID,
				"sender":    msg.Sender,
				"text":      msg.Text,
				"artifacts": msg.Artifacts,
				"mentions":  msg.Mentions,
				"timestamp": msg.Timestamp,
				"metadata":  msg.Metadata,
			})

			// Update persistent meta (message count)
			if thread, thErr := fs.Channel.GetThread(ctx); thErr == nil {
				updateFleetSessionMeta(sessionID, len(thread))
			}
		}
	}
}

// buildAgentList creates a list of agent descriptions for the frontend.
func buildAgentList(fleetCfg *fleet.FleetConfig) []map[string]interface{} {
	if fleetCfg == nil {
		return nil
	}

	agents := make([]map[string]interface{}, 0, len(fleetCfg.Agents))
	for key, agentCfg := range fleetCfg.Agents {
		entry := map[string]interface{}{
			"key":         key,
			"name":        agentCfg.Name,
			"description": agentCfg.Description,
			"mode":        agentCfg.GetMode(),
		}

		// Communication info
		if fleetCfg.Communication != nil {
			for _, node := range fleetCfg.Communication.Flow {
				if node.Role == key {
					entry["talks_to"] = node.TalksTo
					entry["entry_point"] = node.EntryPoint
					break
				}
			}
		}

		agents = append(agents, entry)
	}
	return agents
}
