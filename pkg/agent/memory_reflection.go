package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
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
- Connection details, configuration parameters, or environment-specific information collected during conversation (domains, project names, auth URLs, regions, API endpoints, service types, hostnames, ports, usernames)
- Workarounds discovered after initial failures (what failed, why, what worked)
- Non-obvious file paths, API endpoints, configuration patterns
- Shell command quirks, syntax gotchas, tool-specific behaviors
- Integration details (auth flows, required headers, API schemas, credential names)

NOT durable knowledge (do NOT save):
- Command outputs, resource lists, current status, or anything ephemeral
- Trivial factual information the user provided that has no environment-specific value (e.g., explaining how a command works)
- Generic programming concepts unrelated to this specific environment or toolchain
- Results that change over time (IPs of dynamic resources, pod names, etc.)
- Secret values (passwords, tokens, API keys) — NEVER include actual secret values

IMPORTANT: When connection details, configuration parameters, or credential metadata were exchanged in the conversation, those ARE durable knowledge — save them even if the user provided them. Future sessions need this context to avoid re-asking for the same information.

If you find durable knowledge worth saving, call memory_save with:
- category: a short descriptive heading
- content: the knowledge as concise bullet points
- file: "MEMORY.md" for core connection/environment facts, or a topic-specific path like "tools/sap-ai-core.md" or "workarounds/browser-scraping.md" for procedural knowledge

If there is nothing worth saving, respond with exactly: "No durable knowledge to save."

You may call memory_save multiple times if there are distinct categories of knowledge.`

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
								"description": "A short category heading (e.g., Workarounds, API Patterns, Tool Quirks, Server Configuration)",
							},
							"content": map[string]any{
								"type":        "string",
								"description": "The knowledge to save, as concise bullet points using '- ' prefix",
							},
							"file": map[string]any{
								"type":        "string",
								"description": "Target file relative to memory dir. Use 'MEMORY.md' for core facts (connection details, server info), or a topic-specific path like 'tools/sap-ai-core.md' or 'workarounds/browser.md' for procedural knowledge.",
							},
							"overwrite": map[string]any{
								"type":        "boolean",
								"description": "When true, replaces the entire section instead of appending",
							},
						},
						"required": []string{"category", "content", "file"},
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
func (r *MemoryReflector) executeSave(ctx context.Context, fc *genai.FunctionCall) {
	args := fc.Args
	if args == nil {
		return
	}

	category, _ := args["category"].(string)
	content, _ := args["content"].(string)
	file, _ := args["file"].(string)
	overwrite, _ := args["overwrite"].(bool)

	if category == "" || content == "" {
		slog.Debug("memory reflection skipped save: missing category or content", "component", "memory-reflection")
		return
	}

	// Use the same MemorySave function from the tools package, but call
	// the manager directly to avoid import cycles.
	targetFile := strings.TrimSpace(file)

	if targetFile == "" || strings.EqualFold(targetFile, "MEMORY.md") {
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
		// Knowledge tier: write to specific file
		if r.MemoryStore == nil || r.MemoryStore.Config() == nil {
			slog.Debug("memory reflection skipped knowledge tier save: no store configured", "component", "memory-reflection")
			return
		}

		memDir := r.MemoryStore.Config().MemoryDir
		absPath := filepath.Join(memDir, targetFile)

		// Ensure parent directory exists
		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			slog.Debug("memory reflection failed to create directory", "component", "memory-reflection", "dir", dir, "error", err)
			return
		}

		// Build content with category heading
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("\n## %s\n\n", category))
		sb.WriteString(content)
		sb.WriteString("\n")

		// Append or overwrite
		if overwrite {
			if err := os.WriteFile(absPath, []byte(sb.String()), 0644); err != nil {
				slog.Debug("memory reflection failed to write file", "component", "memory-reflection", "file", targetFile, "error", err)
				return
			}
		} else {
			f, err := os.OpenFile(absPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				slog.Debug("memory reflection failed to open file for append", "component", "memory-reflection", "file", targetFile, "error", err)
				return
			}
			_, err = f.WriteString(sb.String())
			f.Close()
			if err != nil {
				slog.Debug("memory reflection failed to append to file", "component", "memory-reflection", "file", targetFile, "error", err)
				return
			}
		}

		slog.Debug("memory reflection saved to file", "component", "memory-reflection", "file", targetFile, "category", category)

		// Trigger reindex
		if r.MemoryStore != nil {
			go func() {
				if err := r.MemoryStore.ReindexFile(context.Background(), targetFile); err != nil {
					slog.Warn("failed to reindex memory file", "file", targetFile, "error", err)
				}
			}()
		}
	}
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
// details. This replaces the simpler buildTraceSummary.
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
