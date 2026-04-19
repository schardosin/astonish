package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/schardosin/astonish/pkg/memory"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// minOutputForReflection is the minimum length of final output text for a
// zero-tool-call turn to qualify for reflection. Turns with no tool calls
// AND very short output (e.g., "OK", "Done") are trivial acknowledgments.
const minOutputForReflection = 50

// MemoryReflector runs a silent post-task LLM call to decide whether durable
// knowledge was discovered during the turn and, if so, saves it to memory.
//
// This is the "insurance" layer — the system prompt already tells the model to
// save knowledge after overcoming obstacles (Layer 1), but if it forgets,
// the reflector gives it one more chance after the turn completes.
//
// The reflector uses a knowledge-based gate rather than an activity-based one:
// it runs whenever a turn had meaningful interaction, regardless of tool call
// count. A single save_credential call with rich conversation context (domain,
// project name, auth URL) is just as worthy of reflection as a 14-tool-call
// debugging session.
type MemoryReflector struct {
	LLM           model.LLM
	MemoryManager *memory.Manager
	MemoryStore   MemorySaveStore
	DebugMode     bool
}

// MemorySaveStore is defined in pkg/tools/memory_save.go but we re-declare
// the interface here to avoid an import cycle. The reflector only needs the
// store for passing to the save function.
type MemorySaveStore interface {
	ReindexFile(ctx context.Context, relPath string) error
	Config() *memory.StoreConfig
}

// reflectionPrompt is the system instruction for the reflection LLM call.
const reflectionPrompt = `You are a memory management assistant. Your ONLY job is to decide whether the conversation and task execution below contain durable knowledge worth saving to persistent memory.

Durable knowledge includes:
- Connection details, configuration parameters, or environment-specific information (hostnames, API base URLs, auth methods, credential names, ports)
- Workarounds discovered after initial failures (what failed, why, what worked)
- Non-obvious file paths, API endpoints, configuration patterns
- Shell command quirks, syntax gotchas, tool-specific behaviors
- Integration details (auth flows, required headers, API schemas, credential names)

NOT durable knowledge — NEVER save, even if they appear in tool results:
- Lists of resources that change over time (VMs, containers, pods, databases, storage volumes, user accounts, running processes). These MUST be fetched live each time — saving them creates stale snapshots that will become wrong.
- Resource names/IDs and their current mapping (e.g., "ID 100 = my-server, ID 101 = my-database"). These change when resources are added or removed.
- Current status of any resource (running, stopped, healthy, degraded, etc.)
- Command outputs, log snippets, disk usage, memory usage, or any live metrics
- Trivial factual information with no environment-specific value
- Generic programming concepts unrelated to this specific environment
- Secret values (passwords, tokens, API keys) — NEVER include actual secret values

IMPORTANT: Connection details (hostnames, API base URLs, auth methods, credential names) ARE durable knowledge — save them even if the user provided them. However, the actual CONTENTS retrieved from those connections (resource lists, statuses, query results) are NOT durable and must NOT be saved.

If you find durable knowledge worth saving, call memory_save with:
- category: a short descriptive heading (e.g., "SSH Interactive Login", "Proxmox API Patterns")
- content: the knowledge as concise bullet points
- kind: "tools" for tool quirks/CLI syntax/API patterns, "workarounds" for problems+solutions, "infrastructure" for servers/networking/services, "projects" for project-specific knowledge, "others" for anything else. Omit kind for core MEMORY.md facts (connection details, server info, credentials).

If there is nothing worth saving, respond with exactly: "No durable knowledge to save."

You may call memory_save multiple times if there are distinct categories of knowledge.`

// validationPrompt is the system instruction for the post-extraction validation
// LLM call. This runs after the reflector decides to save, comparing proposed
// content against the existing section to filter out duplicates.
const validationPrompt = `You are a memory deduplication filter. You receive existing section content and proposed new content. Your job is to return ONLY the lines from the proposed content that contain genuinely new information not already covered by the existing content.

Rules:
- If a proposed line contains the same URL, hostname, IP, credential name, file path, endpoint, or configuration value that already appears in the existing content — even with different wording — it is NOT new. Remove it.
- If a proposed line is a rephrased or summarized version of information already present, it is NOT new. Remove it.
- If a proposed line adds a genuinely new fact (a value, detail, or piece of knowledge not present anywhere in the existing content), keep it.
- Return ONLY the new lines as bullet points using "- " prefix, preserving their original wording.
- If NOTHING is genuinely new, respond with exactly: NONE`

// Reflect analyzes the execution trace and conversation context, then optionally
// saves knowledge to memory. It runs a single LLM call with the memory_save tool
// available. If the model decides to save, the saves are executed directly.
// This method is silent — it produces no user-visible output.
//
// The events parameter provides the full session conversation for extracting the
// current turn's user/model exchange. Pass nil to skip conversation context
// (reflection will still see the tool execution trace).
func (r *MemoryReflector) Reflect(ctx context.Context, trace *ExecutionTrace, events session.Events) {
	if r == nil || r.LLM == nil || r.MemoryManager == nil {
		return
	}

	// Gate: skip truly empty turns where nothing meaningful happened.
	// A turn qualifies for reflection if it has ANY tool calls OR produced
	// meaningful output text. This allows credential-only turns (1 tool call)
	// and conversation-heavy turns (no tool calls but rich model output)
	// to be reflected upon.
	totalToolCalls := countToolCallsRecursive(trace)
	if totalToolCalls == 0 && len(trace.FinalOutput) < minOutputForReflection {
		slog.Debug("memory reflection skipped: trivial turn",
			"component", "memory-reflection",
			"toolCalls", 0,
			"outputLen", len(trace.FinalOutput))
		return
	}

	// Check if memory_save was already called during the turn (including sub-agents)
	if traceContainsMemorySave(trace) {
		slog.Debug("memory reflection skipped: memory_save already called during turn", "component", "memory-reflection")
		return
	}

	// Build rich context for the reflection LLM: conversation + tool trace
	reflectionContext := buildReflectionContext(trace, events)

	if r.DebugMode {
		slog.Debug("running memory reflection",
			"component", "memory-reflection",
			"toolCalls", totalToolCalls,
			"contextLen", len(reflectionContext))
	}

	// Build the LLM request with memory_save tool
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{
				Parts: []*genai.Part{{Text: reflectionContext}},
				Role:  "user",
			},
		},
		Config: &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{{Text: reflectionPrompt}},
			},
			Tools: []*genai.Tool{{
				FunctionDeclarations: []*genai.FunctionDeclaration{{
					Name:        "memory_save",
					Description: "Save durable knowledge to persistent memory files.",
					ParametersJsonSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"category": map[string]any{
								"type":        "string",
								"description": "A short category heading (e.g., SSH Interactive Login, Proxmox API, Browser Quirks, Server Configuration)",
							},
							"content": map[string]any{
								"type":        "string",
								"description": "The knowledge to save, as concise bullet points using '- ' prefix",
							},
							"kind": map[string]any{
								"type":        "string",
								"enum":        []any{"tools", "workarounds", "infrastructure", "projects", "others"},
								"description": "Knowledge category. 'tools' for tool quirks/CLI/API patterns, 'workarounds' for problems+solutions, 'infrastructure' for servers/networking, 'projects' for project-specific, 'others' for miscellaneous. Omit for core MEMORY.md facts.",
							},
							"overwrite": map[string]any{
								"type":        "boolean",
								"description": "When true, replaces the entire section instead of appending",
							},
						},
						"required": []string{"category", "content"},
					},
				}},
			}},
		},
	}

	// Make the LLM call (non-streaming)
	var lastResp *model.LLMResponse
	for resp, err := range r.LLM.GenerateContent(ctx, req, false) {
		if err != nil {
			slog.Debug("memory reflection LLM error", "component", "memory-reflection", "error", err)
			return
		}
		lastResp = resp
	}

	if lastResp == nil || lastResp.Content == nil {
		slog.Debug("memory reflection: no response from LLM", "component", "memory-reflection")
		return
	}

	// Process the response — look for memory_save function calls
	saveCount := 0
	for _, part := range lastResp.Content.Parts {
		if part.FunctionCall != nil && part.FunctionCall.Name == "memory_save" {
			r.executeSave(ctx, part.FunctionCall)
			saveCount++
		}
	}

	if saveCount > 0 {
		slog.Debug("memory reflection saved entries", "component", "memory-reflection", "count", saveCount)
	} else {
		slog.Debug("memory reflection: model decided nothing worth saving", "component", "memory-reflection")
	}
}

// executeSave runs a single memory_save call using the MemoryManager directly.
// For append operations (overwrite=false), it validates the proposed content
// against the existing section to filter out duplicates before writing.
func (r *MemoryReflector) executeSave(ctx context.Context, fc *genai.FunctionCall) {
	args := fc.Args
	if args == nil {
		return
	}

	category, _ := args["category"].(string)
	content, _ := args["content"].(string)
	kind, _ := args["kind"].(string)
	overwrite, _ := args["overwrite"].(bool)

	if category == "" || content == "" {
		slog.Debug("memory reflection skipped save: missing category or content", "component", "memory-reflection")
		return
	}

	kind = strings.TrimSpace(strings.ToLower(kind))

	// Resolve the file path based on kind
	var filePath string
	var relPath string
	if kind == "" {
		// Core tier: MEMORY.md
		filePath = r.MemoryManager.Path
	} else {
		rel, ok := memory.KnowledgeFiles[kind]
		if !ok {
			slog.Debug("memory reflection skipped save: invalid kind", "component", "memory-reflection", "kind", kind)
			return
		}
		relPath = rel
		if r.MemoryStore != nil && r.MemoryStore.Config() != nil {
			memDir := r.MemoryStore.Config().MemoryDir
			filePath = filepath.Join(memDir, rel)

			// Cross-file section check
			resolvedPath, resolvedRel := memory.ResolveKnowledgeFile(memDir, memory.KnowledgeFiles, filePath, category)
			filePath = resolvedPath
			if resolvedRel != "" {
				relPath = resolvedRel
			}
		}
	}

	// Validate content against existing section (skip for overwrite or missing path)
	if !overwrite && filePath != "" {
		existingContent, found := memory.GetSectionContent(filePath, category)
		if found && existingContent != "" {
			filtered := r.validateContent(ctx, category, existingContent, content)
			if filtered == "" {
				slog.Debug("memory reflection validation filtered all content",
					"component", "memory-reflection", "category", category)
				return
			}
			content = filtered
		}
	}

	if kind == "" {
		// Core tier: append to MEMORY.md
		err := r.MemoryManager.Append(category, content, overwrite)
		if err != nil {
			slog.Debug("memory reflection failed to save to MEMORY.md", "component", "memory-reflection", "error", err)
			return
		}
		slog.Debug("memory reflection saved to MEMORY.md", "component", "memory-reflection", "category", category)

		// Trigger reindex for MEMORY.md
		if r.MemoryStore != nil {
			go func() {
				if err := r.MemoryStore.ReindexFile(context.Background(), "MEMORY.md"); err != nil {
					slog.Warn("failed to reindex memory file", "file", "MEMORY.md", "error", err)
				}
			}()
		}
	} else {
		// Knowledge tier: write to resolved file using section-aware dedup
		if filePath == "" {
			slog.Debug("memory reflection skipped knowledge tier save: no store configured", "component", "memory-reflection")
			return
		}

		if err := memory.AppendToFile(filePath, category, content, overwrite, r.DebugMode); err != nil {
			slog.Debug("memory reflection failed to write file", "component", "memory-reflection", "file", relPath, "error", err)
			return
		}

		slog.Debug("memory reflection saved to file", "component", "memory-reflection", "file", relPath, "category", category)

		// Trigger reindex
		if r.MemoryStore != nil {
			reindexPath := relPath
			go func() {
				if err := r.MemoryStore.ReindexFile(context.Background(), reindexPath); err != nil {
					slog.Warn("failed to reindex memory file", "file", reindexPath, "error", err)
				}
			}()
		}
	}
}

// validateContent makes a small LLM call to compare proposed content against
// existing section content and returns only genuinely new lines. Returns empty
// string if nothing is new.
func (r *MemoryReflector) validateContent(ctx context.Context, category, existingContent, proposedContent string) string {
	userPrompt := fmt.Sprintf("## Existing content in section %q:\n%s\n\n## Proposed new content:\n%s",
		category, existingContent, proposedContent)

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{
				Parts: []*genai.Part{{Text: userPrompt}},
				Role:  "user",
			},
		},
		Config: &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{{Text: validationPrompt}},
			},
		},
	}

	var lastResp *model.LLMResponse
	for resp, err := range r.LLM.GenerateContent(ctx, req, false) {
		if err != nil {
			slog.Debug("memory validation LLM error", "component", "memory-reflection", "error", err)
			// On error, allow the original content through as a fallback
			return proposedContent
		}
		lastResp = resp
	}

	if lastResp == nil || lastResp.Content == nil {
		return proposedContent
	}

	// Extract text response
	var responseText string
	for _, part := range lastResp.Content.Parts {
		if part.Text != "" {
			responseText = strings.TrimSpace(part.Text)
			break
		}
	}

	if responseText == "" || strings.EqualFold(responseText, "NONE") {
		return ""
	}

	if r.DebugMode {
		slog.Debug("memory validation filtered content",
			"component", "memory-reflection",
			"category", category,
			"original_len", len(proposedContent),
			"filtered_len", len(responseText))
	}

	return responseText
}

// countToolCallsRecursive returns the total number of tool calls in the trace,
// including any sub-agent traces attached to delegate_tasks steps.
func countToolCallsRecursive(trace *ExecutionTrace) int {
	if trace == nil {
		return 0
	}
	trace.mu.Lock()
	steps := make([]TraceStep, len(trace.Steps))
	copy(steps, trace.Steps)
	trace.mu.Unlock()

	count := len(steps)
	for _, step := range steps {
		for _, child := range step.SubAgentTraces {
			count += countToolCallsRecursive(child)
		}
	}
	return count
}

// traceContainsMemorySave checks whether memory_save was called in the trace
// or in any sub-agent traces (recursively).
func traceContainsMemorySave(trace *ExecutionTrace) bool {
	if trace == nil {
		return false
	}
	trace.mu.Lock()
	steps := make([]TraceStep, len(trace.Steps))
	copy(steps, trace.Steps)
	trace.mu.Unlock()

	for _, step := range steps {
		if step.ToolName == "memory_save" {
			return true
		}
		for _, child := range step.SubAgentTraces {
			if traceContainsMemorySave(child) {
				return true
			}
		}
	}
	return false
}

// buildReflectionContext creates a rich context for the reflection LLM prompt,
// including the conversation exchange and tool execution trace with sub-agent
// details.
func buildReflectionContext(trace *ExecutionTrace, events session.Events) string {
	var sb strings.Builder

	// Section 1: Conversation context from session events
	if events != nil {
		userText, modelText := extractCurrentTurnConversation(events)
		if userText != "" || modelText != "" {
			sb.WriteString("## Conversation Context\n\n")
			if userText != "" {
				if len(userText) > 2000 {
					userText = userText[:1997] + "..."
				}
				sb.WriteString(fmt.Sprintf("**User:** %s\n\n", userText))
			}
			if modelText != "" {
				if len(modelText) > 3000 {
					modelText = modelText[:2997] + "..."
				}
				sb.WriteString(fmt.Sprintf("**Agent:** %s\n\n", modelText))
			}
		}
	}

	// Section 2: Tool execution trace
	totalCalls := countToolCallsRecursive(trace)
	sb.WriteString("## Tool Execution Trace\n\n")
	sb.WriteString(fmt.Sprintf("**User Request:** %s\n\n", trace.UserRequest))
	sb.WriteString(fmt.Sprintf("**Total Tool Calls:** %d\n\n", totalCalls))

	if len(trace.Steps) > 0 {
		sb.WriteString("### Steps:\n")
		writeTraceSteps(&sb, trace.Steps, 0)
	}

	// Section 3: Final output
	if trace.FinalOutput != "" {
		output := trace.FinalOutput
		if len(output) > 2000 {
			output = output[:1997] + "..."
		}
		sb.WriteString(fmt.Sprintf("\n### Final Response (truncated):\n%s\n", output))
	}

	return sb.String()
}

// writeTraceSteps writes trace steps to the string builder, recursing into
// sub-agent traces with indentation.
func writeTraceSteps(sb *strings.Builder, steps []TraceStep, depth int) {
	indent := strings.Repeat("  ", depth)
	for i, step := range steps {
		status := "OK"
		if !step.Success {
			status = fmt.Sprintf("FAILED: %s", step.Error)
		}

		// Include args summary (truncated)
		argsSummary := ""
		if step.ToolArgs != nil {
			argsBytes, _ := json.Marshal(step.ToolArgs)
			argsSummary = string(argsBytes)
			if len(argsSummary) > 200 {
				argsSummary = argsSummary[:197] + "..."
			}
		}

		sb.WriteString(fmt.Sprintf("%s%d. **%s** [%s] %s\n", indent, i+1, step.ToolName, status, argsSummary))

		// Recurse into sub-agent traces
		for _, child := range step.SubAgentTraces {
			if child == nil {
				continue
			}
			name := "sub-agent"
			if child.UserRequest != "" {
				name = child.UserRequest
				if len(name) > 80 {
					name = name[:77] + "..."
				}
			}
			sb.WriteString(fmt.Sprintf("%s   Sub-agent: %s\n", indent, name))

			child.mu.Lock()
			childSteps := make([]TraceStep, len(child.Steps))
			copy(childSteps, child.Steps)
			child.mu.Unlock()

			writeTraceSteps(sb, childSteps, depth+1)

			// Include sub-agent final output if available
			if child.FinalOutput != "" {
				output := child.FinalOutput
				if len(output) > 500 {
					output = output[:497] + "..."
				}
				sb.WriteString(fmt.Sprintf("%s   Sub-agent result: %s\n", indent, output))
			}
		}
	}
}

// extractCurrentTurnConversation extracts the user message(s) and model text
// responses from the current turn (the most recent user message to end of session).
// Returns (userText, modelText).
func extractCurrentTurnConversation(events session.Events) (string, string) {
	if events == nil || events.Len() == 0 {
		return "", ""
	}

	n := events.Len()

	// Walk backwards to find the last user event
	lastUserIdx := -1
	for i := n - 1; i >= 0; i-- {
		ev := events.At(i)
		if ev.Author == "user" {
			lastUserIdx = i
			break
		}
	}
	if lastUserIdx < 0 {
		return "", ""
	}

	// Collect user text from the user event
	var userParts []string
	userEvent := events.At(lastUserIdx)
	if userEvent.Content != nil {
		for _, part := range userEvent.Content.Parts {
			if part.Text != "" {
				userParts = append(userParts, part.Text)
			}
		}
	}

	// Walk forward from user event to collect model text responses.
	// Skip function calls, function responses, and thought parts —
	// we want the agent's conversational text that summarizes what
	// it did and what information it collected.
	var modelParts []string
	for i := lastUserIdx + 1; i < n; i++ {
		ev := events.At(i)
		if ev.Content == nil {
			continue
		}
		// Only collect from model/agent events
		if ev.Author == "user" {
			break // shouldn't happen, but guard against it
		}
		for _, part := range ev.Content.Parts {
			if part.Text != "" && !part.Thought && part.FunctionCall == nil && part.FunctionResponse == nil {
				modelParts = append(modelParts, part.Text)
			}
		}
	}

	return strings.Join(userParts, "\n"), strings.Join(modelParts, "\n")
}
