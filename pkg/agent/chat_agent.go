package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/schardosin/astonish/pkg/credentials"
	"github.com/schardosin/astonish/pkg/memory"
	"github.com/schardosin/astonish/pkg/provider/llmerror"
	persistentsession "github.com/schardosin/astonish/pkg/session"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
	"gopkg.in/yaml.v3"
)

// KnowledgeSearchResult holds a single result from the knowledge vector search.
type KnowledgeSearchResult struct {
	Path    string
	Score   float64
	Snippet string
}

// KnowledgeSearchFunc performs a vector search and returns matching results.
// Used to auto-retrieve relevant knowledge before LLM execution.
type KnowledgeSearchFunc func(ctx context.Context, query string, maxResults int, minScore float64) ([]KnowledgeSearchResult, error)

// ChatAgent implements a dynamic chat agent without flow definitions.
// It wraps ADK's llmagent in a persistent chat session where the LLM
// decides which tools to call and how to proceed.
//
// Execution records a trace. After reusable tasks, auto-distillation
// generates a flow YAML + knowledge doc. /distill remains as manual fallback.
type ChatAgent struct {
	LLM            model.LLM
	Tools          []tool.Tool
	Toolsets       []tool.Toolset
	SessionService session.Service
	SystemPrompt   *SystemPromptBuilder
	DebugMode      bool
	AutoApprove    bool
	MaxToolCalls   int // Max consecutive tool calls per turn (default: 25)

	// Flow distillation
	FlowSaveDir   string         // Directory for saved flows (default: agents dir)
	FlowRegistry  *FlowRegistry  // Registry for saved flows
	FlowDistiller *FlowDistiller // Distiller for trace-to-YAML conversion

	// Memory and flow reuse
	MemoryManager      *memory.Manager     // Persistent memory manager
	FlowContextBuilder *FlowContextBuilder // Converts flow YAML to execution plan
	FlowSearcher       FlowMemorySearcher  // Vector-based flow matching (nil = LLM-only)
	KnowledgeSearch    KnowledgeSearchFunc // Auto-retrieve relevant knowledge per turn (nil = disabled)

	// Self-management callbacks
	SelfMDRefresher  func() // Called after config changes to regenerate SELF.md
	FlowKnowledgeDir string // Path to memory/flows/ for knowledge docs

	// Credential redaction
	Redactor *credentials.Redactor // Redacts credential values from tool outputs (nil = disabled)

	// Context compaction
	Compactor *persistentsession.Compactor // Manages context window compaction (nil = disabled)

	// Internal: reuse AstonishAgent for approval formatting
	approvalHelper *AstonishAgent

	// Internal: per-session execution traces for on-demand /distill
	traceHistory   map[string][]*ExecutionTrace // keyed by session ID
	pendingDistill map[string]*distillPreview   // keyed by session ID
	traceMu        sync.Mutex                   // protects traceHistory and pendingDistill

	// Image side-channel: images stripped from tool results before they
	// enter session history, available for channels to deliver to users.
	pendingImages []ImageFromTool
	imageMu       sync.Mutex
}

// ImageFromTool holds image data extracted from a tool result before the
// result is persisted to session history. This prevents large base64 blobs
// from polluting the session transcript and being replayed to the LLM.
type ImageFromTool struct {
	Data   []byte // raw image bytes
	Format string // "png" or "jpeg"
}

// distillPreview holds the result of PreviewDistill for use by ConfirmAndDistill.
type distillPreview struct {
	Description string            // LLM-generated task description
	Traces      []*ExecutionTrace // selected traces to distill
}

// DistillSession identifies a session for distillation, providing the
// information needed to look up persisted session events for trace
// reconstruction across daemon restarts.
type DistillSession struct {
	SessionID string // persistent session key (e.g. "telegram:direct:12345")
	AppName   string // ADK app name (always "astonish")
	UserID    string // ADK user ID for session lookup
}

// NewChatAgent creates a ChatAgent with all configured tools and toolsets.
func NewChatAgent(llm model.LLM, internalTools []tool.Tool, toolsets []tool.Toolset,
	sessionService session.Service, promptBuilder *SystemPromptBuilder,
	debugMode bool, autoApprove bool) *ChatAgent {

	maxToolCalls := 100

	return &ChatAgent{
		LLM:            llm,
		Tools:          internalTools,
		Toolsets:       toolsets,
		SessionService: sessionService,
		SystemPrompt:   promptBuilder,
		DebugMode:      debugMode,
		AutoApprove:    autoApprove,
		MaxToolCalls:   maxToolCalls,
		approvalHelper: &AstonishAgent{LLM: llm, AutoApprove: autoApprove},
		traceHistory:   make(map[string][]*ExecutionTrace),
		pendingDistill: make(map[string]*distillPreview),
	}
}

// DrainImages returns and clears all pending images that were extracted from
// tool results during the current agent run. Thread-safe. The channel manager
// calls this to retrieve images for delivery without relying on session events.
func (c *ChatAgent) DrainImages() []ImageFromTool {
	c.imageMu.Lock()
	defer c.imageMu.Unlock()
	imgs := c.pendingImages
	c.pendingImages = nil
	return imgs
}

// extractAndStripImages checks a tool result map for an "image_base64" key.
// If found, the base64 data is decoded and stashed in the pending images queue
// for channel delivery, and the key is replaced with a short placeholder so the
// LLM knows a screenshot was taken without the full binary data polluting the
// session history or being replayed on subsequent LLM calls.
func (c *ChatAgent) extractAndStripImages(output map[string]any) map[string]any {
	if output == nil {
		return output
	}

	b64, ok := output["image_base64"].(string)
	if !ok || b64 == "" {
		return output
	}

	// Decode and stash the image for channel delivery
	data, err := base64.StdEncoding.DecodeString(b64)
	if err == nil && len(data) > 0 {
		format := "png"
		if f, ok := output["format"].(string); ok && f != "" {
			format = f
		}
		c.imageMu.Lock()
		c.pendingImages = append(c.pendingImages, ImageFromTool{
			Data:   data,
			Format: format,
		})
		c.imageMu.Unlock()
	}

	// Replace the base64 blob with a lightweight placeholder.
	// Copy the map to avoid mutating the original.
	stripped := make(map[string]any, len(output))
	for k, v := range output {
		stripped[k] = v
	}
	stripped["image_base64"] = fmt.Sprintf("[screenshot captured, %d bytes]", len(b64))
	return stripped
}

// retryBackoff returns the duration to wait before retrying after a transient
// LLM error. It respects the Retry-After header if present, otherwise uses
// exponential backoff: 2s, 5s, 15s.
func retryBackoff(attempt int, err error) time.Duration {
	// Respect provider's Retry-After if available
	if ra := llmerror.GetRetryAfter(err); ra > 0 {
		// Cap at 60s to avoid absurd waits
		if ra > 60*time.Second {
			ra = 60 * time.Second
		}
		return ra
	}

	// Exponential backoff: 2s, 5s, 15s
	backoffs := []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second}
	if attempt < len(backoffs) {
		return backoffs[attempt]
	}
	return backoffs[len(backoffs)-1]
}

// redactEventText applies credential redaction to any LLM text parts in an event.
// This prevents the LLM from leaking secrets (e.g., from resolve_credential) in
// its text responses to the user. Tool call arguments are NOT affected.
func redactEventText(r *credentials.Redactor, event *session.Event) {
	if r == nil || event == nil {
		return
	}
	if event.LLMResponse.Content != nil {
		for i, part := range event.LLMResponse.Content.Parts {
			if part.Text != "" {
				event.LLMResponse.Content.Parts[i].Text = r.Redact(part.Text)
			}
		}
	}
}

// Run implements the agent.Run interface for ADK.
// It is called by the ADK runner for each user message.
func (c *ChatAgent) Run(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		// Wrap yield to redact credential values from LLM text responses.
		// The LLM may have received raw secrets via resolve_credential and
		// could accidentally echo them. This ensures secrets never reach the user.
		origYield := yield
		yield = func(event *session.Event, err error) bool {
			redactEventText(c.Redactor, event)
			return origYield(event, err)
		}

		// Extract user text
		userText := ""
		if ctx.UserContent() != nil {
			for _, p := range ctx.UserContent().Parts {
				if p.Text != "" {
					userText += p.Text
				}
			}
		}

		if c.DebugMode {
			fmt.Printf("[Chat DEBUG] User message: %s\n", userText)
		}

		// --- Phase A: Dynamic Execution ---
		trace := NewExecutionTrace(userText)

		// Load memory and inject into system prompt
		c.SystemPrompt.MemoryContent = ""
		c.SystemPrompt.ExecutionPlan = ""
		c.SystemPrompt.RelevantKnowledge = ""

		var matchedFlowName string

		if c.MemoryManager != nil {
			memContent, memErr := c.MemoryManager.Load()
			if memErr != nil {
				if c.DebugMode {
					fmt.Printf("[Chat DEBUG] Failed to load memory: %v\n", memErr)
				}
			} else if memContent != "" {
				c.SystemPrompt.MemoryContent = memContent
				if c.DebugMode {
					fmt.Printf("[Chat DEBUG] Loaded memory (%d bytes)\n", len(memContent))
				}
			}

			// Check for matching saved flow
			if c.FlowRegistry != nil && c.FlowContextBuilder != nil {
				flowFile, plan := c.matchAndBuildPlan(userText, memContent)
				if plan != "" {
					matchedFlowName = flowFile
					c.SystemPrompt.ExecutionPlan = plan
					// Emit conversational info about the flow match
					flowDisplayName := strings.TrimSuffix(flowFile, ".yaml")
					flowDisplayName = strings.ReplaceAll(flowDisplayName, "_", " ")
					yield(&session.Event{
						LLMResponse: model.LLMResponse{
							Content: &genai.Content{
								Parts: []*genai.Part{{Text: fmt.Sprintf("I found a saved workflow for this (%s), let me use it.\n", flowDisplayName)}},
								Role:  "model",
							},
						},
					}, nil)
				}
			}
		}

		// Auto-retrieve relevant knowledge from vector store
		if c.KnowledgeSearch != nil && userText != "" {
			searchQuery := buildKnowledgeQuery(userText, matchedFlowName)
			if len(searchQuery) < 5 {
				if c.DebugMode {
					fmt.Printf("[Chat DEBUG] Auto knowledge search: skipped (processed query too short: %q)\n", searchQuery)
				}
			} else {
				results, searchErr := c.KnowledgeSearch(context.Background(), searchQuery, 5, 0.3)
				if searchErr != nil {
					if c.DebugMode {
						fmt.Printf("[Chat DEBUG] Auto knowledge search failed: %v\n", searchErr)
					}
				} else if len(results) > 0 {
					var kb strings.Builder
					for _, r := range results {
						kb.WriteString(fmt.Sprintf("**%s** (relevance: %.0f%%)\n", r.Path, r.Score*100))
						kb.WriteString(r.Snippet)
						kb.WriteString("\n\n")
					}
					c.SystemPrompt.RelevantKnowledge = escapeCurlyPlaceholders(kb.String())
					if c.DebugMode {
						fmt.Printf("[Chat DEBUG] Auto knowledge search: %d results injected for query: %s\n", len(results), truncateQuery(searchQuery, 60))
					}
				} else if c.DebugMode {
					fmt.Printf("[Chat DEBUG] Auto knowledge search: no results for query: %s\n", truncateQuery(searchQuery, 60))
				}
			}
		} else if c.KnowledgeSearch == nil && c.DebugMode {
			fmt.Println("[Chat DEBUG] Auto knowledge search: disabled (KnowledgeSearch not wired)")
		}

		// Build system prompt (includes memory and execution plan if set)
		instruction := c.SystemPrompt.Build()

		// Create the AfterToolCallback for trace recording
		afterToolCallback := func(ctx tool.Context, t tool.Tool, input, output map[string]any, err error) (map[string]any, error) {
			// Redact credential values from tool output before the LLM sees them.
			// Exception: resolve_credential must return raw values so the LLM can
			// use them programmatically (e.g., pipe a password to sshpass via process_write).
			// Secrets in the LLM's text responses are still caught by the session
			// transcript redactor and channel output redactor.
			redactedOutput := output
			if c.Redactor != nil && output != nil && t.Name() != "resolve_credential" {
				redactedOutput = c.Redactor.RedactMap(output)
			}

			// Strip image_base64 from tool results to prevent large binary
			// blobs from entering session history. The raw image bytes are
			// stashed in the ChatAgent's image queue for channel delivery.
			redactedOutput = c.extractAndStripImages(redactedOutput)

			trace.RecordStep(t.Name(), input, redactedOutput, err)
			if c.DebugMode {
				status := "OK"
				if err != nil {
					status = fmt.Sprintf("ERROR: %v", err)
				}
				fmt.Printf("[Chat DEBUG] Tool call recorded: %s -> %s\n", t.Name(), status)
			}
			return redactedOutput, err
		}

		// Build BeforeModelCallbacks
		var beforeModelCallbacks []llmagent.BeforeModelCallback
		if c.Compactor != nil {
			beforeModelCallbacks = append(beforeModelCallbacks, c.Compactor.BeforeModelCallback())
		}

		// Create llmagent with all tools
		llmAgent, err := llmagent.New(llmagent.Config{
			Name:                 "chat",
			Model:                c.LLM,
			Instruction:          instruction,
			Tools:                c.Tools,
			Toolsets:             c.Toolsets,
			BeforeModelCallbacks: beforeModelCallbacks,
			AfterToolCallbacks: []llmagent.AfterToolCallback{
				afterToolCallback,
			},
		})
		if err != nil {
			yield(nil, fmt.Errorf("failed to create chat llmagent: %w", err))
			return
		}

		// Run the llmagent with retry for transient errors (429, 502, 503, etc.)
		const maxRetries = 3
		toolCallCount := 0
		maxToolCalls := c.MaxToolCalls
		lastToolCallSeen := false
		anyTextYielded := false

		for attempt := range maxRetries {
			retried := false
			for event, err := range llmAgent.Run(ctx) {
				if err != nil {
					// Check for retryable errors (rate limit, server overload)
					if llmerror.IsRetryable(err) && attempt < maxRetries-1 {
						wait := retryBackoff(attempt, err)
						if c.DebugMode {
							fmt.Printf("[Chat DEBUG] Retryable error (attempt %d/%d): %v, waiting %v\n",
								attempt+1, maxRetries, err, wait)
						}
						select {
						case <-time.After(wait):
						case <-ctx.Done():
							yield(nil, ctx.Err())
							return
						}
						retried = true
						break // break inner for-range, continue outer retry loop
					}

					// Non-retryable error, or retries exhausted
					if c.DebugMode && attempt > 0 {
						fmt.Printf("[Chat DEBUG] Error after %d retries: %v\n", attempt, err)
					}

					// If we were using an execution plan and it failed, inform the user
					// conversationally. The orphan cleanup in the provider layer ensures
					// the next turn's history is valid.
					if c.SystemPrompt.ExecutionPlan != "" {
						c.SystemPrompt.ExecutionPlan = ""
						if c.DebugMode {
							fmt.Printf("[Chat DEBUG] Flow execution failed: %v, cleared execution plan\n", err)
						}
						yield(&session.Event{
							LLMResponse: model.LLMResponse{
								Content: &genai.Content{
									Parts: []*genai.Part{{Text: "The saved workflow ran into an issue, so I'll try a different approach. Could you repeat your request?"}},
									Role:  "model",
								},
							},
						}, nil)
						return
					}
					yield(nil, err)
					return
				}

				// Count tool calls and capture text output
				if event.LLMResponse.Content != nil {
					for _, p := range event.LLMResponse.Content.Parts {
						if p.FunctionCall != nil {
							toolCallCount++
							lastToolCallSeen = true
							if toolCallCount >= maxToolCalls {
								yield(&session.Event{
									LLMResponse: model.LLMResponse{
										Content: &genai.Content{
											Parts: []*genai.Part{{Text: fmt.Sprintf("\n[Max tool calls reached (%d). Stopping.]", maxToolCalls)}},
											Role:  "model",
										},
									},
								}, nil)
								goto postLoop
							}
						}
						// Track whether any user-facing text was produced
						if p.Text != "" && !p.Thought && p.FunctionCall == nil && p.FunctionResponse == nil {
							anyTextYielded = true
						}
						// Capture text that comes after tool calls (the final formatted output)
						if p.Text != "" && lastToolCallSeen {
							trace.AppendOutput(p.Text)
						}
					}
				}

				// Check for approval pause
				if event.Actions.StateDelta != nil {
					if awaitingVal, ok := event.Actions.StateDelta["awaiting_approval"]; ok {
						if awaiting, ok := awaitingVal.(bool); ok && awaiting {
							// Yield the approval event and return -- the runner will
							// call us again with the user's response
							yield(event, nil)
							return
						}
					}
				}

				// Yield event to the caller (console/web)
				if !yield(event, nil) {
					return
				}
			} // end inner for-range over llmAgent.Run(ctx)

			// If we didn't retry, the run completed successfully — break out
			if !retried {
				break
			}
			// Otherwise, the retry loop continues with the next attempt
		} // end outer retry loop

		// If the LLM made tool calls but never produced user-facing text,
		// yield a synthetic message so the consumer doesn't see silence.
		// This commonly happens after context compaction degrades the
		// conversation history, causing the LLM to call tools but skip
		// the final summary.
		if lastToolCallSeen && !anyTextYielded {
			yield(&session.Event{
				LLMResponse: model.LLMResponse{
					Content: &genai.Content{
						Parts: []*genai.Part{{Text: "I completed the requested actions. Let me know if you'd like me to elaborate on the results or if there's anything else I can help with."}},
						Role:  "model",
					},
				},
			}, nil)
		}

	postLoop:
		// Finalize the trace
		trace.Finalize()

		if c.DebugMode {
			fmt.Printf("[Chat DEBUG] postLoop reached. Trace tool call count: %d\n",
				trace.ToolCallCount())
			for i, step := range trace.Steps {
				fmt.Printf("[Chat DEBUG]   Step %d: %s (success: %v)\n", i+1, step.ToolName, step.Success)
			}
		}

		// Store the trace keyed by session ID for on-demand /distill
		sessionID := ctx.Session().ID()
		c.traceMu.Lock()
		c.traceHistory[sessionID] = append(c.traceHistory[sessionID], trace)
		// Prune: keep at most 20 traces per session
		if len(c.traceHistory[sessionID]) > 20 {
			c.traceHistory[sessionID] = c.traceHistory[sessionID][len(c.traceHistory[sessionID])-20:]
		}
		c.traceMu.Unlock()
	}
}

// reconstructTraces rebuilds execution traces from persisted session events.
// This allows /distill to work across daemon restarts — the session transcript
// on disk contains all the tool call/response events we need.
//
// Strategy: walk events chronologically. Each user message starts a new trace.
// FunctionCall events provide tool name + args. FunctionResponse events provide
// results. Final text after tools becomes the trace output.
func (c *ChatAgent) reconstructTraces(ctx context.Context, ds DistillSession) []*ExecutionTrace {
	resp, err := c.SessionService.Get(ctx, &session.GetRequest{
		AppName:   ds.AppName,
		UserID:    ds.UserID,
		SessionID: ds.SessionID,
	})
	if err != nil || resp.Session == nil {
		if c.DebugMode {
			fmt.Printf("[Chat DEBUG] reconstructTraces: failed to load session %s: %v\n", ds.SessionID, err)
		}
		return nil
	}

	events := resp.Session.Events()
	if events.Len() == 0 {
		return nil
	}

	var traces []*ExecutionTrace
	var current *ExecutionTrace
	// Map pending function calls by name so we can match them with responses.
	// Using name rather than ID because some providers don't set FunctionCall.ID.
	pendingCalls := make(map[string]map[string]any) // tool name -> args

	for i := range events.Len() {
		event := events.At(i)
		if event.LLMResponse.Content == nil {
			continue
		}

		// User message starts a new trace
		if event.Author == "user" {
			// Finalize previous trace if it exists
			if current != nil {
				current.Finalize()
				traces = append(traces, current)
			}
			// Extract user text
			var userText string
			for _, p := range event.LLMResponse.Content.Parts {
				if p.Text != "" {
					userText += p.Text
				}
			}
			current = &ExecutionTrace{
				UserRequest: userText,
				StartedAt:   event.Timestamp,
			}
			pendingCalls = make(map[string]map[string]any)
			continue
		}

		// Agent events — only process if we have an active trace
		if current == nil {
			continue
		}

		for _, part := range event.LLMResponse.Content.Parts {
			if part.FunctionCall != nil {
				// Record the call args for later matching with the response
				pendingCalls[part.FunctionCall.Name] = part.FunctionCall.Args
			}

			if part.FunctionResponse != nil {
				toolName := part.FunctionResponse.Name
				toolArgs := pendingCalls[toolName]
				delete(pendingCalls, toolName)

				// Determine success from the response map
				success := true
				var errMsg string
				if part.FunctionResponse.Response != nil {
					if e, ok := part.FunctionResponse.Response["error"]; ok {
						if es, ok := e.(string); ok && es != "" {
							success = false
							errMsg = es
						}
					}
				}

				step := TraceStep{
					ToolName:  toolName,
					ToolArgs:  toolArgs,
					Success:   success,
					Timestamp: event.Timestamp,
				}
				if part.FunctionResponse.Response != nil {
					step.ToolResult = part.FunctionResponse.Response
				}
				if errMsg != "" {
					step.Error = errMsg
				}
				current.Steps = append(current.Steps, step)
			}

			// Text after tool calls is the final output
			if part.Text != "" && !part.Thought && part.FunctionCall == nil && part.FunctionResponse == nil {
				if len(current.Steps) > 0 {
					current.FinalOutput += part.Text
				}
			}
		}
	}

	// Finalize the last trace
	if current != nil {
		current.Finalize()
		traces = append(traces, current)
	}

	if c.DebugMode {
		fmt.Printf("[Chat DEBUG] reconstructTraces: rebuilt %d traces from session %s\n", len(traces), ds.SessionID)
		for i, t := range traces {
			fmt.Printf("[Chat DEBUG]   Trace %d: %q (%d tool calls)\n", i+1, t.UserRequest, t.ToolCallCount())
		}
	}

	return traces
}

// PreviewDistill analyzes the conversation trace history for the given session
// and identifies the primary task to distill. Returns a description for user
// confirmation. The result is cached internally for use by ConfirmAndDistill.
func (c *ChatAgent) PreviewDistill(ctx context.Context, ds DistillSession) (string, error) {
	sessionID := ds.SessionID

	c.traceMu.Lock()
	sessionTraces := c.traceHistory[sessionID]
	c.traceMu.Unlock()

	// If no in-memory traces, reconstruct from persisted session events.
	// This handles daemon restarts — traces are ephemeral but session
	// events survive on disk.
	if len(sessionTraces) == 0 && c.SessionService != nil && ds.AppName != "" && ds.UserID != "" {
		reconstructed := c.reconstructTraces(ctx, ds)
		if len(reconstructed) > 0 {
			c.traceMu.Lock()
			c.traceHistory[sessionID] = reconstructed
			sessionTraces = reconstructed
			c.traceMu.Unlock()
		}
	}

	// Filter traces that have tool calls (conversational turns are not distillable)
	var substantive []*ExecutionTrace
	for _, t := range sessionTraces {
		if t.ToolCallCount() > 0 {
			substantive = append(substantive, t)
		}
	}

	if len(substantive) == 0 {
		return "", fmt.Errorf("no tasks with tool calls found in this session — nothing to distill")
	}

	// If only one substantive trace, skip the LLM assessment
	if len(substantive) == 1 {
		desc := fmt.Sprintf("%s (%d tool calls)", substantive[0].UserRequest, substantive[0].ToolCallCount())
		c.traceMu.Lock()
		c.pendingDistill[sessionID] = &distillPreview{
			Description: desc,
			Traces:      substantive,
		}
		c.traceMu.Unlock()
		return desc, nil
	}

	// Multiple traces — ask the LLM to identify the primary task
	if c.FlowDistiller == nil {
		// No LLM available for assessment, fall back to most recent substantive trace
		last := substantive[len(substantive)-1]
		desc := fmt.Sprintf("%s (%d tool calls)", last.UserRequest, last.ToolCallCount())
		c.traceMu.Lock()
		c.pendingDistill[sessionID] = &distillPreview{
			Description: desc,
			Traces:      []*ExecutionTrace{last},
		}
		c.traceMu.Unlock()
		return desc, nil
	}

	// Build assessment prompt
	var sb strings.Builder
	sb.WriteString("Analyze these conversation traces and identify the primary TASK worth saving as a reusable workflow.\n\n")

	for i, t := range sessionTraces {
		sb.WriteString(fmt.Sprintf("Trace %d: %s\n", i+1, t.Summary()))
	}

	sb.WriteString("\nRules:\n")
	sb.WriteString("- Select only traces that form a single coherent task with tool calls\n")
	sb.WriteString("- Multiple traces may form ONE task (e.g., first attempt fails, user provides credentials, second attempt succeeds)\n")
	sb.WriteString("- Ignore conversational turns, troubleshooting tangents, and Q&A about previous results\n")
	sb.WriteString("- If multiple distinct tasks exist, pick the most substantial one (most tool calls)\n\n")

	sb.WriteString("Respond with EXACTLY two lines:\n")
	sb.WriteString("traces: <comma-separated trace numbers>\n")
	sb.WriteString("description: <one-line description of the task>\n")

	response, err := c.FlowDistiller.LLM(ctx, sb.String())
	if err != nil {
		// Fall back to most recent substantive trace
		last := substantive[len(substantive)-1]
		desc := fmt.Sprintf("%s (%d tool calls)", last.UserRequest, last.ToolCallCount())
		c.traceMu.Lock()
		c.pendingDistill[sessionID] = &distillPreview{
			Description: desc,
			Traces:      []*ExecutionTrace{last},
		}
		c.traceMu.Unlock()
		return desc, nil
	}

	// Parse response
	var selectedIndices []int
	var description string
	for _, line := range strings.Split(response, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "traces:") {
			parts := strings.Split(strings.TrimSpace(strings.TrimPrefix(line, "traces:")), ",")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				var idx int
				if _, err := fmt.Sscanf(p, "%d", &idx); err == nil && idx >= 1 && idx <= len(sessionTraces) {
					selectedIndices = append(selectedIndices, idx-1) // convert to 0-based
				}
			}
		} else if strings.HasPrefix(line, "description:") {
			description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		}
	}

	// Build the selected traces list
	var selected []*ExecutionTrace
	if len(selectedIndices) > 0 {
		for _, idx := range selectedIndices {
			selected = append(selected, sessionTraces[idx])
		}
	} else {
		// LLM didn't return valid indices, fall back to all substantive traces
		selected = substantive
	}

	if description == "" {
		// Build description from selected traces
		var reqs []string
		for _, t := range selected {
			reqs = append(reqs, t.UserRequest)
		}
		description = strings.Join(reqs, " → ")
	}

	c.traceMu.Lock()
	c.pendingDistill[sessionID] = &distillPreview{
		Description: description,
		Traces:      selected,
	}
	c.traceMu.Unlock()
	return description, nil
}

// ConfirmAndDistill runs flow distillation using the traces identified by
// a prior call to PreviewDistill. The print function receives status/result text.
func (c *ChatAgent) ConfirmAndDistill(ctx context.Context, ds DistillSession, print func(string)) error {
	sessionID := ds.SessionID

	c.traceMu.Lock()
	preview := c.pendingDistill[sessionID]
	delete(c.pendingDistill, sessionID) // clear regardless of outcome
	c.traceMu.Unlock()

	if preview == nil || len(preview.Traces) == 0 {
		return fmt.Errorf("no pending distill preview — call PreviewDistill first")
	}

	if c.FlowDistiller == nil {
		return fmt.Errorf("flow distillation is not configured")
	}

	// Merge selected traces into one combined trace for the distiller
	merged := c.mergeTraces(preview.Traces)

	// Flatten sub-agent traces: replace delegate_tasks steps with children's
	// actual tool calls so the distilled flow has no sub-agent concepts
	flattenTraces(merged)

	print("Distilling execution into a reusable flow...\n")

	// Run distillation
	result, err := c.FlowDistiller.Distill(ctx, DistillRequest{
		UserRequest: merged.UserRequest,
		Trace:       merged,
	})
	if err != nil {
		print(fmt.Sprintf("Flow distillation failed: %v\n", err))
		if result == nil {
			return err
		}
	}

	// Determine save directory
	saveDir := c.FlowSaveDir
	if saveDir == "" {
		configDir, cfgErr := os.UserConfigDir()
		if cfgErr != nil {
			return fmt.Errorf("failed to determine config directory: %w", cfgErr)
		}
		saveDir = filepath.Join(configDir, "astonish", "flows")
	}

	// Create the directory if it doesn't exist
	if mkErr := os.MkdirAll(saveDir, 0755); mkErr != nil {
		return fmt.Errorf("failed to create flow directory: %w", mkErr)
	}

	// Save the YAML file
	filename := result.FlowName + ".yaml"
	flowPath := filepath.Join(saveDir, filename)

	// Avoid overwriting existing files
	if _, statErr := os.Stat(flowPath); statErr == nil {
		filename = fmt.Sprintf("%s_%s.yaml", result.FlowName, time.Now().Format("20060102_150405"))
		flowPath = filepath.Join(saveDir, filename)
	}

	if writeErr := os.WriteFile(flowPath, []byte(result.YAML), 0644); writeErr != nil {
		return fmt.Errorf("failed to write flow file: %w", writeErr)
	}

	// Register in the flow registry
	if c.FlowRegistry != nil {
		entry := FlowRegistryEntry{
			FlowFile:    filename,
			Description: result.Description,
			Tags:        result.Tags,
			CreatedAt:   time.Now(),
		}
		if regErr := c.FlowRegistry.Register(entry); regErr != nil {
			if c.DebugMode {
				fmt.Printf("[Chat DEBUG] Failed to register flow: %v\n", regErr)
			}
		}
	}

	// Build success message
	msg := fmt.Sprintf("\nFlow saved as `%s`\n", flowPath)
	msg += fmt.Sprintf("  Description: %s\n", result.Description)
	if len(result.Tags) > 0 {
		msg += fmt.Sprintf("  Tags: %s\n", strings.Join(result.Tags, ", "))
	}

	// Build run command with parameter suggestions
	runCmd := "astonish flows run " + result.FlowName
	paramFlags := c.extractInputParams(ctx, result.YAML, merged)
	for _, pf := range paramFlags {
		parts := strings.SplitN(pf, "=", 2)
		if len(parts) == 2 && strings.ContainsAny(parts[1], " \t") {
			runCmd += fmt.Sprintf(` -p %s="%s"`, parts[0], parts[1])
		} else {
			runCmd += " -p " + pf
		}
	}
	runCmd += " --auto-approve"
	msg += "\nYou can run this flow with:\n  " + runCmd + "\n"

	print(msg)
	return nil
}

// mergeTraces combines multiple execution traces into a single trace.
// The user request is joined, and all steps are concatenated in order.
func (c *ChatAgent) mergeTraces(traces []*ExecutionTrace) *ExecutionTrace {
	if len(traces) == 1 {
		return traces[0]
	}

	var requests []string
	var allSteps []TraceStep
	var finalOutput string

	for _, t := range traces {
		requests = append(requests, t.UserRequest)
		allSteps = append(allSteps, t.Steps...)
		if t.FinalOutput != "" {
			finalOutput = t.FinalOutput // use the last non-empty output
		}
	}

	return &ExecutionTrace{
		UserRequest: strings.Join(requests, " → "),
		Steps:       allSteps,
		FinalOutput: finalOutput,
		StartedAt:   traces[0].StartedAt,
		EndedAt:     traces[len(traces)-1].EndedAt,
	}
}

// flowYAML is a minimal struct for parsing the distilled YAML to extract input nodes.
type flowYAML struct {
	Nodes []flowNode `yaml:"nodes"`
}

type flowNode struct {
	Name        string            `yaml:"name"`
	Type        string            `yaml:"type"`
	Prompt      string            `yaml:"prompt,omitempty"`
	OutputModel map[string]string `yaml:"output_model,omitempty"`
}

// extractInputParams parses the distilled YAML to find input node names,
// then asks the LLM to fill in the actual values from the execution trace.
// Returns a slice of "nodeName=value" strings suitable for -p flags.
func (c *ChatAgent) extractInputParams(ctx context.Context, yamlStr string, trace *ExecutionTrace) []string {
	if trace == nil || yamlStr == "" || c.FlowDistiller == nil {
		return nil
	}

	// Parse YAML to find input node names and their prompts
	var flow flowYAML
	if err := yaml.Unmarshal([]byte(yamlStr), &flow); err != nil {
		if c.DebugMode {
			fmt.Printf("[Chat DEBUG] Failed to parse YAML for param extraction: %v\n", err)
		}
		return nil
	}

	type inputNode struct {
		name        string
		prompt      string
		outputModel map[string]string
	}
	var inputs []inputNode
	for _, node := range flow.Nodes {
		if node.Type == "input" {
			inputs = append(inputs, inputNode{name: node.Name, prompt: node.Prompt, outputModel: node.OutputModel})
		}
	}
	if len(inputs) == 0 {
		return nil
	}

	// Build a prompt for the LLM to fill in the parameter values
	var sb strings.Builder
	sb.WriteString("Given this execution trace, determine what SHORT value the user would type for each input node.\n\n")

	sb.WriteString("# Execution Trace\n")
	sb.WriteString("User request: " + trace.UserRequest + "\n\n")
	for i, step := range trace.SuccessfulSteps() {
		sb.WriteString(fmt.Sprintf("Step %d: tool=%s\n", i+1, step.ToolName))
		for k, v := range step.ToolArgs {
			sb.WriteString(fmt.Sprintf("  arg %s = %v\n", k, v))
		}
	}

	sb.WriteString("\n# Input Parameters to Fill\n")
	for _, inp := range inputs {
		sb.WriteString(fmt.Sprintf("- %s (prompt: %q)", inp.name, inp.prompt))
		if len(inp.outputModel) > 0 {
			var fields []string
			for k := range inp.outputModel {
				fields = append(fields, k)
			}
			sb.WriteString(fmt.Sprintf(" [extracts fields: %s]", strings.Join(fields, ", ")))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n# Instructions\n")
	sb.WriteString("Each input node shows a prompt to the user and the user types a SHORT answer.\n")
	sb.WriteString("From the trace, determine the EXACT LITERAL value that was used.\n")
	sb.WriteString("The value must be what a user would type at the prompt - concise and minimal.\n\n")
	sb.WriteString("Examples of GOOD values: 192.168.1.200, root, /var/log/syslog, 8080, my-server\n")
	sb.WriteString("Examples of BAD values: the server IP is 192.168.1.200, ssh root user at ip 192.168.1.200\n\n")
	sb.WriteString("Respond with ONLY the parameter values, one per line, in this exact format:\n")
	sb.WriteString("parameter_name=value\n\n")
	sb.WriteString("Do not add quotes, explanations, descriptions, or extra text. Just the key=value lines.\n")

	// Call LLM
	response, err := c.FlowDistiller.LLM(context.Background(), sb.String())
	if err != nil {
		if c.DebugMode {
			fmt.Printf("[Chat DEBUG] LLM param extraction failed: %v\n", err)
		}
		return nil
	}

	if c.DebugMode {
		fmt.Printf("[Chat DEBUG] LLM param extraction response:\n%s\n", response)
	}

	// Parse response: expect "name=value" lines
	validNames := make(map[string]bool, len(inputs))
	for _, inp := range inputs {
		validNames[inp.name] = true
	}

	resolved := make(map[string]string)
	for _, line := range strings.Split(response, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if validNames[key] && val != "" {
			resolved[key] = val
		}
	}

	// Build result in input order
	var params []string
	for _, inp := range inputs {
		if val, ok := resolved[inp.name]; ok {
			params = append(params, inp.name+"="+val)
		} else {
			params = append(params, inp.name+"=<value>")
		}
	}

	return params
}

// matchAndBuildPlan checks if the user's request matches a saved flow.
// Uses vector-based matching first (fast, no LLM call), falling back to
// LLM-based matching when ambiguous or when vector search is unavailable.
// Returns (flowFile, planText) -- both empty if no match.
func (c *ChatAgent) matchAndBuildPlan(userText string, memoryContent string) (string, string) {
	if c.FlowRegistry == nil || c.FlowContextBuilder == nil || c.FlowDistiller == nil {
		return "", ""
	}

	entries := c.FlowRegistry.Entries()
	if len(entries) == 0 {
		return "", ""
	}

	var matchedFile string

	// Strategy 1: Vector-based matching (fast, no LLM call)
	if c.FlowSearcher != nil {
		if c.DebugMode {
			fmt.Printf("[Chat DEBUG] Checking flow registry via vector search...\n")
		}
		flowFile, score, err := c.FlowRegistry.FindMatchVector(context.Background(), c.FlowSearcher, userText)
		if err == nil && flowFile != "" {
			if score >= 0.8 {
				// High confidence — use directly
				matchedFile = flowFile
				if c.DebugMode {
					fmt.Printf("[Chat DEBUG] Vector match (high confidence %.2f): %s\n", score, flowFile)
				}
			} else if score >= 0.6 {
				// Ambiguous — fall through to LLM disambiguation
				if c.DebugMode {
					fmt.Printf("[Chat DEBUG] Vector match (ambiguous %.2f): %s — falling back to LLM\n", score, flowFile)
				}
			}
		}
	}

	// Strategy 2: LLM-based matching (fallback)
	if matchedFile == "" {
		matchPrompt := c.FlowRegistry.BuildMatchPrompt(userText)
		if matchPrompt == "" {
			return "", ""
		}

		if c.DebugMode {
			fmt.Printf("[Chat DEBUG] Checking flow registry via LLM...\n")
		}

		response, err := c.FlowDistiller.LLM(context.Background(), matchPrompt)
		if err != nil {
			if c.DebugMode {
				fmt.Printf("[Chat DEBUG] Flow match LLM call failed: %v\n", err)
			}
			return "", ""
		}

		matchedFile = strings.TrimSpace(response)
		if c.DebugMode {
			fmt.Printf("[Chat DEBUG] Flow match response: %q\n", matchedFile)
		}

		if strings.EqualFold(matchedFile, "NONE") || matchedFile == "" {
			return "", ""
		}

		if !strings.HasSuffix(matchedFile, ".yaml") {
			matchedFile += ".yaml"
		}
	}

	// Find the flow file on disk
	flowPath := c.resolveFlowPath(matchedFile)
	if flowPath == "" {
		if c.DebugMode {
			fmt.Printf("[Chat DEBUG] Matched flow file not found on disk: %s\n", matchedFile)
		}
		return "", ""
	}

	// Read the flow YAML
	flowData, err := os.ReadFile(flowPath)
	if err != nil {
		if c.DebugMode {
			fmt.Printf("[Chat DEBUG] Failed to read flow file: %v\n", err)
		}
		return "", ""
	}

	// Build the execution plan
	plan := c.FlowContextBuilder.BuildExecutionPlan(string(flowData), matchedFile, memoryContent)
	if plan == "" {
		return "", ""
	}

	if c.DebugMode {
		fmt.Printf("[Chat DEBUG] Built execution plan from flow: %s (%d bytes)\n", matchedFile, len(plan))
	}

	return matchedFile, plan
}

// resolveFlowPath finds the full path for a flow file.
// Checks FlowSaveDir, then the default agents directory.
func (c *ChatAgent) resolveFlowPath(filename string) string {
	// Check FlowSaveDir first
	if c.FlowSaveDir != "" {
		p := filepath.Join(c.FlowSaveDir, filename)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Check default flows directory
	configDir, err := os.UserConfigDir()
	if err == nil {
		p := filepath.Join(configDir, "astonish", "flows", filename)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	return ""
}

// truncateQuery shortens a string for debug logging, appending "..." if truncated.
func truncateQuery(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// urlPattern matches http/https URLs in text.
var urlPattern = regexp.MustCompile(`https?://\S+`)

// buildKnowledgeQuery pre-processes a user query for semantic search.
// It strips URLs (which dilute embedding semantics for models like MiniLM)
// and appends matched flow name tokens to boost relevance.
func buildKnowledgeQuery(userText, flowName string) string {
	// Strip URLs — they carry no semantic meaning for the embedding model
	q := urlPattern.ReplaceAllString(userText, "")
	// Collapse whitespace
	q = strings.Join(strings.Fields(q), " ")
	// Append flow name tokens if available (e.g. "download-youtube-video" -> "download youtube video")
	if flowName != "" {
		name := strings.TrimSuffix(flowName, ".yaml")
		name = strings.ReplaceAll(name, "-", " ")
		name = strings.ReplaceAll(name, "_", " ")
		q = strings.TrimSpace(q + " " + name)
	}
	return q
}
