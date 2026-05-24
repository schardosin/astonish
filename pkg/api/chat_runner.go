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
	"github.com/schardosin/astonish/pkg/credentials"
	"github.com/schardosin/astonish/pkg/store"
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
	UserID    string // effective user ID (platform user ID or "studio_user")
	IsNew     bool   // whether this is a newly created session

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

	// titleDone is closed when the title-generation goroutine completes
	// (or immediately if no title generation is needed). The defer block
	// in Run() waits on this channel before closing subscriber channels,
	// ensuring the session_title SSE event reaches the browser.
	titleDone chan struct{}

	// Maximum time to wait for the title goroutine after the "done"
	// event before closing subscribers regardless.
	titleWaitTimeout time.Duration
}

// newChatRunner creates a new ChatRunner with a background context.
// titleWaitTimeout controls how long to wait for the title goroutine
// after the "done" event before closing subscribers. Zero means close
// immediately (used in tests).
func newChatRunner(sessionID, userID string, isNew bool) *ChatRunner {
	ctx, cancel := context.WithCancel(context.Background())
	// Inject session ID into context so tool functions (e.g., memory_save)
	// can tag entries with the session that created them.
	ctx = store.WithSessionID(ctx, sessionID)
	return &ChatRunner{
		SessionID:   sessionID,
		UserID:      userID,
		IsNew:       isNew,
		ctx:         ctx,
		cancel:      cancel,
		subscribers: make(map[string]chan ChatEvent),
		titleDone:   make(chan struct{}),
	}
}

// InjectCredentialStore adds a tenant-scoped credential store to the runner's
// context so that tool functions can retrieve it via store.CredentialStoreFromContext.
// Must be called before Run().
func (cr *ChatRunner) InjectCredentialStore(cs store.CredentialStore) {
	cr.ctx = store.WithCredentialStore(cr.ctx, cs)
}

// InjectMemoryStores adds tenant-scoped memory stores to the runner's context.
// memStore is the team-scoped PG memory store (for saves and single-tier search).
// searcher is the three-tier searcher (personal + team + org) for cross-tier search.
// Tool functions and knowledge callbacks retrieve these via store.*FromContext.
// Must be called before Run().
func (cr *ChatRunner) InjectMemoryStores(memStore store.MemoryStore, searcher store.ThreeTierSearcher) {
	if memStore != nil {
		cr.ctx = store.WithMemoryStore(cr.ctx, memStore)
	}
	if searcher != nil {
		cr.ctx = store.WithThreeTierSearcher(cr.ctx, searcher)
	}
}

// InjectMemorySaveOrMerge adds a cross-session memory merge function to the
// runner's context. When set, the memory_save tool will use this function
// instead of a raw insert, enabling deduplication across sessions via LLM merge.
// Must be called before Run().
func (cr *ChatRunner) InjectMemorySaveOrMerge(fn store.MemorySaveOrMergeFunc) {
	if fn != nil {
		cr.ctx = store.WithMemorySaveOrMerge(cr.ctx, fn)
	}
}

// InjectFlowStore adds a tenant-scoped flow store to the runner's context
// so that drill tools (save_drill, delete_drill, list_drills, read_drill,
// edit_drill) and the run_drill tool can read/write flows from the database
// rather than the local filesystem in platform mode. Must be called before Run().
func (cr *ChatRunner) InjectFlowStore(fs store.FlowStore) {
	cr.ctx = store.WithFlowStore(cr.ctx, fs)
}

// InjectSkillStores adds tenant-scoped skill stores to the runner's context
// so that the skill_lookup tool can resolve skills dynamically per-request
// from the org and team stores (in addition to bundled skills).
// Must be called before Run().
func (cr *ChatRunner) InjectSkillStores(org, team store.SkillStore) {
	cr.ctx = store.WithSkillStores(cr.ctx, &store.SkillStores{Org: org, Team: team})
}

// InjectSchedulerStore adds a tenant-scoped scheduler store to the runner's context
// so that the schedule_job and list_scheduled_jobs tools can operate on the
// correct team's jobs in platform mode. Must be called before Run().
func (cr *ChatRunner) InjectSchedulerStore(ss store.SchedulerStore) {
	cr.ctx = store.WithSchedulerStore(cr.ctx, ss)
}

// InjectDrillReportStore adds a tenant-scoped drill report store to the runner's
// context so that the run_drill tool can persist execution results to the database
// in platform mode. Must be called before Run().
func (cr *ChatRunner) InjectDrillReportStore(rs store.DrillReportStore) {
	cr.ctx = store.WithDrillReportStore(cr.ctx, rs)
}

// InjectMCPServerStores adds tenant-scoped MCP server stores to the runner's context
// so that the chat agent can resolve MCP server configurations from the database
// in platform mode. Must be called before Run().
//
// All three tiers cascade: platform-tier servers are inherited by every org/team,
// org-tier servers are inherited by every team within the org, and team-tier
// servers can override either parent. Pass nil for tiers that don't apply.
func (cr *ChatRunner) InjectMCPServerStores(platform, org, team store.MCPServerStore) {
	cr.ctx = store.WithMCPServerStores(cr.ctx, &store.MCPServerStores{
		Platform: platform,
		Org:      org,
		Team:     team,
	})
}

// InjectFleetStores adds tenant-scoped fleet template and plan stores to the
// runner's context so that fleet tools (save_fleet_plan, list_fleets) can
// read/write from the database in platform mode. Must be called before Run().
func (cr *ChatRunner) InjectFleetStores(templates store.FleetTemplateStore, plans store.FleetPlanStore) {
	if templates != nil {
		cr.ctx = store.WithFleetTemplateStore(cr.ctx, templates)
	}
	if plans != nil {
		cr.ctx = store.WithFleetPlanStore(cr.ctx, plans)
	}
}

// InjectSandboxTemplate adds the team's custom sandbox template name to the
// runner's context. NodeTool reads this at container creation time so that chat
// sessions use the team's pre-configured container image rather than @base.
// Must be called before Run().
func (cr *ChatRunner) InjectSandboxTemplate(tpl string) {
	cr.ctx = store.WithSandboxTemplate(cr.ctx, tpl)
}

// InjectSandboxLayerChain adds the pre-resolved overlay layer chain to the
// runner's context. On K8s, the chain is ordered oldest-first (e.g.,
// ["@base", "<sha256>"]) and used directly as SessionSpec.LayerChain.
// Must be called before Run(), after InjectSandboxTemplate.
func (cr *ChatRunner) InjectSandboxLayerChain(chain []string) {
	cr.ctx = store.WithSandboxLayerChain(cr.ctx, chain)
}

// InjectSandboxLayerChainIfEmpty sets the layer chain only if no chain
// has been injected yet (e.g., by a team-template resolution). Used to
// apply the @base configured layer without overriding team templates.
func (cr *ChatRunner) InjectSandboxLayerChainIfEmpty(chain []string) {
	if existing := store.SandboxLayerChainFromContext(cr.ctx); len(existing) > 0 {
		return // team template already set a chain
	}
	cr.ctx = store.WithSandboxLayerChain(cr.ctx, chain)
}

// InjectSessionService adds a tenant-scoped session store to the runner's context
// so that sub-agents (delegate_tasks) create child sessions in the correct store
// (e.g., pgstore PersonalSessions) rather than the factory-time default (FileStore).
// Must be called before Run().
func (cr *ChatRunner) InjectSessionService(ss store.SessionStore) {
	cr.ctx = store.WithSessionService(cr.ctx, ss)
}

// InjectUserID adds the effective user ID to the runner's context so that
// sub-agents (delegate_tasks) create child sessions with the correct user_id.
// In platform mode, the pgstore user_id column is UUID-typed, so this must be
// the platform user's UUID rather than the factory default ("console_user").
// Must be called before Run().
func (cr *ChatRunner) InjectUserID(id string) {
	cr.ctx = store.WithUserID(cr.ctx, id)
}

// InjectRedactor adds the session's Redactor to the runner's context so that
// tool functions (e.g., memory_save) can call Placeholderize() to replace raw
// credential values with {{CREDENTIAL:name:field}} tokens before persisting.
// Must be called before Run().
func (cr *ChatRunner) InjectRedactor(r *credentials.Redactor) {
	cr.ctx = credentials.WithRedactor(cr.ctx, r)
}

// InjectDisabledTools adds per-team tool restrictions to the runner's context.
// Tools in this list will be filtered from the LLM request and system prompt.
func (cr *ChatRunner) InjectDisabledTools(names []string) {
	cr.ctx = store.WithDisabledTools(cr.ctx, names)
}

// InjectTenantSlugs propagates org/team identity into the runner's context so
// that tools (e.g., list_team_members) can resolve team membership and user
// channels without importing pgstore directly.
func (cr *ChatRunner) InjectTenantSlugs(orgSlug, teamSlug string) {
	cr.ctx = store.WithOrgSlug(cr.ctx, orgSlug)
	cr.ctx = store.WithTeamSlug(cr.ctx, teamSlug)
}

// InjectRunJobFunc adds a scheduler test-execution function to the runner's
// context. This allows the schedule_job tool to execute a dry-run in platform
// mode without going through the unauthenticated HTTP bridge.
func (cr *ChatRunner) InjectRunJobFunc(fn store.RunJobFunc) {
	cr.ctx = store.WithRunJobFunc(cr.ctx, fn)
}

// Run executes the agent in the background. It creates the ADK runner,
// processes events, buffers them for subscribers, and handles completion.
// This method blocks until the agent finishes or the context is cancelled.
func (cr *ChatRunner) Run(
	chatAgent *agent.ChatAgent,
	sessionService session.Service,
	llm model.LLM,
	titleSetter SessionTitleSetter,
	userMsg *genai.Content,
	msg string,
	autoApprove bool,
	systemContext string,
) {
	defer func() {
		cr.doneMu.Lock()
		cr.done = true
		cr.doneMu.Unlock()

		// Send the done event immediately so the frontend can finalize the
		// response and hide the processing spinner. Then wait for the
		// title-generation goroutine (if any) before closing subscriber
		// channels — this ensures the session_title SSE event reaches the
		// browser before the stream ends.
		cr.emitEvent("done", map[string]any{"done": true})

		timeout := cr.titleWaitTimeout
		if timeout <= 0 {
			// No wait configured — close immediately (test default).
			cr.closeSubscribers()
		} else {
			go func() {
				select {
				case <-cr.titleDone:
					// Title goroutine finished — give a brief moment for
					// the SSE flush, then close.
				case <-time.After(timeout):
					// Title took too long — close anyway; the title is
					// still persisted to disk for future loadSessions().
				}
				cr.closeSubscribers()
			}()
		}
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

	// Inject per-turn session context via context overrides (thread-safe).
	// Run() clones the SystemPromptBuilder and applies these on the clone.
	if systemContext != "" {
		cr.ctx = agent.WithPromptOverrides(cr.ctx, &agent.PromptOverrides{
			SessionContext: agent.EscapeCurlyPlaceholders(systemContext),
		})
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
					cr.drainImagesAndFlowOutput(chatAgent, sessionService)
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
				cr.drainImagesAndFlowOutput(chatAgent, sessionService)
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

		// Emit memory_saved when a sub-agent saves a memory
		if evt.Type == "task_tool_result" && evt.ToolName == "memory_save" {
			if resultMap, ok := evt.ToolResult.(map[string]any); ok {
				if saved, ok := resultMap["saved"]; ok && saved == true {
					cr.emitEvent("memory_saved", map[string]any{
						"session_id": cr.SessionID,
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

	for event, runErr := range rnr.Run(cr.ctx, cr.UserID, cr.SessionID, userMsg, adkagent.RunConfig{
		StreamingMode: adkagent.StreamingModeSSE,
	}) {
		if cr.ctx.Err() != nil {
			break
		}

		if runErr != nil {
			lastRunErr = runErr
			cr.emitEvent("error", map[string]any{"error": runErr.Error()})
			persistRunError(cr.ctx, sessionService, cr.UserID, cr.SessionID, runErr)
			break
		}

		// Process state delta for tool approval, spinner, retry, errors
		if event.Actions.StateDelta != nil {
			cr.processStateDelta(event.Actions.StateDelta)
		}

		// Process content parts
		if event.LLMResponse.Content != nil {
			for _, part := range event.LLMResponse.Content.Parts {
				// Skip reasoning/thought parts — they are preserved in session
				// history for providers like DeepSeek but should not be displayed.
				if part.Text != "" && !part.Thought {
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
					cr.drainImagesAndFlowOutput(chatAgent, sessionService)

					// Emit memory_saved SSE event when memory_save tool succeeds
					if part.FunctionResponse.Name == "memory_save" && resp != nil {
						if saved, ok := resp["saved"]; ok && saved == true {
							cr.emitEvent("memory_saved", map[string]any{
								"session_id": cr.SessionID,
							})
						}
					}
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
		for event, runErr := range rnr.Run(cr.ctx, cr.UserID, cr.SessionID, nudgeMsg, adkagent.RunConfig{
			StreamingMode: adkagent.StreamingModeSSE,
		}) {
			if cr.ctx.Err() != nil {
				break
			}
			if runErr != nil {
				cr.emitEvent("error", map[string]any{"error": fmt.Sprintf("Retry also failed: %v", runErr)})
				persistRunError(cr.ctx, sessionService, cr.UserID, cr.SessionID, runErr)
				break
			}
			if event.Actions.StateDelta != nil {
				cr.processStateDelta(event.Actions.StateDelta)
			}
			if event.LLMResponse.Content != nil {
				for _, part := range event.LLMResponse.Content.Parts {
					if part.Text != "" && !part.Thought {
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
						cr.drainImagesAndFlowOutput(chatAgent, sessionService)
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
	// Runs asynchronously — the deferred done event fires immediately so the
	// UI isn't blocked. The defer block waits on cr.titleDone (up to
	// titleWaitTimeout) before closing subscribers, so the session_title
	// SSE event reaches the browser if the LLM responds in time.
	if cr.IsNew && msg != "" && titleSetter != nil {
		go func() {
			defer close(cr.titleDone)
			generateStudioSessionTitle(llm, titleSetter, cr.SessionID, msg, func(title string) {
				cr.emitEvent("session_title", map[string]any{"title": title})
			})
		}()
	} else {
		// No title generation needed — unblock the defer immediately.
		close(cr.titleDone)
	}

	// Post-processing: detect astonish-app code fences in the accumulated response
	// text and emit app_preview events + persist them.
	cr.detectAndEmitAppPreviews(chatAgent, sessionService)

	// Post-processing: detect astonish-report code fences in the accumulated
	// response text and emit report_marker events + persist them. The fence
	// is a SIGNAL only — the file content itself was already created via
	// write_file/edit_file (rule 1: every report uses write_file). The fence
	// flips an artifact's "is this a report?" gate so the frontend embeds it
	// inline as EmbeddedFileViewer instead of showing a small artifact card.
	cr.detectAndEmitReportMarkers(sessionService)
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
func (cr *ChatRunner) drainImagesAndFlowOutput(chatAgent *agent.ChatAgent, sessionService session.Service) {
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
		persistFlowOutput(cr.ctx, sessionService, cr.UserID, cr.SessionID, flowOut)
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
		persistAppPreview(cr.ctx, sessionService, cr.UserID, cr.SessionID, cleanCode, title, version, appID)

		// Update active app state for cross-turn refinement
		chatAgent.SetActiveApp(cr.SessionID, existingApp)
	}
}

// reportMarkerFenceRe matches ```astonish-report code fences. It captures the
// frontmatter body (a YAML-ish key:value block) between the opening and
// closing fences. Mirrors appPreviewFenceRe in shape so both fence types
// are detected by structurally identical post-processors.
var reportMarkerFenceRe = regexp.MustCompile("(?s)```astonish-report\\s*\\n(.*?)\\n```")

// reportMarkerInfo is the parsed content of a single astonish-report fence.
// It is intentionally minimal: the fence is a signal that an artifact should
// be rendered as a report, not a content carrier. Future fields (mermaid hint,
// summary, export formats) attach naturally here without changing the
// surrounding pipeline.
type reportMarkerInfo struct {
	Path  string // required, must match a same-turn artifact's path
	Title string // optional, displayed by EmbeddedFileViewer when present
}

// parseReportMarkerFrontmatter parses the body of a ```astonish-report fence.
// Recognised format (Shape B):
//
//	path: /tmp/q4-revenue.md
//	title: Q4 Revenue Analysis
//	# unknown keys are ignored without error so future extensions don't break parsing
//
// Returns ok=true only when a non-empty path is present. Empty body, missing
// path, or a malformed body all return ok=false; callers WARN-and-skip.
func parseReportMarkerFrontmatter(body string) (info reportMarkerInfo, ok bool) {
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colon := strings.Index(line, ":")
		if colon <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:colon])
		value := strings.TrimSpace(line[colon+1:])
		// Strip optional surrounding quotes a permissive YAML-ish parser would tolerate.
		value = strings.Trim(value, `"'`)
		switch key {
		case "path":
			info.Path = value
		case "title":
			info.Title = value
		}
	}
	if info.Path == "" {
		return reportMarkerInfo{}, false
	}
	return info, true
}

// stripReportMarkerFences removes every ```astonish-report fence from text,
// leaving the surrounding prose intact. Used so the persisted agent message
// transcript and the rendered chat bubble don't show the raw fence to the
// user — it is a signal, not display content. Mirrors the equivalent
// frontend stripping logic in StudioChat.tsx.
func stripReportMarkerFences(text string) string {
	return reportMarkerFenceRe.ReplaceAllString(text, "")
}

// recentTurnArtifactPaths walks the runner's event buffer and returns the
// set of file paths emitted as "artifact" events during the current turn.
// The set is used by detectAndEmitReportMarkers to validate that the path
// inside an astonish-report fence corresponds to a file the agent actually
// wrote. Mismatches are logged-and-skipped so a stray or stale path in the
// model's text cannot fabricate a report marker out of nothing.
func (cr *ChatRunner) recentTurnArtifactPaths() map[string]bool {
	paths := make(map[string]bool)
	cr.eventsMu.RLock()
	defer cr.eventsMu.RUnlock()
	for _, ev := range cr.events {
		if ev.Type != "artifact" {
			continue
		}
		if p, ok := ev.Data["path"].(string); ok && p != "" {
			paths[p] = true
		}
	}
	return paths
}

// detectAndEmitReportMarkers scans the buffered text events for one or more
// ```astonish-report fences. For each well-formed fence whose path matches
// an artifact emitted in this turn, it:
//
//  1. emits a "report_marker" SSE event {path, title} carrying the gate
//     signal the frontend uses to decide on EmbeddedFileViewer rendering;
//  2. persists a structured marker record so the gate state survives
//     server restarts and is rebuilt on session-detail GET;
//  3. dedups repeats — if the LLM re-emits the same path, only one event
//     is sent.
//
// Malformed fences (missing path, unparseable frontmatter) and fences whose
// path doesn't correspond to a same-turn artifact are skipped with a WARN
// log line. The downstream effect is that the artifact remains a plain
// ArtifactCard rather than promoting to a report viewer — a graceful
// degradation that preserves correctness when the LLM mis-signals.
func (cr *ChatRunner) detectAndEmitReportMarkers(sessionService session.Service) {
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

	matches := reportMarkerFenceRe.FindAllStringSubmatch(fullText.String(), -1)
	if len(matches) == 0 {
		return
	}

	turnArtifacts := cr.recentTurnArtifactPaths()
	emitted := make(map[string]bool)

	for _, match := range matches {
		body := match[1]
		info, ok := parseReportMarkerFrontmatter(body)
		if !ok {
			slog.Warn("astonish-report fence missing required path field; ignoring",
				"component", "chat_runner",
				"session", cr.SessionID,
				"body", strings.TrimSpace(body))
			continue
		}
		if !turnArtifacts[info.Path] {
			slog.Warn("astonish-report fence references a path with no matching write_file/edit_file artifact in this turn; ignoring",
				"component", "chat_runner",
				"session", cr.SessionID,
				"path", info.Path)
			continue
		}
		if emitted[info.Path] {
			continue // dedup repeated fences for the same path
		}
		emitted[info.Path] = true

		cr.emitEvent("report_marker", map[string]any{
			"path":  info.Path,
			"title": info.Title,
		})
		persistReportMarker(cr.ctx, sessionService, cr.UserID, cr.SessionID, info.Path, info.Title)
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
