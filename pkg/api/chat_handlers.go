package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/apps"
	adrill "github.com/schardosin/astonish/pkg/drill"
	"github.com/schardosin/astonish/pkg/skills"
	persistentsession "github.com/schardosin/astonish/pkg/session"
	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/tools"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// StudioChatRequest is the request body for POST /api/studio/chat.
type StudioChatRequest struct {
	SessionID        string   `json:"sessionId,omitempty"`
	Message          string   `json:"message"`
	AutoApprove      bool     `json:"autoApprove,omitempty"`
	Debug            bool     `json:"debug,omitempty"`            // reserved for future debug streaming
	SystemContext    string   `json:"systemContext,omitempty"`    // per-turn system instructions (not shown to user)
	PinnedToolGroups []string `json:"pinnedToolGroups,omitempty"` // tool groups to always inject (wizard sessions)
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
	Artifacts     []ArtifactInfo        `json:"artifacts,omitempty"`  // files generated during the session
	TotalUsage    *UsageSummary         `json:"totalUsage,omitempty"` // cumulative token usage across all LLM calls
}

// UsageSummary holds cumulative token usage for a session, derived from
// the UsageMetadata attached to each LLM response in the session transcript.
type UsageSummary struct {
	InputTokens  int32 `json:"inputTokens"`
	OutputTokens int32 `json:"outputTokens"`
	TotalTokens  int32 `json:"totalTokens"`
}

// ArtifactInfo describes a file artifact produced during a session.
type ArtifactInfo struct {
	Path     string `json:"path"`     // absolute file path on disk
	FileName string `json:"fileName"` // basename (e.g., "report.md")
	FileType string `json:"fileType"` // human-readable type (e.g., "Markdown", "Python")
	ToolName string `json:"toolName"` // "write_file" or "edit_file"

	// IsReport flags artifacts the agent explicitly signaled as reports via
	// an ```astonish-report fence in the same turn. Only artifacts with
	// IsReport=true AND fileType="Markdown" AND emitted in the last turn
	// are eligible for inline EmbeddedFileViewer rendering on the frontend;
	// everything else falls back to the compact ArtifactCard.
	IsReport bool `json:"isReport,omitempty"`

	// ReportTitle is the optional title carried in the astonish-report
	// fence's frontmatter (e.g., "Q4 Revenue Analysis"). May be empty even
	// when IsReport is true; the frontend falls back to the file basename.
	ReportTitle string `json:"reportTitle,omitempty"`
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
	Type       string      `json:"type"`                 // user, agent, tool_call, tool_result, subtask_execution, plan, distill_preview, distill_saved, app_preview, system
	Content    string      `json:"content,omitempty"`    // text content
	ToolName   string      `json:"toolName,omitempty"`   // for tool_call/tool_result
	ToolArgs   interface{} `json:"toolArgs,omitempty"`   // for tool_call
	ToolResult interface{} `json:"toolResult,omitempty"` // for tool_result

	// subtask_execution fields — populated for delegate_tasks history reconstruction
	Events []SubTaskEventMsg `json:"events,omitempty"` // lifecycle + activity events
	Tasks  []SubTaskInfoMsg  `json:"tasks,omitempty"`  // task plan (names + descriptions)
	Status string            `json:"status,omitempty"` // complete, partial, error

	// plan fields — populated for announce_plan history reconstruction
	Goal  string        `json:"goal,omitempty"`  // plan title
	Steps []PlanStepMsg `json:"steps,omitempty"` // plan steps with status

	// distill_preview fields — populated for flow distillation review
	YAML        string   `json:"yaml,omitempty"`        // generated flow YAML
	FlowName    string   `json:"flowName,omitempty"`    // suggested flow name
	Description string   `json:"description,omitempty"` // flow description
	Tags        []string `json:"tags,omitempty"`        // flow tags
	Explanation string   `json:"explanation,omitempty"` // human-readable explanation

	// distill_saved fields
	FilePath   string `json:"filePath,omitempty"`   // saved file path
	RunCommand string `json:"runCommand,omitempty"` // suggested run command

	// app_preview fields — populated for generative UI app previews
	AppCode    string `json:"code,omitempty"`    // JSX source code
	AppTitle   string `json:"title,omitempty"`   // app title (extracted from component name)
	AppVersion int    `json:"version,omitempty"` // version number (increments on refinement)
	AppID      string `json:"appId,omitempty"`   // stable UUID for cross-turn matching
}

// SubTaskInfoMsg describes a single task in a delegation plan (for history reconstruction).
type SubTaskInfoMsg struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// SubTaskEventMsg is a synthetic event reconstructed from persisted delegate_tasks data.
type SubTaskEventMsg struct {
	Type     string `json:"type"`                // delegation_start, delegation_complete, task_start, task_complete, task_failed, task_text
	TaskName string `json:"task_name,omitempty"` // which task this event belongs to
	Status   string `json:"status,omitempty"`    // for delegation_complete
	Duration string `json:"duration,omitempty"`  // for task_complete/task_failed
	Error    string `json:"error,omitempty"`     // for task_failed
	Text     string `json:"text,omitempty"`      // for task_text (sub-agent result output)
}

// PlanStepMsg describes a step in the execution plan (for history reconstruction).
type PlanStepMsg struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"` // pending, running, complete, failed
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

	// Stop all background runners
	getChatRunnerRegistry().StopAll()

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

// PreWarm initializes the chat agent in the background so the first request is fast.
// In platform mode, the provided context must carry store.Services for config resolution.
// Safe to call concurrently with incoming requests — ensureReady is mutex-protected.
func (cm *ChatManager) PreWarm(ctx context.Context) error {
	return cm.ensureReady(ctx)
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
// The agent runs in a background goroutine (decoupled from the HTTP request
// lifecycle) so navigating away or closing the browser tab does NOT kill the
// running task. The SSE connection acts as a viewer/subscriber — disconnecting
// just removes the subscriber while the agent keeps working. Reconnecting
// (via StudioChatStreamHandler) replays missed events and resumes live streaming.
func StudioChatHandler(w http.ResponseWriter, r *http.Request) {
	var req StudioChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	userID := effectiveUserID(r)

	cm := GetChatManager()
	if err := cm.ensureReady(r.Context()); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, http.StatusInternalServerError, "Streaming unsupported")
		return
	}

	comp := cm.components
	chatAgent := comp.ChatAgent

	// Resolve session service per-request:
	// Platform mode: regular chat uses personal sessions (private-first);
	// fleet sub-sessions use team sessions (shared).
	// Personal mode falls back to the singleton file-based session service.
	var sessionService session.Service
	var titleSetter SessionTitleSetter
	if svc := store.FromRequest(r); svc != nil && svc.PersonalSessions != nil {
		sessionService = svc.PersonalSessions
		titleSetter = svc.PersonalSessions
	} else if svc := store.FromRequest(r); svc != nil && svc.Sessions != nil {
		// Fallback: if personal sessions not wired (shouldn't happen), use team
		sessionService = svc.Sessions
		titleSetter = svc.Sessions
	} else {
		sessionService = comp.SessionService
		titleSetter = cm.fileStore() // may be nil in edge cases
	}

	// Handle slash commands server-side (these are lightweight, no background runner needed)
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
			// Try store from request first (platform mode), then global registry
			var wizardFound bool
			if svc := store.FromRequest(r); svc != nil && svc.FleetTemplates != nil {
				if cfgAny, ok := svc.FleetTemplates.GetFleet(r.Context(), hint); ok {
					if cfg, ok := cfgAny.(*fleet.FleetConfig); ok && cfg.PlanWizard != nil {
						eventData["wizard_description"] = cfg.PlanWizard.Description
						eventData["wizard_system_prompt"] = cfg.PlanWizard.SystemPrompt
						if len(cfg.PlanWizard.PinnedToolGroups) > 0 {
							eventData["pinned_tool_groups"] = cfg.PlanWizard.PinnedToolGroups
						}
						wizardFound = true
					}
				}
			}
			if !wizardFound {
				if reg := GetFleetRegistry(); reg != nil {
					if cfg, ok := reg.GetFleet(hint); ok && cfg.PlanWizard != nil {
						eventData["wizard_description"] = cfg.PlanWizard.Description
						eventData["wizard_system_prompt"] = cfg.PlanWizard.SystemPrompt
						if len(cfg.PlanWizard.PinnedToolGroups) > 0 {
							eventData["pinned_tool_groups"] = cfg.PlanWizard.PinnedToolGroups
						}
					}
				}
			}
		}
		SendSSE(w, flusher, "fleet_plan_redirect", eventData)
		SendSSE(w, flusher, "done", map[string]interface{}{"done": true})
		return
	}

	if strings.HasPrefix(msg, "/") {
		// Use r.Context() for slash commands since they're lightweight
		handleSlashCommand(r.Context(), w, flusher, cm, sessionService, msg, userID, req.SessionID)
		return
	}

	// Intercept messages for pending distill review (interactive modification loop).
	if req.SessionID != "" && chatAgent.HasPendingDistillReview(req.SessionID) {
		intent := chatAgent.ClassifyDistillReviewIntent(r.Context(), msg)

		switch intent {
		case agent.DistillIntentSave:
			// Save the flow — persist a clean user message
			userText := msg
			if msg == "__distill_save__" {
				userText = "Save Flow"
			}
			persistSessionMessage(r.Context(), sessionService, userID, req.SessionID, "user", userText)

		// Enrich context with composite FlowStore (personal-first) so
		// SaveDistillReview persists to the user's personal store.
		saveCtx := r.Context()
		if svc := store.FromRequest(r); svc != nil && (svc.PersonalFlows != nil || svc.Flows != nil) {
			saveCtx = store.WithFlowStore(saveCtx, store.NewCompositeFlowStore(svc.PersonalFlows, svc.Flows))
		}

			filePath, runCmd, err := chatAgent.SaveDistillReview(saveCtx, req.SessionID)
			if err != nil {
				errText := fmt.Sprintf("Failed to save flow: %v", err)
				SendSSE(w, flusher, "text", map[string]interface{}{"text": errText})
				persistSessionMessage(r.Context(), sessionService, userID, req.SessionID, "model", errText)
			} else {
				SendSSE(w, flusher, "distill_saved", map[string]interface{}{
					"filePath":   filePath,
					"runCommand": runCmd,
				})
				persistDistillSaved(r.Context(), sessionService, userID, req.SessionID, filePath, runCmd)
			}
			SendSSE(w, flusher, "done", map[string]interface{}{"done": true})
			return

		case agent.DistillIntentCancel:
			persistSessionMessage(r.Context(), sessionService, userID, req.SessionID, "user", msg)
			chatAgent.CancelDistillReview(req.SessionID)
			responseText := "Distill review cancelled."
			SendSSE(w, flusher, "text", map[string]interface{}{"text": responseText})
			persistSessionMessage(r.Context(), sessionService, userID, req.SessionID, "model", responseText)
			SendSSE(w, flusher, "done", map[string]interface{}{"done": true})
			return

		case agent.DistillIntentTestRun:
			persistSessionMessage(r.Context(), sessionService, userID, req.SessionID, "user", msg)

			if chatAgent.FlowRunner == nil {
				errText := "Test run is not available — the flow runner is not configured in this environment."
				SendSSE(w, flusher, "text", map[string]interface{}{"text": errText})
				persistSessionMessage(r.Context(), sessionService, userID, req.SessionID, "model", errText)
				SendSSE(w, flusher, "done", map[string]interface{}{"done": true})
				return
			}

			SendSSE(w, flusher, "text", map[string]interface{}{"text": "Running test execution with the original parameters...\n\n"})

			// Inject PG credential store into dry-run context so that
			// BeforeToolCallback in the flow agent can resolve {{CREDENTIAL:...}}
			// placeholders in tool arguments (e.g., shell_command).
			dryCtx := r.Context()
			if svc := store.FromRequest(r); svc != nil && (svc.PersonalCredentials != nil || svc.Credentials != nil) {
				dryCtx = store.WithCredentialStore(dryCtx, store.NewMergedCredentialStore(svc.PersonalCredentials, svc.Credentials))
			}
			dryResult, dryErr := chatAgent.DryRunDistilledFlow(dryCtx, req.SessionID)

			var analysisText string
			if dryErr != nil {
				analysisText = fmt.Sprintf("**Test run failed to start:** %v\n\nThe flow could not be executed. This may indicate a configuration issue. You can say \"fix it\" and I'll attempt to correct the flow, or describe specific changes you'd like.", dryErr)
			} else {
				// Use LLM to intelligently evaluate the result
				assessment := chatAgent.EvaluateDryRunResult(r.Context(), req.SessionID, dryResult)

				if dryResult.Success {
					output := strings.TrimSpace(dryResult.Output)
					if output != "" && len(output) > 3000 {
						output = output[:3000] + "\n... (truncated)"
					}
					if output != "" {
						analysisText = fmt.Sprintf("**Test run completed.**\n\n**Output:**\n```\n%s\n```\n\n%s", output, assessment)
					} else {
						analysisText = fmt.Sprintf("**Test run completed.**\n\n%s", assessment)
					}
				} else {
					output := strings.TrimSpace(dryResult.Output)
					if len(output) > 2000 {
						output = output[:2000] + "\n... (truncated)"
					}
					analysisText = fmt.Sprintf("**Test run failed.**\n\n%s", assessment)
					if output != "" {
						analysisText += fmt.Sprintf("\n\n**Partial output:**\n```\n%s\n```", output)
					}
				}
			}

			SendSSE(w, flusher, "text", map[string]interface{}{"text": analysisText})
			persistSessionMessage(r.Context(), sessionService, userID, req.SessionID, "model", analysisText)
			SendSSE(w, flusher, "done", map[string]interface{}{"done": true})
			return

		default: // DistillIntentModify
			persistSessionMessage(r.Context(), sessionService, userID, req.SessionID, "user", msg)
			SendSSE(w, flusher, "text", map[string]interface{}{"text": "Modifying flow...\n"})
			review, err := chatAgent.ModifyDistillReview(r.Context(), req.SessionID, msg)
			if err != nil {
				errText := fmt.Sprintf("Failed to modify flow: %v\nYou can try another change, type `save` to save as-is, or `cancel` to abort.", err)
				SendSSE(w, flusher, "text", map[string]interface{}{"text": errText})
				persistSessionMessage(r.Context(), sessionService, userID, req.SessionID, "model", errText)
			} else {
				SendSSE(w, flusher, "distill_preview", map[string]interface{}{
					"yaml":        review.YAML,
					"flowName":    review.FlowName,
					"description": review.Description,
					"tags":        review.Tags,
					"explanation": review.Explanation,
				})
				persistDistillPreview(r.Context(), sessionService, userID, req.SessionID, review)
			}
			SendSSE(w, flusher, "done", map[string]interface{}{"done": true})
			return
		}
	}

	// Lazy reconstruction of active app state from session history.
	// Handles the case where the server restarted and in-memory state was lost.
	if req.SessionID != "" && !chatAgent.HasActiveApp(req.SessionID) {
		getResp, err := sessionService.Get(r.Context(), &session.GetRequest{
			AppName:   studioChatAppName,
			UserID:    userID,
			SessionID: req.SessionID,
		})
		if err == nil && getResp != nil && getResp.Session != nil {
			if app := reconstructActiveApp(getResp.Session.Events()); app != nil {
				chatAgent.SetActiveApp(req.SessionID, app)
			}
		}
	}

	// Intercept messages for active app refinement (iterative visual app loop).
	// Unlike distill (which fully intercepts), "refine" falls through to normal
	// agent flow with context injection; only "save" is handled as early-return.
	if req.SessionID != "" && chatAgent.HasActiveApp(req.SessionID) {
		llmFunc := makeLLMFuncFromModel(comp.LLM)
		appIntent := chatAgent.ClassifyAppIntent(r.Context(), msg, llmFunc)

		switch appIntent.Intent {
		case agent.AppIntentSave, agent.AppIntentDone:
			// User wants to save the app — persist to disk, clear state, acknowledge
			activeApp := chatAgent.GetActiveApp(req.SessionID)
			userText := msg
			if msg == "__app_save__" || msg == "__app_done__" || strings.HasPrefix(msg, "__app_save__:") {
				userText = "Save"
			}
			persistSessionMessage(r.Context(), sessionService, userID, req.SessionID, "user", userText)

			// Save the app to disk
			var savedPath, savedName string
			if activeApp != nil {
				// Use custom name from LLM classification if provided, otherwise fall back to auto-title
				appName := activeApp.Title
				if appIntent.SaveName != "" {
					appName = appIntent.SaveName
				}
				savedApp := &apps.VisualApp{
					Name:        appName,
					Description: appName,
					Code:        activeApp.Code,
					Version:     activeApp.Version,
					SessionID:   req.SessionID,
				}
				var saveErr error
				// Save to personal store first, team store as fallback.
				if svc := store.FromRequest(r); svc != nil && svc.PersonalApps != nil {
					savedPath, saveErr = svc.PersonalApps.Save(r.Context(), savedApp)
				} else if svc := store.FromRequest(r); svc != nil && svc.Apps != nil {
					savedPath, saveErr = svc.Apps.Save(r.Context(), savedApp)
				} else {
					saveErr = fmt.Errorf("app store not available")
				}
				if saveErr != nil {
					slog.Error("failed to save app", "error", saveErr)
				}
				savedName = apps.Slugify(appName)
			}

			chatAgent.ClearActiveApp(req.SessionID)

			responseText := "App saved! You can find it in the Apps tab, or continue chatting."
			if savedPath == "" {
				responseText = "App refinement complete. You can continue chatting or start a new app."
			}
			SendSSE(w, flusher, "text", map[string]interface{}{"text": responseText})
			SendSSE(w, flusher, "app_saved", map[string]interface{}{
				"name": savedName,
				"path": savedPath,
			})
			persistSessionMessage(r.Context(), sessionService, userID, req.SessionID, "model", responseText)
			SendSSE(w, flusher, "done", map[string]interface{}{"done": true})
			return

		case agent.AppIntentRefine:
			// Inject current app source into system context so the LLM
			// knows it's refining an existing component, then fall through
			// to the normal agent flow.
			activeApp := chatAgent.GetActiveApp(req.SessionID)
			if activeApp != nil {
				refinementCtx := agent.BuildAppRefinementContext(activeApp)
				if req.SystemContext != "" {
					req.SystemContext = req.SystemContext + "\n\n" + refinementCtx
				} else {
					req.SystemContext = refinementCtx
				}
				// Record the modification request in the active app history
				chatAgent.RecordAppModification(req.SessionID, msg)
			}
			// Fall through to normal agent flow

		case agent.AppIntentUnrelated:
			// Clear active app context and proceed normally
			chatAgent.ClearActiveApp(req.SessionID)
			// Fall through to normal agent flow
		}
	}

	// Create or resume session
	sessionID := req.SessionID
	isNew := false
	if sessionID == "" {
		resp, err := sessionService.Create(r.Context(), &session.CreateRequest{
			AppName: studioChatAppName,
			UserID:  userID,
		})
		if err != nil {
			SendErrorSSE(w, flusher, fmt.Sprintf("Failed to create session: %v", err))
			return
		}
		sessionID = resp.Session.ID()
		isNew = true
	}

	// Prepare user message
	var userMsg *genai.Content
	if msg != "" {
		userMsg = agent.NewTimestampedUserContent(msg)
	}

	// Seed ActiveApp from system context when opening a saved app for refinement.
	// This avoids making the LLM re-emit the component on the first turn.
	var seededAppPreview map[string]any
	if isNew && req.SystemContext != "" {
		if code, title := extractAppFromSystemContext(req.SystemContext); code != "" {
			appID := uuid.New().String()
			activeApp := &agent.ActiveApp{
				AppID:    appID,
				Title:    title,
				Code:     code,
				Versions: []string{},
				Version:  1,
			}
			chatAgent.SetActiveApp(sessionID, activeApp)
			persistAppPreview(r.Context(), sessionService, userID, sessionID, code, title, 1, appID)
			seededAppPreview = map[string]any{
				"code":    code,
				"title":   title,
				"version": 1,
				"appId":   appID,
			}
		}
	}

	// Launch background runner — the agent runs independently of this HTTP request.
	runner := newChatRunner(sessionID, userID, isNew)
	runner.titleWaitTimeout = 30 * time.Second // wait for title goroutine before closing SSE

	// Inject tenant-scoped credential store into the runner context so that
	// credential tools (list_credentials, resolve_credential, etc.) can access
	// the correct store for this team/org in platform mode.
	// The BeforeToolCallback in chat_agent_run.go also checks this context value
	// for credential placeholder substitution ({{CREDENTIAL:...}} tokens),
	// falling back to the agent's file-based CredentialStore field.
	//
	// In platform mode, we inject a merged store (personal-first, team-fallback)
	// so the LLM resolves the user's personal credentials first, then team creds.
	// Writes from chat always go to the personal store.
	if svc := store.FromRequest(r); svc != nil {
		if svc.PersonalCredentials != nil || svc.Credentials != nil {
			merged := store.NewMergedCredentialStore(svc.PersonalCredentials, svc.Credentials)
			runner.InjectCredentialStore(merged)

			// Hydrate the shared Redactor from the PG-backed credential store.
			// This ensures the Redactor knows about all credential values for
			// this user's session — critical for tool output redaction and
			// preventing secret leakage into session history.
			if chatAgent.Redactor != nil {
				chatAgent.Redactor.HydrateFromStore(merged)
			}
		}
	}

	// Inject the Redactor into the runner context so that memory_save can
	// call Placeholderize() to replace raw credential values with actionable
	// {{CREDENTIAL:name:field}} tokens before persisting to memory.
	if chatAgent.Redactor != nil {
		runner.InjectRedactor(chatAgent.Redactor)
	}

	// Inject tenant-scoped memory stores into the runner context so that
	// memory_search, memory_save tools, and the KnowledgeSearch callback can
	// use the PG-backed stores (team + three-tier) in platform mode.
	if svc := store.FromRequest(r); svc != nil {
		memStore := svc.Memory
		// If personal memory mode is active, the memory_save tool should
		// write to the user's personal store instead of team.
		// The ThreeTierSearcher remains unchanged (always searches all tiers).
		if r.Header.Get("X-Astonish-Memory-Mode") == "personal" && svc.TenantRouter != nil {
			if pu := GetPlatformUser(r); pu != nil {
				if orgStore, err := svc.TenantRouter.ForOrg(pu.OrgSlug); err == nil {
					memStore = orgStore.ForUser(pu.ID).Memories()
				}
			}
		}
		runner.InjectMemoryStores(memStore, svc.MemorySearcher)

		// Inject the cross-session memory merge function so that memory_save
		// performs dedup/merge instead of blind inserts. This requires the
		// PlatformReflector (which provides the LLM for merge calls).
		if chatAgent.PlatformReflector != nil {
			runner.InjectMemorySaveOrMerge(chatAgent.PlatformReflector.MemorySaveOrMergeFunc())
		}
	}

	// Inject composite flow store (personal-first, team-fallback) into the runner
	// context so that drill tools, run_flow, search_flows, etc. resolve personal
	// flows first and save to personal. Users promote to team explicitly.
	if svc := store.FromRequest(r); svc != nil && (svc.PersonalFlows != nil || svc.Flows != nil) {
		runner.InjectFlowStore(store.NewCompositeFlowStore(svc.PersonalFlows, svc.Flows))
	}

	// Inject tenant-scoped drill report store into the runner context so that
	// the run_drill tool can persist execution results to the database.
	if svc := store.FromRequest(r); svc != nil && svc.DrillReports != nil {
		runner.InjectDrillReportStore(svc.DrillReports)
	}

	// Inject tenant-scoped skill stores into the runner context so that
	// the skill_lookup tool can resolve skills from platform, org, and team stores.
	if svc := store.FromRequest(r); svc != nil && (svc.PlatformSkills != nil || svc.Skills != nil || svc.TeamSkills != nil) {
		runner.InjectSkillStores(svc.PlatformSkills, svc.Skills, svc.TeamSkills)

		// Build and inject a merged skill index (platform + org + team) so the LLM
		// sees custom platform skills in the "Available Skills" section of the system prompt.
		if merged := buildMergedSkillIndex(r.Context(), svc.PlatformSkills, svc.Skills, svc.TeamSkills); merged != "" {
			runner.InjectSkillIndex(merged)
		}
	}

	// Inject tenant-scoped MCP server stores into the runner context so that
	// the chat agent can resolve MCP server configs from platform, org, and
	// team stores. Platform-tier servers (e.g. standard servers like Tavily
	// installed at scope=platform) cascade down into every org/team chat.
	if svc := store.FromRequest(r); svc != nil && (svc.PlatformMCPServers != nil || svc.MCPServers != nil || svc.TeamMCPServers != nil) {
		runner.InjectMCPServerStores(svc.PlatformMCPServers, svc.MCPServers, svc.TeamMCPServers)
	}

	// Inject tenant-scoped scheduler store into the runner context so that
	// the schedule_job and list_scheduled_jobs tools operate on the correct team.
	if svc := store.FromRequest(r); svc != nil && svc.Scheduler != nil {
		runner.InjectSchedulerStore(svc.Scheduler)
	}

	// Inject tenant-scoped fleet stores into the runner context so that
	// fleet tools (save_fleet_plan, list_fleets) can read/write from the DB.
	if svc := store.FromRequest(r); svc != nil && (svc.FleetTemplates != nil || svc.FleetPlans != nil) {
		runner.InjectFleetStores(svc.FleetTemplates, svc.FleetPlans)
	}

	// Inject the team's custom sandbox template so that chat containers use
	// the team's pre-configured image rather than always falling back to @base.
	if svc := store.FromRequest(r); svc != nil && svc.Settings != nil {
		if settings, err := svc.Settings.Get(r.Context()); err == nil && settings.TemplateName != "" {
			runner.InjectSandboxTemplate(settings.TemplateName)
			// On K8s, resolve the template name → layer chain via the
			// platform DB so the pod gets a real layer chain (not the
			// human-readable template name which is not a valid layer ID).
			if chain := resolveTemplateLayerChain(r.Context(), settings.TemplateName); len(chain) > 0 {
				runner.InjectSandboxLayerChain(chain)
			}
			// On OpenShell, resolve the template name → custom image ref.
			if img := resolveTemplateImage(r.Context(), settings.TemplateName); img != "" {
				runner.InjectSandboxImage(img)
			}
		}
	}

	// Resolve @base configuration layer. When the admin has run Configure
	// Base Sandbox, the resulting delta layer is stored as @base's
	// top_layer_id. Sessions must include it in their chain so they see
	// the installed tools. Only applies when no team-template chain was
	// already injected (team templates include @base in their own chain).
	if chain := resolveBaseLayerChain(r.Context()); len(chain) > 0 {
		runner.InjectSandboxLayerChainIfEmpty(chain)
	}
	// On OpenShell, resolve @base's custom image if set by admin.
	if img := resolveBaseImage(r.Context()); img != "" {
		runner.InjectSandboxImageIfEmpty(img)
	}

	// Inject per-team disabled tool list so the agent filters them out.
	if svc := store.FromRequest(r); svc != nil && svc.Settings != nil {
		if settings, err := svc.Settings.Get(r.Context()); err == nil && len(settings.DisabledTools) > 0 {
			runner.InjectDisabledTools(settings.DisabledTools)
		}
	}

	// Inject the per-request session service so that sub-agents (delegate_tasks)
	// create child sessions in the correct store (pgstore PersonalSessions in
	// platform mode) rather than the factory-time default (FileStore).
	if svc := store.FromRequest(r); svc != nil && svc.PersonalSessions != nil {
		runner.InjectSessionService(svc.PersonalSessions)
	}

	// Inject the effective user ID so sub-agents create child sessions with the
	// correct user_id (UUID in platform mode). Without this, the SubAgentManager
	// would use its factory-time default ("console_user") which fails pgstore's
	// UUID column constraint.
	runner.InjectUserID(userID)

	// Inject org/team slugs so tools can resolve team membership in platform mode.
	if tc := store.TenantContextFrom(r.Context()); tc != nil {
		runner.InjectTenantSlugs(tc.OrgSlug, tc.TeamSlug)
	}

	// Inject RunJobFunc so schedule_job test execution works in platform mode
	// (bypasses the unauthenticated HTTP bridge which can't pass auth middleware).
	if exec := GetExecutor(); exec != nil {
		reqSvc := store.FromRequest(r)
		runner.InjectRunJobFunc(func(ctx context.Context, jobID string) (string, error) {
			if reqSvc == nil || reqSvc.Scheduler == nil {
				return "", fmt.Errorf("scheduler store not available")
			}
			storeJob := reqSvc.Scheduler.Get(ctx, jobID)
			if storeJob == nil {
				return "", fmt.Errorf("job %q not found", jobID)
			}
			job := storeJobToSchedulerJob(storeJob)
			return exec.Execute(ctx, job)
		})
	}

	// If we seeded an app preview, emit it through the runner so the frontend
	// shows the AppPreviewCard immediately (before the LLM responds).
	if seededAppPreview != nil {
		runner.emitEvent("app_preview", seededAppPreview)
	}

	registry := getChatRunnerRegistry()
	registry.Register(sessionID, runner)

	// Also register the runner's cancel in the ChatManager so the stop endpoint
	// (POST /api/studio/sessions/{id}/stop) can cancel the background runner.
	cm.registerStream(sessionID, runner.Stop)

	go func() {
		defer registry.Unregister(sessionID)
		defer cm.unregisterStream(sessionID)
		defer func() {
			if r := recover(); r != nil {
				slog.Error("chat runner panic recovered",
					"session", sessionID,
					"panic", fmt.Sprintf("%v", r))
				runner.EmitPanicError(fmt.Sprintf("Internal error: %v", r))
			}
		}()
		runner.Run(chatAgent, sessionService, comp.LLM, titleSetter, userMsg, msg, req.AutoApprove, req.SystemContext, req.PinnedToolGroups)
	}()

	// Become an SSE viewer: subscribe to the runner and forward events to the browser.
	streamRunnerEvents(w, flusher, r.Context(), runner)
}

// StudioChatStreamHandler handles GET /api/studio/sessions/{id}/stream.
// It reconnects to an active background chat runner, replaying missed events
// and then streaming live events. If no runner is active, it returns 404.
func StudioChatStreamHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["id"]
	if sessionID == "" {
		respondError(w, http.StatusBadRequest, "session ID required")
		return
	}

	registry := getChatRunnerRegistry()
	runner := registry.Get(sessionID)
	if runner == nil {
		respondError(w, http.StatusNotFound, "no active runner for session")
		return
	}

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, http.StatusInternalServerError, "Streaming unsupported")
		return
	}

	streamRunnerEvents(w, flusher, r.Context(), runner)
}

// StudioChatStatusHandler handles GET /api/studio/sessions/{id}/status.
// Returns the current state of a session's background runner.
func StudioChatStatusHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["id"]
	if sessionID == "" {
		respondError(w, http.StatusBadRequest, "session ID required")
		return
	}

	registry := getChatRunnerRegistry()
	runner := registry.Get(sessionID)

	resp := map[string]interface{}{
		"sessionId": sessionID,
		"running":   runner != nil && !runner.IsDone(),
	}
	if runner != nil {
		resp["eventCount"] = runner.EventCount()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// streamRunnerEvents subscribes to a ChatRunner and streams events as SSE to the client.
// It first replays buffered history (catch-up), then streams live events.
// Returns when the runner completes or the HTTP client disconnects.
func streamRunnerEvents(w http.ResponseWriter, flusher http.Flusher, httpCtx context.Context, runner *ChatRunner) {
	subscriberID := fmt.Sprintf("sse-%d", time.Now().UnixNano())

	// Subscribe BEFORE reading history to avoid missing events between
	// history read and subscription (same pattern as fleet sessions).
	eventCh := runner.Subscribe(subscriberID)
	defer runner.Unsubscribe(subscriberID)

	// Replay buffered history (catch-up for reconnecting clients)
	seen := make(map[string]bool)
	history := runner.GetHistory()
	for _, event := range history {
		seen[event.ID] = true
		SendSSE(w, flusher, event.Type, event.Data)
	}

	// If the runner is already done and we've replayed all history, we're done.
	if runner.IsDone() {
		return
	}

	// Stream live events
	for {
		select {
		case <-httpCtx.Done():
			// Browser disconnected — the runner keeps going, we just stop streaming.
			return
		case event, ok := <-eventCh:
			if !ok {
				// Channel closed — runner is done
				return
			}
			// Deduplicate events that were in both history and live stream
			if seen[event.ID] {
				continue
			}
			SendSSE(w, flusher, event.Type, event.Data)
		}
	}
}

// handleSlashCommand processes slash commands and sends results as SSE events.
func handleSlashCommand(ctx context.Context, w io.Writer, flusher http.Flusher, cm *ChatManager, sessionService session.Service, cmd, userID, sessionID string) {
	comp := cm.components
	chatAgent := comp.ChatAgent

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
			shortID := persistentsession.SafeShortID(sessionID, 16)
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
			UserID:  userID,
		})
		if err != nil {
			SendErrorSSE(w, flusher, fmt.Sprintf("Failed to create session: %v", err))
		} else {
			SendSSE(w, flusher, "new_session", map[string]interface{}{
				"sessionId": resp.Session.ID(),
			})
		}

	case cmd == "/distill":
		// Persist the user's /distill command to the session
		persistSessionMessage(ctx, sessionService, userID, sessionID, "user", "/distill")

		if sessionID == "" {
			responseText := "No active session to distill."
			SendSSE(w, flusher, "text", map[string]interface{}{"text": responseText})
			persistSessionMessage(ctx, sessionService, userID, sessionID, "model", responseText)
		} else {
			// Enrich context with SessionService and FlowStore so that
			// PreviewDistill can reconstruct traces from PG (platform mode)
			// and DistillToReview/SaveDistillReview can save to PG FlowStore.
			distillCtx := ctx
			if svc := store.FromContext(ctx); svc != nil {
				if svc.PersonalSessions != nil {
					distillCtx = store.WithSessionService(distillCtx, svc.PersonalSessions)
				}
			if svc.PersonalFlows != nil || svc.Flows != nil {
				distillCtx = store.WithFlowStore(distillCtx, store.NewCompositeFlowStore(svc.PersonalFlows, svc.Flows))
			}
			}

			ds := agent.DistillSession{
				SessionID: sessionID,
				AppName:   studioChatAppName,
				UserID:    userID,
			}
			// Identify traces and immediately run distillation (no confirmation step)
			_, err := chatAgent.PreviewDistill(distillCtx, ds)
			if err != nil {
				responseText := fmt.Sprintf("Cannot distill: %v", err)
				SendSSE(w, flusher, "text", map[string]interface{}{"text": responseText})
				persistSessionMessage(ctx, sessionService, userID, sessionID, "model", responseText)
			} else {
				// Run distillation and send preview directly
				review, distillErr := chatAgent.DistillToReview(distillCtx, ds, func(text string) {
					SendSSE(w, flusher, "text", map[string]interface{}{"text": text})
				})
				if distillErr != nil {
					errText := fmt.Sprintf("Distillation failed: %v", distillErr)
					SendSSE(w, flusher, "text", map[string]interface{}{"text": errText})
					persistSessionMessage(ctx, sessionService, userID, sessionID, "model", errText)
				} else {
					SendSSE(w, flusher, "distill_preview", map[string]interface{}{
						"yaml":        review.YAML,
						"flowName":    review.FlowName,
						"description": review.Description,
						"tags":        review.Tags,
						"explanation": review.Explanation,
					})
					persistDistillPreview(ctx, sessionService, userID, sessionID, review)

					// Offer to test-run the flow (conversational — like the scheduler workflow)
					if chatAgent.FlowRunner != nil {
						offerText := "\n**Should we run a test before saving?**"
						SendSSE(w, flusher, "text", map[string]interface{}{"text": offerText})
						persistSessionMessage(ctx, sessionService, userID, sessionID, "model", offerText)
					}
				}
			}
		}

	default:
		SendSSE(w, flusher, "system", map[string]interface{}{
			"content": fmt.Sprintf("Unknown command: `%s`. Type `/help` for available commands.", cmd),
		})
	}
	SendSSE(w, flusher, "done", map[string]interface{}{"done": true})
}

// makeLLMFuncFromModel wraps an ADK model.LLM into a simple prompt→response function
// suitable for lightweight classification calls (e.g. app refinement intent).
func makeLLMFuncFromModel(llm model.LLM) func(ctx context.Context, prompt string) (string, error) {
	if llm == nil {
		return nil
	}
	return func(ctx context.Context, prompt string) (string, error) {
		req := &model.LLMRequest{
			Contents: []*genai.Content{
				{
					Parts: []*genai.Part{{Text: prompt}},
					Role:  "user",
				},
			},
		}
		var text string
		for resp, err := range llm.GenerateContent(ctx, req, false) {
			if err != nil {
				return text, err
			}
			if resp.Content != nil {
				for _, p := range resp.Content.Parts {
					if p.Text != "" {
						text += p.Text
					}
				}
			}
		}
		return text, nil
	}
}

// refinementCodeBlockRe matches the fenced code block after "### Current Source Code"
// in the system context injected by the "Improve with AI" flow.
var refinementCodeBlockRe = regexp.MustCompile("(?s)### Current Source Code\\s*```jsx\\s*\\n(.+?)\\n```")

// extractAppFromSystemContext detects the "## Active App Refinement" marker
// in the system context and extracts the app's current code and title.
// Returns ("", "") if the system context does not contain a refinement payload.
func extractAppFromSystemContext(systemContext string) (code, title string) {
	if !strings.Contains(systemContext, "## Active App Refinement") {
		return "", ""
	}
	matches := refinementCodeBlockRe.FindStringSubmatch(systemContext)
	if len(matches) < 2 {
		return "", ""
	}
	code = strings.TrimSpace(matches[1])
	if code == "" {
		return "", ""
	}
	title = extractComponentTitle(code)
	return code, title
}

// buildMergedSkillIndex builds a skill index string containing platform skills
// plus any org-level and team-level skills from the platform DB.
// This is used to populate the "Available Skills" section in the system prompt
// so the LLM knows about custom platform skills and will call skill_lookup for them.
func buildMergedSkillIndex(ctx context.Context, platformStore, orgStore, teamStore store.SkillStore) string {
	var all []skills.Skill

	// 1. Platform skills (base layer, inherited by all orgs/teams)
	if platformStore != nil {
		if platformSkills, err := platformStore.LoadAll(ctx); err == nil {
			for _, s := range platformSkills {
				all = append(all, skills.Skill{
					Name:        s.Name,
					Description: s.Description,
					OS:          s.OS,
					RequireBins: s.RequireBins,
					RequireEnv:  s.RequireEnv,
					Source:      "platform",
				})
			}
		}
	}

	// 2. Org skills (if store available)
	if orgStore != nil {
		if orgSkills, err := orgStore.LoadAll(ctx); err == nil {
			for _, s := range orgSkills {
				all = append(all, skills.Skill{
					Name:        s.Name,
					Description: s.Description,
					OS:          s.OS,
					RequireBins: s.RequireBins,
					RequireEnv:  s.RequireEnv,
					Source:      "org",
				})
			}
		}
	}

	// 3. Team skills (highest priority, can override org/platform names)
	if teamStore != nil {
		if teamSkills, err := teamStore.LoadAll(ctx); err == nil {
			for _, s := range teamSkills {
				all = append(all, skills.Skill{
					Name:        s.Name,
					Description: s.Description,
					OS:          s.OS,
					RequireBins: s.RequireBins,
					RequireEnv:  s.RequireEnv,
					Source:      "team",
				})
			}
		}
	}

	if len(all) == 0 {
		return ""
	}

	// Deduplicate preferring later entries (team > org > platform).
	// Reverse-iterate and collect, then reverse to preserve original order — O(n).
	seen := make(map[string]bool, len(all))
	var deduped []skills.Skill
	for i := len(all) - 1; i >= 0; i-- {
		name := strings.ToLower(all[i].Name)
		if !seen[name] {
			seen[name] = true
			deduped = append(deduped, all[i])
		}
	}
	// Reverse to restore platform → org → team display order
	for i, j := 0, len(deduped)-1; i < j; i, j = i+1, j-1 {
		deduped[i], deduped[j] = deduped[j], deduped[i]
	}

	return skills.BuildSkillIndex(deduped)
}
