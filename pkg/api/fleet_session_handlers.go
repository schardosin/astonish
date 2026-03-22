package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/sandbox"
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

	// Pre-initialize the session context so that the session appears as
	// "running" immediately (PostHumanMessage checks fs.ctx != nil).
	// Run() will reuse this context instead of creating its own.
	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	fleetSession.InitContext(sessionCtx, sessionCancel)

	// Resolve per-session workspace directory. Each session gets its own
	// isolated workspace (via git clone --local from the base) under the sessions dir.
	// The base workspace (~/astonish_projects/<repo-name>/) is where the wizard
	// cloned the repo and generated AGENTS.md.
	baseDir := plan.ResolveWorkspaceDir()
	var workspaceDir string
	if fleetCfg.ProjectContext != nil || plan.ResolveProjectSource() != nil {
		fileStore := getFleetFileStore()
		if fileStore != nil {
			workspaceDir = fleet.ResolveSessionWorkspaceDir(
				fileStore.BaseDir(), fleetSession.ID, "" /* chat sessions use short session ID */)
			if err := fleet.SetupSessionWorkspace(workspaceDir, plan.ResolveProjectSource(), baseDir); err != nil {
				log.Printf("[fleet] Warning: could not set up workspace %s: %v", workspaceDir, err)
				workspaceDir = "" // fall back to legacy behavior
			}
		}
		// Fall back to the legacy shared workspace if no file store is available
		if workspaceDir == "" {
			workspaceDir = baseDir
			if workspaceDir != "" {
				if err := os.MkdirAll(workspaceDir, 0755); err != nil {
					log.Printf("[fleet] Warning: could not create workspace %s: %v", workspaceDir, err)
					workspaceDir = ""
				}
			}
		}
	}
	fleetSession.WorkspaceDir = workspaceDir

	// Wire sandbox container for this fleet session (no-op if sandbox disabled)
	ghToken := ""
	if credStore := getAPICredentialStore(); credStore != nil {
		resolved, _ := fleet.ResolveCredentials(plan, credStore)
		ghToken = fleet.GitHubToken(resolved)
	}
	wireFleetSandbox(fleetSession, plan, ghToken)

	// Register in session registry and persist metadata
	registry := getFleetSessionRegistry()
	registry.Register(fleetSession)
	persistFleetSessionMeta(fleetSession, fleetCfg, 0, "")
	wireFleetTranscript(fleetSession)

	// Capture the transcript callback so we can compose additional callbacks on top.
	transcriptCallback := fleetSession.OnMessagePosted
	existingDoneCallback := fleetSession.OnSessionDone

	// Auto-start: set ResumeTarget to the entry point agent so Run()
	// activates it immediately on the first iteration without needing
	// a message in the channel. This avoids posting fake customer messages.
	entryPoint := fleetCfg.GetEntryPoint()
	if entryPoint != "" {
		fleetSession.ResumeTarget = entryPoint
	}

	// Start in background. Post the real initial message (if any) and run
	// the session loop immediately. Project context (AGENTS.md) is loaded
	// instantly from the base workspace where the wizard generated it.
	go func() {
		defer func() {
			registry.Unregister(fleetSession.ID)
			log.Printf("[fleet] Session %s removed from registry", fleetSession.ID)
		}()

		// Load project context (AGENTS.md) from the base workspace.
		// The wizard generated it during plan creation; no regeneration needed.
		if baseDir != "" && fleetCfg.ProjectContext != nil {
			pc := fleet.LoadProjectContextFile(baseDir, fleetCfg.ProjectContext)
			if pc != "" {
				fleetSession.ProjectContext = pc
				log.Printf("[fleet] Project context loaded from base for session %s (%d bytes)",
					fleetSession.ID, len(pc))
			}
		}

		// Post the real initial customer message if the user provided one.
		// This goes into the channel so the entry point agent sees it in
		// its thread context when activated via ResumeTarget.
		// MemoryKeys are stamped here so the message is scoped to the entry
		// point agent's memory (not globally visible to all agents).
		// The Run() loop handles transcript persistence via notifyMessagePosted.
		if initialMessage != "" {
			msg := fleet.Message{
				Sender:     "customer",
				Text:       initialMessage,
				MemoryKeys: []string{entryPoint},
			}
			if err := channel.PostMessage(context.Background(), msg); err != nil {
				log.Printf("[fleet] Error posting initial message: %v", err)
			}
		}

		if err := fleetSession.Run(sessionCtx); err != nil {
			log.Printf("[fleet] Session %s error: %v", fleetSession.ID, err)
		}
	}()

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
		entryPoint := cfg.GetEntryPoint()
		initialMsg := fleet.Message{
			Sender:     "customer",
			Text:       req.Message,
			MemoryKeys: []string{entryPoint},
		}
		if err := channel.PostMessage(context.Background(), initialMsg); err != nil {
			log.Printf("[fleet] Error posting initial message: %v", err)
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
		ID:           fs.ID,
		AppName:      studioChatAppName,
		UserID:       studioChatUserID,
		CreatedAt:    now,
		UpdatedAt:    now,
		Title:        fmt.Sprintf("Fleet: %s", fleetCfg.Name),
		FleetKey:     fs.FleetKey,
		FleetName:    fleetCfg.Name,
		IssueNumber:  issueNumber,
		Repo:         repo,
		WorkspaceDir: fs.WorkspaceDir,
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
//
// MemoryKeys are encoded into the InvocationID field as a suffix:
// "fleet-turn-N|mem:architect,po". This piggybacks on an existing free-form
// string field to avoid schema changes to the ADK Event struct.
// eventsToFleetMessages in fleet_recover.go parses it back out.
//
// For backward compatibility, old ThreadKey values are also accepted and
// converted to MemoryKeys on read (see eventsToFleetMessages).
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

	invocationID := fmt.Sprintf("fleet-turn-%d", invocationNum)
	if len(msg.MemoryKeys) > 0 {
		invocationID += "|mem:" + strings.Join(msg.MemoryKeys, ",")
	} else if msg.ThreadKey != "" {
		// Backward compat: old code may still set ThreadKey
		invocationID += "|thread:" + msg.ThreadKey
	}

	return &adksession.Event{
		LLMResponse: adkmodel.LLMResponse{
			Content: content,
		},
		ID:           msg.ID,
		Timestamp:    msg.Timestamp,
		InvocationID: invocationID,
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
	fs.Cleanup() // destroy sandbox container + clean session registry
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
				"id":          msg.ID,
				"sender":      msg.Sender,
				"text":        msg.Text,
				"memory_keys": msg.ResolveMemoryKeys(),
				"artifacts":   msg.Artifacts,
				"mentions":    msg.Mentions,
				"timestamp":   msg.Timestamp,
				"metadata":    msg.Metadata,
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
				"id":          msg.ID,
				"sender":      msg.Sender,
				"text":        msg.Text,
				"memory_keys": msg.ResolveMemoryKeys(),
				"artifacts":   msg.Artifacts,
				"mentions":    msg.Mentions,
				"timestamp":   msg.Timestamp,
				"metadata":    msg.Metadata,
			})

			// Update persistent meta (message count)
			if thread, thErr := fs.Channel.GetThread(ctx); thErr == nil {
				updateFleetSessionMeta(sessionID, len(thread))
			}
		}
	}
}

// FleetSessionThreadsHandler handles GET /api/studio/fleet/sessions/{id}/threads.
// Returns a list of unique pairwise threads in a fleet session with summary info.
// Works for both active sessions (from in-memory channel) and completed sessions
// (from JSONL transcript).
func FleetSessionThreadsHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["id"]

	messages, err := getFleetMessages(sessionID, r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Group messages by agent memory ownership and compute summaries.
	// Each agent gets a summary showing how many messages are in their memory.
	type agentMemorySummary struct {
		AgentKey     string   `json:"agent_key"`
		Participants []string `json:"participants"`
		MessageCount int      `json:"message_count"`
		FirstTS      string   `json:"first_timestamp,omitempty"`
		LastTS       string   `json:"last_timestamp,omitempty"`
	}

	agentMap := make(map[string]*agentMemorySummary)
	participantMap := make(map[string]map[string]bool) // agentKey -> set of senders in their memory

	for _, msg := range messages {
		keys := msg.ResolveMemoryKeys()
		if len(keys) == 0 {
			// System/global message — attribute to _system
			keys = []string{"_system"}
		}

		for _, key := range keys {
			ts, ok := agentMap[key]
			if !ok {
				ts = &agentMemorySummary{AgentKey: key}
				agentMap[key] = ts
				participantMap[key] = make(map[string]bool)
			}

			ts.MessageCount++
			participantMap[key][msg.Sender] = true

			tsStr := msg.Timestamp.Format("2006-01-02T15:04:05Z07:00")
			if ts.FirstTS == "" || tsStr < ts.FirstTS {
				ts.FirstTS = tsStr
			}
			if tsStr > ts.LastTS {
				ts.LastTS = tsStr
			}
		}
	}

	// Build participants lists
	for key, ts := range agentMap {
		pmap := participantMap[key]
		parts := make([]string, 0, len(pmap))
		for p := range pmap {
			parts = append(parts, p)
		}
		ts.Participants = parts
	}

	// Collect into a sorted list (system first, then alphabetical)
	threads := make([]agentMemorySummary, 0, len(agentMap))
	if sys, ok := agentMap["_system"]; ok {
		threads = append(threads, *sys)
	}
	for key, ts := range agentMap {
		if key != "_system" {
			threads = append(threads, *ts)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"threads": threads,
	})
}

// FleetSessionMessagesHandler handles GET /api/studio/fleet/sessions/{id}/messages.
// Returns fleet-level conversation messages, optionally filtered by agent memory.
// Works for both active sessions (from in-memory channel) and completed sessions
// (from JSONL transcript).
//
// Query params:
//
//	agent=dev  - filter to messages in the given agent's memory (includes system messages)
func FleetSessionMessagesHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["id"]

	messages, err := getFleetMessages(sessionID, r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Filter by agent memory if requested
	agentFilter := r.URL.Query().Get("agent")
	if agentFilter != "" {
		filtered := make([]fleet.Message, 0, len(messages))
		for _, msg := range messages {
			if msg.InAgentMemory(agentFilter) {
				filtered = append(filtered, msg)
			}
		}
		messages = filtered
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"messages": messages,
	})
}

// getFleetMessages returns all fleet messages for a session. It first checks
// the active session registry (in-memory), then falls back to reading the
// JSONL transcript (completed sessions).
func getFleetMessages(sessionID string, ctx context.Context) ([]fleet.Message, error) {
	// Try active session first
	registry := getFleetSessionRegistry()
	if fs := registry.Get(sessionID); fs != nil {
		thread, err := fs.Channel.GetThread(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get thread: %w", err)
		}
		return thread, nil
	}

	// Fall back to JSONL transcript
	fileStore := getFleetFileStore()
	if fileStore == nil {
		return nil, fmt.Errorf("session %s not found (no active session, no file store)", sessionID)
	}

	events, err := fileStore.ReadTranscriptEvents(studioChatAppName, studioChatUserID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("session %s not found: %w", sessionID, err)
	}

	messages := eventsToFleetMessages(events)
	if len(messages) == 0 {
		return nil, fmt.Errorf("session %s has no messages", sessionID)
	}

	return messages, nil
}

// wireFleetSandbox creates sandbox infrastructure for a fleet session when
// sandbox mode is enabled. It creates a LazyNodeClient, wraps the global
// SubAgentManager tools with NodeTool proxies, and wires cleanup.
//
// When sandbox is NOT enabled, this is a no-op and the fleet session runs
// tools on the host as before.
//
// Parameters:
//   - fleetSession: the fleet session to wire sandbox into
//   - plan: the fleet plan (for Template and Credentials fields)
//   - ghToken: resolved GitHub token (may be empty)
func wireFleetSandbox(fleetSession *fleet.FleetSession, plan *fleet.FleetPlan, ghToken string) {
	// Load config to check if sandbox is enabled
	appCfg, err := config.LoadAppConfig()
	if err != nil || appCfg == nil {
		return
	}

	if !sandbox.IsSandboxEnabled(&appCfg.Sandbox) {
		return
	}

	sandboxClient, sandboxErr := sandbox.SetupSandboxRuntime()
	if sandboxErr != nil {
		log.Printf("[fleet-sandbox] Warning: sandbox enabled but setup failed: %v (tools will run on host)", sandboxErr)
		return
	}

	sessRegistry, regErr := sandbox.NewSessionRegistry()
	if regErr != nil {
		log.Printf("[fleet-sandbox] Warning: failed to create session registry: %v", regErr)
		return
	}

	tplRegistry, tplErr := sandbox.NewTemplateRegistry()
	if tplErr != nil {
		log.Printf("[fleet-sandbox] Warning: failed to create template registry: %v", tplErr)
		// Continue with nil — overlay will fall back gracefully
	}

	// Determine which template to use: plan template or @base
	template := ""
	if plan != nil {
		template = plan.Template
	}

	// Create a lazy node client for this fleet session
	lazyNode := sandbox.NewLazyNodeClient(sandboxClient, sessRegistry, tplRegistry, template)

	// Build env vars to inject into the container
	lazyNode.Env = buildSandboxEnv(plan, ghToken)

	// Wrap the global SubAgentManager tools with NodeTool proxies.
	// The SubAgentManager.Tools already contains the host-wrapped tools
	// (for chat sessions). Fleet sessions need their own copy wrapped
	// with their own LazyNodeClient. Double-wrapping is harmless.
	subAgentMgr := tools.GetSubAgentManager()
	if subAgentMgr == nil {
		log.Printf("[fleet-sandbox] Warning: sub-agent manager not available")
		lazyNode.Cleanup()
		return
	}

	// Wrap both regular tools and fleet-only tools
	wrappedTools := sandbox.WrapToolsWithNodeClient(subAgentMgr.Tools, lazyNode)
	if subAgentMgr.FleetTools != nil {
		wrappedFleetTools := sandbox.WrapToolsWithNodeClient(subAgentMgr.FleetTools, lazyNode)
		wrappedTools = append(wrappedTools, wrappedFleetTools...)
	}

	fleetSession.SandboxTools = wrappedTools

	// Set workspace to the project directory inside the container.
	// The wizard records this path when it clones the repo during template
	// creation (e.g., "/root/juicytrade"). Falls back to /root for plans
	// without a configured container workspace.
	if plan != nil && plan.ContainerWorkspaceDir != "" {
		fleetSession.WorkspaceDir = plan.ContainerWorkspaceDir
	} else {
		fleetSession.WorkspaceDir = "/root"
	}

	// Wire cleanup to destroy the container on session deletion (NOT on Run() exit)
	fleetSession.OnCleanup = func() {
		lazyNode.Cleanup()
	}

	log.Printf("[fleet-sandbox] Session %s: sandbox enabled (template=%q, env_keys=%d)",
		fleetSession.ID, template, len(lazyNode.Env))
}

// buildSandboxEnv builds the environment variable map for a fleet session's
// sandbox container. This injects credentials into the container so tools
// like `gh` and `git` can authenticate.
func buildSandboxEnv(plan *fleet.FleetPlan, ghToken string) map[string]string {
	env := make(map[string]string)

	// GH_TOKEN enables GitHub CLI and git credential helper
	if ghToken != "" {
		env["GH_TOKEN"] = ghToken
	}

	// Resolve BIFROST_API_KEY from credential store for delegate subprocess auth
	credStore := getAPICredentialStore()
	if credStore != nil && plan != nil {
		resolved, _ := fleet.ResolveCredentials(plan, credStore)
		// If the plan has a github credential but we didn't get ghToken from
		// the plan activator, try to resolve it here
		if ghToken == "" {
			if t := fleet.GitHubToken(resolved); t != "" {
				env["GH_TOKEN"] = t
			}
		}
	}

	// BIFROST_API_KEY for delegate sub-processes (OpenCode)
	if key := os.Getenv("BIFROST_API_KEY"); key != "" {
		env["BIFROST_API_KEY"] = key
	}

	return env
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
