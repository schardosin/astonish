package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/schardosin/astonish/pkg/agent"
	persistentsession "github.com/schardosin/astonish/pkg/session"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// ChatEvent represents a single event produced by a background chat runner.
// It mirrors the SSE events that were previously emitted inline.
type ChatEvent struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"` // text, tool_call, tool_result, image, flow_output, approval, auto_approved, thinking, retry, error, error_info, session, done
	Data      map[string]any `json:"data"`
	Timestamp time.Time      `json:"timestamp"`
}

// ChatRunner manages a single background agent execution for a Studio chat session.
// It decouples the agent run from the HTTP request lifecycle, allowing the browser
// to disconnect and reconnect without killing the agent. This follows the same
// pattern as channel-based execution (Telegram/Email) where the agent runs
// independently and results are delivered asynchronously.
type ChatRunner struct {
	SessionID string
	IsNew     bool // whether this is a newly created session

	ctx    context.Context
	cancel context.CancelFunc

	// Event buffer and subscriber management
	events      []ChatEvent
	eventsMu    sync.RWMutex
	subscribers map[string]chan ChatEvent
	subMu       sync.RWMutex

	// Completion state
	done   bool
	doneMu sync.RWMutex
}

// newChatRunner creates a new ChatRunner with a background context.
func newChatRunner(sessionID string, isNew bool) *ChatRunner {
	ctx, cancel := context.WithCancel(context.Background())
	return &ChatRunner{
		SessionID:   sessionID,
		IsNew:       isNew,
		ctx:         ctx,
		cancel:      cancel,
		subscribers: make(map[string]chan ChatEvent),
	}
}

// Run executes the agent in the background. It creates the ADK runner,
// processes events, buffers them for subscribers, and handles completion.
// This method blocks until the agent finishes or the context is cancelled.
func (cr *ChatRunner) Run(
	chatAgent *agent.ChatAgent,
	sessionService session.Service,
	llm model.LLM,
	fileStore *persistentsession.FileStore,
	userMsg *genai.Content,
	msg string,
	autoApprove bool,
	systemContext string,
) {
	defer func() {
		cr.doneMu.Lock()
		cr.done = true
		cr.doneMu.Unlock()

		// Send done event and close all subscriber channels
		cr.emitEvent("done", map[string]any{"done": true})
		cr.closeSubscribers()
	}()

	// Send session info first
	cr.emitEvent("session", map[string]any{
		"sessionId": cr.SessionID,
		"isNew":     cr.IsNew,
	})

	// Prepare the ADK runner
	adkAgent, err := adkagent.New(adkagent.Config{
		Name:        "astonish_chat",
		Description: "Astonish intelligent chat agent",
		Run:         chatAgent.Run,
	})
	if err != nil {
		cr.emitEvent("error", map[string]any{"error": fmt.Sprintf("Failed to create agent: %v", err)})
		return
	}

	rnr, err := runner.New(runner.Config{
		AppName:        studioChatAppName,
		Agent:          adkAgent,
		SessionService: sessionService,
	})
	if err != nil {
		cr.emitEvent("error", map[string]any{"error": fmt.Sprintf("Failed to create runner: %v", err)})
		return
	}

	// Set auto-approve for this request
	chatAgent.AutoApprove = autoApprove

	// Inject per-turn session context
	if systemContext != "" {
		chatAgent.SystemPrompt.SessionContext = agent.EscapeCurlyPlaceholders(systemContext)
		defer func() { chatAgent.SystemPrompt.SessionContext = "" }()
	}

	// Wire transparent sub-agent streaming
	chatAgent.UIEventCallback = func(event *session.Event) {
		if event == nil || event.LLMResponse.Content == nil {
			return
		}

		// When SubTaskProgressCallback is active, sub-agent events are rendered
		// inside the TaskPlanPanel via subtask_progress SSE events. Suppress flat
		// text/tool_call/tool_result emission to avoid duplicate rendering.
		// Still drain images and flow output — these are side-channel data that
		// must be emitted regardless of which rendering path is active.
		if chatAgent.SubTaskProgressCallback != nil {
			for _, part := range event.LLMResponse.Content.Parts {
				if part.FunctionResponse != nil {
					cr.drainImagesAndFlowOutput(chatAgent)
				}
			}
			return
		}

		for _, part := range event.LLMResponse.Content.Parts {
			if part.Text != "" && !part.Thought {
				cr.emitEvent("text", map[string]any{"text": part.Text})
			}
			if part.FunctionCall != nil {
				args := part.FunctionCall.Args
				if chatAgent.Redactor != nil && args != nil {
					args = chatAgent.Redactor.RedactMap(args)
				}
				cr.emitEvent("tool_call", map[string]any{
					"name": part.FunctionCall.Name,
					"args": args,
				})
			}
			if part.FunctionResponse != nil {
				resp := part.FunctionResponse.Response
				if chatAgent.Redactor != nil && resp != nil {
					resp = chatAgent.Redactor.RedactMap(resp)
				}
				cr.emitEvent("tool_result", map[string]any{
					"name":   part.FunctionResponse.Name,
					"result": summarizeToolResult(resp),
				})
				cr.drainImagesAndFlowOutput(chatAgent)
			}
		}
	}
	defer func() { chatAgent.UIEventCallback = nil }()

	// Wire structured sub-task progress events for task plan visualization.
	// These are emitted as `subtask_progress` SSE events, carrying lifecycle
	// info (delegation_start, task_start, task_complete) and tagged activity
	// (task_tool_call, task_tool_result, task_text) with the task name.
	// Also carries plan events (plan_announced, plan_step_update) from the
	// announce_plan tool.
	chatAgent.SubTaskProgressCallback = func(evt agent.SubTaskProgressEvent) {
		data := map[string]any{
			"event_type": evt.Type,
			"task_name":  evt.TaskName,
		}
		// Include fields conditionally to keep payloads lean
		if len(evt.Tasks) > 0 {
			data["tasks"] = evt.Tasks
		}
		if evt.Status != "" {
			data["status"] = evt.Status
		}
		if evt.Duration != "" {
			data["duration"] = evt.Duration
		}
		if evt.Error != "" {
			data["error"] = evt.Error
		}
		if evt.ToolName != "" {
			data["tool_name"] = evt.ToolName
		}
		if evt.ToolArgs != nil {
			if chatAgent.Redactor != nil {
				if argsMap, ok := evt.ToolArgs.(map[string]any); ok {
					data["tool_args"] = chatAgent.Redactor.RedactMap(argsMap)
				} else {
					data["tool_args"] = evt.ToolArgs
				}
			} else {
				data["tool_args"] = evt.ToolArgs
			}
		}
		if evt.ToolResult != nil {
			if resultMap, ok := evt.ToolResult.(map[string]any); ok {
				data["tool_result"] = summarizeToolResult(resultMap)
			} else {
				data["tool_result"] = evt.ToolResult
			}
		}
		if evt.Text != "" {
			data["text"] = evt.Text
		}
		// Plan-specific fields
		if evt.PlanGoal != "" {
			data["plan_goal"] = evt.PlanGoal
		}
		if len(evt.PlanSteps) > 0 {
			data["plan_steps"] = evt.PlanSteps
		}
		if evt.StepName != "" {
			data["step_name"] = evt.StepName
		}
		if evt.StepStatus != "" {
			data["step_status"] = evt.StepStatus
		}
		cr.emitEvent("subtask_progress", data)

		// Special handling: when a sub-agent calls browser_request_human and
		// returns a VNC proxy URL, emit an additional tool_result event so the
		// frontend renders the BrowserView component. Without this, the VNC URL
		// is buried inside the subtask_progress event and the user never sees
		// the browser panel.
		if evt.Type == "task_tool_result" && evt.ToolName == "browser_request_human" {
			if resultMap, ok := evt.ToolResult.(map[string]any); ok {
				if _, hasVNC := resultMap["vnc_proxy_url"]; hasVNC {
					cr.emitEvent("tool_result", map[string]any{
						"name":   "browser_request_human",
						"result": resultMap,
					})
				}
			}
		}
	}
	defer func() { chatAgent.SubTaskProgressCallback = nil }()

	// Run the agent and emit events.
	// Track whether the run produced a proper completion or was truncated.
	seenPartialText := false
	var lastRunErr error
	hasContent := false // true if any non-partial text or tool call was emitted

	for event, runErr := range rnr.Run(cr.ctx, studioChatUserID, cr.SessionID, userMsg, adkagent.RunConfig{
		StreamingMode: adkagent.StreamingModeSSE,
	}) {
		if cr.ctx.Err() != nil {
			break
		}

		if runErr != nil {
			lastRunErr = runErr
			cr.emitEvent("error", map[string]any{"error": runErr.Error()})
			persistRunError(cr.ctx, sessionService, cr.SessionID, runErr)
			break
		}

		// Process state delta for tool approval, spinner, retry, errors
		if event.Actions.StateDelta != nil {
			cr.processStateDelta(event.Actions.StateDelta)
		}

		// Process content parts
		if event.LLMResponse.Content != nil {
			for _, part := range event.LLMResponse.Content.Parts {
				if part.Text != "" {
					if event.LLMResponse.Partial {
						seenPartialText = true
						cr.emitEvent("text", map[string]any{"text": part.Text})
					} else if !seenPartialText {
						hasContent = true
						cr.emitEvent("text", map[string]any{"text": part.Text})
					} else {
						seenPartialText = false
						hasContent = true
					}
				}
				if part.FunctionCall != nil {
					hasContent = true
					// Suppress plan tool calls — their effect is visible via the PlanPanel,
					// showing them as raw tool_call messages adds noise.
					if part.FunctionCall.Name == "announce_plan" {
						continue
					}
					args := part.FunctionCall.Args
					if chatAgent.Redactor != nil && args != nil {
						args = chatAgent.Redactor.RedactMap(args)
					}
					cr.emitEvent("tool_call", map[string]any{
						"name": part.FunctionCall.Name,
						"args": args,
					})
				}
				if part.FunctionResponse != nil {
					hasContent = true
					// Suppress plan tool results — no useful info for the user.
					if part.FunctionResponse.Name == "announce_plan" {
						continue
					}
					resp := part.FunctionResponse.Response
					if chatAgent.Redactor != nil && resp != nil {
						resp = chatAgent.Redactor.RedactMap(resp)
					}
					cr.emitEvent("tool_result", map[string]any{
						"name":   part.FunctionResponse.Name,
						"result": summarizeToolResult(resp),
					})
					cr.drainImagesAndFlowOutput(chatAgent)
				}
			}
		}

		// Emit usage event when the provider reports token counts.
		// Only for non-partial responses to avoid duplicate emissions.
		if event.LLMResponse.UsageMetadata != nil && !event.LLMResponse.Partial {
			um := event.LLMResponse.UsageMetadata
			cr.emitEvent("usage", map[string]any{
				"input_tokens":  um.PromptTokenCount,
				"output_tokens": um.CandidatesTokenCount,
				"total_tokens":  um.TotalTokenCount,
			})
		}
	}

	// Safety net: if the run loop exited due to a stream truncation error
	// (from the provider detecting no finish_reason), attempt a single retry
	// by re-running the agent. The session history already contains the partial
	// response, so the LLM will see the conversation so far and continue.
	if lastRunErr != nil && isStreamTruncationError(lastRunErr) && cr.ctx.Err() == nil {
		slog.Warn("LLM stream was truncated, attempting retry",
			"session", cr.SessionID,
			"error", lastRunErr.Error())
		cr.emitEvent("retry", map[string]any{
			"attempt":    1,
			"maxRetries": 1,
			"reason":     "LLM stream was truncated — retrying automatically",
		})

		// Small delay before retry to avoid hammering the API
		time.Sleep(2 * time.Second)

		// Build a nudge message to continue the conversation
		nudgeMsg := &genai.Content{
			Role:  "user",
			Parts: []*genai.Part{{Text: "Your previous response was cut off mid-stream. Please continue from where you left off and complete the task."}},
		}

		seenPartialText = false
		for event, runErr := range rnr.Run(cr.ctx, studioChatUserID, cr.SessionID, nudgeMsg, adkagent.RunConfig{
			StreamingMode: adkagent.StreamingModeSSE,
		}) {
			if cr.ctx.Err() != nil {
				break
			}
			if runErr != nil {
				cr.emitEvent("error", map[string]any{"error": fmt.Sprintf("Retry also failed: %v", runErr)})
				persistRunError(cr.ctx, sessionService, cr.SessionID, runErr)
				break
			}
			if event.Actions.StateDelta != nil {
				cr.processStateDelta(event.Actions.StateDelta)
			}
			if event.LLMResponse.Content != nil {
				for _, part := range event.LLMResponse.Content.Parts {
					if part.Text != "" {
						if event.LLMResponse.Partial {
							seenPartialText = true
							cr.emitEvent("text", map[string]any{"text": part.Text})
						} else if !seenPartialText {
							cr.emitEvent("text", map[string]any{"text": part.Text})
						} else {
							seenPartialText = false
						}
					}
					if part.FunctionCall != nil {
						if part.FunctionCall.Name == "announce_plan" {
							continue
						}
						args := part.FunctionCall.Args
						if chatAgent.Redactor != nil && args != nil {
							args = chatAgent.Redactor.RedactMap(args)
						}
						cr.emitEvent("tool_call", map[string]any{
							"name": part.FunctionCall.Name,
							"args": args,
						})
					}
					if part.FunctionResponse != nil {
						if part.FunctionResponse.Name == "announce_plan" {
							continue
						}
						resp := part.FunctionResponse.Response
						if chatAgent.Redactor != nil && resp != nil {
							resp = chatAgent.Redactor.RedactMap(resp)
						}
						cr.emitEvent("tool_result", map[string]any{
							"name":   part.FunctionResponse.Name,
							"result": summarizeToolResult(resp),
						})
						cr.drainImagesAndFlowOutput(chatAgent)
					}
				}
			}

			// Emit usage event for retry loop too.
			if event.LLMResponse.UsageMetadata != nil && !event.LLMResponse.Partial {
				um := event.LLMResponse.UsageMetadata
				cr.emitEvent("usage", map[string]any{
					"input_tokens":  um.PromptTokenCount,
					"output_tokens": um.CandidatesTokenCount,
					"total_tokens":  um.TotalTokenCount,
				})
			}
		}
	} else if lastRunErr == nil && !hasContent && cr.ctx.Err() == nil {
		// The run loop exited cleanly but produced no content at all.
		// This shouldn't happen in normal operation — surface it to the user.
		cr.emitEvent("error", map[string]any{
			"error": "The model returned an empty response. Please try sending your message again.",
		})
	}

	// Generate title for new sessions after first exchange.
	// This runs synchronously (before the deferred done/closeSubscribers) so the
	// session_title SSE event reaches the browser while the connection is still open.
	if cr.IsNew && msg != "" {
		if fileStore != nil {
			generateStudioSessionTitle(llm, fileStore, cr.SessionID, msg, func(title string) {
				cr.emitEvent("session_title", map[string]any{"title": title})
			})
		}
	}

	// Post-processing: detect astonish-app code fences in the accumulated response
	// text and emit app_preview events + persist them.
	cr.detectAndEmitAppPreviews(chatAgent, sessionService)
}

// processStateDelta extracts approval, retry, error, and thinking events from state deltas.
func (cr *ChatRunner) processStateDelta(delta map[string]any) {
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
		cr.emitEvent("approval", map[string]any{
			"tool":    toolName,
			"options": options,
		})
	}

	if autoApproved, ok := delta["auto_approved"].(bool); ok && autoApproved {
		if toolName, ok := delta["approval_tool"].(string); ok {
			cr.emitEvent("auto_approved", map[string]any{"tool": toolName})
		}
	}

	if retryInfoVal, ok := delta["_retry_info"]; ok {
		if retryInfo, ok := retryInfoVal.(map[string]interface{}); ok {
			cr.emitEvent("retry", map[string]any{
				"attempt":    toInt(retryInfo["attempt"]),
				"maxRetries": toInt(retryInfo["max_retries"]),
				"reason":     retryInfo["reason"],
			})
		}
	}

	if failureInfoVal, ok := delta["_failure_info"]; ok {
		if failureInfo, ok := failureInfoVal.(map[string]interface{}); ok {
			cr.emitEvent("error_info", map[string]any{
				"title":         failureInfo["title"],
				"reason":        failureInfo["reason"],
				"suggestion":    failureInfo["suggestion"],
				"originalError": failureInfo["original_error"],
			})
		}
	}

	if spinnerText, ok := delta["_spinner_text"].(string); ok {
		cr.emitEvent("thinking", map[string]any{"text": spinnerText})
	}
}

// drainImagesAndFlowOutput drains images, flow output, and file artifacts from the chat agent and emits them as events.
func (cr *ChatRunner) drainImagesAndFlowOutput(chatAgent *agent.ChatAgent) {
	for _, img := range chatAgent.DrainImages() {
		mimeType := "image/png"
		if img.Format == "jpeg" || img.Format == "jpg" {
			mimeType = "image/jpeg"
		}
		cr.emitEvent("image", map[string]any{
			"data":     base64.StdEncoding.EncodeToString(img.Data),
			"mimeType": mimeType,
		})
	}
	if flowOut := chatAgent.DrainFlowOutput(); flowOut != "" {
		cr.emitEvent("flow_output", map[string]any{"content": flowOut})
	}
	for _, file := range chatAgent.DrainFiles() {
		cr.emitEvent("artifact", map[string]any{
			"path":      file.Path,
			"tool_name": file.ToolName,
		})
	}
}

// appPreviewFenceRe matches ```astonish-app code fences. It captures the code content
// between the opening and closing fences.
var appPreviewFenceRe = regexp.MustCompile("(?s)```astonish-app\\s*\\n(.*?)\\n```")

// detectAndEmitAppPreviews scans the buffered text events for astonish-app code fences.
// When found, it emits an app_preview event, persists it to the session transcript,
// and updates the active app state on the ChatAgent for cross-turn refinement.
func (cr *ChatRunner) detectAndEmitAppPreviews(chatAgent *agent.ChatAgent, sessionService session.Service) {
	// Reconstruct the full response text from buffered text events
	cr.eventsMu.RLock()
	var fullText strings.Builder
	for _, ev := range cr.events {
		if ev.Type == "text" {
			if t, ok := ev.Data["text"].(string); ok {
				fullText.WriteString(t)
			}
		}
	}
	cr.eventsMu.RUnlock()

	text := fullText.String()
	matches := appPreviewFenceRe.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return
	}

	// Check if there's an existing active app for this session
	existingApp := chatAgent.GetActiveApp(cr.SessionID)

	for _, match := range matches {
		code := strings.TrimSpace(match[1])
		if code == "" {
			continue
		}

		// Strip optional YAML-style frontmatter (title: ...\n---\n) that LLMs
		// sometimes emit before the JSX. The frontmatter title is used below;
		// the clean code (without frontmatter) is what gets persisted, sent to
		// the sandbox, and compared for dedup.
		cleanCode, fmTitle := stripAppFrontmatter(code)

		// Skip if the LLM re-emitted the exact same code that was already seeded
		// (e.g., on the first turn of an "Improve with AI" session).
		if existingApp != nil && existingApp.Code == cleanCode {
			continue
		}

		// Prefer the frontmatter title; fall back to extracting from JSX.
		title := fmTitle
		if title == "" {
			title = extractComponentTitle(cleanCode)
		}

		var appID string
		var version int

		if existingApp != nil {
			// Refinement of existing app — keep appId, increment version
			appID = existingApp.AppID
			version = existingApp.Version + 1
			existingApp.Versions = append(existingApp.Versions, existingApp.Code)
			existingApp.Code = cleanCode
			existingApp.Version = version
			existingApp.Title = title
		} else {
			// New app — generate fresh appId
			appID = uuid.New().String()
			version = 1
			existingApp = &agent.ActiveApp{
				AppID:    appID,
				Title:    title,
				Code:     cleanCode,
				Versions: []string{},
				Version:  version,
			}
		}

		cr.emitEvent("app_preview", map[string]any{
			"code":        cleanCode,
			"title":       title,
			"description": "",
			"version":     version,
			"appId":       appID,
		})

		// Persist to session transcript
		persistAppPreview(cr.ctx, sessionService, cr.SessionID, cleanCode, title, version, appID)

		// Update active app state for cross-turn refinement
		chatAgent.SetActiveApp(cr.SessionID, existingApp)
	}
}

// extractComponentTitle tries to find the main component name from JSX code.
// It prioritizes "export default function X" / "export default const X" patterns
// over helper components defined earlier in the file.
func extractComponentTitle(code string) string {
	// Priority 1: export default function/const declaration
	exportDefaultRe := regexp.MustCompile(`(?m)^export\s+default\s+function\s+([A-Z][a-zA-Z0-9]*)`)
	if m := exportDefaultRe.FindStringSubmatch(code); len(m) > 1 {
		return splitCamelCase(m[1])
	}
	exportDefaultConstRe := regexp.MustCompile(`(?m)^export\s+default\s+(?:const|let)\s+([A-Z][a-zA-Z0-9]*)`)
	if m := exportDefaultConstRe.FindStringSubmatch(code); len(m) > 1 {
		return splitCamelCase(m[1])
	}

	// Priority 2: Look for "export default X" at end, then find "function X" or "const X" above
	exportDefaultNameRe := regexp.MustCompile(`(?m)^export\s+default\s+([A-Z][a-zA-Z0-9]*)\s*;?\s*$`)
	if m := exportDefaultNameRe.FindStringSubmatch(code); len(m) > 1 {
		return splitCamelCase(m[1])
	}

	// Priority 3: Last PascalCase function/const (main component is typically last, helpers above)
	funcRe := regexp.MustCompile(`(?m)^(?:export\s+)?function\s+([A-Z][a-zA-Z0-9]*)`)
	matches := funcRe.FindAllStringSubmatch(code, -1)
	if len(matches) > 0 {
		return splitCamelCase(matches[len(matches)-1][1])
	}
	constRe := regexp.MustCompile(`(?m)^(?:export\s+)?(?:const|let)\s+([A-Z][a-zA-Z0-9]*)`)
	matches = constRe.FindAllStringSubmatch(code, -1)
	if len(matches) > 0 {
		return splitCamelCase(matches[len(matches)-1][1])
	}

	return "App Preview"
}

// stripAppFrontmatter removes optional YAML-style frontmatter from app code.
// LLMs sometimes emit code fences with a "title: ...\n---\n" header before the
// actual JSX. This function strips it, returning the clean JSX code and the
// extracted title (if any). If no frontmatter is found, the original code is
// returned unchanged with an empty title.
//
// Recognised format:
//
//	title: My App Title
//	description: optional description
//	---
//	function App() { ... }
func stripAppFrontmatter(code string) (cleanCode string, fmTitle string) {
	sepIdx := strings.Index(code, "\n---\n")
	if sepIdx < 0 {
		return code, ""
	}
	header := code[:sepIdx]
	// Sanity-check: the header must look like YAML key-value lines, not JSX.
	// We require at least a "title:" line to treat it as frontmatter.
	if !strings.Contains(header, "title:") {
		return code, ""
	}
	// Extract title value
	for _, line := range strings.Split(header, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "title:") {
			fmTitle = strings.TrimSpace(strings.TrimPrefix(line, "title:"))
		}
	}
	cleanCode = strings.TrimSpace(code[sepIdx+len("\n---\n"):])
	return cleanCode, fmTitle
}

// splitCamelCase converts "SalesDashboard" to "Sales Dashboard".
func splitCamelCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteRune(' ')
		}
		result.WriteRune(r)
	}
	return result.String()
}

// isStreamTruncationError returns true if the error indicates the LLM stream
// was truncated (no finish_reason received). This is the specific error
// produced by the OpenAI provider when a gateway timeout or connection drop
// occurs mid-stream.
func isStreamTruncationError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "stream ended without a finish_reason")
}

// emitEvent creates a ChatEvent and sends it to all subscribers.
func (cr *ChatRunner) emitEvent(eventType string, data map[string]any) {
	event := ChatEvent{
		ID:        fmt.Sprintf("%s-%d", eventType, time.Now().UnixNano()),
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now(),
	}

	// Buffer the event
	cr.eventsMu.Lock()
	cr.events = append(cr.events, event)
	cr.eventsMu.Unlock()

	// Broadcast to subscribers (non-blocking)
	cr.subMu.RLock()
	for _, ch := range cr.subscribers {
		select {
		case ch <- event:
		default:
			// subscriber channel full, drop event (subscriber is too slow)
		}
	}
	cr.subMu.RUnlock()
}

// Subscribe returns a channel that receives events from this runner.
// The channel is buffered to avoid blocking the runner.
func (cr *ChatRunner) Subscribe(id string) <-chan ChatEvent {
	ch := make(chan ChatEvent, 200)
	cr.subMu.Lock()
	cr.subscribers[id] = ch
	cr.subMu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber. The subscriber's channel is closed.
func (cr *ChatRunner) Unsubscribe(id string) {
	cr.subMu.Lock()
	if ch, ok := cr.subscribers[id]; ok {
		close(ch)
		delete(cr.subscribers, id)
	}
	cr.subMu.Unlock()
}

// GetHistory returns all buffered events for catch-up replay.
func (cr *ChatRunner) GetHistory() []ChatEvent {
	cr.eventsMu.RLock()
	defer cr.eventsMu.RUnlock()
	history := make([]ChatEvent, len(cr.events))
	copy(history, cr.events)
	return history
}

// IsDone returns whether the runner has completed execution.
func (cr *ChatRunner) IsDone() bool {
	cr.doneMu.RLock()
	defer cr.doneMu.RUnlock()
	return cr.done
}

// Stop cancels the background context, terminating the agent run.
func (cr *ChatRunner) Stop() {
	cr.cancel()
}

// closeSubscribers closes all subscriber channels. Called when the runner is done.
func (cr *ChatRunner) closeSubscribers() {
	cr.subMu.Lock()
	for id, ch := range cr.subscribers {
		close(ch)
		delete(cr.subscribers, id)
	}
	cr.subMu.Unlock()
}

// Context returns the runner's context for external cancellation checks.
func (cr *ChatRunner) Context() context.Context {
	return cr.ctx
}

// EventCount returns the number of buffered events.
func (cr *ChatRunner) EventCount() int {
	cr.eventsMu.RLock()
	defer cr.eventsMu.RUnlock()
	return len(cr.events)
}

// chatRunnerRegistry is a thread-safe registry of active ChatRunner instances.
type chatRunnerRegistry struct {
	runners map[string]*ChatRunner
	mu      sync.RWMutex
}

var (
	globalChatRunnerRegistry *chatRunnerRegistry
	chatRunnerRegistryOnce   sync.Once
)

// getChatRunnerRegistry returns the singleton registry.
func getChatRunnerRegistry() *chatRunnerRegistry {
	chatRunnerRegistryOnce.Do(func() {
		globalChatRunnerRegistry = &chatRunnerRegistry{
			runners: make(map[string]*ChatRunner),
		}
	})
	return globalChatRunnerRegistry
}

// Register stores a runner for a session. If a previous runner exists for the
// same session, it is stopped and replaced.
func (r *chatRunnerRegistry) Register(sessionID string, runner *ChatRunner) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if prev, ok := r.runners[sessionID]; ok {
		prev.Stop()
	}
	r.runners[sessionID] = runner
}

// Get returns the runner for a session, or nil if none exists.
func (r *chatRunnerRegistry) Get(sessionID string) *ChatRunner {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.runners[sessionID]
}

// Unregister removes the runner for a session.
func (r *chatRunnerRegistry) Unregister(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.runners, sessionID)
}

// Stop cancels a runner for a session and removes it from the registry.
func (r *chatRunnerRegistry) Stop(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if runner, ok := r.runners[sessionID]; ok {
		runner.Stop()
		delete(r.runners, sessionID)
	}
}

// StopAll cancels all runners and clears the registry.
func (r *chatRunnerRegistry) StopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, runner := range r.runners {
		runner.Stop()
		delete(r.runners, id)
	}
}

// Cleanup removes completed runners from the registry.
func (r *chatRunnerRegistry) Cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, runner := range r.runners {
		if runner.IsDone() {
			delete(r.runners, id)
		}
	}
}

// IsRunning returns true if there is an active (not done) runner for the session.
func (r *chatRunnerRegistry) IsRunning(sessionID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	runner, ok := r.runners[sessionID]
	if !ok {
		return false
	}
	return !runner.IsDone()
}

// startCleanupLoop starts a background goroutine that periodically removes
// completed runners from the registry. Called once at init.
func (r *chatRunnerRegistry) startCleanupLoop() {
	go func() {
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			r.Cleanup()
		}
	}()
}

func init() {
	// Start the cleanup loop when the package is loaded
	registry := getChatRunnerRegistry()
	registry.startCleanupLoop()

	_ = slog.Default() // suppress unused import if needed
}
