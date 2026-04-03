package agent

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"strings"
	"time"

	"github.com/schardosin/astonish/pkg/credentials"
	"github.com/schardosin/astonish/pkg/provider/llmerror"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// Run implements the agent.Run interface for ADK.
// It is called by the ADK runner for each user message.
func (c *ChatAgent) Run(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		// Wrap yield to strip chain-of-thought content and redact credentials.
		// The think-tag filter is stateful (tracks whether we are inside a
		// <think> block across streaming chunks), so it must be created once
		// per Run invocation.
		thinkFilter := &thinkTagFilter{}
		origYield := yield
		yield = func(event *session.Event, err error) bool {
			filterEventThinkContent(thinkFilter, event)
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
			slog.Debug("user message", "component", "chat", "text", userText)
		}

		// --- Phase A: Dynamic Execution ---
		trace := NewExecutionTrace(userText)

		// Per-turn dynamic content: auto-retrieved knowledge.
		// Appended to the end of the system prompt via
		// SystemPromptBuilder.RelevantKnowledge field,
		// so it carries system-level authority for instruction following.
		var relevantKnowledge string
		var knowledgeTrackingResults []KnowledgeSearchResult // for session tracking event

		// Auto-retrieve relevant knowledge from vector store.
		// Three partitioned searches: guidance (max 3) + general knowledge (max 5).
		// Flow documents are discovered naturally through the general search.
		// When a high-confidence flow match is found, it is loaded as an
		// actionable execution plan rather than passive knowledge.
		if (c.KnowledgeSearch != nil || c.KnowledgeSearchByCategory != nil) && userText != "" {
			searchQuery := buildKnowledgeQuery(userText)
			if len(searchQuery) < 5 {
				if c.DebugMode {
					slog.Debug("auto knowledge search skipped: query too short", "component", "chat", "query", searchQuery)
				}
			} else {
				// Build an augmented BM25 query that includes conversational
				// context from the last model response. This helps follow-up
				// queries like "show me per VM" find relevant docs by matching
				// topic keywords (e.g., "proxmox", "server") that the user's
				// message alone doesn't contain. Vector search stays on the
				// raw query to avoid semantic dilution.
				bm25Query := ""
				if tail := lastModelResponseTail(ctx.Session().Events(), 300); tail != "" {
					bm25Query = tail + " " + searchQuery
					if c.DebugMode {
						slog.Debug("augmented BM25 query with conversation context",
							"component", "chat",
							"bm25_query_len", len(bm25Query))
					}
				}

				var allResults []KnowledgeSearchResult

				// Partition 1: Guidance docs (how-to instructions for capabilities)
				if c.KnowledgeSearchByCategory != nil {
					guidanceResults, err := c.KnowledgeSearchByCategory(context.Background(), searchQuery, bm25Query, 3, 0.3, "guidance")
					if err != nil {
						if c.DebugMode {
							slog.Debug("guidance search failed", "component", "chat", "error", err)
						}
					} else {
						allResults = append(allResults, guidanceResults...)
					}
				}

				// Partition 2: Everything else (memory, skills, flows, knowledge)
				if c.KnowledgeSearch != nil {
					knowledgeResults, err := c.KnowledgeSearch(context.Background(), searchQuery, bm25Query, 5, 0.3)
					if err != nil {
						if c.DebugMode {
							slog.Debug("knowledge search failed", "component", "chat", "error", err)
						}
					} else {
						allResults = append(allResults, knowledgeResults...)
					}
				}

				// Deduplicate
				allResults = deduplicateSearchResults(allResults)

				// Format remaining results as knowledge text
				if len(allResults) > 0 {
					knowledgeTrackingResults = allResults
					var kb strings.Builder
					for _, r := range allResults {
						kb.WriteString(fmt.Sprintf("**%s** (relevance: %.0f%%)\n", r.Path, r.Score*100))
						kb.WriteString(r.Snippet)
						kb.WriteString("\n\n")
					}
					relevantKnowledge = EscapeCurlyPlaceholders(kb.String())
					if c.DebugMode {
						slog.Debug("auto knowledge search results injected", "component", "chat", "results", len(allResults), "query", truncateQuery(searchQuery, 60))
					}
				} else if c.DebugMode {
					slog.Debug("auto knowledge search: no results", "component", "chat", "query", truncateQuery(searchQuery, 60))
				}
			}
		} else if c.KnowledgeSearch == nil && c.KnowledgeSearchByCategory == nil && c.DebugMode {
			slog.Debug("auto knowledge search disabled: no search functions wired", "component", "chat")
		}

		// Persist a tracking event recording what knowledge was injected.
		// Content is nil so ADK's ContentsRequestProcessor skips it (never
		// sent to the LLM) and eventsToMessages() skips it (never shown in UI).
		// The event is still written to the session .jsonl file for diagnostics.
		yieldKnowledgeTrackingEvent(yield, relevantKnowledge, "", knowledgeTrackingResults)

		// Auto-retrieve relevant tools from the tool index.
		// Matches drive two things: (1) prompt text listing relevant tools,
		// (2) dynamic injection of concrete tool instances into the LLM request
		// via DynamicToolInjectionCallback so the LLM can call them directly.
		var relevantTools string
		var toolMatches []ToolMatch
		var toolSearchQuery string
		if c.ToolIndex != nil && userText != "" {
			toolSearchQuery = buildKnowledgeQuery(userText)
			// For short messages ("looks good", "use it", "yes"), the user text
			// alone lacks topical signal for tool discovery. Augment the query
			// with the tail of the last LLM response, which typically contains
			// the question or action prompt that gives us context.
			if len(toolSearchQuery) < shortQueryThreshold {
				if tail := lastModelResponseTail(ctx.Session().Events(), 200); tail != "" {
					toolSearchQuery = tail + " " + toolSearchQuery
					if c.DebugMode {
						slog.Debug("short message — augmented tool search query with LLM context", "component", "chat")
					}
				}
			}
			if len(toolSearchQuery) >= 5 {
				matches, err := c.ToolIndex.SearchHybrid(context.Background(), toolSearchQuery, 8, 0.005)
				if err != nil {
					if c.DebugMode {
						slog.Debug("tool index search failed", "component", "chat", "error", err)
					}
				} else {
					toolMatches = matches
					if len(matches) > 0 {
						relevantTools = FormatToolMatchesForPrompt(matches)
						if c.DebugMode {
							slog.Debug("tool index search results", "component", "chat", "matches", len(matches), "query", truncateQuery(toolSearchQuery, 60))
						}
					}
				}
			}
		}
		yieldToolTrackingEvent(yield, toolSearchQuery, relevantTools, toolMatches)

		// Store per-turn tool matches for the DynamicToolInjectionCallback
		// and reset any search_tools discoveries from the previous turn.
		c.dynamicToolMatches = toolMatches
		c.searchToolsMu.Lock()
		c.searchToolsResults = nil
		c.searchToolsMu.Unlock()

		// Set per-turn dynamic fields on the system prompt builder, then build.
		// These are appended at the end of the system prompt so the static prefix
		// remains cacheable by providers.
		c.SystemPrompt.RelevantKnowledge = relevantKnowledge
		c.SystemPrompt.RelevantTools = relevantTools
		instruction := c.SystemPrompt.Build()

		// Capture session identity for use in AfterToolCallback closure.
		sessionID := ctx.Session().ID()
		sessionAppName := ctx.Session().AppName()
		sessionUserID := ctx.Session().UserID()

		// Shared variable between Before and After tool callbacks for
		// credential placeholder restore. Safe because ADK processes tool
		// calls sequentially within a single invocation.
		var credentialRestore func()

		// Create the AfterToolCallback for trace recording
		afterToolCallback := func(ctx tool.Context, t tool.Tool, input, output map[string]any, err error) (map[string]any, error) {
			// Restore credential placeholders in the args map. This undoes the
			// in-place substitution from BeforeToolCallback, ensuring the session
			// event (which shares the same args map by reference) retains
			// {{CREDENTIAL:...}} placeholders instead of real secret values.
			if credentialRestore != nil {
				credentialRestore()
				credentialRestore = nil
			}

			// Redact credential values from all tool outputs before the LLM sees them.
			// resolve_credential now returns {{CREDENTIAL:...}} placeholders instead
			// of raw values, so no exemption is needed — placeholders pass through
			// the redactor unchanged.
			redactedOutput := output
			if c.Redactor != nil && output != nil {
				redactedOutput = c.Redactor.RedactMap(output)
			}

			// Strip image_base64 from tool results to prevent large binary
			// blobs from entering session history. The raw image bytes are
			// stashed in the ChatAgent's image queue for channel delivery.
			redactedOutput = c.extractAndStripImages(redactedOutput)

			// Strip large flow output from run_flow results. The full output
			// is stashed for direct delivery to the user (via SSE or channel),
			// and replaced with a short pointer so the LLM doesn't try to
			// summarize or reproduce it. The output is already AI-generated
			// content that should not be re-processed by another LLM.
			if t.Name() == "run_flow" && redactedOutput != nil {
				redactedOutput = c.extractAndStripFlowOutput(redactedOutput)
			}

			trace.RecordStep(t.Name(), input, redactedOutput, err)
			if c.DebugMode {
				status := "OK"
				if err != nil {
					status = fmt.Sprintf("ERROR: %v", err)
				}
				slog.Debug("tool call recorded", "component", "chat", "tool", t.Name(), "status", status)
			}

			// After save_credential succeeds, retroactively redact the current
			// session transcript. The redactor now knows the new secret values,
			// so user messages that contained raw secrets (submitted before the
			// credential was saved) can be scrubbed on disk and in memory.
			if t.Name() == "save_credential" && err == nil && c.RedactSessionFunc != nil {
				if redactErr := c.RedactSessionFunc(sessionAppName, sessionUserID, sessionID); redactErr != nil {
					if c.DebugMode {
						slog.Debug("retroactive session redaction failed", "component", "chat", "error", redactErr)
					}
				} else if c.DebugMode {
					slog.Debug("retroactive session redaction completed", "component", "chat")
				}
			}

			return redactedOutput, err
		}

		// Build BeforeToolCallbacks — credential placeholder substitution.
		// When the LLM uses {{CREDENTIAL:name:field}} tokens in tool args
		// (from resolve_credential output), this callback replaces them with
		// real values just before the tool executes. The AfterToolCallback
		// restores the original placeholders so the session event (which
		// shares the same args map by reference) never persists real secrets.
		var beforeToolCallbacks []llmagent.BeforeToolCallback

		if c.CredentialStore != nil {
			store := c.CredentialStore
			beforeToolCallbacks = append(beforeToolCallbacks, func(ctx tool.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
				credentialRestore = credentials.SubstituteAndRestore(args, store)
				return nil, nil // proceed with (possibly mutated) args
			})
		}

		// Build BeforeModelCallbacks
		var beforeModelCallbacks []llmagent.BeforeModelCallback

		// Truncate oversized tool responses before they reach the model
		beforeModelCallbacks = append(beforeModelCallbacks, TruncateToolResponsesCallback())

		// Dynamically inject relevant tools into each LLM request.
		// Fires on every LLM API call (including after tool results), adding
		// tools from hybrid search matches and search_tools discoveries.
		beforeModelCallbacks = append(beforeModelCallbacks, c.DynamicToolInjectionCallback())

		if c.Compactor != nil {
			beforeModelCallbacks = append(beforeModelCallbacks, c.Compactor.BeforeModelCallback())
		}

		// Create llmagent with static tools
		llmAgent, err := llmagent.New(llmagent.Config{
			Name:                 "chat",
			Model:                c.LLM,
			Instruction:          instruction,
			Tools:                c.Tools,
			Toolsets:             c.Toolsets,
			BeforeToolCallbacks:  beforeToolCallbacks,
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
		// Also handles unknown tool errors (model hallucinated a tool name).
		const maxRetries = 3
		const maxUnknownToolRetries = 2 // separate cap for tool name hallucinations
		toolCallCount := 0
		maxToolCalls := c.MaxToolCalls
		lastToolCallSeen := false
		anyTextYielded := false
		unknownToolRetries := 0

		// Track the last FunctionCall parts seen so we can build synthetic
		// error responses when ADK rejects an unknown tool name.
		var lastFunctionCalls []*genai.FunctionCall

		for attempt := range maxRetries {

			retried := false
			for event, err := range llmAgent.Run(ctx) {
				if err != nil {
					// Check for retryable errors (rate limit, server overload)
					if llmerror.IsRetryable(err) && attempt < maxRetries-1 {
						wait := retryBackoff(attempt, err)
						if c.DebugMode {
							slog.Debug("retryable error", "component", "chat", "attempt", attempt+1, "maxRetries", maxRetries, "error", err, "wait", wait)
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

					// Check for unknown tool error (model hallucinated a tool name).
					// Instead of aborting the turn, inject a synthetic FunctionResponse
					// with a corrective error message so the LLM can self-correct.
					if isUnknownToolError(err) && unknownToolRetries < maxUnknownToolRetries && len(lastFunctionCalls) > 0 {
						unknownToolRetries++
						if c.DebugMode {
							slog.Debug("unknown tool error", "component", "chat", "retry", unknownToolRetries, "maxRetries", maxUnknownToolRetries, "error", err)
						}
						syntheticEvent := buildUnknownToolResponse(lastFunctionCalls, c.Tools, c.Toolsets)
						yield(syntheticEvent, nil) // runner persists this to session
						lastFunctionCalls = nil
						retried = true
						break // re-run llmAgent to let the LLM see the error and retry
					}

					// Non-retryable error, or retries exhausted
					if c.DebugMode && attempt > 0 {
						slog.Debug("error after retries", "component", "chat", "attempts", attempt, "error", err)
					}

					yield(nil, err)
					return
				}

				// Count tool calls, track FunctionCall parts, and capture text output
				if event.LLMResponse.Content != nil {
					// Collect FunctionCalls from this event for unknown-tool recovery
					var eventFunctionCalls []*genai.FunctionCall
					for _, p := range event.LLMResponse.Content.Parts {
						if p.FunctionCall != nil {
							eventFunctionCalls = append(eventFunctionCalls, p.FunctionCall)
						}
					}
					if len(eventFunctionCalls) > 0 {
						lastFunctionCalls = eventFunctionCalls
					}

					for _, p := range event.LLMResponse.Content.Parts {
						if p.FunctionCall != nil {
							toolCallCount++
							lastToolCallSeen = true
							if toolCallCount >= maxToolCalls {
								yield(&session.Event{
									LLMResponse: model.LLMResponse{
										Content: &genai.Content{
											Parts: []*genai.Part{{Text: fmt.Sprintf("\n\nI've been working on this for a while now (%d tool calls). Let me pause here — would you like me to continue?", maxToolCalls)}},
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
			slog.Debug("postLoop reached", "component", "chat", "toolCallCount", trace.ToolCallCount())
			for i, step := range trace.Steps {
				slog.Debug("trace step", "component", "chat", "step", i+1, "tool", step.ToolName, "success", step.Success)
			}
		}

		// Post-task memory reflection: give the LLM one last chance to save
		// durable knowledge discovered during the turn. Runs silently — no
		// events are yielded to the user.
		if c.MemoryReflector != nil {
			c.MemoryReflector.Reflect(ctx, trace)
		}

		// Store the trace keyed by session ID for on-demand /distill
		c.traceMu.Lock()
		c.traceHistory[sessionID] = append(c.traceHistory[sessionID], trace)
		// Prune: keep at most 20 traces per session
		if len(c.traceHistory[sessionID]) > 20 {
			c.traceHistory[sessionID] = c.traceHistory[sessionID][len(c.traceHistory[sessionID])-20:]
		}
		c.traceMu.Unlock()
	}
}
