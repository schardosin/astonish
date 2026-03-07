package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	adksession "google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"

	"github.com/google/uuid"
	"github.com/schardosin/astonish/pkg/memory"
	persistentsession "github.com/schardosin/astonish/pkg/session"
)

// SubAgentConfig holds configuration for the sub-agent system.
type SubAgentConfig struct {
	MaxDepth      int           `yaml:"max_depth,omitempty" json:"max_depth,omitempty"`           // Max delegation nesting (default: 2)
	MaxConcurrent int           `yaml:"max_concurrent,omitempty" json:"max_concurrent,omitempty"` // Max parallel sub-agents (default: 5)
	TaskTimeout   time.Duration `yaml:"task_timeout,omitempty" json:"task_timeout,omitempty"`     // Per-task timeout (default: 5m)
}

// SubAgentTask describes a single sub-agent task to execute.
type SubAgentTask struct {
	Name         string   // Short identifier for the sub-agent (e.g. "researcher", "coder")
	Instructions string   // Task-specific instructions for the sub-agent
	Description  string   // Brief description of what to accomplish
	ToolFilter   []string // Specific tool names to include (empty = all allowed)
	Model        string   // Override model (empty = use parent's model)
	Provider     string   // Override provider (empty = use parent's provider)

	// CustomPrompt, when true, uses Instructions directly as the LLM system prompt
	// instead of wrapping it with buildChildPrompt(). This is used by fleet agents
	// that build their own complete prompt (via fleet.BuildAgentPrompt).
	CustomPrompt bool

	// TimeoutOverride, when > 0, overrides the SubAgentManager's Config.TaskTimeout
	// for this specific task. Used by the fleet orchestrator which needs more time
	// than individual worker sub-agents.
	TimeoutOverride time.Duration

	// SessionState holds additional key-value pairs to inject into the child session's
	// initial state. This allows callers to pass metadata that tools running inside
	// the sub-agent can access via ctx.State().Get(key).
	SessionState map[string]any

	// OnEvent is an optional callback invoked for each event produced by the
	// sub-agent's runner. It enables real-time progress streaming from sub-agents
	// (e.g., fleet orchestrator progress). The callback must be safe to call
	// from the RunTask goroutine. If nil, events are consumed silently.
	OnEvent func(event *adksession.Event)

	// Internal: set by SubAgentManager, not by callers
	ParentDepth int    // Current nesting depth
	ParentID    string // Parent session ID for linking
}

// TaskResult holds the outcome of a single sub-agent task execution.
type TaskResult struct {
	Name      string          // Matches SubAgentTask.Name
	Status    string          // "success", "error", "timeout"
	Result    string          // Final text output from the sub-agent
	Trace     *ExecutionTrace // Execution trace for the sub-agent's work
	ToolCalls int             // Number of tool calls made
	Duration  time.Duration   // Wall clock time for the task
	Error     string          // Error message if Status != "success"
}

// SubAgentManager orchestrates the execution of sub-agent tasks.
type SubAgentManager struct {
	// Parent context
	LLM            model.LLM                    // Parent's LLM (used for children unless overridden)
	Tools          []tool.Tool                  // All internal tools available
	FleetTools     []tool.Tool                  // Fleet-only tools (e.g., run_fleet_phase) not in main agent's tool list
	Toolsets       []tool.Toolset               // MCP toolsets
	SessionService adksession.Service           // Session persistence
	MemoryManager  *memory.Manager              // Memory manager for context injection (nil = disabled)
	Compactor      *persistentsession.Compactor // Context window compactor for sub-agents (nil = disabled)
	AppName        string                       // Application name for sessions
	UserID         string                       // User ID for sessions

	// Configuration
	Config SubAgentConfig

	// Internal
	sem chan struct{} // concurrency semaphore
}

// excludedChildTools are tools that sub-agents must NOT have access to.
var excludedChildTools = map[string]bool{
	"memory_save":       true, // Children can't write memory
	"delegate_tasks":    true, // Prevent recursive delegation
	"schedule_job":      true, // Children can't schedule jobs
	"save_credential":   true, // Children can't modify credentials
	"remove_credential": true, // Children can't remove credentials
	"opencode":          true, // OpenCode delegation is fleet-agent-only (via FleetTools)
}

// NewSubAgentManager creates a new SubAgentManager with the given configuration.
func NewSubAgentManager(cfg SubAgentConfig) *SubAgentManager {
	if cfg.MaxDepth <= 0 {
		cfg.MaxDepth = 2
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 5
	}
	if cfg.TaskTimeout <= 0 {
		cfg.TaskTimeout = 5 * time.Minute
	}

	sem := make(chan struct{}, cfg.MaxConcurrent)
	return &SubAgentManager{
		Config: cfg,
		sem:    sem,
	}
}

// RunTasks executes multiple sub-agent tasks concurrently and returns results.
// Tasks are fan-out with a semaphore controlling concurrency.
// This method blocks until all tasks complete (or timeout).
func (m *SubAgentManager) RunTasks(ctx context.Context, tasks []SubAgentTask) []TaskResult {
	results := make([]TaskResult, len(tasks))
	var wg sync.WaitGroup

	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, t SubAgentTask) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case m.sem <- struct{}{}:
				defer func() { <-m.sem }()
			case <-ctx.Done():
				results[idx] = TaskResult{
					Name:   t.Name,
					Status: "timeout",
					Error:  "context cancelled before task started",
				}
				return
			}

			results[idx] = m.RunTask(ctx, t)
		}(i, task)
	}

	wg.Wait()
	return results
}

// RunTask executes a single sub-agent task synchronously.
// It creates a child session, builds a filtered ChatAgent, runs the full
// agent loop, collects the output and trace, then returns the result.
func (m *SubAgentManager) RunTask(ctx context.Context, task SubAgentTask) TaskResult {
	start := time.Now()

	// Apply task timeout (use override if set)
	timeout := m.Config.TaskTimeout
	if task.TimeoutOverride > 0 {
		timeout = task.TimeoutOverride
	}
	taskCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Depth check
	if task.ParentDepth >= m.Config.MaxDepth {
		return TaskResult{
			Name:     task.Name,
			Status:   "error",
			Error:    fmt.Sprintf("max delegation depth %d reached", m.Config.MaxDepth),
			Duration: time.Since(start),
		}
	}

	// Filter tools for the child
	childTools := m.filterTools(task.ToolFilter)

	// Build child system prompt: use custom prompt if set, otherwise build default
	var childPrompt string
	if task.CustomPrompt && task.Instructions != "" {
		childPrompt = task.Instructions
	} else {
		childPrompt = m.buildChildPrompt(task)
	}

	// Create child session linked to parent
	childSessionID := uuid.NewString()
	createState := map[string]any{}
	if task.ParentID != "" {
		createState[persistentsession.StateKeyParentID] = task.ParentID
	}
	// Inject caller-provided session state
	for k, v := range task.SessionState {
		createState[k] = v
	}

	_, err := m.SessionService.Create(taskCtx, &adksession.CreateRequest{
		AppName:   m.AppName,
		UserID:    m.UserID,
		SessionID: childSessionID,
		State:     createState,
	})
	if err != nil {
		return TaskResult{
			Name:     task.Name,
			Status:   "error",
			Error:    fmt.Sprintf("failed to create child session: %v", err),
			Duration: time.Since(start),
		}
	}

	// Persist the task name as the session title so fleet reconstruction
	// can derive phase/agent info from titles like "fleet-<fleet>-<phase>".
	if fs, ok := m.SessionService.(*persistentsession.FileStore); ok {
		_ = fs.SetSessionTitle(childSessionID, task.Name)
	}

	// Wire context compaction for sub-agents to prevent exceeding the context window
	// during long multi-step tool work (e.g., fleet agents reading/writing many files).
	var beforeModelCallbacks []llmagent.BeforeModelCallback

	// Truncate oversized tool responses before they reach the model. This prevents
	// a single large response (e.g., file_tree on /) from causing a 400 Bad Request.
	// Must run BEFORE compaction so the compactor sees reasonable-sized content.
	beforeModelCallbacks = append(beforeModelCallbacks, TruncateToolResponsesCallback())

	if m.Compactor != nil {
		beforeModelCallbacks = append(beforeModelCallbacks, m.Compactor.BeforeModelCallback())
	}

	// Create child LLM agent via ADK
	childAgent, err := llmagent.New(llmagent.Config{
		Name:                 task.Name,
		Model:                m.LLM,
		Instruction:          childPrompt,
		Tools:                childTools,
		Toolsets:             m.filterToolsets(),
		BeforeModelCallbacks: beforeModelCallbacks,
	})
	if err != nil {
		return TaskResult{
			Name:     task.Name,
			Status:   "error",
			Error:    fmt.Sprintf("failed to create child agent: %v", err),
			Duration: time.Since(start),
		}
	}

	// Create runner
	r, err := runner.New(runner.Config{
		AppName:        m.AppName,
		Agent:          childAgent,
		SessionService: m.SessionService,
	})
	if err != nil {
		return TaskResult{
			Name:     task.Name,
			Status:   "error",
			Error:    fmt.Sprintf("failed to create runner: %v", err),
			Duration: time.Since(start),
		}
	}

	// Build user message from task description
	userMsg := genai.NewContentFromText(task.Description, genai.RoleUser)

	// Execute the agent and collect results
	trace := NewExecutionTrace(task.Description)
	var outputParts []string
	var toolCallCount int

	for event, runErr := range r.Run(taskCtx, m.UserID, childSessionID, userMsg, adkagent.RunConfig{}) {
		if runErr != nil {
			trace.Finalize()
			return TaskResult{
				Name:      task.Name,
				Status:    "error",
				Result:    strings.Join(outputParts, ""),
				Error:     fmt.Sprintf("agent run error: %v", runErr),
				Trace:     trace,
				ToolCalls: toolCallCount,
				Duration:  time.Since(start),
			}
		}

		if event == nil {
			continue
		}

		// Forward event to callback for real-time progress streaming
		if task.OnEvent != nil {
			task.OnEvent(event)
		}

		// Collect text output
		if event.LLMResponse.Content != nil {
			for _, part := range event.LLMResponse.Content.Parts {
				if part.Text != "" {
					outputParts = append(outputParts, part.Text)
				}
				// Record tool calls in trace
				if part.FunctionCall != nil {
					toolCallCount++
					args := make(map[string]any)
					if part.FunctionCall.Args != nil {
						for k, v := range part.FunctionCall.Args {
							args[k] = v
						}
					}
					trace.RecordStep(part.FunctionCall.Name, args, nil, nil)
				}
				// Record tool results in trace
				if part.FunctionResponse != nil {
					// Update the last trace step with the result
					trace.mu.Lock()
					if len(trace.Steps) > 0 {
						lastStep := &trace.Steps[len(trace.Steps)-1]
						if lastStep.ToolName == part.FunctionResponse.Name {
							lastStep.ToolResult = part.FunctionResponse.Response
							lastStep.Success = true
						}
					}
					trace.mu.Unlock()
				}
			}
		}
	}

	trace.Finalize()
	finalOutput := strings.Join(outputParts, "")
	trace.AppendOutput(finalOutput)

	// Check if context was cancelled (timeout)
	status := "success"
	errMsg := ""
	if taskCtx.Err() != nil {
		status = "timeout"
		errMsg = "task timed out"
	}

	return TaskResult{
		Name:      task.Name,
		Status:    status,
		Result:    finalOutput,
		Trace:     trace,
		ToolCalls: toolCallCount,
		Duration:  time.Since(start),
		Error:     errMsg,
	}
}

// filterTools returns tools allowed for sub-agents, excluding dangerous ones
// and optionally filtering to a specific set.
// When an allow list is specified, tools are drawn from both Tools and FleetTools.
// Fleet tools are only accessible via explicit allow list (they are in excludedChildTools
// so they are always excluded from the default "all tools" path).
func (m *SubAgentManager) filterTools(allowList []string) []tool.Tool {
	allowSet := make(map[string]bool, len(allowList))
	for _, name := range allowList {
		allowSet[name] = true
	}

	var filtered []tool.Tool

	// Search main tools
	for _, t := range m.Tools {
		name := t.Name()

		// Always exclude dangerous tools (unless explicitly allowed AND tool is in FleetTools)
		if excludedChildTools[name] {
			continue
		}

		// If allow list specified, only include those tools
		if len(allowSet) > 0 && !allowSet[name] {
			continue
		}

		filtered = append(filtered, t)
	}

	// If an allow list is specified, also search FleetTools for requested tools.
	// This allows the orchestrator to get run_fleet_phase via its tool filter
	// even though it's in excludedChildTools for the main tools path.
	if len(allowSet) > 0 {
		for _, t := range m.FleetTools {
			name := t.Name()
			if allowSet[name] {
				filtered = append(filtered, t)
			}
		}
	}

	return filtered
}

// filterToolsets returns toolsets for sub-agents (passes through all MCP toolsets).
func (m *SubAgentManager) filterToolsets() []tool.Toolset {
	return m.Toolsets
}

// buildChildPrompt constructs the system prompt for a sub-agent.
func (m *SubAgentManager) buildChildPrompt(task SubAgentTask) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("You are %q, a focused sub-agent working on a specific task.\n\n", task.Name))

	sb.WriteString("## Your Task\n")
	sb.WriteString(task.Description)
	sb.WriteString("\n\n")

	if task.Instructions != "" {
		sb.WriteString("## Instructions\n")
		sb.WriteString(task.Instructions)
		sb.WriteString("\n\n")
	}

	sb.WriteString("## Behavior Rules\n")
	sb.WriteString("- You are a sub-agent. Focus ONLY on the task described above.\n")
	sb.WriteString("- Complete the task efficiently using the tools available to you.\n")
	sb.WriteString("- When done, provide a clear summary of what you accomplished and any relevant results.\n")
	sb.WriteString("- Do NOT ask clarifying questions — work with the information provided.\n")
	sb.WriteString("- Do NOT attempt to save to memory or schedule jobs — you don't have those capabilities.\n")
	sb.WriteString("- Do NOT write scripts or code unless explicitly asked — use your tools directly.\n")
	sb.WriteString("- If you encounter an error, report it clearly in your response.\n")

	// Tool-specific operational guidance based on what tools the child actually has
	childTools := m.filterTools(task.ToolFilter)
	childToolSet := make(map[string]bool, len(childTools))
	for _, t := range childTools {
		childToolSet[t.Name()] = true
	}

	if childToolSet["http_request"] {
		sb.WriteString("\n## HTTP Requests\n")
		sb.WriteString("- Use the `http_request` tool for all API calls. Do NOT write scripts or use shell_command for HTTP.\n")
		sb.WriteString("- Set `credential` to a stored credential name for authenticated requests (the auth header is injected automatically).\n")
		sb.WriteString("- Use `list_credentials` to see available credentials if you need to find the right one.\n")
		sb.WriteString("- For JSON APIs, Content-Type is set automatically when the body is JSON.\n")
	}

	if childToolSet["resolve_credential"] {
		sb.WriteString("\n## Credentials\n")
		sb.WriteString("- Use `resolve_credential` to get raw credential fields (username, password) for non-HTTP use.\n")
		sb.WriteString("- Use `list_credentials` to discover available credentials.\n")
		sb.WriteString("- You cannot create or modify credentials — only read existing ones.\n")
	}

	// Load and inject memory content if available
	if m.MemoryManager != nil {
		memContent, err := m.MemoryManager.Load()
		if err == nil && memContent != "" {
			sb.WriteString("\n## Context (from persistent memory)\n")
			sb.WriteString(escapeCurlyPlaceholders(memContent))
			sb.WriteString("\n")
		}
	}

	return sb.String()
}
