package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/credentials"
	adrill "github.com/schardosin/astonish/pkg/drill"
	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/sandbox"
	persistentsession "github.com/schardosin/astonish/pkg/session"
	"github.com/schardosin/astonish/pkg/tools"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// titleThinkTagRe strips <think>/<thinking> blocks that some models emit in
// title-generation responses.
var titleThinkTagRe = regexp.MustCompile(`(?s)<(?:think|thinking)>.*?</(?:think|thinking)>`)

// StudioChatRequest is the request body for POST /api/studio/chat.
type StudioChatRequest struct {
	SessionID     string `json:"sessionId,omitempty"`
	Message       string `json:"message"`
	AutoApprove   bool   `json:"autoApprove,omitempty"`
	SystemContext string `json:"systemContext,omitempty"` // per-turn system instructions (not shown to user)
}

// StudioSessionResponse is a single session in list responses.
type StudioSessionResponse struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
	MessageCount int    `json:"messageCount"`
	FleetKey     string `json:"fleetKey,omitempty"`
	FleetName    string `json:"fleetName,omitempty"`
	IssueNumber  int    `json:"issueNumber,omitempty"`
	Repo         string `json:"repo,omitempty"`
}

// StudioSessionDetailResponse is the response for GET /api/studio/sessions/{id}.
type StudioSessionDetailResponse struct {
	StudioSessionResponse
	Messages      []StudioMessage       `json:"messages"`
	FleetMessages []FleetMessageSummary `json:"fleetMessages,omitempty"`
}

// FleetMessageSummary is a fleet message returned when loading fleet session history.
type FleetMessageSummary struct {
	ID        string         `json:"id,omitempty"`
	Sender    string         `json:"sender"`
	Text      string         `json:"text"`
	Mentions  []string       `json:"mentions,omitempty"`
	Timestamp string         `json:"timestamp,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// StudioMessage is a simplified message for the frontend.
type StudioMessage struct {
	Type       string      `json:"type"`                 // user, agent, tool_call, tool_result, system
	Content    string      `json:"content,omitempty"`    // text content
	ToolName   string      `json:"toolName,omitempty"`   // for tool_call/tool_result
	ToolArgs   interface{} `json:"toolArgs,omitempty"`   // for tool_call
	ToolResult interface{} `json:"toolResult,omitempty"` // for tool_result
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
	SandboxEnabled    bool
	StartupNotices    []string
	ShutdownSandbox   func() // stops containers without destroying (for daemon shutdown)
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

// ShutdownContainers stops sandbox containers without destroying them.
// Used during graceful daemon shutdown — containers are preserved so sessions
// can reconnect after restart. Unlike Reset(), this does not tear down the
// chat agent or destroy containers.
func (cm *ChatManager) ShutdownContainers() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.components != nil && cm.components.ShutdownSandbox != nil {
		cm.components.ShutdownSandbox()
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

	// /fleet [task]: open fleet dialog, optionally pre-populated with the task
	if msg == "/fleet" || strings.HasPrefix(msg, "/fleet ") {
		task := strings.TrimSpace(strings.TrimPrefix(msg, "/fleet"))
		SendSSE(w, flusher, "fleet_redirect", map[string]interface{}{
			"task": task,
		})
		SendSSE(w, flusher, "done", map[string]interface{}{"done": true})
		return
	}

	// /drill: start a drill suite creation conversation
	if msg == "/drill" || strings.HasPrefix(msg, "/drill ") {
		hint := strings.TrimSpace(strings.TrimPrefix(msg, "/drill"))
		eventData := map[string]interface{}{
			"hint":                 hint,
			"wizard_system_prompt": tools.GetDrillWizardPrompt(),
		}
		SendSSE(w, flusher, "drill_redirect", eventData)
		SendSSE(w, flusher, "done", map[string]interface{}{"done": true})
		return
	}

	// /drill-add <suite>: add new drills to an existing suite
	if strings.HasPrefix(msg, "/drill-add ") {
		suiteName := strings.TrimSpace(strings.TrimPrefix(msg, "/drill-add"))
		if suiteName == "" {
			SendSSE(w, flusher, "error", map[string]interface{}{"error": "Usage: /drill-add <suite_name>"})
			SendSSE(w, flusher, "done", map[string]interface{}{"done": true})
			return
		}
		dirs := adrill.DefaultDrillDirs()
		suite, err := adrill.FindSuite(dirs, suiteName)
		if err != nil {
			SendSSE(w, flusher, "error", map[string]interface{}{"error": fmt.Sprintf("Suite %q not found: %v", suiteName, err)})
			SendSSE(w, flusher, "done", map[string]interface{}{"done": true})
			return
		}
		suiteContext := adrill.BuildSuiteContext(suite)
		eventData := map[string]interface{}{
			"suite_name":           suiteName,
			"wizard_system_prompt": tools.GetDrillAddPrompt(suiteName, suiteContext),
		}
		SendSSE(w, flusher, "drill_add_redirect", eventData)
		SendSSE(w, flusher, "done", map[string]interface{}{"done": true})
		return
	}

	// /fleet-plan: start a fleet plan creation conversation
	if msg == "/fleet-plan" || strings.HasPrefix(msg, "/fleet-plan ") {
		hint := strings.TrimSpace(strings.TrimPrefix(msg, "/fleet-plan"))
		eventData := map[string]interface{}{
			"hint": hint,
		}
		// If the hint is a fleet template key, look up the wizard config
		if hint != "" {
			if reg := GetFleetRegistry(); reg != nil {
				if cfg, ok := reg.GetFleet(hint); ok && cfg.PlanWizard != nil {
					eventData["wizard_description"] = cfg.PlanWizard.Description
					eventData["wizard_system_prompt"] = cfg.PlanWizard.SystemPrompt
				}
			}
		}
		SendSSE(w, flusher, "fleet_plan_redirect", eventData)
		SendSSE(w, flusher, "done", map[string]interface{}{"done": true})
		return
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

	// Inject per-turn session context (e.g., fleet plan wizard instructions).
	// Escape {variable} patterns to prevent ADK's InjectSessionState from
	// trying to resolve them as session state keys (e.g. {task} in YAML examples).
	if req.SystemContext != "" {
		chatAgent.SystemPrompt.SessionContext = agent.EscapeCurlyPlaceholders(req.SystemContext)
		defer func() { chatAgent.SystemPrompt.SessionContext = "" }()
	}

	// Prepare user message (with absolute timestamp for temporal context;
	// see agent.NewTimestampedUserContent for cache-stability rationale).
	var userMsg *genai.Content
	if msg != "" {
		userMsg = agent.NewTimestampedUserContent(msg)
	}

	// Mutex for safe concurrent SSE writes
	var sseMu sync.Mutex
	safeSendSSE := func(eventType string, data interface{}) {
		sseMu.Lock()
		defer sseMu.Unlock()
		SendSSE(w, flusher, eventType, data)
	}

	// Wire transparent sub-agent streaming: sub-agent events are forwarded
	// to the Studio UI in real-time via UIEventCallback. The main LLM only
	// receives a compact summary (DelegateTasksResult), but the user sees
	// every tool call, result, and image as if the main thread did the work.
	chatAgent.UIEventCallback = func(event *session.Event) {
		if event == nil {
			return
		}
		if event.LLMResponse.Content == nil {
			return
		}
		for _, part := range event.LLMResponse.Content.Parts {
			if part.Text != "" && !part.Thought {
				safeSendSSE("text", map[string]interface{}{
					"text": part.Text,
				})
			}
			if part.FunctionCall != nil {
				args := part.FunctionCall.Args
				if chatAgent.Redactor != nil && args != nil {
					args = chatAgent.Redactor.RedactMap(args)
				}
				safeSendSSE("tool_call", map[string]interface{}{
					"name": part.FunctionCall.Name,
					"args": args,
				})
			}
			if part.FunctionResponse != nil {
				resp := part.FunctionResponse.Response
				if chatAgent.Redactor != nil && resp != nil {
					resp = chatAgent.Redactor.RedactMap(resp)
				}
				safeSendSSE("tool_result", map[string]interface{}{
					"name":   part.FunctionResponse.Name,
					"result": summarizeToolResult(resp),
				})
				// Drain images stashed by ForwardSubTaskEvent's extractAndStripImages
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
	defer func() { chatAgent.UIEventCallback = nil }()

	// Run the agent and stream events
	for event, runErr := range rnr.Run(ctx, studioChatUserID, sessionID, userMsg, adkagent.RunConfig{
		StreamingMode: adkagent.StreamingModeSSE,
	}) {
		if runErr != nil {
			safeSendSSE("error", map[string]string{"error": runErr.Error()})

			// Persist the error to the session so it survives page refresh.
			// Without this, the error SSE event is transient — on reload the
			// user sees their message but no indication of the failure.
			persistRunError(ctx, sessionService, sessionID, runErr)
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
				// Tool call — redact args so piped secrets (e.g. process_write
				// with a password from resolve_credential) are not exposed in the UI.
				if part.FunctionCall != nil {
					args := part.FunctionCall.Args
					if chatAgent.Redactor != nil && args != nil {
						args = chatAgent.Redactor.RedactMap(args)
					}
					safeSendSSE("tool_call", map[string]interface{}{
						"name": part.FunctionCall.Name,
						"args": args,
					})
				}
				// Tool result — redact so resolve_credential raw secrets
				// (which are intentionally unredacted for the LLM) are not
				// exposed in the UI.
				if part.FunctionResponse != nil {
					resp := part.FunctionResponse.Response
					if chatAgent.Redactor != nil && resp != nil {
						resp = chatAgent.Redactor.RedactMap(resp)
					}
					safeSendSSE("tool_result", map[string]interface{}{
						"name":   part.FunctionResponse.Name,
						"result": summarizeToolResult(resp),
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
				"- `/fleet [task]` — Start a fleet session with an autonomous agent team\n" +
				"- `/fleet-plan [hint]` — Create a reusable fleet plan through guided conversation\n" +
				"- `/drill [hint]` — Create a drill suite with guided wizard\n" +
				"- `/drill-add <suite>` — Add new drills to an existing suite\n" +
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
		if comp.SandboxEnabled {
			status += "- Sandbox: enabled\n"
		} else {
			status += "- Sandbox: disabled\n"
		}
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
			FleetKey:     m.FleetKey,
			FleetName:    m.FleetName,
			IssueNumber:  m.IssueNumber,
			Repo:         m.Repo,
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

	// Fleet sessions: read transcript and return fleet-style messages
	if meta.FleetKey != "" {
		transcriptPath := filepath.Join(fs.BaseDir(), studioChatAppName, studioChatUserID, sessionID+".jsonl")
		transcript := persistentsession.NewTranscript(transcriptPath)
		events, readErr := transcript.ReadEvents()

		var fleetMessages []FleetMessageSummary
		if readErr == nil && len(events) > 0 {
			fleetMessages = fleetEventsToMessages(events)
		}

		resp := StudioSessionDetailResponse{
			StudioSessionResponse: StudioSessionResponse{
				ID:           meta.ID,
				Title:        meta.Title,
				CreatedAt:    meta.CreatedAt.Format(time.RFC3339),
				UpdatedAt:    meta.UpdatedAt.Format(time.RFC3339),
				MessageCount: meta.MessageCount,
				FleetKey:     meta.FleetKey,
				FleetName:    meta.FleetName,
				IssueNumber:  meta.IssueNumber,
				Repo:         meta.Repo,
			},
			Messages:      []StudioMessage{},
			FleetMessages: fleetMessages,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
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
	var redactor *credentials.Redactor
	if cm.components != nil && cm.components.ChatAgent != nil {
		redactor = cm.components.ChatAgent.Redactor
	}
	messages := eventsToMessages(getResp.Session.Events(), redactor)

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

	// If this is an active fleet session, stop it and clean up sandbox
	registry := getFleetSessionRegistry()
	if fs := registry.Get(sessionID); fs != nil {
		fs.Stop()
		fs.Cleanup() // destroy sandbox container + clean session registry
		registry.Unregister(sessionID)
	}

	// Clean up per-session workspace directory if one was recorded.
	// Read metadata before deleting the session (deletion removes metadata).
	if fileStore := getFleetFileStore(); fileStore != nil {
		if meta, metaErr := fileStore.GetSessionMeta(sessionID); metaErr == nil && meta.WorkspaceDir != "" {
			if cleanErr := fleet.CleanupSessionWorkspace(meta.WorkspaceDir); cleanErr != nil {
				slog.Warn("could not clean up workspace", "component", "fleet", "workspace", meta.WorkspaceDir, "error", cleanErr)
			}
		}
	}

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

	// Best-effort: destroy sandbox container if one exists for this session
	sandbox.TryDestroySessionContainer(sessionID)

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
// An optional redactor is applied to all text parts and tool args/results to prevent
// credential exposure. This is the defense-in-depth layer: even if retroactive transcript
// redaction missed a secret, the UI will never display it in plaintext.
func eventsToMessages(events session.Events, redactor *credentials.Redactor) []StudioMessage {
	var messages []StudioMessage
	var lastInvocationID string // track invocation boundary for coalescing

	for i := range events.Len() {
		event := events.At(i)
		if event.LLMResponse.Content == nil {
			continue
		}

		role := string(event.LLMResponse.Content.Role)
		eventInvID := event.InvocationID

		for _, part := range event.LLMResponse.Content.Parts {
			if part.Text != "" {
				msgType := "agent"
				text := part.Text
				if role == "user" {
					msgType = "user"
					// Strip the timestamp prefix injected by NewTimestampedUserContent.
					// Format: "[2026-03-20 14:30:05 UTC]\n<text>"
					text = stripUserMessageTimestamp(text)
				}
				// Defense-in-depth: redact any credential values from text
				// before sending to the frontend. This catches secrets in user
				// messages and agent responses that may have been persisted
				// before the credential was registered with the redactor.
				if redactor != nil {
					text = redactor.Redact(text)
				}
				// Coalesce with previous message of same type, but only within
				// the same invocation. Different invocations represent separate
				// user turns — merging across them produces garbled messages
				// (e.g. when multiple user messages have no model response between them).
				if len(messages) > 0 && eventInvID == lastInvocationID {
					last := &messages[len(messages)-1]
					if last.Type == msgType && last.ToolName == "" {
						last.Content += text
						continue
					}
				}
				messages = append(messages, StudioMessage{
					Type:    msgType,
					Content: text,
				})
				lastInvocationID = eventInvID
			}
			if part.FunctionCall != nil {
				args := part.FunctionCall.Args
				if redactor != nil && args != nil {
					args = redactor.RedactMap(args)
				}
				messages = append(messages, StudioMessage{
					Type:     "tool_call",
					ToolName: part.FunctionCall.Name,
					ToolArgs: args,
				})
			}
			if part.FunctionResponse != nil {
				resp := part.FunctionResponse.Response
				if redactor != nil && resp != nil {
					resp = redactor.RedactMap(resp)
				}
				messages = append(messages, StudioMessage{
					Type:       "tool_result",
					ToolName:   part.FunctionResponse.Name,
					ToolResult: summarizeToolResult(resp),
				})
			}
		}
	}

	return messages
}

// userTimestampRe matches the timestamp prefix injected by NewTimestampedUserContent.
// Format: "[2026-03-20 14:30:05 UTC]\n"
var userTimestampRe = regexp.MustCompile(`^\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} \w+\]\n`)

// stripUserMessageTimestamp removes the timestamp prefix from a user message
// that was injected by NewTimestampedUserContent. This keeps the timestamp in
// the persisted event (for the LLM's context) while displaying clean text to
// the user in the Studio UI.
func stripUserMessageTimestamp(text string) string {
	return userTimestampRe.ReplaceAllString(text, "")
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

// fleetEventsToMessages converts ADK events from a fleet transcript back into
// fleet-style message summaries for the frontend.
func fleetEventsToMessages(events []*session.Event) []FleetMessageSummary {
	var messages []FleetMessageSummary
	for _, event := range events {
		if event.LLMResponse.Content == nil {
			continue
		}
		// Extract text from all parts
		var text string
		for _, part := range event.LLMResponse.Content.Parts {
			if part.Text != "" {
				text += part.Text
			}
		}
		if text == "" {
			continue
		}

		// Determine sender from Author field (set by fleetMessageToEvent)
		sender := event.Author
		if sender == "" {
			// Fallback: infer from role
			if event.LLMResponse.Content.Role == genai.RoleUser {
				sender = "customer"
			} else {
				sender = "agent"
			}
		}
		if sender == "user" {
			sender = "customer"
		}

		messages = append(messages, FleetMessageSummary{
			ID:        event.ID,
			Sender:    sender,
			Text:      text,
			Timestamp: event.Timestamp.Format(time.RFC3339Nano),
		})
	}
	return messages
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

	title = titleThinkTagRe.ReplaceAllString(title, "")
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

// persistRunError saves a synthetic model event to the session when the runner
// returns an error. Without this, errors are only sent as transient SSE events
// and disappear on page reload — the user sees their message but no indication
// that the model failed.
func persistRunError(ctx context.Context, svc session.Service, sessionID string, runErr error) {
	resp, err := svc.Get(ctx, &session.GetRequest{
		AppName:   studioChatAppName,
		UserID:    studioChatUserID,
		SessionID: sessionID,
	})
	if err != nil {
		slog.Error("failed to get session", "component", "persistRunError", "session_id", sessionID, "error", err)
		return
	}

	errorEvent := &session.Event{
		ID:        fmt.Sprintf("error-%d", time.Now().UnixMilli()),
		Author:    "model",
		Timestamp: time.Now(),
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{
				Role:  "model",
				Parts: []*genai.Part{{Text: fmt.Sprintf("[Error: %s]", runErr.Error())}},
			},
		},
	}

	if err := svc.AppendEvent(ctx, resp.Session, errorEvent); err != nil {
		slog.Error("failed to append error event to session", "component", "persistRunError", "session_id", sessionID, "error", err)
	}
}
