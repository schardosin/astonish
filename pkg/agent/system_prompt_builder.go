package agent

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"google.golang.org/adk/tool"
)

// SystemPromptBuilder constructs context-aware system prompts for chat mode.
type SystemPromptBuilder struct {
	Tools                 []tool.Tool
	Toolsets              []tool.Toolset
	WorkspaceDir          string
	CustomPrompt          string
	MemoryContent         string // Contents of MEMORY.md (loaded per turn)
	ExecutionPlan         string // Flow-based execution plan (set when a flow matches)
	WebSearchAvailable    bool   // Whether a web search MCP tool is configured
	WebExtractAvailable   bool   // Whether a web extract MCP tool is configured
	WebSearchToolName     string // Name of the configured search tool (e.g. "tavily-search")
	WebExtractToolName    string // Name of the configured extract tool (e.g. "tavily-extract")
	BrowserAvailable      bool   // Whether a browser automation MCP tool is configured (e.g. Playwright)
	MemorySearchAvailable bool   // Whether semantic memory search is available
}

// Build constructs the full system prompt.
func (b *SystemPromptBuilder) Build() string {
	var sb strings.Builder

	// 1. Identity
	sb.WriteString("You are Astonish, an AI assistant with access to tools.\n")
	sb.WriteString("You help users accomplish tasks by calling tools and reasoning through problems.\n\n")

	// 2. Custom prompt (if set by user)
	if b.CustomPrompt != "" {
		sb.WriteString(b.CustomPrompt)
		sb.WriteString("\n\n")
	}

	// 3. Available tools listing
	sb.WriteString("## Available Tools\n\n")

	for _, t := range b.Tools {
		sb.WriteString(fmt.Sprintf("- **%s**: %s\n", t.Name(), t.Description()))
	}

	// Include MCP toolset tools
	if len(b.Toolsets) > 0 {
		ctx := &minimalReadonlyContext{Context: context.Background()}
		for _, ts := range b.Toolsets {
			mcpTools, err := ts.Tools(ctx)
			if err != nil {
				continue
			}
			for _, t := range mcpTools {
				sb.WriteString(fmt.Sprintf("- **%s**: %s\n", t.Name(), t.Description()))
			}
		}
	}

	// 4. Tool use guidance
	sb.WriteString("\n## Tool Use\n\n")
	sb.WriteString("- ALWAYS attempt to accomplish tasks using your tools first. Never explain how the user could do something themselves when you can do it directly with a tool call.\n")
	sb.WriteString("- When a task can be accomplished in multiple ways, briefly present the options and ask the user which they prefer before proceeding. Keep options concise (one line each).\n")
	sb.WriteString("- You have full access to the local machine via shell_command. You can run any command including SSH, curl, network tools, package managers, git, docker, etc.\n")
	sb.WriteString("- For multi-step tasks, execute steps sequentially, using tool results to inform next steps.\n")
	sb.WriteString("- If a tool call fails, analyze the error and try a different approach before giving up.\n")
	sb.WriteString("- Only ask the user for help when you genuinely cannot proceed (e.g., you need credentials or access you don't have).\n")
	sb.WriteString("- When you have enough information to answer, respond directly and concisely.\n")

	// 4b. Web capabilities — always render since web_fetch is a built-in tool
	sb.WriteString("\n## Web Access\n\n")
	sb.WriteString("You have a built-in `web_fetch` tool that can fetch and extract content from any URL.\n\n")

	sb.WriteString("**MANDATORY tool selection rules for web tasks:**\n\n")
	// Use dynamic numbering so rules remain correct across availability permutations
	ruleN := 1
	sb.WriteString(fmt.Sprintf("%d. **For any specific URL**, you MUST use `web_fetch` first. Do NOT skip it in favor of other tools.\n", ruleN))
	ruleN++

	// Prefer free/local browser before paid extract provider when available
	if b.BrowserAvailable {
		sb.WriteString(fmt.Sprintf("%d. If `web_fetch` returns empty, navigation-only, or broken content (common with JS-heavy pages), THEN try the same URL using Playwright browser tools (e.g., `browser_navigate` + `browser_snapshot`). This runs locally and is free.\n", ruleN))
		ruleN++
	}

	if b.WebExtractAvailable {
		extractName := b.WebExtractToolName
		if extractName == "" {
			extractName = "the configured web extract tool"
		}
		// Place extract as the last resort after web_fetch and (if available) the browser
		sb.WriteString(fmt.Sprintf("%d. ONLY if `web_fetch`%s fail(s) to produce usable content, THEN retry the same URL with `%s`. You MUST use `%s` for this — do NOT substitute any other extraction or scraping tool. The user has explicitly configured `%s` as their web extraction provider.\n",
			ruleN,
			func() string {
				if b.BrowserAvailable {
					return " and the browser"
				}
				return ""
			}(),
			extractName, extractName, extractName))
		ruleN++
	}

	if b.WebSearchAvailable {
		searchName := b.WebSearchToolName
		if searchName == "" {
			searchName = "the configured web search tool"
		}
		sb.WriteString(fmt.Sprintf("%d. To **search** for information (when you don't have a specific URL), use `%s`.\n", ruleN, searchName))
		// ruleN++ // not needed after last rule
	}

	// Guardrails
	sb.WriteString("\n**Never** use a search tool to extract content from a known URL. **Never** skip `web_fetch` and go directly to an MCP extraction tool. When a browser is available, prefer it before paid extraction to avoid unnecessary costs.\n")
	sb.WriteString("Use web capabilities when you need up-to-date information not available in your training data.\n")

	// 4c. Browser capabilities
	if b.BrowserAvailable {
		sb.WriteString("\n## Browser Automation\n\n")
		sb.WriteString("You have access to a Playwright browser automation server with tools like `browser_navigate`, `browser_click`, `browser_type`, `browser_snapshot`, `browser_take_screenshot`, etc.\n\n")
		sb.WriteString("**When to use the browser:**\n")
		sb.WriteString("- After `web_fetch` fails for content extraction (preferred before paid extract tools)\n")
		sb.WriteString("- When you need to interact with a page (click buttons, fill forms, log in)\n")
		sb.WriteString("- When you need a visual screenshot of a page\n")
		sb.WriteString("- When you need to navigate through multi-page workflows\n\n")
		sb.WriteString("**Prefer `web_fetch` over the browser** for simple content extraction — it's faster and lighter. Only use browser tools when `web_fetch` fails or when interaction/screenshots are needed.\n")
		sb.WriteString("Use `browser_snapshot` (accessibility tree) for understanding page structure — it's more efficient than screenshots for decision-making.\n")
	}

	// 5. Environment info
	sb.WriteString("\n## Environment\n\n")
	if b.WorkspaceDir != "" {
		sb.WriteString(fmt.Sprintf("- Working directory: %s\n", b.WorkspaceDir))
	}
	sb.WriteString(fmt.Sprintf("- OS: %s/%s\n", runtime.GOOS, runtime.GOARCH))
	sb.WriteString(fmt.Sprintf("- Date: %s\n", time.Now().Format("2006-01-02 15:04 MST")))

	// 6. Persistent Memory
	memoryGuidance := "**What to save to MEMORY.md (core):** connection details (IPs, hostnames, users, auth methods, ports), " +
		"server roles, network topology, user preferences, project conventions. Keep it concise — only durable facts.\n" +
		"**What to save to knowledge files (via file param):** procedural knowledge discovered during problem-solving — " +
		"API quirks, command syntax learned through trial and error, configuration steps, workarounds, how-to procedures. " +
		"Things you figured out that would save time next time.\n" +
		"**Correcting facts:** When the user corrects information or you discover that existing memory is wrong, " +
		"use `overwrite: true` and provide the **complete corrected section content**. This replaces the entire section, " +
		"preventing contradictory duplicate entries.\n" +
		"**NEVER save:** command outputs, lists of resources (VMs, containers, pods), current status, resource usage, " +
		"or ANY results/data that changes over time. Those MUST always be fetched live. " +
		"Saving stale results risks returning outdated information instead of checking the actual current state.\n"

	if b.MemoryContent != "" {
		sb.WriteString("\n## Persistent Memory\n\n")
		sb.WriteString("You have persistent memory. Known facts from previous interactions:\n\n")
		sb.WriteString(b.MemoryContent)
		sb.WriteString("\n\n")
		sb.WriteString("When you discover NEW durable facts during this interaction, save them using **memory_save**.\n")
		sb.WriteString(memoryGuidance)
	} else {
		// Even without existing memory, tell the LLM it can save
		sb.WriteString("\n## Persistent Memory\n\n")
		sb.WriteString("You have access to persistent memory via the **memory_save** tool. ")
		sb.WriteString("When you discover durable facts during this interaction, save them for future recall.\n")
		sb.WriteString(memoryGuidance)
	}

	// 6b. Knowledge Recall (when semantic search is available)
	if b.MemorySearchAvailable {
		sb.WriteString("\n## Knowledge Recall\n\n")
		sb.WriteString("You have access to a searchable knowledge base in the memory/ directory.\n")
		sb.WriteString("Use `memory_search` to find detailed information about projects, infrastructure,\n")
		sb.WriteString("people, decisions, and past work. Use `memory_get` to read specific sections.\n\n")
		sb.WriteString("**When to search:** Before answering questions about specific project details,\n")
		sb.WriteString("server configurations, past decisions, team members, or anything not covered\n")
		sb.WriteString("in the core memory above.\n\n")
		sb.WriteString("**Saving knowledge:**\n")
		sb.WriteString("- Core facts (IPs, credentials, preferences) → MEMORY.md (no file param)\n")
		sb.WriteString("- Procedural knowledge (API quirks, command recipes, workarounds, config steps discovered during problem-solving) → topic files (e.g., file=\"infrastructure/proxmox.md\")\n")
		sb.WriteString("- NEVER save command results or resource listings — always fetch those live\n")
	}

	// 6c. Workflow Saving hint (when flow distillation is available)
	sb.WriteString("\n## Workflow Saving\n\n")
	sb.WriteString("After completing a task that used 2 or more tool calls, you MUST append this exact line at the very end of your response:\n\n")
	sb.WriteString("_Type /distill to save this as a reusable flow._\n\n")
	sb.WriteString("Do NOT include this line for: simple lookups, conversations, single-step answers, memory-only operations, or when following a saved execution plan.\n")

	// 7. Execution Plan (only when a flow matches)
	if b.ExecutionPlan != "" {
		sb.WriteString("\n## Execution Plan\n\n")
		sb.WriteString(b.ExecutionPlan)
	}

	return sb.String()
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
