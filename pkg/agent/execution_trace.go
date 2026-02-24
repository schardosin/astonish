package agent

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// ExecutionTrace records tool calls and LLM reasoning during chat execution.
// Used for flow distillation -- the trace captures what actually happened
// so a YAML flow can be generated from real execution data.
type ExecutionTrace struct {
	UserRequest string      `json:"userRequest"`
	Steps       []TraceStep `json:"steps"`
	FinalOutput string      `json:"finalOutput,omitempty"` // LLM's final text response (for format replication)
	StartedAt   time.Time   `json:"startedAt"`
	EndedAt     time.Time   `json:"endedAt"`
	mu          sync.Mutex
}

// TraceStep records a single tool call during execution.
type TraceStep struct {
	ToolName       string            `json:"toolName"`
	ToolArgs       map[string]any    `json:"toolArgs"`
	ToolResult     map[string]any    `json:"toolResult,omitempty"`
	DurationMs     int64             `json:"durationMs"`
	Success        bool              `json:"success"`
	Error          string            `json:"error,omitempty"`
	Timestamp      time.Time         `json:"timestamp"`
	SubAgentName   string            `json:"subAgentName,omitempty"`   // Set when this step is a delegate_tasks call
	SubAgentTraces []*ExecutionTrace `json:"subAgentTraces,omitempty"` // Child agent traces (attached after delegation)
}

// NewExecutionTrace creates a new trace for a user request.
func NewExecutionTrace(userRequest string) *ExecutionTrace {
	return &ExecutionTrace{
		UserRequest: userRequest,
		StartedAt:   time.Now(),
	}
}

// RecordStep records a tool invocation in the trace.
func (t *ExecutionTrace) RecordStep(toolName string, args map[string]any, result map[string]any, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	step := TraceStep{
		ToolName:  toolName,
		ToolArgs:  args,
		Success:   err == nil,
		Timestamp: time.Now(),
	}
	if result != nil {
		step.ToolResult = result
	}
	if err != nil {
		step.Error = err.Error()
	}
	t.Steps = append(t.Steps, step)
}

// Finalize marks the trace as complete.
func (t *ExecutionTrace) Finalize() {
	t.EndedAt = time.Now()
}

// AppendOutput appends text to the final LLM output for format replication.
func (t *ExecutionTrace) AppendOutput(text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.FinalOutput += text
}

// ToolCallCount returns the number of tool calls recorded.
func (t *ExecutionTrace) ToolCallCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.Steps)
}

// SuccessfulSteps returns only steps that succeeded (for distillation).
func (t *ExecutionTrace) SuccessfulSteps() []TraceStep {
	t.mu.Lock()
	defer t.mu.Unlock()

	var result []TraceStep
	for _, s := range t.Steps {
		if s.Success {
			result = append(result, s)
		}
	}
	return result
}

// Summary returns a compact one-line summary of the trace for LLM analysis.
// Format: User: "<request>" | Tools: tool1, tool2 (<N> calls) | Output: <len> chars
func (t *ExecutionTrace) Summary() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Distinct tool names
	toolSet := make(map[string]bool)
	for _, s := range t.Steps {
		toolSet[s.ToolName] = true
	}
	var toolNames []string
	for name := range toolSet {
		toolNames = append(toolNames, name)
	}

	var sb strings.Builder
	// Truncate user request for readability
	req := t.UserRequest
	if len(req) > 100 {
		req = req[:97] + "..."
	}
	sb.WriteString(fmt.Sprintf("User: %q", req))

	if len(t.Steps) > 0 {
		sb.WriteString(fmt.Sprintf(" | Tools: %s (%d calls)", strings.Join(toolNames, ", "), len(t.Steps)))
	} else {
		sb.WriteString(" | Tools: none")
	}

	sb.WriteString(fmt.Sprintf(" | Output: %d chars", len(t.FinalOutput)))
	return sb.String()
}
