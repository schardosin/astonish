package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"sync"

	"github.com/schardosin/astonish/pkg/credentials"
	"github.com/schardosin/astonish/pkg/memory"
	persistentsession "github.com/schardosin/astonish/pkg/session"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// KnowledgeSearchResult holds a single result from the knowledge vector search.
type KnowledgeSearchResult struct {
	Path     string
	Score    float64
	Snippet  string
	Category string // e.g. "guidance", "skill", "flow", "knowledge"
}

// KnowledgeSearchFunc performs a hybrid search and returns matching results.
// Used to auto-retrieve relevant knowledge before LLM execution.
// The bm25Query parameter provides conversational context for BM25 keyword
// matching; when empty, BM25 uses the same query as vector search.
type KnowledgeSearchFunc func(ctx context.Context, query string, bm25Query string, maxResults int, minScore float64) ([]KnowledgeSearchResult, error)

// KnowledgeSearchByCategoryFunc performs a hybrid search filtered by category.
// Categories: "guidance", "skill", "flow", "self", "instructions", "knowledge".
type KnowledgeSearchByCategoryFunc func(ctx context.Context, query string, bm25Query string, maxResults int, minScore float64, category string) ([]KnowledgeSearchResult, error)

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

	// Memory and knowledge
	MemoryManager             *memory.Manager               // Persistent memory manager
	MemoryReflector           *MemoryReflector              // Post-task memory reflection (nil = disabled)
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
	CredentialStore   *credentials.Store                            // Credential store for placeholder substitution (nil = disabled)
	PendingSecrets    *credentials.PendingVault                     // Per-session vault for <<<SECRET_N>>> token resolution (nil = disabled)
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

	// Flow output side-channel: large flow outputs are stripped from the
	// tool result (so the LLM doesn't try to summarize them) and stashed
	// here for direct delivery to the user via SSE or channel output.
	pendingFlowOutput string
	flowOutputMu      sync.Mutex
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

// DrainFlowOutput returns and clears any pending flow output that was
// extracted from a run_flow tool result during the current agent run.
// Thread-safe. The SSE handler calls this to deliver the full flow output
// directly to the user without it being re-processed by the chat LLM.
func (c *ChatAgent) DrainFlowOutput() string {
	c.flowOutputMu.Lock()
	defer c.flowOutputMu.Unlock()
	out := c.pendingFlowOutput
	c.pendingFlowOutput = ""
	return out
}

// extractAndStripFlowOutput checks a run_flow tool result for a large "output"
// field. If found, the full output is stashed for direct delivery to the user,
// and replaced with a short pointer so the LLM does not try to summarize it.
// Returns a new map (does not mutate the original).
func (c *ChatAgent) extractAndStripFlowOutput(output map[string]any) map[string]any {
	const minStripLen = 500 // only strip outputs larger than this

	rawOutput, ok := output["output"].(string)
	if !ok || len(rawOutput) <= minStripLen {
		return output
	}

	// Stash the full output for direct delivery
	c.flowOutputMu.Lock()
	c.pendingFlowOutput = rawOutput
	c.flowOutputMu.Unlock()

	// Replace with a pointer — copy the map to avoid mutating the original
	stripped := make(map[string]any, len(output))
	for k, v := range output {
		stripped[k] = v
	}
	stripped["output"] = fmt.Sprintf(
		"[Flow output (%d characters) has been delivered directly to the user's screen. "+
			"Do NOT reproduce, summarize, or paraphrase it — the user already sees the full content. "+
			"Just present the input_options/input_prompt if any, or confirm the flow completed successfully.]",
		len(rawOutput),
	)
	return stripped
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
