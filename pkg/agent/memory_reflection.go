package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/schardosin/astonish/pkg/memory"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// minToolCallsForReflection is the minimum number of tool calls in a turn
// before memory reflection activates. Trivial tasks (1-2 tool calls) rarely
// produce knowledge worth saving.
const minToolCallsForReflection = 3

// MemoryReflector runs a silent post-task LLM call to decide whether durable
// knowledge was discovered during the turn and, if so, saves it to memory.
//
// This is the "insurance" layer — the system prompt already tells the model to
// save knowledge after overcoming obstacles (Layer 1), but if it forgets,
// the reflector gives it one more chance after the turn completes.
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
const reflectionPrompt = `You are a memory management assistant. Your ONLY job is to decide whether the task execution trace below contains durable knowledge worth saving to persistent memory.

Durable knowledge includes:
- Workarounds discovered after initial failures (what failed, why, what worked)
- Non-obvious file paths, API endpoints, configuration patterns
- Shell command quirks, syntax gotchas, tool-specific behaviors
- Integration details (auth flows, required headers, API schemas)

NOT durable knowledge (do NOT save):
- Command outputs, resource lists, current status, or anything ephemeral
- Information the user explicitly provided (they already know it)
- Standard/well-documented procedures that any developer would know
- Results that change over time (IPs of dynamic resources, pod names, etc.)

If you find durable knowledge worth saving, call memory_save with:
- category: a short descriptive heading
- content: the knowledge as concise bullet points
- file: a topic-specific path like "tools/sap-ai-core.md" or "workarounds/browser-scraping.md"

If there is nothing worth saving, respond with exactly: "No durable knowledge to save."

You may call memory_save multiple times if there are distinct categories of knowledge.`

// Reflect analyzes the execution trace and optionally saves knowledge to memory.
// It runs a single LLM call with the memory_save tool available. If the model
// decides to save, the saves are executed directly. This method is silent — it
// produces no user-visible output.
func (r *MemoryReflector) Reflect(ctx context.Context, trace *ExecutionTrace) {
	if r == nil || r.LLM == nil || r.MemoryManager == nil {
		return
	}

	// Check qualification
	toolCallCount := trace.ToolCallCount()
	if toolCallCount < minToolCallsForReflection {
		if r.DebugMode {
			fmt.Printf("[Memory Reflection] Skipped: only %d tool calls (threshold: %d)\n",
				toolCallCount, minToolCallsForReflection)
		}
		return
	}

	// Check if memory_save was already called during the turn
	for _, step := range trace.Steps {
		if step.ToolName == "memory_save" {
			if r.DebugMode {
				fmt.Println("[Memory Reflection] Skipped: memory_save already called during turn")
			}
			return
		}
	}

	// Build compact trace summary for the reflection prompt
	traceSummary := buildTraceSummary(trace)

	if r.DebugMode {
		fmt.Printf("[Memory Reflection] Running reflection on %d tool calls\n", toolCallCount)
	}

	// Build the LLM request with memory_save tool
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{
				Parts: []*genai.Part{{Text: traceSummary}},
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
								"description": "A short category heading (e.g., Workarounds, API Patterns, Tool Quirks)",
							},
							"content": map[string]any{
								"type":        "string",
								"description": "The knowledge to save, as concise bullet points using '- ' prefix",
							},
							"file": map[string]any{
								"type":        "string",
								"description": "Target file relative to memory dir (e.g., 'tools/sap-ai-core.md', 'workarounds/browser.md')",
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
			if r.DebugMode {
				fmt.Printf("[Memory Reflection] LLM error: %v\n", err)
			}
			return
		}
		lastResp = resp
	}

	if lastResp == nil || lastResp.Content == nil {
		if r.DebugMode {
			fmt.Println("[Memory Reflection] No response from LLM")
		}
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

	if r.DebugMode {
		if saveCount > 0 {
			fmt.Printf("[Memory Reflection] Saved %d knowledge entries\n", saveCount)
		} else {
			fmt.Println("[Memory Reflection] Model decided nothing worth saving")
		}
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
		if r.DebugMode {
			fmt.Println("[Memory Reflection] Skipped save: missing category or content")
		}
		return
	}

	// Use the same MemorySave function from the tools package, but call
	// the manager directly to avoid import cycles.
	targetFile := strings.TrimSpace(file)

	if targetFile == "" || strings.EqualFold(targetFile, "MEMORY.md") {
		// Core tier: append to MEMORY.md
		err := r.MemoryManager.Append(category, content, overwrite)
		if err != nil {
			if r.DebugMode {
				fmt.Printf("[Memory Reflection] Failed to save to MEMORY.md: %v\n", err)
			}
			return
		}
		if r.DebugMode {
			fmt.Printf("[Memory Reflection] Saved to MEMORY.md under '%s'\n", category)
		}

		// Trigger reindex for MEMORY.md
		if r.MemoryStore != nil {
			go func() {
				_ = r.MemoryStore.ReindexFile(context.Background(), "MEMORY.md")
			}()
		}
	} else {
		// Knowledge tier: write to specific file
		if r.MemoryStore == nil || r.MemoryStore.Config() == nil {
			if r.DebugMode {
				fmt.Println("[Memory Reflection] Skipped knowledge tier save: no store configured")
			}
			return
		}

		memDir := r.MemoryStore.Config().MemoryDir
		absPath := filepath.Join(memDir, targetFile)

		// Ensure parent directory exists
		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			if r.DebugMode {
				fmt.Printf("[Memory Reflection] Failed to create directory %s: %v\n", dir, err)
			}
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
				if r.DebugMode {
					fmt.Printf("[Memory Reflection] Failed to write %s: %v\n", targetFile, err)
				}
				return
			}
		} else {
			f, err := os.OpenFile(absPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				if r.DebugMode {
					fmt.Printf("[Memory Reflection] Failed to append to %s: %v\n", targetFile, err)
				}
				return
			}
			_, err = f.WriteString(sb.String())
			f.Close()
			if err != nil {
				if r.DebugMode {
					fmt.Printf("[Memory Reflection] Failed to append to %s: %v\n", targetFile, err)
				}
				return
			}
		}

		if r.DebugMode {
			fmt.Printf("[Memory Reflection] Saved to %s under '%s'\n", targetFile, category)
		}

		// Trigger reindex
		if r.MemoryStore != nil {
			go func() {
				_ = r.MemoryStore.ReindexFile(context.Background(), targetFile)
			}()
		}
	}
}

// buildTraceSummary creates a compact summary of the execution trace for
// the reflection LLM prompt.
func buildTraceSummary(trace *ExecutionTrace) string {
	var sb strings.Builder

	sb.WriteString("## Task Execution Trace\n\n")
	sb.WriteString(fmt.Sprintf("**User Request:** %s\n\n", trace.UserRequest))
	sb.WriteString(fmt.Sprintf("**Tool Calls:** %d\n\n", len(trace.Steps)))

	// List each step with success/failure
	sb.WriteString("### Steps:\n")
	for i, step := range trace.Steps {
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

		sb.WriteString(fmt.Sprintf("%d. **%s** [%s] %s\n", i+1, step.ToolName, status, argsSummary))
	}

	// Include final output summary
	if trace.FinalOutput != "" {
		output := trace.FinalOutput
		if len(output) > 1000 {
			output = output[:997] + "..."
		}
		sb.WriteString(fmt.Sprintf("\n### Final Response (truncated):\n%s\n", output))
	}

	return sb.String()
}
