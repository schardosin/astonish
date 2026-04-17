package agent

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"google.golang.org/adk/tool"
)

// AgentIdentity holds the agent's configured persona for web portal interactions.
// When populated, it is rendered into the system prompt so the LLM knows what
// name, username, and email to use when filling registration forms.
type AgentIdentity struct {
	Name     string
	Username string
	Email    string
	Bio      string
	Website  string
	Locale   string
	Timezone string
}

// IsConfigured returns true if at least one identity field is set.
func (id *AgentIdentity) IsConfigured() bool {
	return id != nil && (id.Name != "" || id.Username != "" || id.Email != "")
}

// SystemPromptBuilder constructs context-aware system prompts for chat mode.
//
// The prompt is organized into three tiers:
//   - Tier 1 (Static Core): Identity, behavior, permissions, environment, capabilities.
//     ~800 tokens. Stable across turns for KV-cache reuse.
//   - Tier 2 (Indexed Guidance): Detailed how-to docs for each capability, stored as
//     memory/guidance/*.md and retrieved via the vector store. Zero tokens in the prompt.
//   - Tier 3 (Per-Turn Dynamic): Channel hints, scheduler hints, session context,
//     execution plans, and auto-retrieved knowledge. These are appended at the end
//     of the system prompt so the static prefix remains cacheable by providers.
type SystemPromptBuilder struct {
	Tools                 []tool.Tool
	Toolsets              []tool.Toolset
	WorkspaceDir          string
	CustomPrompt          string
	InstructionsContent   string         // Contents of INSTRUCTIONS.md (behavior directives)
	WebSearchAvailable    bool           // Whether a web search MCP tool is configured
	WebExtractAvailable   bool           // Whether a web extract MCP tool is configured
	WebSearchToolName     string         // Name of the configured search tool (e.g. "tavily-search")
	WebExtractToolName    string         // Name of the configured extract tool (e.g. "tavily-extract")
	BrowserAvailable      bool           // Whether built-in browser tools are registered
	MemorySearchAvailable bool           // Whether semantic memory search is available
	ChannelHints          string         // Channel-specific output constraints (empty = console mode)
	SchedulerHints        string         // Scheduler-specific output constraints (empty = not a scheduled run)
	SkillIndex            string         // Lightweight skill listing (Tier 1 — names and descriptions only)
	Identity              *AgentIdentity // Agent persona for web portal interactions
	FleetSection          string         // Pre-built "Available Fleets" section (empty = no fleets loaded)
	SessionContext        string         // Per-turn context injected by the caller (e.g., fleet plan wizard instructions)
	Timezone              string         // IANA timezone (e.g. "America/New_York")
	RelevantKnowledge     string         // Per-turn auto-retrieved knowledge from vector store (empty = none)
	RelevantTools         string         // Per-turn auto-retrieved tool matches from tool index (empty = none)
	Catalog               []*ToolGroup   // Tool groups available for delegation via delegate_tasks (nil = no delegation)
}

// Build constructs the full system prompt.
//
// The output is deliberately compact (~800 tokens static) to maximize
// attention budget for smaller models. Detailed guidance for complex
// capabilities (browser, credentials, scheduling, etc.) is stored in
// memory/guidance/*.md and delivered via auto-knowledge retrieval.
func (b *SystemPromptBuilder) Build() string {
	var sb strings.Builder

	// ── Tier 1: Static Core ──────────────────────────────────────

	// 1. Identity
	sb.WriteString("You are Astonish, an AI assistant with access to tools.\n")
	sb.WriteString("You help users accomplish tasks by calling tools and reasoning through problems.\n\n")

	// 1b. Channel-specific output constraints (set per-turn by channel manager)
	if b.ChannelHints != "" {
		sb.WriteString("## Output Constraints\n\n")
		sb.WriteString(b.ChannelHints)
		sb.WriteString("\n\n")
	}

	// 1c. Scheduler-specific output constraints (set per-turn by scheduler executor)
	if b.SchedulerHints != "" {
		sb.WriteString("## Execution Context\n\n")
		sb.WriteString(b.SchedulerHints)
		sb.WriteString("\n\n")
	}

	// 1d. Per-turn session context (e.g., fleet plan wizard instructions)
	if b.SessionContext != "" {
		sb.WriteString("## Session Task\n\n")
		sb.WriteString(b.SessionContext)
		sb.WriteString("\n\n")
	}

	// 2. Custom prompt (if set by user in config.yaml)
	if b.CustomPrompt != "" {
		sb.WriteString(b.CustomPrompt)
		sb.WriteString("\n\n")
	}

	// 2b. Behavior Instructions (from INSTRUCTIONS.md — user-editable)
	if b.InstructionsContent != "" {
		sb.WriteString("## Behavior Instructions\n\n")
		sb.WriteString(b.InstructionsContent)
		sb.WriteString("\n\n")
	}

	// 3. Tool Use (compact — detailed guidance is in memory/guidance/*.md)
	sb.WriteString("## Tool Use\n\n")
	sb.WriteString("- ALWAYS attempt tasks using tools first. Never explain how the user could do it.\n")
	sb.WriteString("- When multiple approaches exist, briefly present options and ask the user which they prefer.\n")
	sb.WriteString("- If a tool fails, try a different approach before giving up.\n")
	sb.WriteString("- Prefer read_file/edit_file/write_file over shell sed/awk/echo/cat for file operations.\n")
	sb.WriteString("- http_request CANNOT reach private/RFC1918 IPs (192.168.x.x, 10.x.x.x, 172.16-31.x.x) or localhost. Use curl via shell_command for private network endpoints.\n")
	sb.WriteString("- For multi-step tasks, execute sequentially, report progress, and search memory first for prior solutions.\n")
	sb.WriteString("- After completing a task where you overcame obstacles or discovered non-obvious solutions, save the knowledge using memory_save. Search memory_search(\"memory usage\") first to retrieve the full saving guidelines.\n")
	sb.WriteString("- When the user asks you to do something, briefly acknowledge before starting work.\n")
	if b.SkillIndex != "" {
		sb.WriteString("- **Skill-first rule:** When a task matches any Available Skill, you MUST call `skill_lookup` to load it — no exceptions. Do this alongside your first batch of tool calls (e.g. parallel with memory_search). The skill provides canonical commands and context that may be newer than stored memory. Having prior knowledge of a working method is NOT a reason to skip loading the skill.\n")
	}
	if b.hasSearchToolsTool() {
		sb.WriteString("- When you're unsure which tool or tool group to use for a task, call `search_tools` with a description of what you need. Do NOT guess tool availability — verify via `search_tools`.\n")
		sb.WriteString("- When asked to list available tools, call `search_tools(query=\"*\")` to get the verified complete inventory. Do not reconstruct the list from memory.\n")
	}

	// 3b. Knowledge Context — teaches the model about injected knowledge
	sb.WriteString("\n## Knowledge Context\n\n")
	sb.WriteString("Your system prompt may include a `[Knowledge For This Task]` section at the end. ")
	sb.WriteString("This contains VERIFIED information retrieved from memory — real IPs, working commands, credentials, and workarounds proven in previous sessions.\n\n")
	sb.WriteString("ALWAYS use the specific details from knowledge sections (IPs, ports, URLs, tool choices, commands) instead of defaults or assumptions. ")
	sb.WriteString("If knowledge says to use a specific IP, use that IP — not localhost or a standard default. ")
	sb.WriteString("If knowledge says to use a specific tool or approach, follow it exactly.\n")
	sb.WriteString("The knowledge section already contains the most relevant memory results for this task — do not call memory_search to re-fetch information already present in it.\n")

	// 4. Environment
	sb.WriteString("\n## Environment\n\n")
	if b.WorkspaceDir != "" {
		sb.WriteString(fmt.Sprintf("- Working directory: %s\n", b.WorkspaceDir))
	}
	sb.WriteString(fmt.Sprintf("- OS: %s/%s\n", runtime.GOOS, runtime.GOARCH))
	if b.Timezone != "" {
		sb.WriteString(fmt.Sprintf("- Timezone: %s\n", b.Timezone))
	}

	// 5. Agent Identity (for web portal interactions)
	if b.Identity.IsConfigured() {
		sb.WriteString("\n## Agent Identity\n\n")
		sb.WriteString("You have a configured identity for web portal registrations and interactions. ")
		sb.WriteString("Use these details when filling registration forms, creating profiles, or identifying yourself on websites:\n\n")
		if b.Identity.Name != "" {
			sb.WriteString(fmt.Sprintf("- **Name:** %s\n", b.Identity.Name))
		}
		if b.Identity.Username != "" {
			sb.WriteString(fmt.Sprintf("- **Username:** %s\n", b.Identity.Username))
		}
		if b.Identity.Email != "" {
			sb.WriteString(fmt.Sprintf("- **Email:** %s\n", b.Identity.Email))
		}
		if b.Identity.Bio != "" {
			sb.WriteString(fmt.Sprintf("- **Bio:** %s\n", b.Identity.Bio))
		}
		if b.Identity.Website != "" {
			sb.WriteString(fmt.Sprintf("- **Website:** %s\n", b.Identity.Website))
		}
		if b.Identity.Locale != "" {
			sb.WriteString(fmt.Sprintf("- **Locale:** %s\n", b.Identity.Locale))
		}
		if b.Identity.Timezone != "" {
			sb.WriteString(fmt.Sprintf("- **Timezone:** %s\n", b.Identity.Timezone))
		}
		sb.WriteString("\n")
		sb.WriteString("**Guidelines:**\n")
		sb.WriteString("- If the username is taken on a portal, try appending digits or underscores (e.g. `username_01`)\n")
		sb.WriteString("- For email verification, use the `email_wait` tool to wait for the confirmation email, then extract the verification link\n")
		sb.WriteString("- If you encounter a CAPTCHA during registration, use `browser_request_human` to hand control to the user\n")
		sb.WriteString("- Always save successful account registrations to persistent memory (credential store for passwords, MEMORY.md for account details)\n")
	}

	// 6. Capabilities (dynamic list + guidance hint)
	sb.WriteString("\n## Capabilities\n\n")
	capsLine := b.buildCapabilitiesLine()
	sb.WriteString(fmt.Sprintf("You have tools for: %s.\n", capsLine))
	sb.WriteString("Detailed step-by-step guidance for complex capabilities (browser automation, credential management, ")
	sb.WriteString("job scheduling, task delegation, process management, web access patterns, memory usage) is stored in memory. ")
	sb.WriteString("Use `memory_search` with the capability name (e.g., \"browser automation\", \"credential management\", ")
	sb.WriteString("\"job scheduling\") to retrieve instructions before using a complex feature for the first time in a conversation.\n")

	// 6a2. Web search/extract tool hints — tell the LLM the exact tool names
	// so it doesn't fall back to web_fetch when a dedicated search tool is configured.
	if b.WebSearchAvailable && b.WebSearchToolName != "" {
		sb.WriteString(fmt.Sprintf("\n**Web search tool:** `%s` — use this tool for web searches. Do NOT use `web_fetch` for search queries.\n", b.WebSearchToolName))
	}
	if b.WebExtractAvailable && b.WebExtractToolName != "" {
		sb.WriteString(fmt.Sprintf("**Web extract tool:** `%s` — use this tool to extract content from URLs when `web_fetch` fails.\n", b.WebExtractToolName))
	}

	// 6b. Task delegation — list available tool groups for delegate_tasks
	if len(b.Catalog) > 0 {
		sb.WriteString("\n## Task Delegation\n\n")
		sb.WriteString("`delegate_tasks` runs tasks in isolated sub-agents with their own sessions. ")
		sb.WriteString("Use it for: parallel execution of independent tasks, long-running operations, or tasks requiring isolation. ")
		sb.WriteString("Do NOT use it just to access tools — relevant tools are injected automatically and can be called directly.\n\n")

		sb.WriteString("**Planning strategy:**\n")
		sb.WriteString("1. For multi-step tasks, call `announce_plan` first to show the user your approach as a visible checklist.\n")
		sb.WriteString("2. Decompose complex goals into independent, parallelizable sub-tasks (each with a clear deliverable).\n")
		sb.WriteString("3. Keep each sub-task focused: one research question, one file operation, one API interaction.\n")
		sb.WriteString("4. If tasks have dependencies, run them in separate `delegate_tasks` calls (first batch completes before the second starts).\n")
		sb.WriteString("5. Give each sub-task a descriptive name (e.g., \"fetch-pricing-page\", \"analyze-competitors\") — users see these as progress steps.\n")
		sb.WriteString("6. After all sub-tasks complete, **synthesize** the results yourself — don't just concatenate sub-agent output.\n")
		sb.WriteString("7. For substantial final outputs (reports, analyses, comparisons), save as a file with `write_file` so the user can download it.\n")
		sb.WriteString("8. Plan steps are updated automatically as tools complete — do NOT try to update them manually.\n\n")

		sb.WriteString("**Available tool groups (for delegation):**\n")
		ctx := &minimalReadonlyContext{Context: context.Background()}
		for _, g := range b.Catalog {
			toolCount := len(g.Tools)
			for _, ts := range g.Toolsets {
				if mcpTools, err := ts.Tools(ctx); err == nil {
					toolCount += len(mcpTools)
				}
			}
			sb.WriteString(fmt.Sprintf("- **%s** (%d tools) — %s\n", g.Name, toolCount, g.Description))
		}
		sb.WriteString("\nExamples: `tools: [\"browser\"]`, `tools: [\"core\", \"web\"]`, `tools: [\"core\", \"mcp:github\"]`\n")
	}

	// 6c2. Skill index (lightweight listing of available CLI tool skills)
	if b.SkillIndex != "" {
		sb.WriteString("\n")
		sb.WriteString(b.SkillIndex)
	}

	// 6j. Fleet awareness (when fleet definitions are loaded)
	if b.FleetSection != "" {
		sb.WriteString(b.FleetSection)
	}

	// ── Tier 3: Per-Turn Dynamic ─────────────────────────────────
	// Execution plans and auto-retrieved knowledge are appended here at
	// the end of the system prompt. Placing them last means the static
	// prefix (Tier 1 + Tier 2) remains stable for provider KV-cache
	// prefix matching, while the dynamic tail changes per turn.

	// Per-turn relevant tools from the tool index — these tools are
	// dynamically injected and available for direct invocation.
	if b.RelevantTools != "" {
		sb.WriteString("\n## Relevant Tools For This Request\n\n")
		sb.WriteString("These tools are available for this request — call them directly. ")
		sb.WriteString("Use `search_tools` if you need additional tools not listed here.\n\n")
		sb.WriteString(b.RelevantTools)
	}

	if b.RelevantKnowledge != "" {
		sb.WriteString("\n## Knowledge For This Task\n\n")
		sb.WriteString("CRITICAL — You MUST apply the following knowledge when executing the user's current request. ")
		sb.WriteString("It contains proven commands, specific flags, and workarounds that are KNOWN TO WORK ")
		sb.WriteString("from previous sessions. Use the exact commands and approaches described here.\n")
		sb.WriteString("Note: This knowledge does NOT replace loading relevant skills via `skill_lookup` — always load matching skills for up-to-date context.\n\n")
		sb.WriteString(b.RelevantKnowledge)
	}

	// NOTE: Date/time is NOT included here. It is prepended to each user
	// message via NewTimestampedUserContent(), keeping the system prompt
	// prefix as stable as possible for provider KV-cache reuse.

	return sb.String()
}

// buildCapabilitiesLine produces a comma-separated list of available capability
// names based on which tools are registered.
func (b *SystemPromptBuilder) buildCapabilitiesLine() string {
	var caps []string

	// Always present (core tools)
	caps = append(caps, "file operations", "shell commands", "web fetching")

	if b.BrowserAvailable {
		caps = append(caps, "browser automation")
	}
	if b.hasCredentialTools() {
		caps = append(caps, "credential management")
	}
	if b.hasSchedulerTools() {
		caps = append(caps, "job scheduling")
	}
	if b.hasProcessTools() {
		caps = append(caps, "process management")
	}
	if b.hasHttpRequestTool() {
		caps = append(caps, "HTTP API requests")
	}
	if b.hasDelegateTasksTool() {
		caps = append(caps, "task delegation")
	}
	if b.hasFlowTools() {
		caps = append(caps, "flow execution")
	}
	if b.MemorySearchAvailable {
		caps = append(caps, "persistent memory")
	}
	if b.WebSearchAvailable {
		if b.WebSearchToolName != "" {
			caps = append(caps, fmt.Sprintf("web search via `%s`", b.WebSearchToolName))
		} else {
			caps = append(caps, "web search")
		}
	}
	if b.WebExtractAvailable {
		if b.WebExtractToolName != "" {
			caps = append(caps, fmt.Sprintf("web content extraction via `%s`", b.WebExtractToolName))
		} else {
			caps = append(caps, "web content extraction")
		}
	}
	if b.hasEmailTools() {
		caps = append(caps, "email")
	}
	if b.FleetSection != "" {
		caps = append(caps, "fleet agents")
	}

	return strings.Join(caps, ", ")
}

// ToolCount returns the total number of tools available.
func (b *SystemPromptBuilder) ToolCount() int {
	count := len(b.Tools)
	if len(b.Toolsets) > 0 {
		ctx := &minimalReadonlyContext{Context: context.Background()}
		for _, ts := range b.Toolsets {
			mcpTools, err := ts.Tools(ctx)
			if err != nil {
				continue
			}
			count += len(mcpTools)
		}
	}
	return count
}

// hasSchedulerTools returns true if schedule_job is among the available tools.
func (b *SystemPromptBuilder) hasSchedulerTools() bool {
	for _, t := range b.Tools {
		if t.Name() == "schedule_job" {
			return true
		}
	}
	return false
}

// hasCredentialTools returns true if save_credential is among the available tools.
func (b *SystemPromptBuilder) hasCredentialTools() bool {
	for _, t := range b.Tools {
		if t.Name() == "save_credential" {
			return true
		}
	}
	return false
}

// hasProcessTools returns true if process_read is among the available tools.
func (b *SystemPromptBuilder) hasProcessTools() bool {
	for _, t := range b.Tools {
		if t.Name() == "process_read" {
			return true
		}
	}
	return false
}

// hasHttpRequestTool returns true if http_request is among the available tools.
func (b *SystemPromptBuilder) hasHttpRequestTool() bool {
	for _, t := range b.Tools {
		if t.Name() == "http_request" {
			return true
		}
	}
	return false
}

// hasDelegateTasksTool returns true if delegate_tasks is among the available tools.
func (b *SystemPromptBuilder) hasDelegateTasksTool() bool {
	for _, t := range b.Tools {
		if t.Name() == "delegate_tasks" {
			return true
		}
	}
	return false
}

// hasHandoffTool returns true if browser_request_human is among the available tools.
func (b *SystemPromptBuilder) hasHandoffTool() bool {
	for _, t := range b.Tools {
		if t.Name() == "browser_request_human" {
			return true
		}
	}
	return false
}

// hasEmailTools returns true if email_list is among the available tools.
func (b *SystemPromptBuilder) hasEmailTools() bool {
	for _, t := range b.Tools {
		if t.Name() == "email_list" {
			return true
		}
	}
	return false
}

// hasSearchToolsTool returns true if search_tools is among the available tools.
func (b *SystemPromptBuilder) hasSearchToolsTool() bool {
	for _, t := range b.Tools {
		if t.Name() == "search_tools" {
			return true
		}
	}
	return false
}

// hasFlowTools returns true if search_flows is among the available tools.
func (b *SystemPromptBuilder) hasFlowTools() bool {
	for _, t := range b.Tools {
		if t.Name() == "search_flows" {
			return true
		}
	}
	return false
}

// ToolNames returns a list of all available tool names.
func (b *SystemPromptBuilder) ToolNames() []string {
	var names []string
	for _, t := range b.Tools {
		names = append(names, t.Name())
	}
	if len(b.Toolsets) > 0 {
		ctx := &minimalReadonlyContext{Context: context.Background()}
		for _, ts := range b.Toolsets {
			mcpTools, err := ts.Tools(ctx)
			if err != nil {
				continue
			}
			for _, t := range mcpTools {
				names = append(names, t.Name())
			}
		}
	}
	return names
}
