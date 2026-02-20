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
	Tools         []tool.Tool
	Toolsets      []tool.Toolset
	WorkspaceDir  string
	CustomPrompt  string
	MemoryContent string // Contents of MEMORY.md (loaded per turn)
	ExecutionPlan string // Flow-based execution plan (set when a flow matches)
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

	// 5. Environment info
	sb.WriteString("\n## Environment\n\n")
	if b.WorkspaceDir != "" {
		sb.WriteString(fmt.Sprintf("- Working directory: %s\n", b.WorkspaceDir))
	}
	sb.WriteString(fmt.Sprintf("- OS: %s/%s\n", runtime.GOOS, runtime.GOARCH))
	sb.WriteString(fmt.Sprintf("- Date: %s\n", time.Now().Format("2006-01-02 15:04 MST")))

	// 6. Persistent Memory
	memoryGuidance := "**What to save:** connection details (IPs, hostnames, users, auth methods, ports), " +
		"server roles, network topology, user preferences, project conventions.\n" +
		"**What NOT to save:** lists of VMs/containers/pods, their running status, resource usage (RAM, disk, CPU), " +
		"command outputs, or ANY data that changes over time. Those must always be fetched live.\n"

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
