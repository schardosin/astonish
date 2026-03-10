package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// --- opencode tool ---

// summarizeThresholdBytes is the minimum output size before summarization kicks in.
// Outputs smaller than this are returned as-is (e.g., short explore results).
const summarizeThresholdBytes = 4 * 1024 // 4KB

// summarizeInputCapBytes is the maximum raw output sent to the summarization LLM.
// Even a 200KB output can be effectively summarized from the first 30KB.
const summarizeInputCapBytes = 30 * 1024 // 30KB

// maxOutputStoredBytes caps the Output field stored in the ADK FunctionResponse.
// This is a safety net: even after LLM summarization and fallback truncation,
// we enforce a hard limit. The agent only needs a brief summary of what happened;
// OpenCode retains full context via session_id continuation.
const maxOutputStoredBytes = 8 * 1024 // 8KB

// openCodeSummarizer is an optional LLM function used to summarize verbose
// OpenCode responses. When set, large outputs are replaced with concise summaries
// so the calling agent's session context stays lean. OpenCode's own session
// (accessible via session_id continuation) retains the full context internally.
var openCodeSummarizer func(ctx context.Context, prompt string) (string, error)

// SetOpenCodeSummarizer registers an LLM function for summarizing OpenCode
// responses. Call this during launcher initialization with the same LLM used
// by the compactor/distiller.
func SetOpenCodeSummarizer(fn func(ctx context.Context, prompt string) (string, error)) {
	openCodeSummarizer = fn
}

// OpenCodeArgs is the input schema for the opencode tool.
type OpenCodeArgs struct {
	Task      string `json:"task" jsonschema:"The task description for OpenCode. Be specific and include all context needed."`
	Dir       string `json:"dir" jsonschema:"Working directory for OpenCode. This should be the project root."`
	SessionID string `json:"session_id,omitempty" jsonschema:"Optional: continue an existing OpenCode session by its ID. Use this for follow-up tasks in the same context."`
	Model     string `json:"model,omitempty" jsonschema:"Optional: override the model in provider/model format (e.g., 'bifrost/sapaicore/anthropic--claude-4.6-opus')."`
	Agent     string `json:"agent,omitempty" jsonschema:"Optional: OpenCode agent to use. Default is 'build' (full tool access). Other options: 'explore' (read-only)."`
}

// OpenCodeResult is the output of the opencode tool.
type OpenCodeResult struct {
	Status    string               `json:"status"`               // "success", "error", "timeout"
	Output    string               `json:"output"`               // Text output from OpenCode
	SessionID string               `json:"session_id,omitempty"` // Session ID for continuation
	Error     string               `json:"error,omitempty"`      // Error message
	Tokens    map[string]any       `json:"tokens,omitempty"`     // Token usage from last step
	Trace     []OpenCodeTraceEvent `json:"trace,omitempty"`      // Execution trace for replay on reload
}

// OpenCodeTraceEvent is a lightweight record of an OpenCode event, stored in the
// tool result so fleet reconstruction can replay opencode_* events on page reload.
type OpenCodeTraceEvent struct {
	Type    string `json:"type"`              // opencode_text, opencode_tool_call, opencode_tool_result, opencode_step_start, opencode_step_finish
	Detail  string `json:"detail,omitempty"`  // Tool name for tool_call/tool_result
	Message string `json:"message,omitempty"` // Summary message
	Text    string `json:"text,omitempty"`    // Full text content (tool args, output, LLM text)
}

// openCodeBinaryPath caches the resolved path to the opencode binary.
var openCodeBinaryPath string

// FindOpenCodeBinary locates the opencode binary.
func FindOpenCodeBinary() (string, error) {
	if openCodeBinaryPath != "" {
		return openCodeBinaryPath, nil
	}

	// Check PATH first
	if p, err := exec.LookPath("opencode"); err == nil {
		openCodeBinaryPath = p
		return p, nil
	}

	// Check known locations
	knownPaths := []string{
		os.ExpandEnv("$HOME/.opencode/bin/opencode"),
		os.ExpandEnv("$HOME/.local/share/opencode/bin/opencode"),
		os.ExpandEnv("$HOME/.local/bin/opencode"),
	}
	for _, p := range knownPaths {
		if _, err := os.Stat(p); err == nil {
			openCodeBinaryPath = p
			return p, nil
		}
	}

	return "", fmt.Errorf("opencode binary not found in PATH or known locations")
}

// openCodeEvent represents a single JSON event from opencode --format json output.
//
// Event types and their key fields:
//   - step_start: part.type="step-start"
//   - text:       part.type="text", part.text="..."
//   - tool_use:   part.type="tool", part.tool="write", part.state.status="completed",
//     part.state.input={object}, part.state.output="string or object"
//   - step_finish: part.type="step-finish", part.reason="stop"|"tool-calls",
//     part.tokens={total,input,output,...}
type openCodeEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionID"`
	Part      struct {
		Type   string         `json:"type"`
		Text   string         `json:"text"`
		Reason string         `json:"reason"`
		Tokens map[string]any `json:"tokens"`
		Tool   string         `json:"tool"`
		State  struct {
			Input  any    `json:"input"`  // string or object (tool args)
			Output any    `json:"output"` // string or object (tool result)
			Status string `json:"status"` // "completed", "error", "running"
		} `json:"state"`
	} `json:"part"`
}

// formatOpenCodeValue converts an any value (string or object) to a readable string
// for progress event display.
func formatOpenCodeValue(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func runOpenCode(ctx tool.Context, args OpenCodeArgs) (OpenCodeResult, error) {
	if strings.TrimSpace(args.Task) == "" {
		return OpenCodeResult{
			Status: "error",
			Error:  "Task description is required.",
		}, nil
	}

	if strings.TrimSpace(args.Dir) == "" {
		return OpenCodeResult{
			Status: "error",
			Error:  "Working directory (dir) is required.",
		}, nil
	}

	binary, err := FindOpenCodeBinary()
	if err != nil {
		return OpenCodeResult{
			Status: "error",
			Error:  err.Error(),
		}, nil
	}

	// OpenCode tasks (especially in fleet sessions) routinely need 10-30 minutes
	// for complex multi-step work. The timeout is hardcoded because the LLM
	// consistently underestimates execution time when given control over it.
	const openCodeTimeout = 45 * 60 // 45 minutes in seconds

	// Build command arguments
	// Global flags go before "run", subcommand flags go after "run"
	var cmdArgs []string

	// Global flags
	if args.Agent != "" {
		cmdArgs = append(cmdArgs, "--agent", args.Agent)
	}

	cmdArgs = append(cmdArgs, "run")

	// Subcommand flags
	cmdArgs = append(cmdArgs, "--format", "json")
	cmdArgs = append(cmdArgs, "--dir", expandPath(args.Dir))

	if args.SessionID != "" {
		cmdArgs = append(cmdArgs, "--session", args.SessionID)
	}
	if args.Model != "" {
		cmdArgs = append(cmdArgs, "--model", args.Model)
	}

	// The task goes as positional arguments
	cmdArgs = append(cmdArgs, args.Task)

	// Create command with timeout
	cmdCtx, cancel := context.WithTimeout(context.Background(), time.Duration(openCodeTimeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, binary, cmdArgs...)
	cmd.Dir = expandPath(args.Dir)

	// Inherit full environment from the parent process.
	// This ensures API keys (BIFROST_API_KEY, etc.) are available.
	cmd.Env = os.Environ()

	// Capture stderr for error reporting
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	// Get stdout pipe for streaming JSON events
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return OpenCodeResult{
			Status: "error",
			Error:  fmt.Sprintf("Failed to create stdout pipe: %v", err),
		}, nil
	}

	if err := cmd.Start(); err != nil {
		return OpenCodeResult{
			Status: "error",
			Error:  fmt.Sprintf("Failed to start opencode: %v", err),
		}, nil
	}

	// Parse NDJSON events from stdout
	var textParts []string
	var sessionID string
	var tokens map[string]any
	var trace []OpenCodeTraceEvent

	scanner := bufio.NewScanner(stdout)
	// Increase buffer size for large outputs
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var evt openCodeEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue // Skip malformed lines
		}

		// Capture session ID from any event
		if evt.SessionID != "" {
			sessionID = evt.SessionID
		}

		// Collect text output
		if evt.Type == "text" && evt.Part.Text != "" {
			textParts = append(textParts, evt.Part.Text)
		}

		// Capture token usage from step_finish
		if evt.Type == "step_finish" && evt.Part.Tokens != nil {
			tokens = evt.Part.Tokens
		}

		// Build execution trace for replay
		switch evt.Type {
		case "text":
			if evt.Part.Text != "" {
				trace = append(trace, OpenCodeTraceEvent{
					Type:    "opencode_text",
					Message: truncateString(evt.Part.Text, 500),
					Text:    evt.Part.Text,
				})
			}
		case "tool_use":
			toolName := evt.Part.Tool
			if toolName == "" {
				toolName = "unknown"
			}
			status := evt.Part.State.Status
			inputStr := formatOpenCodeValue(evt.Part.State.Input)
			outputStr := formatOpenCodeValue(evt.Part.State.Output)

			trace = append(trace, OpenCodeTraceEvent{
				Type:    "opencode_tool_call",
				Detail:  toolName,
				Message: fmt.Sprintf("OpenCode calling: %s", toolName),
				Text:    inputStr,
			})
			if status == "completed" || status == "error" {
				trace = append(trace, OpenCodeTraceEvent{
					Type:    "opencode_tool_result",
					Detail:  toolName,
					Message: fmt.Sprintf("OpenCode %s returned (%s)", toolName, status),
					Text:    outputStr,
				})
			}
		case "step_start":
			trace = append(trace, OpenCodeTraceEvent{
				Type:    "opencode_step_start",
				Message: "OpenCode step started",
			})
		case "step_finish":
			reason := evt.Part.Reason
			if reason == "" {
				reason = "completed"
			}
			trace = append(trace, OpenCodeTraceEvent{
				Type:    "opencode_step_finish",
				Message: fmt.Sprintf("OpenCode step finished: %s", reason),
			})
		}
	}

	// Wait for the command to finish
	waitErr := cmd.Wait()

	// Build raw output from collected text parts
	rawOutput := strings.Join(textParts, "")

	// Summarize output for all return paths. The summarizer replaces verbose
	// output with a concise summary so the calling agent's ADK session stays
	// lean. OpenCode's own session (accessible via session_id continuation)
	// retains the full context internally.
	summarizedOutput := summarizeOpenCodeOutput(ctx, rawOutput, "success", sessionID)

	// Strip trace to a lightweight summary. The full trace was needed for UI
	// replay but was the #1 cause of context bloat: each opencode call stored
	// 60-140KB of tool inputs/outputs in the ADK session. After 5-7 calls the
	// accumulated trace data alone hit 400-800KB, causing LLM requests to
	// timeout. The stripped trace keeps structural events (step counts, tool
	// names) without the full text content.
	leanTrace := stripTraceVerboseContent(trace)

	// Check for timeout
	if cmdCtx.Err() == context.DeadlineExceeded {
		summarizedOutput = summarizeOpenCodeOutput(ctx, rawOutput, "timeout", sessionID)
		return OpenCodeResult{
			Status:    "timeout",
			Output:    capOutput(summarizedOutput),
			SessionID: sessionID,
			Error:     fmt.Sprintf("OpenCode timed out after %d seconds", openCodeTimeout),
			Tokens:    tokens,
			Trace:     leanTrace,
		}, nil
	}

	// Check for process error
	if waitErr != nil {
		errMsg := stderrBuf.String()
		if errMsg == "" {
			errMsg = waitErr.Error()
		}
		// Still return any output we collected before the error
		if rawOutput == "" {
			summarizedOutput = errMsg
		} else {
			summarizedOutput = summarizeOpenCodeOutput(ctx, rawOutput, "error", sessionID)
		}
		return OpenCodeResult{
			Status:    "error",
			Output:    capOutput(summarizedOutput),
			SessionID: sessionID,
			Error:     strings.TrimSpace(errMsg),
			Tokens:    tokens,
			Trace:     leanTrace,
		}, nil
	}

	if summarizedOutput == "" {
		summarizedOutput = "(no text output)"
	}

	return OpenCodeResult{
		Status:    "success",
		Output:    capOutput(summarizedOutput),
		SessionID: sessionID,
		Tokens:    tokens,
		Trace:     leanTrace,
	}, nil
}

// summarizeOpenCodeOutput replaces a verbose OpenCode output with a concise
// LLM-generated summary. This is critical for keeping the calling agent's
// session context lean: OpenCode retains full context internally (via session_id
// continuation), so the caller only needs to know what happened, not every detail.
//
// Returns the original output unchanged if:
//   - No summarizer is registered
//   - Output is below the threshold (small enough to keep as-is)
//   - The summarization LLM call fails (falls back to simple truncation)
func summarizeOpenCodeOutput(ctx context.Context, rawOutput, status, sessionID string) string {
	if openCodeSummarizer == nil {
		return rawOutput
	}
	if len(rawOutput) < summarizeThresholdBytes {
		return rawOutput
	}

	// Cap the input to the summarizer to avoid sending a huge prompt
	inputForSummary := rawOutput
	if len(inputForSummary) > summarizeInputCapBytes {
		inputForSummary = inputForSummary[:summarizeInputCapBytes] + "\n\n[... output truncated for summarization ...]"
	}

	prompt := fmt.Sprintf(`Summarize the following OpenCode session output concisely.

Preserve these details exactly:
- Files created or modified (with full paths)
- Test results (pass/fail counts, specific failure messages if any)
- Git commits made (commit messages and short hashes)
- Errors or warnings encountered
- Key decisions or outcomes

Keep the summary under 500 words. Do not include raw source code or full file contents.
Write in past tense as a factual report of what was accomplished.

OpenCode session status: %s
OpenCode session ID: %s

--- OpenCode Output ---
%s`, status, sessionID, inputForSummary)

	// Use a short timeout for the summarization call
	summaryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	summary, err := openCodeSummarizer(summaryCtx, prompt)
	if err != nil {
		log.Printf("[opencode-summarize] LLM summarization failed, using fallback: %v", err)
		return fallbackSummarize(rawOutput)
	}

	if strings.TrimSpace(summary) == "" {
		log.Printf("[opencode-summarize] LLM returned empty summary, using fallback")
		return fallbackSummarize(rawOutput)
	}

	log.Printf("[opencode-summarize] Summarized %d bytes to %d bytes", len(rawOutput), len(summary))
	return summary
}

// fallbackSummarize creates a simple truncated summary when the LLM is unavailable.
// Keeps the first 2KB (usually contains the plan/approach) and last 1KB (usually
// contains the final result/commit message).
func fallbackSummarize(rawOutput string) string {
	const headSize = 2048
	const tailSize = 1024

	if len(rawOutput) <= headSize+tailSize+100 {
		return rawOutput
	}

	head := rawOutput[:headSize]
	tail := rawOutput[len(rawOutput)-tailSize:]
	return head + "\n\n[... output summarized: " +
		fmt.Sprintf("%d bytes omitted", len(rawOutput)-headSize-tailSize) +
		" ...]\n\n" + tail
}

// capOutput enforces a hard limit on the output stored in the ADK FunctionResponse.
// This is the final safety net: even if the LLM summarizer produces a long summary
// or falls back to truncation, the stored output stays within budget.
func capOutput(output string) string {
	if len(output) <= maxOutputStoredBytes {
		return output
	}
	// Keep the first portion and append a truncation notice
	return output[:maxOutputStoredBytes] + "\n\n[... capped at 8KB for context budget ...]"
}

// stripTraceVerboseContent removes the full text content from trace events,
// keeping only the structural events (step_start, step_finish) and tool call
// summaries (name only, no full input/output). This prevents the trace from
// bloating the tool response stored in the ADK session history.
func stripTraceVerboseContent(trace []OpenCodeTraceEvent) []OpenCodeTraceEvent {
	var stripped []OpenCodeTraceEvent
	for _, evt := range trace {
		switch evt.Type {
		case "opencode_step_start", "opencode_step_finish":
			// Keep structural events as-is (they have no large text)
			stripped = append(stripped, evt)
		case "opencode_tool_call":
			// Keep tool name but drop full input text
			stripped = append(stripped, OpenCodeTraceEvent{
				Type:    evt.Type,
				Detail:  evt.Detail,
				Message: evt.Message,
			})
		case "opencode_tool_result":
			// Keep tool name and status but drop full output text
			stripped = append(stripped, OpenCodeTraceEvent{
				Type:    evt.Type,
				Detail:  evt.Detail,
				Message: evt.Message,
			})
		case "opencode_text":
			// Keep a short preview of text events
			stripped = append(stripped, OpenCodeTraceEvent{
				Type:    evt.Type,
				Message: evt.Message,
			})
		}
	}
	return stripped
}

// NewOpenCodeTool creates the opencode tool for fleet agent delegation.
func NewOpenCodeTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "opencode",
		Description: `Delegate a task to OpenCode, an AI-powered coding agent.

OpenCode can read files, write files, run shell commands, search code, and perform complex
software engineering tasks autonomously. It operates in the specified working directory and
has full access to the filesystem and terminal.

Use this tool when you need to:
- Write or modify code
- Run builds, tests, or other commands
- Perform complex multi-step implementations
- Search and analyze codebases

Arguments:
- task: Detailed description of what OpenCode should do. Include all context, file paths,
  requirements, and constraints. Be specific and thorough.
- dir: The project's working directory (e.g., ~/myproject)
- session_id: Optional. Continue a previous OpenCode session for follow-up work.
- model: Optional. Override the AI model (provider/model format).
- agent: Optional. OpenCode agent type. Default 'build' has full tool access.
- timeout: Optional. Seconds to wait (default: 300, max: 3600).

Tips:
- Be explicit about file paths and expected outputs
- Include relevant context (requirements, design decisions, conventions)
- For follow-up tasks, use the session_id from a previous result
- Set a higher timeout for complex implementations (e.g., 600)`,
	}, runOpenCode)
}
