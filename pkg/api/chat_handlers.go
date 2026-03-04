package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/agent"
	persistentsession "github.com/schardosin/astonish/pkg/session"
	"github.com/schardosin/astonish/pkg/tools"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// StudioChatRequest is the request body for POST /api/studio/chat.
type StudioChatRequest struct {
	SessionID   string `json:"sessionId,omitempty"`
	Message     string `json:"message"`
	AutoApprove bool   `json:"autoApprove,omitempty"`
}

// StudioSessionResponse is a single session in list responses.
type StudioSessionResponse struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
	MessageCount int    `json:"messageCount"`
}

// StudioSessionDetailResponse is the response for GET /api/studio/sessions/{id}.
type StudioSessionDetailResponse struct {
	StudioSessionResponse
	Messages []StudioMessage `json:"messages"`
}

// StudioMessage is a simplified message for the frontend.
type StudioMessage struct {
	Type       string      `json:"type"`                 // user, agent, tool_call, tool_result, system, fleet_execution
	Content    string      `json:"content,omitempty"`    // text content
	ToolName   string      `json:"toolName,omitempty"`   // for tool_call/tool_result
	ToolArgs   interface{} `json:"toolArgs,omitempty"`   // for tool_call
	ToolResult interface{} `json:"toolResult,omitempty"` // for tool_result

	// Fleet execution fields (for type=fleet_execution)
	FleetEvents  []FleetEventMsg `json:"events,omitempty"` // reconstructed fleet events
	FleetStatus  string          `json:"status,omitempty"` // "complete"
	CurrentPhase *string         `json:"currentPhase,omitempty"`
	CurrentAgent *string         `json:"currentAgent,omitempty"`
}

// FleetEventMsg is a single reconstructed fleet event for the frontend panel.
type FleetEventMsg struct {
	Type    string         `json:"type"` // phase_start, phase_complete, worker_tool_call, etc.
	Phase   string         `json:"phase,omitempty"`
	Agent   string         `json:"agent,omitempty"`
	Message string         `json:"message,omitempty"`
	Detail  string         `json:"detail,omitempty"`
	Args    map[string]any `json:"args,omitempty"`
	Result  interface{}    `json:"result,omitempty"`
	Text    string         `json:"text,omitempty"`
}

// StudioChatComponents holds the wired components needed by Studio chat handlers.
// Set via SetStudioChatInitFunc from the launcher package to avoid import cycles.
type StudioChatComponents struct {
	ChatAgent         *agent.ChatAgent
	LLM               model.LLM
	SessionService    session.Service
	ProviderName      string
	ModelName         string
	Compactor         *persistentsession.Compactor
	InternalToolCount int
	MemoryActive      bool
	Cleanup           func()
}

// ChatManager manages a singleton chat agent for Studio chat.
type ChatManager struct {
	mu         sync.Mutex
	components *StudioChatComponents
	initFn     func(ctx context.Context) (*StudioChatComponents, error)

	// active holds cancel functions for in-flight SSE streams keyed by session ID.
	active map[string]context.CancelFunc
	amu    sync.Mutex
}

const (
	studioChatAppName = "astonish"
	studioChatUserID  = "studio_user"
)

// globalChatManager is the singleton for Studio chat.
var globalChatManager *ChatManager
var chatManagerOnce sync.Once

// GetChatManager returns the singleton ChatManager.
func GetChatManager() *ChatManager {
	chatManagerOnce.Do(func() {
		globalChatManager = &ChatManager{
			active: make(map[string]context.CancelFunc),
		}
	})
	return globalChatManager
}

// SetStudioChatInitFunc sets the factory function used to initialize the chat agent.
// Called from the launcher package to avoid import cycles.
func SetStudioChatInitFunc(fn func(ctx context.Context) (*StudioChatComponents, error)) {
	GetChatManager().initFn = fn
}

// Reset tears down the current chat agent so the next request re-initializes
// with fresh config from disk. This is called when settings change (provider,
// model, MCP, etc.) to ensure new chats pick up the updated configuration.
func (cm *ChatManager) Reset() {
	// Cancel all in-flight SSE streams first.
	cm.amu.Lock()
	for sid, cancel := range cm.active {
		cancel()
		delete(cm.active, sid)
	}
	cm.amu.Unlock()

	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.components != nil {
		if cm.components.Cleanup != nil {
			cm.components.Cleanup()
		}
		cm.components = nil
	}
}

// ensureReady lazily initializes the ChatAgent on first use.
func (cm *ChatManager) ensureReady(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.components != nil {
		return nil
	}
	if cm.initFn == nil {
		return fmt.Errorf("studio chat not initialized: no init function set")
	}

	components, err := cm.initFn(ctx)
	if err != nil {
		return err
	}

	cm.components = components
	return nil
}

// registerStream stores a cancel function for an active SSE stream.
func (cm *ChatManager) registerStream(sessionID string, cancel context.CancelFunc) {
	cm.amu.Lock()
	defer cm.amu.Unlock()
	if prev, ok := cm.active[sessionID]; ok {
		prev()
	}
	cm.active[sessionID] = cancel
}

// unregisterStream removes the cancel function for a session.
func (cm *ChatManager) unregisterStream(sessionID string) {
	cm.amu.Lock()
	defer cm.amu.Unlock()
	delete(cm.active, sessionID)
}

// cancelStream cancels an active stream for a session.
func (cm *ChatManager) cancelStream(sessionID string) {
	cm.amu.Lock()
	defer cm.amu.Unlock()
	if cancel, ok := cm.active[sessionID]; ok {
		cancel()
		delete(cm.active, sessionID)
	}
}

// fileStore returns the FileStore from the components, or nil.
func (cm *ChatManager) fileStore() *persistentsession.FileStore {
	if cm.components == nil {
		return nil
	}
	fs, _ := cm.components.SessionService.(*persistentsession.FileStore)
	return fs
}

// StudioChatHandler handles POST /api/studio/chat with SSE streaming.
func StudioChatHandler(w http.ResponseWriter, r *http.Request) {
	var req StudioChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cm := GetChatManager()
	if err := cm.ensureReady(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

	// Create a cancellable context for this stream
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	comp := cm.components
	chatAgent := comp.ChatAgent
	sessionService := comp.SessionService

	// Handle slash commands server-side
	msg := strings.TrimSpace(req.Message)
	fleetRequested := false

	// /fleet: handle bare form as a slash command; /fleet <task> falls through to normal message flow
	if msg == "/fleet" {
		info := tools.ListAvailableFleets()
		SendSSE(w, flusher, "system", map[string]interface{}{"content": info})
		SendSSE(w, flusher, "done", map[string]interface{}{"done": true})
		return
	}
	if strings.HasPrefix(msg, "/fleet ") {
		// Strip the /fleet prefix; the task goes to the agent as a normal user message.
		msg = strings.TrimSpace(strings.TrimPrefix(msg, "/fleet"))
		fleetRequested = true
	}

	if strings.HasPrefix(msg, "/") {
		handleSlashCommand(ctx, w, flusher, cm, msg, req.SessionID)
		return
	}

	// Create or resume session
	sessionID := req.SessionID
	isNew := false
	if sessionID == "" {
		resp, err := sessionService.Create(ctx, &session.CreateRequest{
			AppName: studioChatAppName,
			UserID:  studioChatUserID,
		})
		if err != nil {
			SendErrorSSE(w, flusher, fmt.Sprintf("Failed to create session: %v", err))
			return
		}
		sessionID = resp.Session.ID()
		isNew = true
	}

	// Register this stream so it can be cancelled
	cm.registerStream(sessionID, cancel)
	defer cm.unregisterStream(sessionID)

	// Send session info first (so frontend knows the session ID for new sessions)
	SendSSE(w, flusher, "session", map[string]interface{}{
		"sessionId": sessionID,
		"isNew":     isNew,
	})

	// Prepare the ADK runner
	adkAgent, err := adkagent.New(adkagent.Config{
		Name:        "astonish_chat",
		Description: "Astonish intelligent chat agent",
		Run:         chatAgent.Run,
	})
	if err != nil {
		SendErrorSSE(w, flusher, fmt.Sprintf("Failed to create agent: %v", err))
		return
	}

	rnr, err := runner.New(runner.Config{
		AppName:        studioChatAppName,
		Agent:          adkAgent,
		SessionService: sessionService,
	})
	if err != nil {
		SendErrorSSE(w, flusher, fmt.Sprintf("Failed to create runner: %v", err))
		return
	}

	// Set auto-approve for this request
	chatAgent.AutoApprove = req.AutoApprove

	// Prepare user message
	var userMsg *genai.Content
	if msg != "" {
		if fleetRequested {
			// /fleet <task>: send as a multi-part user message with fleet instruction
			userMsg = &genai.Content{
				Role: genai.RoleUser,
				Parts: []*genai.Part{
					genai.NewPartFromText("[FLEET MODE] Use fleet_plan to create a plan for this task, then fleet_execute to run it. Do NOT do the work yourself."),
					genai.NewPartFromText(msg),
				},
			}
		} else {
			userMsg = genai.NewContentFromText(msg, genai.RoleUser)
		}
	}

	// Mutex for safe concurrent SSE writes (main event loop + fleet progress goroutine)
	var sseMu sync.Mutex
	safeSendSSE := func(eventType string, data interface{}) {
		sseMu.Lock()
		defer sseMu.Unlock()
		SendSSE(w, flusher, eventType, data)
	}

	// Start fleet progress streaming goroutine. It polls for a fleet progress channel
	// (created when fleet_execute is called) and forwards events as SSE.
	// Uses its own context so we can cancel it after the event loop finishes.
	fleetCtx, fleetCancel := context.WithCancel(ctx)
	fleetProgressDone := make(chan struct{})
	go func() {
		defer close(fleetProgressDone)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		var ch <-chan tools.FleetProgressEvent
		for {
			select {
			case <-fleetCtx.Done():
				return
			case <-ticker.C:
				ch = tools.GetFleetProgressCh(sessionID)
				if ch != nil {
					ticker.Stop()
					goto stream
				}
			}
		}
	stream:
		for {
			select {
			case <-fleetCtx.Done():
				return
			case evt, ok := <-ch:
				if !ok {
					return // channel closed, fleet execution done
				}
				payload := map[string]interface{}{
					"type":    evt.Type,
					"phase":   evt.Phase,
					"agent":   evt.Agent,
					"message": evt.Message,
					"detail":  evt.Detail,
				}
				// Include rich data fields when present (for full sub-thread rendering)
				if evt.Args != nil {
					payload["args"] = evt.Args
				}
				if evt.Result != nil {
					payload["result"] = evt.Result
				}
				if evt.Text != "" {
					payload["text"] = evt.Text
				}
				safeSendSSE("fleet_progress", payload)
			}
		}
	}()

	// Run the agent and stream events
	for event, runErr := range rnr.Run(ctx, studioChatUserID, sessionID, userMsg, adkagent.RunConfig{
		StreamingMode: adkagent.StreamingModeSSE,
	}) {
		if runErr != nil {
			safeSendSSE("error", map[string]string{"error": runErr.Error()})
			break
		}

		// Process state delta for tool approval, spinner, retry, errors
		if event.Actions.StateDelta != nil {
			delta := event.Actions.StateDelta

			// Tool approval request
			if optsVal, ok := delta["approval_options"]; ok {
				toolName, _ := delta["approval_tool"].(string)
				var options []interface{}
				switch v := optsVal.(type) {
				case []string:
					for _, s := range v {
						options = append(options, s)
					}
				case []interface{}:
					options = v
				}
				safeSendSSE("approval", map[string]interface{}{
					"tool":    toolName,
					"options": options,
				})
			}

			// Auto-approval notification
			if autoApproved, ok := delta["auto_approved"].(bool); ok && autoApproved {
				if toolName, ok := delta["approval_tool"].(string); ok {
					safeSendSSE("auto_approved", map[string]interface{}{
						"tool": toolName,
					})
				}
			}

			// Retry info
			if retryInfoVal, ok := delta["_retry_info"]; ok {
				if retryInfo, ok := retryInfoVal.(map[string]interface{}); ok {
					attempt := toInt(retryInfo["attempt"])
					maxRetries := toInt(retryInfo["max_retries"])
					reason, _ := retryInfo["reason"].(string)
					safeSendSSE("retry", map[string]interface{}{
						"attempt":    attempt,
						"maxRetries": maxRetries,
						"reason":     reason,
					})
				}
			}

			// Failure info
			if failureInfoVal, ok := delta["_failure_info"]; ok {
				if failureInfo, ok := failureInfoVal.(map[string]interface{}); ok {
					title, _ := failureInfo["title"].(string)
					reason, _ := failureInfo["reason"].(string)
					originalError, _ := failureInfo["original_error"].(string)
					suggestion, _ := failureInfo["suggestion"].(string)
					safeSendSSE("error_info", map[string]interface{}{
						"title":         title,
						"reason":        reason,
						"suggestion":    suggestion,
						"originalError": originalError,
					})
				}
			}

			// Spinner / thinking text
			if spinnerText, ok := delta["_spinner_text"].(string); ok {
				safeSendSSE("thinking", map[string]interface{}{
					"text": spinnerText,
				})
			}
		}

		// Process content parts
		if event.LLMResponse.Content != nil {
			for _, part := range event.LLMResponse.Content.Parts {
				// Streaming text
				if part.Text != "" {
					safeSendSSE("text", map[string]interface{}{
						"text": part.Text,
					})
				}
				// Tool call
				if part.FunctionCall != nil {
					safeSendSSE("tool_call", map[string]interface{}{
						"name": part.FunctionCall.Name,
						"args": part.FunctionCall.Args,
					})
				}
				// Tool result
				if part.FunctionResponse != nil {
					safeSendSSE("tool_result", map[string]interface{}{
						"name":   part.FunctionResponse.Name,
						"result": summarizeToolResult(part.FunctionResponse.Response),
					})
					// Drain any images stashed by the tool (e.g., browser screenshots)
					for _, img := range chatAgent.DrainImages() {
						mimeType := "image/png"
						if img.Format == "jpeg" || img.Format == "jpg" {
							mimeType = "image/jpeg"
						}
						safeSendSSE("image", map[string]interface{}{
							"data":     base64.StdEncoding.EncodeToString(img.Data),
							"mimeType": mimeType,
						})
					}
				}
			}
		}
	}

	// Stop fleet progress goroutine and wait for it to finish
	fleetCancel()
	<-fleetProgressDone

	// Generate title for new sessions after first exchange
	if isNew && msg != "" {
		if fs := cm.fileStore(); fs != nil {
			go generateStudioSessionTitle(comp.LLM, fs, sessionID, msg)
		}
	}

	SendSSE(w, flusher, "done", map[string]interface{}{"done": true})
}

// handleSlashCommand processes slash commands and sends results as SSE events.
func handleSlashCommand(ctx context.Context, w io.Writer, flusher http.Flusher, cm *ChatManager, cmd, sessionID string) {
	comp := cm.components
	chatAgent := comp.ChatAgent
	sessionService := comp.SessionService

	switch {
	case cmd == "/help":
		SendSSE(w, flusher, "system", map[string]interface{}{
			"content": "**Available commands:**\n" +
				"- `/status` — Show current provider, model, tools, and memory status\n" +
				"- `/new` — Start a fresh conversation\n" +
				"- `/compact` — Show context window usage and compaction status\n" +
				"- `/distill` — Distill the last task into a reusable flow\n" +
				"- `/fleet <task>` — Start a fleet-based task with specialized agents\n" +
				"- `/help` — Show this help message",
		})

	case cmd == "/status":
		status := fmt.Sprintf("**Status**\n- Provider: `%s`\n- Model: `%s`\n", comp.ProviderName, comp.ModelName)
		if comp.Compactor != nil {
			est, win := comp.Compactor.TokenUsage()
			pct := float64(0)
			if win > 0 {
				pct = float64(est) / float64(win) * 100
			}
			status += fmt.Sprintf("- Context: %d / %d tokens (%.0f%%)\n", est, win, pct)
		}
		status += fmt.Sprintf("- Tools: %d internal\n", comp.InternalToolCount)
		if comp.MemoryActive {
			status += "- Memory: active\n"
		} else {
			status += "- Memory: disabled\n"
		}
		if chatAgent.FlowRegistry != nil {
			entries := chatAgent.FlowRegistry.Entries()
			status += fmt.Sprintf("- Flows: %d saved\n", len(entries))
		}
		if sessionID != "" {
			shortID := sessionID
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}
			status += fmt.Sprintf("- Session: `%s`", shortID)
		}
		SendSSE(w, flusher, "system", map[string]interface{}{"content": status})

	case cmd == "/compact":
		if comp.Compactor == nil {
			SendSSE(w, flusher, "system", map[string]interface{}{"content": "Compaction is disabled."})
		} else {
			est, win := comp.Compactor.TokenUsage()
			pct := float64(0)
			if win > 0 {
				pct = float64(est) / float64(win) * 100
			}
			msg := fmt.Sprintf("**Context Window**\n- Tokens: %d / %d (%.0f%%)\n- Threshold: %.0f%%\n- Compactions: %d",
				est, win, pct, comp.Compactor.Threshold*100, comp.Compactor.CompactionCount())
			SendSSE(w, flusher, "system", map[string]interface{}{"content": msg})
		}

	case cmd == "/new":
		resp, err := sessionService.Create(ctx, &session.CreateRequest{
			AppName: studioChatAppName,
			UserID:  studioChatUserID,
		})
		if err != nil {
			SendErrorSSE(w, flusher, fmt.Sprintf("Failed to create session: %v", err))
		} else {
			SendSSE(w, flusher, "new_session", map[string]interface{}{
				"sessionId": resp.Session.ID(),
			})
		}

	case cmd == "/distill":
		if sessionID == "" {
			SendSSE(w, flusher, "system", map[string]interface{}{
				"content": "No active session to distill.",
			})
		} else {
			ds := agent.DistillSession{
				SessionID: sessionID,
				AppName:   studioChatAppName,
				UserID:    studioChatUserID,
			}
			description, err := chatAgent.PreviewDistill(ctx, ds)
			if err != nil {
				SendSSE(w, flusher, "system", map[string]interface{}{
					"content": fmt.Sprintf("Cannot distill: %v", err),
				})
			} else {
				SendSSE(w, flusher, "distill_preview", map[string]interface{}{
					"description": description,
					"sessionId":   sessionID,
				})
			}
		}

	default:
		SendSSE(w, flusher, "system", map[string]interface{}{
			"content": fmt.Sprintf("Unknown command: `%s`. Type `/help` for available commands.", cmd),
		})
	}
	SendSSE(w, flusher, "done", map[string]interface{}{"done": true})
}

// StudioSessionsHandler handles GET /api/studio/sessions.
func StudioSessionsHandler(w http.ResponseWriter, r *http.Request) {
	cm := GetChatManager()
	if err := cm.ensureReady(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fs := cm.fileStore()
	if fs == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]StudioSessionResponse{})
		return
	}

	metas, err := fs.ListSessionMetas(studioChatAppName, studioChatUserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Sort by updated_at descending (most recent first)
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].UpdatedAt.After(metas[j].UpdatedAt)
	})

	sessions := make([]StudioSessionResponse, 0, len(metas))
	for _, m := range metas {
		sessions = append(sessions, StudioSessionResponse{
			ID:           m.ID,
			Title:        m.Title,
			CreatedAt:    m.CreatedAt.Format(time.RFC3339),
			UpdatedAt:    m.UpdatedAt.Format(time.RFC3339),
			MessageCount: m.MessageCount,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions)
}

// StudioSessionHandler handles GET /api/studio/sessions/{id}.
func StudioSessionHandler(w http.ResponseWriter, r *http.Request) {
	cm := GetChatManager()
	if err := cm.ensureReady(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sessionID := mux.Vars(r)["id"]
	fs := cm.fileStore()
	if fs == nil {
		http.Error(w, "File store not available", http.StatusInternalServerError)
		return
	}

	// Get session metadata
	meta, err := fs.GetSessionMeta(sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}

	// Get session transcript
	getResp, err := fs.Get(r.Context(), &session.GetRequest{
		AppName:   studioChatAppName,
		UserID:    studioChatUserID,
		SessionID: sessionID,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load session: %v", err), http.StatusInternalServerError)
		return
	}

	// Transform ADK events into simplified messages
	messages := eventsToMessages(getResp.Session.Events(), fs, sessionID)

	resp := StudioSessionDetailResponse{
		StudioSessionResponse: StudioSessionResponse{
			ID:           meta.ID,
			Title:        meta.Title,
			CreatedAt:    meta.CreatedAt.Format(time.RFC3339),
			UpdatedAt:    meta.UpdatedAt.Format(time.RFC3339),
			MessageCount: meta.MessageCount,
		},
		Messages: messages,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// StudioDeleteSessionHandler handles DELETE /api/studio/sessions/{id}.
func StudioDeleteSessionHandler(w http.ResponseWriter, r *http.Request) {
	cm := GetChatManager()
	if err := cm.ensureReady(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sessionID := mux.Vars(r)["id"]
	sessionService := cm.components.SessionService

	err := sessionService.Delete(r.Context(), &session.DeleteRequest{
		AppName:   studioChatAppName,
		UserID:    studioChatUserID,
		SessionID: sessionID,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete session: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// StudioStopHandler handles POST /api/studio/sessions/{id}/stop.
func StudioStopHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["id"]
	cm := GetChatManager()
	cm.cancelStream(sessionID)
	w.WriteHeader(http.StatusNoContent)
}

// eventsToMessages transforms ADK session events into a flat message list for the frontend.
// When fs and sessionID are provided, fleet_execute tool calls are enriched with
// reconstructed fleet execution data from child session transcripts.
func eventsToMessages(events session.Events, fs *persistentsession.FileStore, sessionID string) []StudioMessage {
	var messages []StudioMessage

	for i := range events.Len() {
		event := events.At(i)
		if event.LLMResponse.Content == nil {
			continue
		}

		role := string(event.LLMResponse.Content.Role)

		for _, part := range event.LLMResponse.Content.Parts {
			if part.Text != "" {
				msgType := "agent"
				if role == "user" {
					msgType = "user"
				}
				// Coalesce with previous message of same type
				if len(messages) > 0 {
					last := &messages[len(messages)-1]
					if last.Type == msgType && last.ToolName == "" {
						last.Content += part.Text
						continue
					}
				}
				messages = append(messages, StudioMessage{
					Type:    msgType,
					Content: part.Text,
				})
			}
			if part.FunctionCall != nil {
				messages = append(messages, StudioMessage{
					Type:     "tool_call",
					ToolName: part.FunctionCall.Name,
					ToolArgs: part.FunctionCall.Args,
				})
			}
			if part.FunctionResponse != nil {
				// When fleet_execute completes, reconstruct the fleet execution panel
				// from child session transcripts so it survives page reload
				if part.FunctionResponse.Name == "fleet_execute" && fs != nil && sessionID != "" {
					if fleetMsg := buildFleetExecutionMessage(fs, sessionID); fleetMsg != nil {
						messages = append(messages, *fleetMsg)
					}
				}
				messages = append(messages, StudioMessage{
					Type:       "tool_result",
					ToolName:   part.FunctionResponse.Name,
					ToolResult: summarizeToolResult(part.FunctionResponse.Response),
				})
			}
		}
	}

	return messages
}

// buildFleetExecutionMessage reconstructs a fleet_execution message from child
// session transcripts. This allows the FleetExecutionPanel to render on reload
// even though the real-time fleet_progress SSE events are ephemeral.
func buildFleetExecutionMessage(fs *persistentsession.FileStore, parentSessionID string) *StudioMessage {
	// Find orchestrator session (direct child of the main session)
	children, err := fs.ListChildren(parentSessionID)
	if err != nil || len(children) == 0 {
		return nil
	}

	var fleetEvents []FleetEventMsg

	// Process each orchestrator child session
	for _, orchestrator := range children {
		// Read orchestrator transcript to find run_fleet_phase calls
		orchEvents, err := fs.ReadTranscriptEvents(orchestrator.AppName, orchestrator.UserID, orchestrator.ID)
		if err != nil || len(orchEvents) == 0 {
			continue
		}

		// Walk orchestrator events to reconstruct phase starts/completions
		for _, ev := range orchEvents {
			if ev.Content == nil {
				continue
			}
			for _, part := range ev.Content.Parts {
				if part.FunctionCall != nil && part.FunctionCall.Name == "run_fleet_phase" {
					phaseName := "unknown"
					agentKey := "unknown"
					if args := part.FunctionCall.Args; args != nil {
						if p, ok := args["phase"].(string); ok && p != "" {
							phaseName = p
						}
						if a, ok := args["primary"].(string); ok && a != "" {
							agentKey = a
						}
					}
					fleetEvents = append(fleetEvents, FleetEventMsg{
						Type:    "phase_start",
						Phase:   phaseName,
						Agent:   agentKey,
						Message: fmt.Sprintf("Starting phase: %s (agent: %s)", phaseName, agentKey),
					})
				}
				if part.FunctionResponse != nil && part.FunctionResponse.Name == "run_fleet_phase" {
					phaseName := ""
					status := "success"
					if resp := part.FunctionResponse.Response; resp != nil {
						if p, ok := resp["phase"].(string); ok {
							phaseName = p
						}
						if s, ok := resp["status"].(string); ok {
							status = s
						}
					}
					evtType := "phase_complete"
					if status != "success" {
						evtType = "phase_failed"
					}
					fleetEvents = append(fleetEvents, FleetEventMsg{
						Type:    evtType,
						Phase:   phaseName,
						Message: fmt.Sprintf("Phase %s: %s", phaseName, status),
					})
				}
				// Orchestrator text
				if part.Text != "" && part.FunctionCall == nil && part.FunctionResponse == nil && !part.Thought {
					fleetEvents = append(fleetEvents, FleetEventMsg{
						Type:    "text",
						Message: part.Text,
						Text:    part.Text,
					})
				}
			}
		}

		// Find worker sessions (children of the orchestrator)
		workers, err := fs.ListChildren(orchestrator.ID)
		if err != nil {
			continue
		}

		for _, worker := range workers {
			workerEvents, err := fs.ReadTranscriptEvents(worker.AppName, worker.UserID, worker.ID)
			if err != nil || len(workerEvents) == 0 {
				continue
			}

			// Derive phase/agent from the worker session title or the task user message
			phaseName, agentKey := extractPhaseInfo(worker.Title, workerEvents)

			for _, ev := range workerEvents {
				if ev.Content == nil {
					continue
				}
				role := string(ev.Content.Role)
				for _, part := range ev.Content.Parts {
					// Skip user messages (they're the task description)
					if role == "user" {
						continue
					}
					if part.FunctionCall != nil {
						fleetEvents = append(fleetEvents, FleetEventMsg{
							Type:    "worker_tool_call",
							Phase:   phaseName,
							Agent:   agentKey,
							Message: fmt.Sprintf("[%s/%s] Calling %s", phaseName, agentKey, part.FunctionCall.Name),
							Detail:  part.FunctionCall.Name,
							Args:    part.FunctionCall.Args,
						})
					}
					if part.FunctionResponse != nil {
						fleetEvents = append(fleetEvents, FleetEventMsg{
							Type:    "worker_tool_result",
							Phase:   phaseName,
							Agent:   agentKey,
							Message: fmt.Sprintf("[%s/%s] %s returned", phaseName, agentKey, part.FunctionResponse.Name),
							Detail:  part.FunctionResponse.Name,
							Result:  summarizeToolResult(part.FunctionResponse.Response),
						})
					}
					if part.Text != "" && part.FunctionCall == nil && part.FunctionResponse == nil && !part.Thought {
						fleetEvents = append(fleetEvents, FleetEventMsg{
							Type:    "worker_text",
							Phase:   phaseName,
							Agent:   agentKey,
							Message: part.Text,
							Text:    part.Text,
						})
					}
				}
			}
		}
	}

	if len(fleetEvents) == 0 {
		return nil
	}

	return &StudioMessage{
		Type:        "fleet_execution",
		FleetEvents: fleetEvents,
		FleetStatus: "complete",
	}
}

// extractPhaseInfo derives the phase name and agent key from a worker session.
// It first tries the session title (format: "fleet-<fleet>-<phase>"), then
// scans the first user message for context.
func extractPhaseInfo(title string, events []*session.Event) (string, string) {
	// Worker session names follow the pattern "fleet-<fleet>-<phase>"
	if strings.HasPrefix(title, "fleet-") {
		parts := strings.SplitN(title, "-", 3)
		if len(parts) >= 3 {
			return parts[2], ""
		}
	}

	// Fall back: scan first user message for phase/agent hints
	// The orchestrator's task description often starts with the phase context
	if len(events) > 0 && events[0].Content != nil {
		for _, part := range events[0].Content.Parts {
			if part.Text != "" {
				// Try to extract from task text patterns like "[phase/agent]"
				text := part.Text
				if idx := strings.Index(text, "## Phase"); idx >= 0 {
					line := text[idx:]
					if end := strings.Index(line, "\n"); end > 0 {
						return strings.TrimSpace(line[8:end]), ""
					}
				}
			}
		}
	}

	return "unknown", ""
}

// summarizeToolResult converts a tool response map into a display-friendly value.
func summarizeToolResult(resp map[string]any) interface{} {
	if resp == nil {
		return nil
	}
	if v, ok := resp["result"]; ok {
		if s, ok := v.(string); ok {
			return truncateResult(s)
		}
	}
	if v, ok := resp["output"]; ok {
		if s, ok := v.(string); ok {
			return truncateResult(s)
		}
	}
	if v, ok := resp["error"]; ok {
		return v
	}
	return resp
}

// truncateResult limits tool output to a reasonable length for display.
func truncateResult(s string) string {
	const maxLen = 2000
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n\n... (truncated)"
}

// generateStudioSessionTitle calls the LLM to produce a short session title.
func generateStudioSessionTitle(llm model.LLM, store *persistentsession.FileStore, sessionID, userMessage string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	prompt := fmt.Sprintf(
		"Generate a concise title (5-7 words max) for a conversation that starts with this message. "+
			"Return ONLY the title, no quotes, no punctuation at the end.\n\nUser message: %s", userMessage)

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText(prompt, genai.RoleUser),
		},
		Config: &genai.GenerateContentConfig{
			Temperature:     genai.Ptr(float32(0.3)),
			MaxOutputTokens: 30,
		},
	}

	var title string
	for resp, err := range llm.GenerateContent(ctx, req, false) {
		if err != nil {
			return
		}
		if resp.Content != nil {
			for _, part := range resp.Content.Parts {
				title += part.Text
			}
		}
	}

	title = strings.TrimSpace(title)
	if title == "" {
		return
	}
	if len(title) > 80 {
		title = title[:77] + "..."
	}

	_ = store.SetSessionTitle(sessionID, title)
}

// toInt converts an interface{} to int (handles float64 from JSON).
func toInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	default:
		return 0
	}
}
