package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"iter"
	"log/slog"
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
	Path     string
	Score    float64
	Snippet  string
	Category string // e.g. "guidance", "skill", "flow", "knowledge"
}

// KnowledgeSearchFunc performs a vector search and returns matching results.
// Used to auto-retrieve relevant knowledge before LLM execution.
type KnowledgeSearchFunc func(ctx context.Context, query string, maxResults int, minScore float64) ([]KnowledgeSearchResult, error)

// KnowledgeSearchByCategoryFunc performs a vector search filtered by category.
// Categories: "guidance", "skill", "flow", "self", "instructions", "knowledge".
type KnowledgeSearchByCategoryFunc func(ctx context.Context, query string, maxResults int, minScore float64, category string) ([]KnowledgeSearchResult, error)

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
	MemoryManager             *memory.Manager               // Persistent memory manager
	MemoryReflector           *MemoryReflector              // Post-task memory reflection (nil = disabled)
	FlowContextBuilder        *FlowContextBuilder           // Converts flow YAML to execution plan
	KnowledgeSearch           KnowledgeSearchFunc           // Auto-retrieve relevant knowledge per turn (nil = disabled)
	KnowledgeSearchByCategory KnowledgeSearchByCategoryFunc // Auto-retrieve guidance docs per turn (nil = disabled)

	// Tool discovery
	ToolIndex *ToolIndex // Semantic tool index for auto-discovery (nil = disabled)

	// Dynamic tool injection: per-turn state for the BeforeModelCallback
	// that injects relevant tools into each LLM request.
	dynamicToolMatches []ToolMatch // from hybrid search on user message (reset each turn)
	searchToolsResults []string    // tool names found via search_tools calls within current turn
	searchToolsMu      sync.Mutex  // protects searchToolsResults

	// Self-management callbacks
	SelfMDRefresher func() // Called after config changes to regenerate SELF.md

	// Credential redaction
	Redactor          *credentials.Redactor                         // Redacts credential values from tool outputs (nil = disabled)
	RedactSessionFunc func(appName, userID, sessionID string) error // Called after save_credential to retroactively redact the session transcript (nil = disabled)

	// Context compaction
	Compactor *persistentsession.Compactor // Manages context window compaction (nil = disabled)

	// Sub-task transparency: when set, sub-agent events (tool calls, results,
	// text) are forwarded to the UI in real-time during delegate_tasks execution.
	// This callback streams display-only events that bypass session persistence.
	// Set by the launcher (console or Studio SSE handler). Thread-safe: may be
	// called concurrently from multiple sub-agent goroutines.
	UIEventCallback func(event *session.Event)

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

// RegisterSearchToolsResults records tool names discovered by search_tools
// during the current turn. The DynamicToolInjectionCallback reads these
// on the next intra-turn BeforeModelCallback firing, making the tools
// immediately available for the LLM to call.
func (c *ChatAgent) RegisterSearchToolsResults(toolNames []string) {
	c.searchToolsMu.Lock()
	defer c.searchToolsMu.Unlock()
	c.searchToolsResults = append(c.searchToolsResults, toolNames...)
}

// DynamicToolInjectionCallback returns a BeforeModelCallback that injects
// relevant tools into each LLM request based on two sources:
//
//  1. Automatic: hybrid search matches computed at the start of each user turn
//  2. Explicit: tool names discovered via search_tools calls within the turn
//
// This fires on every LLM API call (including after tool results), so tools
// found via search_tools become available on the very next LLM call within
// the same turn.
func (c *ChatAgent) DynamicToolInjectionCallback() llmagent.BeforeModelCallback {
	return func(_ agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
		if c.ToolIndex == nil {
			return nil, nil
		}

		// Collect tool names to inject from both sources.
		toolsToInject := make(map[string]bool)

		// Source 1: hybrid search matches (set at start of turn)
		for _, m := range c.dynamicToolMatches {
			if !m.IsMainTool {
				toolsToInject[m.ToolName] = true
			}
		}

		// Source 2: search_tools explicit discoveries (accumulated intra-turn)
		c.searchToolsMu.Lock()
		for _, name := range c.searchToolsResults {
			toolsToInject[name] = true
		}
		c.searchToolsMu.Unlock()

		if len(toolsToInject) == 0 {
			return nil, nil
		}

		// Inject each tool into the request.
		injected := 0
		for toolName := range toolsToInject {
			if _, exists := req.Tools[toolName]; exists {
				continue // already registered (static main-thread tool)
			}
			entry := c.ToolIndex.GetToolEntry(toolName)
			if entry == nil || entry.Tool == nil {
				continue
			}
			packToolIntoRequest(req, entry.Tool)
			injected++
		}

		if c.DebugMode && injected > 0 {
			slog.Debug("dynamic tool injection", "component", "chat", "injected", injected)
		}

		return nil, nil
	}
}

// toolWithDeclaration matches ADK's internal FunctionTool interface for tools
// that can declare their JSON schema. All function-based tools implement this.
type toolWithDeclaration interface {
	Declaration() *genai.FunctionDeclaration
}

// packToolIntoRequest adds a tool to an LLM request for both dispatch and
// schema declaration. This replicates the logic from ADK's internal PackTool
// (toolutils.go) and Astonish's NodeTool.ProcessRequest.
func packToolIntoRequest(req *model.LLMRequest, t tool.Tool) {
	if req.Tools == nil {
		req.Tools = make(map[string]any)
	}
	name := t.Name()
	if _, ok := req.Tools[name]; ok {
		return // already registered
	}
	req.Tools[name] = t

	// Get the function declaration via type assertion — tool.Tool doesn't
	// include Declaration(), but all function-based tools implement it.
	dt, ok := t.(toolWithDeclaration)
	if !ok {
		return
	}
	decl := dt.Declaration()
	if decl == nil {
		return
	}
	if req.Config == nil {
		req.Config = &genai.GenerateContentConfig{}
	}
	// Find existing FunctionDeclarations block (all function tools share one).
	var funcTool *genai.Tool
	for _, gt := range req.Config.Tools {
		if gt != nil && gt.FunctionDeclarations != nil {
			funcTool = gt
			break
		}
	}
	if funcTool == nil {
		req.Config.Tools = append(req.Config.Tools, &genai.Tool{
			FunctionDeclarations: []*genai.FunctionDeclaration{decl},
		})
	} else {
		funcTool.FunctionDeclarations = append(funcTool.FunctionDeclarations, decl)
	}
}

// ForwardSubTaskEvent processes a sub-agent event for transparent delegation.
// It extracts images from FunctionResponse parts (stashing them in pendingImages
// so DrainImages can deliver them to the UI), then forwards the event to
// UIEventCallback for real-time display. Thread-safe: may be called concurrently
// from multiple sub-agent goroutines.
func (c *ChatAgent) ForwardSubTaskEvent(event *session.Event) {
	if event == nil {
		return
	}

	// Extract images from tool responses before forwarding.
	// This ensures browser_take_screenshot images from sub-agents flow
	// through the same pipeline as main-thread images.
	if event.LLMResponse.Content != nil {
		for _, part := range event.LLMResponse.Content.Parts {
			if part.FunctionResponse != nil && part.FunctionResponse.Response != nil {
				// extractAndStripImages is thread-safe (uses c.imageMu)
				part.FunctionResponse.Response = c.extractAndStripImages(part.FunctionResponse.Response)
			}
		}
	}

	// Forward to the UI callback for real-time rendering
	if c.UIEventCallback != nil {
		c.UIEventCallback(event)
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

// thinkTagPattern matches <think>...</think> and <thinking>...</thinking> blocks
// (including content spanning multiple lines). Used for non-streaming contexts
// (e.g., trace reconstruction) where the full text is available at once.
var thinkTagPattern = regexp.MustCompile(`(?s)<(?:think|thinking)>.*?</(?:think|thinking)>`)

// openThinkTags lists the opening tags we recognise as chain-of-thought markers.
var openThinkTags = []string{"<think>", "<thinking>"}

// closeThinkTags lists the corresponding closing tags, same index as openThinkTags.
var closeThinkTags = []string{"</think>", "</thinking>"}

// thinkTagFilter is a stateful streaming filter that strips <think>/<thinking>
// blocks from LLM text that arrives in small chunks (one event per token group).
//
// A regex cannot work here because a single <think>…</think> block is typically
// split across dozens of streaming events.  Instead we track whether we are
// currently "inside" a think block and buffer partial tag matches so that no
// tag fragment leaks into the output.
type thinkTagFilter struct {
	inside bool   // true while we are between an open and close tag
	buf    string // buffered bytes that *might* be the start of a tag
}

// Feed processes a chunk of streamed text and returns the portion that should
// be shown to the user (empty string means the chunk was suppressed).
// The second return value is true when the filter consumed or suppressed any
// think-tag content during this call, which lets the caller distinguish
// whitespace remnants of stripping from legitimate whitespace.
func (f *thinkTagFilter) Feed(chunk string) (string, bool) {
	var out strings.Builder
	stripped := false
	// Prepend anything buffered from the previous chunk.
	input := f.buf + chunk
	f.buf = ""

	for len(input) > 0 {
		if f.inside {
			stripped = true // suppressing content inside a think block
			// We are inside a think block — look for a closing tag.
			idx := f.indexOfAnyClose(input)
			if idx == -1 {
				// No closing tag found yet.  Check if the tail of input
				// could be the start of a closing tag (e.g. "</thi").
				prefixLen := f.longestClosingPrefix(input)
				if prefixLen > 0 {
					f.buf = input[len(input)-prefixLen:]
				}
				// Everything is suppressed (still inside).
				return out.String(), stripped
			}
			// Found a closing tag — skip past it.
			closeTag := f.matchingCloseAt(input, idx)
			input = input[idx+len(closeTag):]
			f.inside = false
			continue
		}

		// We are outside a think block — look for an opening tag.
		idx, tag := f.indexOfAnyOpen(input)
		if idx == -1 {
			// No opening tag found.  But the tail might be a partial
			// opening tag (e.g., "<thin"), so buffer that part.
			prefixLen := f.longestOpeningPrefix(input)
			if prefixLen > 0 {
				out.WriteString(input[:len(input)-prefixLen])
				f.buf = input[len(input)-prefixLen:]
			} else {
				out.WriteString(input)
			}
			return out.String(), stripped
		}
		// Emit everything before the opening tag.
		out.WriteString(input[:idx])
		input = input[idx+len(tag):]
		f.inside = true
		stripped = true
	}
	return out.String(), stripped
}

// indexOfAnyClose returns the byte index in s where any closing tag starts, or -1.
func (f *thinkTagFilter) indexOfAnyClose(s string) int {
	best := -1
	for _, tag := range closeThinkTags {
		if i := strings.Index(s, tag); i != -1 && (best == -1 || i < best) {
			best = i
		}
	}
	return best
}

// matchingCloseAt returns which closing tag starts at s[idx:].
func (f *thinkTagFilter) matchingCloseAt(s string, idx int) string {
	for _, tag := range closeThinkTags {
		if strings.HasPrefix(s[idx:], tag) {
			return tag
		}
	}
	return closeThinkTags[0] // fallback
}

// indexOfAnyOpen returns the byte index and the matching opening tag, or -1.
func (f *thinkTagFilter) indexOfAnyOpen(s string) (int, string) {
	bestIdx := -1
	bestTag := ""
	for _, tag := range openThinkTags {
		if i := strings.Index(s, tag); i != -1 && (bestIdx == -1 || i < bestIdx) {
			bestIdx = i
			bestTag = tag
		}
	}
	return bestIdx, bestTag
}

// longestOpeningPrefix returns the length of the longest suffix of s that
// is a proper prefix of one of the opening tags (e.g., "<thi" is a prefix
// of "<think>").  Returns 0 if no suffix matches.
func (f *thinkTagFilter) longestOpeningPrefix(s string) int {
	// The longest opening tag is "<thinking>" (10 chars), so we only need to
	// check the last 9 chars at most (a proper prefix is shorter than the tag).
	maxCheck := 9
	if len(s) < maxCheck {
		maxCheck = len(s)
	}
	for l := maxCheck; l >= 1; l-- {
		suffix := s[len(s)-l:]
		for _, tag := range openThinkTags {
			if strings.HasPrefix(tag, suffix) {
				return l
			}
		}
	}
	return 0
}

// longestClosingPrefix returns the length of the longest suffix of s that
// is a proper prefix of one of the closing tags.
func (f *thinkTagFilter) longestClosingPrefix(s string) int {
	maxCheck := 11 // "</thinking>" is 12 chars; proper prefix is at most 11
	if len(s) < maxCheck {
		maxCheck = len(s)
	}
	for l := maxCheck; l >= 1; l-- {
		suffix := s[len(s)-l:]
		for _, tag := range closeThinkTags {
			if strings.HasPrefix(tag, suffix) {
				return l
			}
		}
	}
	return 0
}

// filterEventThinkContent uses the streaming filter to strip think-tag content
// and also drops parts flagged with the structured Thought field.
func filterEventThinkContent(f *thinkTagFilter, event *session.Event) {
	if event == nil {
		return
	}
	content := event.LLMResponse.Content
	if content == nil {
		return
	}
	cleaned := make([]*genai.Part, 0, len(content.Parts))
	for _, part := range content.Parts {
		// Drop parts flagged as chain-of-thought by the provider.
		if part.Thought {
			continue
		}
		if part.Text != "" {
			filtered, stripped := f.Feed(part.Text)
			if filtered == "" {
				continue
			}
			// Only drop whitespace-only remnants when the filter actually
			// stripped think-tag content from this chunk.  Legitimate
			// whitespace (e.g., "\n\n" between markdown sections) must
			// pass through to preserve formatting.
			if stripped && strings.TrimSpace(filtered) == "" {
				continue
			}
			part = &genai.Part{
				Text:                filtered,
				InlineData:          part.InlineData,
				FileData:            part.FileData,
				FunctionCall:        part.FunctionCall,
				FunctionResponse:    part.FunctionResponse,
				ExecutableCode:      part.ExecutableCode,
				CodeExecutionResult: part.CodeExecutionResult,
			}
		}
		cleaned = append(cleaned, part)
	}
	event.LLMResponse.Content = &genai.Content{
		Parts: cleaned,
		Role:  content.Role,
	}
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

		// Per-turn dynamic content: execution plan and knowledge.
		// These are appended to the end of the system prompt via
		// SystemPromptBuilder.ExecutionPlan / RelevantKnowledge fields,
		// so they carry system-level authority for instruction following.
		var executionPlan string
		var relevantKnowledge string
		var knowledgeTrackingResults []KnowledgeSearchResult // for session tracking event

		var memContent string

		if c.MemoryManager != nil {
			// Load memory content for flow plan building (parameter resolution)
			mc, memErr := c.MemoryManager.Load()
			if memErr != nil {
				if c.DebugMode {
					slog.Debug("failed to load memory", "component", "chat", "error", memErr)
				}
			} else {
				memContent = mc
			}
		}

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
				var allResults []KnowledgeSearchResult

				// Partition 1: Guidance docs (how-to instructions for capabilities)
				if c.KnowledgeSearchByCategory != nil {
					guidanceResults, err := c.KnowledgeSearchByCategory(context.Background(), searchQuery, 3, 0.3, "guidance")
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
					knowledgeResults, err := c.KnowledgeSearch(context.Background(), searchQuery, 5, 0.3)
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

				// Check for flow matches among the results.
				// If a single high-confidence flow is found, load the full flow
				// YAML and build an execution plan. If multiple flows match above
				// threshold, leave them as knowledge so the LLM can ask the user.
				const flowScoreThreshold = 0.6
				allResults, executionPlan = c.extractFlowFromResults(allResults, flowScoreThreshold, memContent, yield)

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
		yieldKnowledgeTrackingEvent(yield, relevantKnowledge, executionPlan, knowledgeTrackingResults)

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
		c.SystemPrompt.ExecutionPlan = executionPlan
		c.SystemPrompt.RelevantKnowledge = relevantKnowledge
		c.SystemPrompt.RelevantTools = relevantTools
		instruction := c.SystemPrompt.Build()

		// Capture session identity for use in AfterToolCallback closure.
		sessionID := ctx.Session().ID()
		sessionAppName := ctx.Session().AppName()
		sessionUserID := ctx.Session().UserID()

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

					// If we were using an execution plan and it failed, inform the user
					// conversationally. The orphan cleanup in the provider layer ensures
					// the next turn's history is valid.
					if executionPlan != "" {
						if c.DebugMode {
							slog.Debug("flow execution failed, cleared execution plan", "component", "chat", "error", err)
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
			slog.Debug("reconstructTraces: failed to load session", "component", "chat", "sessionID", ds.SessionID, "error", err)
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
					cleaned := thinkTagPattern.ReplaceAllString(part.Text, "")
					current.FinalOutput += cleaned
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
		slog.Debug("reconstructTraces: rebuilt traces", "component", "chat", "traces", len(traces), "sessionID", ds.SessionID)
		for i, t := range traces {
			slog.Debug("reconstructed trace", "component", "chat", "trace", i+1, "request", t.UserRequest, "toolCalls", t.ToolCallCount())
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
				slog.Debug("failed to register flow", "component", "chat", "error", regErr)
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
			slog.Debug("failed to parse yaml for param extraction", "component", "chat", "error", err)
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
			slog.Debug("llm param extraction failed", "component", "chat", "error", err)
		}
		return nil
	}

	if c.DebugMode {
		slog.Debug("llm param extraction response", "component", "chat", "response", response)
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

// timestampPattern matches the [YYYY-MM-DD HH:MM:SS UTC] prefix prepended
// by NewTimestampedUserContent. This prefix dilutes embedding queries
// (especially for short tool descriptions) and must be stripped.
var timestampPattern = regexp.MustCompile(`^\[?\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}\s+\w+\]?\s*`)

// buildKnowledgeQuery pre-processes a user query for semantic search.
// It strips timestamps and URLs (which dilute embedding semantics for models like MiniLM).
func buildKnowledgeQuery(userText string) string {
	// Strip leading timestamp prefix (e.g., "[2026-03-28 03:10:11 UTC]")
	q := timestampPattern.ReplaceAllString(userText, "")
	// Strip URLs — they carry no semantic meaning for the embedding model
	q = urlPattern.ReplaceAllString(q, "")
	// Collapse whitespace
	q = strings.Join(strings.Fields(q), " ")
	return q
}

// shortQueryThreshold is the maximum character length for a user message to
// be considered "short" — i.e., lacking enough context for accurate tool
// discovery. When the cleaned query is shorter than this, we augment it
// with the tail of the last LLM response to provide topical context.
// Examples: "looks good" (10), "use it" (6), "go for it" (9), "yes" (3).
const shortQueryThreshold = 40

// lastModelResponseTail extracts the trailing text from the last model
// response in the session event history. This provides topical context
// when the user's message is too short to be meaningful for search
// (e.g., "looks good", "use it"). The tail is where the LLM's question
// or action prompt typically lives (e.g., "shall I proceed with the
// fleet plan?"), which carries the semantic signal we need.
//
// Returns at most maxLen characters from the end of the combined model
// text parts. Returns "" if no model response is found.
func lastModelResponseTail(events session.Events, maxLen int) string {
	if events == nil {
		return ""
	}
	// Walk backwards to find the last model content event.
	// Model responses may be split across multiple streaming events
	// (each with a small text fragment). We want the last *complete*
	// response, which is the contiguous sequence of model events
	// ending at the most recent model event before the user's message.
	n := events.Len()
	var textParts []string
	foundModel := false
	for i := n - 1; i >= 0; i-- {
		ev := events.At(i)
		if ev.Author == "user" {
			if foundModel {
				break // we've collected all model parts before this user message
			}
			continue // skip user events before we find model events
		}
		if ev.Author != "chat" {
			if foundModel {
				break // non-model, non-user event after finding model text
			}
			continue
		}
		// It's a model event — extract text parts
		if ev.Content != nil {
			for _, part := range ev.Content.Parts {
				if part.Text != "" {
					textParts = append(textParts, part.Text)
					foundModel = true
				}
			}
		}
	}
	if len(textParts) == 0 {
		return ""
	}
	// textParts are in reverse order (we walked backwards), reverse them
	for i, j := 0, len(textParts)-1; i < j; i, j = i+1, j-1 {
		textParts[i], textParts[j] = textParts[j], textParts[i]
	}
	full := strings.Join(textParts, "")
	// Take the tail — the question/prompt is usually at the end
	if len(full) > maxLen {
		full = full[len(full)-maxLen:]
	}
	// Strip markdown formatting noise
	full = strings.ReplaceAll(full, "**", "")
	full = strings.ReplaceAll(full, "```", "")
	full = strings.ReplaceAll(full, "#", "")
	// Collapse whitespace
	full = strings.Join(strings.Fields(full), " ")
	return full
}

// extractFlowFromResults scans knowledge search results for flow documents.
// If a single high-confidence flow is found (score >= threshold), it loads the
// corresponding YAML, builds an execution plan, emits a user notification,
// and returns the remaining (non-flow) results plus the plan text.
// If multiple flows score above threshold, all are kept as regular knowledge
// so the LLM can present the options and ask the user which to use.
func (c *ChatAgent) extractFlowFromResults(
	results []KnowledgeSearchResult,
	threshold float64,
	memContent string,
	yield func(*session.Event, error) bool,
) ([]KnowledgeSearchResult, string) {
	if c.FlowContextBuilder == nil {
		return results, ""
	}

	// Find flow results above threshold
	var flowHits []KnowledgeSearchResult
	for _, r := range results {
		if r.Category == "flow" && r.Score >= threshold {
			flowHits = append(flowHits, r)
		}
	}

	// Multiple high-confidence flows: ambiguous — let the LLM ask the user.
	// Keep all results as-is so the LLM sees both flow descriptions.
	if len(flowHits) != 1 {
		return results, ""
	}

	bestFlow := flowHits[0]

	// Derive the YAML filename from the knowledge doc path:
	// "flows/youtube_knowledge_extractor.md" -> "youtube_knowledge_extractor.yaml"
	baseName := strings.TrimPrefix(bestFlow.Path, "flows/")
	baseName = strings.TrimSuffix(baseName, ".md")
	flowFile := baseName + ".yaml"

	// Resolve the flow YAML path on disk
	flowPath := c.resolveFlowPath(flowFile)
	if flowPath == "" {
		if c.DebugMode {
			slog.Debug("flow doc found but yaml not on disk", "component", "chat", "flowFile", flowFile)
		}
		return results, ""
	}

	// Read the flow YAML
	flowData, err := os.ReadFile(flowPath)
	if err != nil {
		if c.DebugMode {
			slog.Debug("failed to read flow yaml", "component", "chat", "error", err)
		}
		return results, ""
	}

	// Build the execution plan
	plan := c.FlowContextBuilder.BuildExecutionPlan(string(flowData), flowFile, memContent)
	if plan == "" {
		return results, ""
	}

	if c.DebugMode {
		slog.Debug("flow discovered via knowledge search", "component", "chat", "flowFile", flowFile, "score", bestFlow.Score, "planBytes", len(plan))
	}

	// Emit conversational notification about the flow match
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

	// Remove the flow doc from knowledge results to avoid duplication
	// (the execution plan already contains all the flow information).
	var remaining []KnowledgeSearchResult
	for _, r := range results {
		if r.Path != bestFlow.Path {
			remaining = append(remaining, r)
		}
	}

	return remaining, plan
}

// isUnknownToolError checks whether an error from ADK is caused by the LLM
// calling a tool name that doesn't exist. ADK returns this as a hard error
// (fmt.Errorf("unknown tool: %q")) rather than a tool error response, which
// means the LLM never gets feedback about its mistake.
func isUnknownToolError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "unknown tool:")
}

// buildUnknownToolResponse creates a synthetic FunctionResponse event for
// orphaned FunctionCall parts that referenced unknown tool names. The response
// includes the error message and a hint listing available tool names so the
// LLM can self-correct on retry.
func buildUnknownToolResponse(calls []*genai.FunctionCall, tools []tool.Tool, toolsets []tool.Toolset) *session.Event {
	// Collect available tool names for the hint
	var toolNames []string
	for _, t := range tools {
		toolNames = append(toolNames, t.Name())
	}
	// Include toolset names as a group hint (individual tools require context to resolve)
	for _, ts := range toolsets {
		toolNames = append(toolNames, ts.Name()+".*")
	}
	hint := strings.Join(toolNames, ", ")

	var parts []*genai.Part
	for _, fc := range calls {
		parts = append(parts, &genai.Part{
			FunctionResponse: &genai.FunctionResponse{
				ID:   fc.ID,
				Name: fc.Name,
				Response: map[string]any{
					"error": fmt.Sprintf(
						"Unknown tool %q. This tool does not exist. Available tools: %s. "+
							"Use the correct tool name and try again.",
						fc.Name, hint,
					),
				},
			},
		})
	}

	ev := session.NewEvent("unknown-tool-recovery")
	ev.Author = "chat"
	ev.LLMResponse = model.LLMResponse{
		Content: &genai.Content{
			Role:  "user",
			Parts: parts,
		},
	}
	return ev
}

// deduplicateSearchResults removes duplicate knowledge results that appear
// in both the guidance and general knowledge partitions. Deduplication is by
// path + snippet content. Earlier entries (guidance) take priority.
func deduplicateSearchResults(results []KnowledgeSearchResult) []KnowledgeSearchResult {
	seen := make(map[string]bool)
	var deduped []KnowledgeSearchResult
	for _, r := range results {
		// Key by path + first 100 chars of snippet to catch overlapping chunks
		snippet := r.Snippet
		if len(snippet) > 100 {
			snippet = snippet[:100]
		}
		k := r.Path + ":" + snippet
		if !seen[k] {
			seen[k] = true
			deduped = append(deduped, r)
		}
	}
	return deduped
}

// yieldKnowledgeTrackingEvent emits a content-less session event that records
// what knowledge was injected (or that none was found) for this turn.
//
// Because Content is nil, ADK's ContentsRequestProcessor skips the event when
// building the LLM request (it checks content == nil at contents_processor.go:71)
// and eventsToMessages() skips it in the Studio UI. The event is still persisted
// to the session .jsonl file, making it available for diagnostic inspection.
func yieldKnowledgeTrackingEvent(
	yield func(*session.Event, error) bool,
	relevantKnowledge, executionPlan string,
	results []KnowledgeSearchResult,
) {
	// Build result summaries for the tracking payload.
	resultEntries := make([]map[string]any, 0, len(results))
	for _, r := range results {
		resultEntries = append(resultEntries, map[string]any{
			"path":     r.Path,
			"score":    r.Score,
			"category": r.Category,
		})
	}

	// Determine injection type.
	injectionType := "none"
	if executionPlan != "" && relevantKnowledge != "" {
		injectionType = "plan+knowledge"
	} else if executionPlan != "" {
		injectionType = "plan"
	} else if relevantKnowledge != "" {
		injectionType = "knowledge"
	}

	// Estimate token count (~4 chars per token).
	estimatedTokens := (len(relevantKnowledge) + len(executionPlan)) / 4

	yield(&session.Event{
		ID:        fmt.Sprintf("knowledge-%d", time.Now().UnixMilli()),
		Author:    "system",
		Timestamp: time.Now(),
		Actions: session.EventActions{
			StateDelta: map[string]any{
				"_knowledge_injection": map[string]any{
					"type":             injectionType,
					"results":          resultEntries,
					"estimated_tokens": estimatedTokens,
				},
			},
		},
	}, nil)
}

// yieldToolTrackingEvent emits a content-less session event that records
// what tools were discovered (or that none were found) for this turn.
// Mirrors yieldKnowledgeTrackingEvent for the tool index.
func yieldToolTrackingEvent(
	yield func(*session.Event, error) bool,
	query, relevantTools string,
	matches []ToolMatch,
) {
	matchEntries := make([]map[string]any, 0, len(matches))
	for _, m := range matches {
		matchEntries = append(matchEntries, map[string]any{
			"tool":  m.ToolName,
			"group": m.GroupName,
			"score": m.Score,
		})
	}

	estimatedTokens := len(relevantTools) / 4

	yield(&session.Event{
		ID:        fmt.Sprintf("tools-%d", time.Now().UnixMilli()),
		Author:    "system",
		Timestamp: time.Now(),
		Actions: session.EventActions{
			StateDelta: map[string]any{
				"_tool_injection": map[string]any{
					"query":            query,
					"matches":          matchEntries,
					"match_count":      len(matches),
					"estimated_tokens": estimatedTokens,
				},
			},
		},
	}, nil)
}
