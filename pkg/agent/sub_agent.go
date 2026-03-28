package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	adksession "google.golang.org/adk/session"
	"google.golang.org/adk/tool"

	"github.com/google/uuid"
	"github.com/schardosin/astonish/pkg/credentials"
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

	// OverrideTools, when non-nil, replaces the tools that would normally be
	// selected by resolveTools(). This is used by fleet sessions to provide
	// sandbox-wrapped tool copies without mutating the global SubAgentManager
	// singleton. The caller is responsible for applying any tool filter before
	// setting this field.
	OverrideTools []tool.Tool

	// OverrideToolsets, when non-nil, replaces the MCP toolsets that would
	// normally come from resolveTools(). Used by fleet sessions to provide
	// sandbox-wired MCP toolset copies (with ContainerMCPTransport) that route
	// MCP server processes through the fleet's container.
	OverrideToolsets []tool.Toolset

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

// ToolGroup defines a named group of tools that sub-agents can request.
// The LLM references groups by name in the delegate_tasks tool's "tools" field
// (e.g., ["core", "browser", "mcp:github"]). Groups can contain regular tools,
// MCP toolsets, or both.
type ToolGroup struct {
	Name        string         // Group identifier (e.g., "core", "browser", "mcp:github")
	Description string         // Human-readable description for system prompt guidance
	Tools       []tool.Tool    // Regular tools in this group
	Toolsets    []tool.Toolset // MCP toolsets in this group
}

// SubAgentManager orchestrates the execution of sub-agent tasks.
type SubAgentManager struct {
	// Parent context
	LLM            model.LLM                    // Parent's LLM (used for children unless overridden)
	ToolGroups     map[string]*ToolGroup        // Named tool groups for sub-agent tool resolution
	FleetTools     []tool.Tool                  // Fleet-only tools (e.g., run_fleet_phase) not in main agent's tool list
	SessionService adksession.Service           // Session persistence
	MemoryManager  *memory.Manager              // Memory manager for context injection (nil = disabled)
	Compactor      *persistentsession.Compactor // Context window compactor for sub-agents (nil = disabled)
	Redactor       *credentials.Redactor        // Redacts credential values from tool outputs (nil = disabled)
	AppName        string                       // Application name for sessions
	UserID         string                       // User ID for sessions

	// Configuration
	Config SubAgentConfig

	// EventForwarder, when set, is called for each event produced by sub-agent
	// runners spawned via delegate_tasks. It enables transparent delegation:
	// events stream to the UI in real-time while the main LLM only receives
	// a compact summary. Set by the launcher to ChatAgent.ForwardSubTaskEvent.
	// Thread-safe: may be called from multiple sub-agent goroutines.
	EventForwarder func(event *adksession.Event)

	// OnChildSession, when set, is called after a sub-agent session is created
	// but before the sub-agent starts running. It receives the parent and child
	// session IDs. Used to alias the child session to the parent's sandbox
	// container so sub-agents share the same container instead of creating new
	// ones. Set by the launcher to NodeClientPool.Alias.
	OnChildSession func(parentSessionID, childSessionID string)

	// Tool discovery: ToolIndex enables sub-agents to auto-discover which tools
	// they need based on the task description. When a sub-agent is created with
	// an empty ToolFilter, the index is queried to find relevant tool groups.
	ToolIndex *ToolIndex

	// SearchToolsTool is injected into every sub-agent so it can discover
	// additional tools mid-execution via explicit search.
	SearchToolsTool tool.Tool

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

// IsExcludedChildTool returns true if the named tool is in the exclusion list.
// Used by sandbox wrapping to replicate the same filtering as resolveTools().
func IsExcludedChildTool(name string) bool {
	return excludedChildTools[name]
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

	// Resolve tools for the child from requested groups/names
	childTools := task.OverrideTools
	var childToolsets []tool.Toolset
	if task.OverrideToolsets != nil {
		childToolsets = task.OverrideToolsets
	}
	var resolveWarnings []string
	if childTools == nil {
		// If the parent specified tool groups, use those directly.
		// If the parent specified nothing (empty ToolFilter), auto-discover
		// tools using the ToolIndex based on the task description.
		toolFilter := task.ToolFilter
		if len(toolFilter) == 0 && m.ToolIndex != nil && task.Description != "" {
			discoveredGroups := m.ToolIndex.SearchGroupsHybrid(
				context.Background(), task.Description, 12, 0.005,
			)
			if len(discoveredGroups) > 0 {
				toolFilter = discoveredGroups
			}
		}
		childTools, childToolsets, resolveWarnings = m.resolveTools(toolFilter)
	}

	// Inject search_tools into every sub-agent so it can discover additional
	// tools mid-execution if its initial set is insufficient.
	if m.SearchToolsTool != nil {
		childTools = append(childTools, m.SearchToolsTool)
	}

	// If tool resolution produced warnings AND resolved zero tools, fail early
	// with a clear message so the calling LLM can self-correct.
	if len(resolveWarnings) > 0 && len(childTools) == 0 && len(childToolsets) == 0 {
		return TaskResult{
			Name:     task.Name,
			Status:   "error",
			Error:    strings.Join(resolveWarnings, "; "),
			Duration: time.Since(start),
		}
	}

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

	// Alias the child session to the parent's sandbox container so sub-agents
	// share the same container instead of creating new ones.
	if m.OnChildSession != nil && task.ParentID != "" {
		m.OnChildSession(task.ParentID, childSessionID)
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

	// Wire credential redaction so sub-agent tool outputs don't leak secrets
	// into the session transcript. The resolve_credential exemption is kept so
	// the sub-agent LLM can still use raw values programmatically.
	var afterToolCallbacks []llmagent.AfterToolCallback
	if m.Redactor != nil {
		redactor := m.Redactor
		afterToolCallbacks = append(afterToolCallbacks, func(ctx tool.Context, t tool.Tool, input, output map[string]any, err error) (map[string]any, error) {
			if output != nil && t.Name() != "resolve_credential" {
				return redactor.RedactMap(output), err
			}
			return output, err
		})
	}

	// Create child LLM agent via ADK
	childAgent, err := llmagent.New(llmagent.Config{
		Name:                 task.Name,
		Model:                m.LLM,
		Instruction:          childPrompt,
		Tools:                childTools,
		Toolsets:             childToolsets,
		BeforeModelCallbacks: beforeModelCallbacks,
		AfterToolCallbacks:   afterToolCallbacks,
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

	// Build user message from task description (with absolute timestamp for
	// temporal context; see NewTimestampedUserContent for cache-stability rationale).
	userMsg := NewTimestampedUserContent(task.Description)

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

		// Collect text output (skip thought/reasoning parts — these are
		// internal chain-of-thought and should not appear in the result).
		if event.LLMResponse.Content != nil {
			for _, part := range event.LLMResponse.Content.Parts {
				if part.Text != "" && !part.Thought {
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

	// Prepend any tool resolution warnings so the calling LLM sees them
	if len(resolveWarnings) > 0 {
		finalOutput = strings.Join(resolveWarnings, "\n") + "\n\n" + finalOutput
	}

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

// resolveTools resolves the requested tool names/groups into concrete tools
// and toolsets for a sub-agent. Each name in the request can be:
//   - A group name (e.g., "core", "browser", "mcp:github") → expands to all tools/toolsets in that group
//   - An individual tool name (e.g., "grep_search") → includes that specific tool
//
// If the request is empty, the sub-agent gets ZERO tools (callers must be explicit).
// Excluded tools (delegate_tasks, memory_save, etc.) are always removed unless
// the tool comes from FleetTools via explicit allow-list.
func (m *SubAgentManager) resolveTools(request []string) ([]tool.Tool, []tool.Toolset, []string) {
	if len(request) == 0 {
		return nil, nil, nil
	}

	// Separate group names from individual tool names
	var groupNames []string
	individualNames := make(map[string]bool)
	var unknownGroups []string
	for _, name := range request {
		if _, isGroup := m.ToolGroups[name]; isGroup {
			groupNames = append(groupNames, name)
		} else {
			individualNames[name] = true
		}
	}

	// Collect tools and toolsets from requested groups
	seen := make(map[string]bool) // dedup by tool name
	var resultTools []tool.Tool
	var resultToolsets []tool.Toolset

	for _, gName := range groupNames {
		g := m.ToolGroups[gName]
		for _, t := range g.Tools {
			name := t.Name()
			if excludedChildTools[name] || seen[name] {
				continue
			}
			seen[name] = true
			resultTools = append(resultTools, t)
		}
		resultToolsets = append(resultToolsets, g.Toolsets...)
	}

	// Resolve individual tool names by searching all groups
	if len(individualNames) > 0 {
		for _, g := range m.ToolGroups {
			for _, t := range g.Tools {
				name := t.Name()
				if !individualNames[name] || excludedChildTools[name] || seen[name] {
					continue
				}
				seen[name] = true
				resultTools = append(resultTools, t)
			}
		}
		// Also search FleetTools for individually requested tools
		// (fleet tools are only accessible via explicit request)
		for _, t := range m.FleetTools {
			name := t.Name()
			if individualNames[name] && !seen[name] {
				seen[name] = true
				resultTools = append(resultTools, t)
			}
		}

		// Check for individual names that didn't resolve to any tool —
		// these are likely misspelled group names (e.g., "drills" instead of "drill").
		for name := range individualNames {
			if !seen[name] && !excludedChildTools[name] {
				unknownGroups = append(unknownGroups, name)
			}
		}
	}

	// Build warnings for unresolved names
	var warnings []string
	if len(unknownGroups) > 0 {
		sort.Strings(unknownGroups)
		available := make([]string, 0, len(m.ToolGroups))
		for gName := range m.ToolGroups {
			available = append(available, gName)
		}
		sort.Strings(available)
		warnings = append(warnings, fmt.Sprintf(
			"WARNING: unknown tool group(s) or tool name(s): %v — not found in any group. Available groups: %v",
			unknownGroups, available,
		))
	}

	return resultTools, resultToolsets, warnings
}

// AllTools returns all tools from all groups (used for SELF.md generation and flow distillation).
func (m *SubAgentManager) AllTools() []tool.Tool {
	seen := make(map[string]bool)
	var all []tool.Tool
	for _, g := range m.ToolGroups {
		for _, t := range g.Tools {
			if !seen[t.Name()] {
				seen[t.Name()] = true
				all = append(all, t)
			}
		}
	}
	return all
}

// AllToolsets returns all toolsets from all groups (used for SELF.md generation and flow distillation).
func (m *SubAgentManager) AllToolsets() []tool.Toolset {
	var all []tool.Toolset
	for _, g := range m.ToolGroups {
		all = append(all, g.Toolsets...)
	}
	return all
}

// AvailableGroups returns summaries of all tool groups for system prompt generation.
// Returns groups sorted by name for deterministic output.
func (m *SubAgentManager) AvailableGroups() []*ToolGroup {
	groups := make([]*ToolGroup, 0, len(m.ToolGroups))
	for _, g := range m.ToolGroups {
		groups = append(groups, g)
	}
	// Sort by name for deterministic output
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Name < groups[j].Name
	})
	return groups
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
	resolvedTools, _, _ := m.resolveTools(task.ToolFilter)
	childToolSet := make(map[string]bool, len(resolvedTools))
	for _, t := range resolvedTools {
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
			sb.WriteString(EscapeCurlyPlaceholders(memContent))
			sb.WriteString("\n")
		}
	}

	return sb.String()
}
