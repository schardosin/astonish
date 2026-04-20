package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/credentials"
	persistentsession "github.com/schardosin/astonish/pkg/session"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// titleThinkTagRe strips <think>/<thinking> blocks that some models emit in
// title-generation responses.
var titleThinkTagRe = regexp.MustCompile(`(?s)<(?:think|thinking)>.*?</(?:think|thinking)>`)

// userTimestampRe matches the timestamp prefix injected by NewTimestampedUserContent.
// Format: "[2026-03-20 14:30:05 UTC]\n"
var userTimestampRe = regexp.MustCompile(`^\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} \w+\]\n`)

// pendingDelegation holds buffered data from a delegate_tasks FunctionCall
// until the matching FunctionResponse arrives, so we can reconstruct a
// complete subtask_execution message.
type pendingDelegation struct {
	tasks []SubTaskInfoMsg // task plan from the call args
}

// eventsToMessages transforms ADK session events into a flat message list for the frontend.
// An optional redactor is applied to all text parts and tool args/results to prevent
// credential exposure. This is the defense-in-depth layer: even if retroactive transcript
// redaction missed a secret, the UI will never display it in plaintext.
//
// delegate_tasks tool calls and their responses are reconstructed into
// subtask_execution messages that render as TaskPlanPanel in the frontend,
// preserving the task plan visualization for completed sessions.
func eventsToMessages(events session.Events, redactor *credentials.Redactor) []StudioMessage {
	var messages []StudioMessage
	var lastInvocationID string // track invocation boundary for coalescing

	// Buffer delegate_tasks calls by their FunctionCall ID so we can match
	// them with the corresponding FunctionResponse. ADK function calls carry
	// a unique ID that the response echoes back.
	pendingDelegations := make(map[string]*pendingDelegation)

	// Buffer write_file/edit_file calls so we can emit inline artifact
	// messages when the corresponding FunctionResponse confirms success.
	type pendingWriteFile struct {
		path     string
		toolName string
	}
	pendingWriteFiles := make(map[string]pendingWriteFile)

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

				// Check for structured distill messages (model only).
				// These are persisted with a special prefix marker and must
				// be reconstructed as their own message types, not coalesced.
				if role == "model" {
					if dm := tryParseDistillMessage(text); dm != nil {
						messages = append(messages, *dm)
						lastInvocationID = eventInvID
						continue
					}
					if am := tryParseAppPreviewMessage(text); am != nil {
						messages = append(messages, *am)
						lastInvocationID = eventInvID
						continue
					}
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
				// Intercept delegate_tasks calls: buffer the task plan and
				// suppress the flat tool_call message. The matching
				// FunctionResponse below will produce a subtask_execution
				// message instead.
				if part.FunctionCall.Name == "delegate_tasks" {
					pd := extractDelegationPlan(part.FunctionCall.Args)
					pendingDelegations[part.FunctionCall.ID] = pd
					continue
				}

				// Intercept announce_plan: emit a plan message immediately.
				if part.FunctionCall.Name == "announce_plan" {
					msg := buildPlanMessage(part.FunctionCall.Args)
					messages = append(messages, msg)
					continue
				}

				// Intercept update_plan: update the most recent plan message's step.
				if part.FunctionCall.Name == "update_plan" {
					applyPlanStepUpdate(messages, part.FunctionCall.Args)
					continue
				}

				// Buffer write_file/edit_file calls to emit artifact messages
				// when their FunctionResponse confirms success.
				if part.FunctionCall.Name == "write_file" || part.FunctionCall.Name == "edit_file" {
					p := extractFilePath(part.FunctionCall.Name, part.FunctionCall.Args)
					if p != "" {
						pendingWriteFiles[part.FunctionCall.ID] = pendingWriteFile{
							path:     p,
							toolName: part.FunctionCall.Name,
						}
					}
				}

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
				// Intercept delegate_tasks responses: combine with the
				// buffered task plan to produce a subtask_execution message.
				if part.FunctionResponse.Name == "delegate_tasks" {
					msg := buildSubTaskExecutionMessage(
						pendingDelegations[part.FunctionResponse.ID],
						part.FunctionResponse.Response,
					)
					delete(pendingDelegations, part.FunctionResponse.ID)
					messages = append(messages, msg)
					continue
				}

				// Suppress plan tool responses — no useful display info.
				if part.FunctionResponse.Name == "announce_plan" || part.FunctionResponse.Name == "update_plan" {
					continue
				}

				resp := part.FunctionResponse.Response
				if redactor != nil && resp != nil {
					resp = redactor.RedactMap(resp)
				}
				messages = append(messages, StudioMessage{
					Type:       "tool_result",
					ToolName:   part.FunctionResponse.Name,
					ToolResult: summarizeToolResult(resp),
				})

				// Emit inline artifact message for successful write_file/edit_file
				if pw, ok := pendingWriteFiles[part.FunctionResponse.ID]; ok {
					delete(pendingWriteFiles, part.FunctionResponse.ID)
					// Only emit if the tool succeeded (no error in response)
					hasError := false
					if resp != nil {
						if _, errField := resp["error"]; errField {
							hasError = true
						}
					}
					if !hasError {
						messages = append(messages, StudioMessage{
							Type:     "artifact",
							Content:  pw.path,
							ToolName: pw.toolName,
						})
					}
				}
			}
		}
	}

	// Post-process: for completed sessions, promote any remaining "pending" or
	// "running" plan steps to "complete". The session is done, so all steps
	// should reflect their final state. Only "failed" steps stay as-is.
	finalizePlanSteps(messages)

	return messages
}

// extractDelegationPlan parses the task plan from a delegate_tasks FunctionCall args map.
func extractDelegationPlan(args map[string]any) *pendingDelegation {
	pd := &pendingDelegation{}
	if args == nil {
		return pd
	}

	tasksRaw, ok := args["tasks"]
	if !ok {
		return pd
	}

	tasksList, ok := tasksRaw.([]any)
	if !ok {
		return pd
	}

	for _, t := range tasksList {
		taskMap, ok := t.(map[string]any)
		if !ok {
			continue
		}
		name, _ := taskMap["name"].(string)
		// The description field is called "task" in the tool input schema
		desc, _ := taskMap["task"].(string)
		if name != "" {
			pd.tasks = append(pd.tasks, SubTaskInfoMsg{
				Name:        name,
				Description: desc,
			})
		}
	}
	return pd
}

// buildSubTaskExecutionMessage reconstructs a subtask_execution message from
// a buffered delegation plan and the delegate_tasks response.
func buildSubTaskExecutionMessage(pd *pendingDelegation, resp map[string]any) StudioMessage {
	msg := StudioMessage{
		Type:   "subtask_execution",
		Status: "complete",
	}

	// Use tasks from the buffered call if available
	if pd != nil {
		msg.Tasks = pd.tasks
	}

	// Build synthetic events from the response results
	var events []SubTaskEventMsg

	// Opening event
	events = append(events, SubTaskEventMsg{
		Type: "delegation_start",
	})

	if resp != nil {
		// Extract overall status
		if status, ok := resp["status"].(string); ok {
			msg.Status = status
		}

		// Extract per-task results
		if resultsRaw, ok := resp["results"]; ok {
			if resultsList, ok := resultsRaw.([]any); ok {
				for _, r := range resultsList {
					rMap, ok := r.(map[string]any)
					if !ok {
						continue
					}
					taskName, _ := rMap["name"].(string)
					taskStatus, _ := rMap["status"].(string)
					taskDuration, _ := rMap["duration"].(string)
					taskError, _ := rMap["error"].(string)
					taskResult, _ := rMap["result"].(string)

					// Emit task_start
					events = append(events, SubTaskEventMsg{
						Type:     "task_start",
						TaskName: taskName,
					})

					// Emit task_text with the sub-agent's result output (if any)
					if taskResult != "" {
						events = append(events, SubTaskEventMsg{
							Type:     "task_text",
							TaskName: taskName,
							Text:     taskResult,
						})
					}

					// Emit task_complete or task_failed
					if taskStatus == "success" {
						events = append(events, SubTaskEventMsg{
							Type:     "task_complete",
							TaskName: taskName,
							Duration: taskDuration,
						})
					} else {
						events = append(events, SubTaskEventMsg{
							Type:     "task_failed",
							TaskName: taskName,
							Duration: taskDuration,
							Error:    taskError,
						})
					}
				}
			}
		}
	}

	// Closing event
	events = append(events, SubTaskEventMsg{
		Type:   "delegation_complete",
		Status: msg.Status,
	})

	msg.Events = events

	// If we didn't get tasks from the call args (edge case: missing FunctionCall ID match),
	// try to reconstruct task names from the response results.
	if len(msg.Tasks) == 0 && resp != nil {
		if resultsRaw, ok := resp["results"]; ok {
			if resultsList, ok := resultsRaw.([]any); ok {
				for _, r := range resultsList {
					rMap, ok := r.(map[string]any)
					if !ok {
						continue
					}
					name, _ := rMap["name"].(string)
					if name != "" {
						msg.Tasks = append(msg.Tasks, SubTaskInfoMsg{
							Name: name,
						})
					}
				}
			}
		}
	}

	return msg
}

// buildPlanMessage creates a plan message from an announce_plan FunctionCall args map.
func buildPlanMessage(args map[string]any) StudioMessage {
	msg := StudioMessage{
		Type: "plan",
	}

	if args == nil {
		return msg
	}

	msg.Goal, _ = args["goal"].(string)

	if stepsRaw, ok := args["steps"]; ok {
		if stepsList, ok := stepsRaw.([]any); ok {
			for _, s := range stepsList {
				sMap, ok := s.(map[string]any)
				if !ok {
					continue
				}
				name, _ := sMap["name"].(string)
				desc, _ := sMap["description"].(string)
				if name != "" {
					msg.Steps = append(msg.Steps, PlanStepMsg{
						Name:        name,
						Description: desc,
						Status:      "pending",
					})
				}
			}
		}
	}

	return msg
}

// applyPlanStepUpdate updates the most recent plan message's step status
// based on an update_plan FunctionCall args map.
func applyPlanStepUpdate(messages []StudioMessage, args map[string]any) {
	if args == nil {
		return
	}

	stepName, _ := args["step"].(string)
	stepStatus, _ := args["status"].(string)
	if stepName == "" || stepStatus == "" {
		return
	}

	// Find the most recent plan message (search from end)
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Type == "plan" {
			for j := range messages[i].Steps {
				if messages[i].Steps[j].Name == stepName {
					messages[i].Steps[j].Status = stepStatus
				}
			}
			return
		}
	}
}

// finalizePlanSteps promotes any "pending" or "running" plan steps to "complete"
// for completed session history. Only "failed" steps are left as-is.
func finalizePlanSteps(messages []StudioMessage) {
	for i := range messages {
		if messages[i].Type == "plan" {
			for j := range messages[i].Steps {
				s := messages[i].Steps[j].Status
				if s == "pending" || s == "running" {
					messages[i].Steps[j].Status = "complete"
				}
			}
		}
	}
}

// collectUsage sums UsageMetadata from all LLM responses in the session
// transcript. Each non-partial event with a non-nil UsageMetadata represents
// one API call's token counts. Returns nil if no usage data is present.
func collectUsage(events session.Events) *UsageSummary {
	var input, output, total int32
	for i := range events.Len() {
		event := events.At(i)
		if um := event.LLMResponse.UsageMetadata; um != nil {
			input += um.PromptTokenCount
			output += um.CandidatesTokenCount
			total += um.TotalTokenCount
		}
	}
	if total == 0 {
		return nil
	}
	return &UsageSummary{
		InputTokens:  input,
		OutputTokens: output,
		TotalTokens:  total,
	}
}

// collectArtifacts scans ADK session events for successful write_file/edit_file
// tool calls and returns a deduplicated list of file artifacts. This is used to
// populate the artifacts field in the session detail API response, enabling the
// file panel in the UI for completed sessions.
func collectArtifacts(events session.Events) []ArtifactInfo {
	// Track pending write_file/edit_file calls by FunctionCall ID
	type pendingWrite struct {
		path     string
		toolName string
	}
	pending := make(map[string]pendingWrite)

	// Deduplicate by path (keep the last write to each file)
	seen := make(map[string]bool)
	var artifacts []ArtifactInfo

	for i := range events.Len() {
		event := events.At(i)
		if event.LLMResponse.Content == nil {
			continue
		}

		for _, part := range event.LLMResponse.Content.Parts {
			if part.FunctionCall != nil {
				name := part.FunctionCall.Name
				if name == "write_file" || name == "edit_file" {
					path := extractFilePath(name, part.FunctionCall.Args)
					if path != "" {
						pending[part.FunctionCall.ID] = pendingWrite{
							path:     path,
							toolName: name,
						}
					}
				}
			}
			if part.FunctionResponse != nil {
				pw, ok := pending[part.FunctionResponse.ID]
				if !ok {
					continue
				}
				delete(pending, part.FunctionResponse.ID)

				// Check if the tool succeeded (no error in response)
				if resp := part.FunctionResponse.Response; resp != nil {
					if _, hasErr := resp["error"]; hasErr {
						continue
					}
				}

				// Deduplicate by path
				if seen[pw.path] {
					continue
				}
				seen[pw.path] = true

				artifacts = append(artifacts, ArtifactInfo{
					Path:     pw.path,
					FileName: filepath.Base(pw.path),
					FileType: fileTypeFromExt(filepath.Ext(pw.path)),
					ToolName: pw.toolName,
				})
			}
		}
	}

	return artifacts
}

// extractFilePath gets the file path from a write_file or edit_file tool call args.
func extractFilePath(toolName string, args map[string]any) string {
	if args == nil {
		return ""
	}
	switch toolName {
	case "write_file":
		if p, ok := args["file_path"].(string); ok {
			return p
		}
	case "edit_file":
		if p, ok := args["path"].(string); ok {
			return p
		}
	}
	return ""
}

// fileTypeFromExt returns a human-readable file type from a file extension.
func fileTypeFromExt(ext string) string {
	ext = strings.ToLower(ext)
	switch ext {
	case ".md", ".markdown":
		return "Markdown"
	case ".go":
		return "Go"
	case ".py":
		return "Python"
	case ".js":
		return "JavaScript"
	case ".ts":
		return "TypeScript"
	case ".tsx":
		return "TypeScript JSX"
	case ".jsx":
		return "JSX"
	case ".json":
		return "JSON"
	case ".yaml", ".yml":
		return "YAML"
	case ".html", ".htm":
		return "HTML"
	case ".css":
		return "CSS"
	case ".sh", ".bash":
		return "Shell"
	case ".sql":
		return "SQL"
	case ".txt":
		return "Text"
	case ".csv":
		return "CSV"
	case ".xml":
		return "XML"
	case ".toml":
		return "TOML"
	case ".rs":
		return "Rust"
	case ".java":
		return "Java"
	case ".rb":
		return "Ruby"
	case ".php":
		return "PHP"
	case ".c", ".h":
		return "C"
	case ".cpp", ".hpp", ".cc":
		return "C++"
	case ".swift":
		return "Swift"
	case ".kt":
		return "Kotlin"
	case ".dockerfile":
		return "Dockerfile"
	case ".env":
		return "Environment"
	default:
		if ext == "" {
			return "File"
		}
		return strings.TrimPrefix(ext, ".")
	}
}

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

	if err := store.SetSessionTitle(sessionID, title); err != nil {
		slog.Warn("failed to set session title", "session_id", sessionID, "error", err)
	}
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

// persistSessionMessage appends a user or model text message to the session.
// This is used for interactions that bypass runner.Run() (like slash commands)
// so they appear in the persisted session history.
func persistSessionMessage(ctx context.Context, svc session.Service, sessionID, role, text string) {
	if svc == nil || sessionID == "" || text == "" {
		return
	}
	resp, err := svc.Get(ctx, &session.GetRequest{
		AppName:   studioChatAppName,
		UserID:    studioChatUserID,
		SessionID: sessionID,
	})
	if err != nil {
		slog.Error("failed to get session for persist", "component", "persistSessionMessage", "session_id", sessionID, "error", err)
		return
	}

	author := role
	if role == "model" {
		author = "model"
	}

	event := &session.Event{
		ID:        fmt.Sprintf("%s-%d", role, time.Now().UnixMilli()),
		Author:    author,
		Timestamp: time.Now(),
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{
				Role:  role,
				Parts: []*genai.Part{{Text: text}},
			},
		},
	}

	if err := svc.AppendEvent(ctx, resp.Session, event); err != nil {
		slog.Error("failed to append event to session", "component", "persistSessionMessage", "session_id", sessionID, "role", role, "error", err)
	}
}

// readArtifactContentFromSession scans a session's persisted events for a
// write_file FunctionCall whose file_path matches the requested path, and
// returns the content argument. This is used as a fallback when the actual
// file no longer exists on disk (e.g., written to /tmp, or inside a sandbox
// container that is stopped).
func readArtifactContentFromSession(fs *persistentsession.FileStore, sessionID, filePath string) (string, bool) {
	if fs == nil {
		return "", false
	}

	getResp, err := fs.Get(context.Background(), &session.GetRequest{
		AppName:   studioChatAppName,
		UserID:    studioChatUserID,
		SessionID: sessionID,
	})
	if err != nil {
		return "", false
	}

	events := getResp.Session.Events()
	cleanTarget := filepath.Clean(filePath)

	// Scan from the end to find the most recent write to this path
	for i := events.Len() - 1; i >= 0; i-- {
		event := events.At(i)
		if event.LLMResponse.Content == nil {
			continue
		}
		for _, part := range event.LLMResponse.Content.Parts {
			if part.FunctionCall == nil {
				continue
			}
			if part.FunctionCall.Name != "write_file" {
				continue
			}
			args := part.FunctionCall.Args
			if args == nil {
				continue
			}
			p, _ := args["file_path"].(string)
			if filepath.Clean(p) != cleanTarget {
				continue
			}
			content, _ := args["content"].(string)
			if content != "" {
				return content, true
			}
		}
	}
	return "", false
}

// --- Distill preview/saved persistence ---
//
// Distill preview and saved messages are persisted as model text events with a
// special prefix marker so eventsToMessages can reconstruct them as structured
// StudioMessage objects. The format is:
//   [distill_preview]<JSON payload>
//   [distill_saved]<JSON payload>

const distillPreviewPrefix = "[distill_preview]"
const distillSavedPrefix = "[distill_saved]"

// distillPreviewPayload is the JSON structure persisted inside the text event.
type distillPreviewPayload struct {
	YAML        string   `json:"yaml"`
	FlowName    string   `json:"flowName"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	Explanation string   `json:"explanation,omitempty"`
}

// distillSavedPayload is the JSON structure persisted inside the text event.
type distillSavedPayload struct {
	FilePath   string `json:"filePath"`
	RunCommand string `json:"runCommand"`
}

// persistDistillPreview serializes a DistillReview as a structured text event.
func persistDistillPreview(ctx context.Context, svc session.Service, sessionID string, review *agent.DistillReview) {
	if svc == nil || sessionID == "" || review == nil {
		return
	}
	payload := distillPreviewPayload{
		YAML:        review.YAML,
		FlowName:    review.FlowName,
		Description: review.Description,
		Tags:        review.Tags,
		Explanation: review.Explanation,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Error("failed to marshal distill preview", "error", err)
		return
	}
	persistSessionMessage(ctx, svc, sessionID, "model", distillPreviewPrefix+string(data))
}

// persistDistillSaved serializes a distill-saved result as a structured text event.
func persistDistillSaved(ctx context.Context, svc session.Service, sessionID, filePath, runCmd string) {
	if svc == nil || sessionID == "" {
		return
	}
	payload := distillSavedPayload{
		FilePath:   filePath,
		RunCommand: runCmd,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Error("failed to marshal distill saved", "error", err)
		return
	}
	persistSessionMessage(ctx, svc, sessionID, "model", distillSavedPrefix+string(data))
}

// tryParseDistillMessage checks if a text starts with a distill marker prefix
// and returns a structured StudioMessage if so. Returns nil if not a distill message.
func tryParseDistillMessage(text string) *StudioMessage {
	if strings.HasPrefix(text, distillPreviewPrefix) {
		jsonStr := text[len(distillPreviewPrefix):]
		var payload distillPreviewPayload
		if err := json.Unmarshal([]byte(jsonStr), &payload); err != nil {
			return nil
		}
		return &StudioMessage{
			Type:        "distill_preview",
			YAML:        payload.YAML,
			FlowName:    payload.FlowName,
			Description: payload.Description,
			Tags:        payload.Tags,
			Explanation: payload.Explanation,
		}
	}
	if strings.HasPrefix(text, distillSavedPrefix) {
		jsonStr := text[len(distillSavedPrefix):]
		var payload distillSavedPayload
		if err := json.Unmarshal([]byte(jsonStr), &payload); err != nil {
			return nil
		}
		return &StudioMessage{
			Type:       "distill_saved",
			FilePath:   payload.FilePath,
			RunCommand: payload.RunCommand,
		}
	}
	return nil
}

// --- App preview persistence ---
//
// App preview messages are persisted using the same prefix-marker pattern as distill.
// Format: [app_preview]<JSON payload>

const appPreviewPrefix = "[app_preview]"

// appPreviewPayload is the JSON structure persisted inside the text event.
type appPreviewPayload struct {
	Code        string `json:"code"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Version     int    `json:"version"`
	AppID       string `json:"appId,omitempty"`
}

// persistAppPreview serializes an app preview as a structured text event.
func persistAppPreview(ctx context.Context, svc session.Service, sessionID, code, title string, version int, appID string) {
	if svc == nil || sessionID == "" || code == "" {
		return
	}
	payload := appPreviewPayload{
		Code:    code,
		Title:   title,
		Version: version,
		AppID:   appID,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Error("failed to marshal app preview", "error", err)
		return
	}
	persistSessionMessage(ctx, svc, sessionID, "model", appPreviewPrefix+string(data))
}

// tryParseAppPreviewMessage checks if a text starts with the app_preview marker prefix
// and returns a structured StudioMessage if so. Returns nil if not an app preview message.
func tryParseAppPreviewMessage(text string) *StudioMessage {
	if !strings.HasPrefix(text, appPreviewPrefix) {
		return nil
	}
	jsonStr := text[len(appPreviewPrefix):]
	var payload appPreviewPayload
	if err := json.Unmarshal([]byte(jsonStr), &payload); err != nil {
		return nil
	}
	return &StudioMessage{
		Type:        "app_preview",
		AppCode:     payload.Code,
		AppTitle:    payload.Title,
		Description: payload.Description,
		AppVersion:  payload.Version,
		AppID:       payload.AppID,
	}
}

// reconstructActiveApp scans session events for app_preview prefix markers and
// rebuilds the ActiveApp state. Used when the server restarts and in-memory state
// is lost. Returns nil if no app previews are found in the session.
func reconstructActiveApp(events session.Events) *agent.ActiveApp {
	var latestApp *agent.ActiveApp

	for i := range events.Len() {
		event := events.At(i)
		if event.LLMResponse.Content == nil {
			continue
		}
		for _, part := range event.LLMResponse.Content.Parts {
			if part.Text == "" {
				continue
			}
			if !strings.HasPrefix(part.Text, appPreviewPrefix) {
				continue
			}
			jsonStr := part.Text[len(appPreviewPrefix):]
			var payload appPreviewPayload
			if err := json.Unmarshal([]byte(jsonStr), &payload); err != nil {
				continue
			}

			if latestApp == nil || (payload.AppID != "" && payload.AppID != latestApp.AppID) {
				// New app (or first app found)
				latestApp = &agent.ActiveApp{
					AppID:    payload.AppID,
					Title:    payload.Title,
					Code:     payload.Code,
					Versions: []string{},
					Version:  payload.Version,
				}
			} else {
				// Same app, newer version — append previous code to history
				latestApp.Versions = append(latestApp.Versions, latestApp.Code)
				latestApp.Code = payload.Code
				latestApp.Version = payload.Version
				latestApp.Title = payload.Title
			}
		}
	}
	return latestApp
}
