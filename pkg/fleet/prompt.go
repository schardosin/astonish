package fleet

import (
	"fmt"
	"regexp"
	"strings"
)

// curlyPlaceholderRe matches {variable} patterns that ADK's InjectSessionState
// would try to resolve as session state keys. We escape them in prompt text.
var curlyPlaceholderRe = regexp.MustCompile(`\{([^{}]+)\}`)

// BuildSystemPromptSection generates the fleet section for the ChatAgent's
// system prompt. This provides lightweight fleet awareness: listing available
// fleets and explaining fleet mode.
func BuildSystemPromptSection(
	fleets []FleetSummary,
	fleetResolver func(key string) (*FleetConfig, bool),
) string {
	if len(fleets) == 0 {
		return ""
	}

	var sb strings.Builder

	sb.WriteString("\n## Fleet-Based Development\n\n")
	sb.WriteString("You have access to agent fleets: teams of specialized AI agents that\n")
	sb.WriteString("collaborate on complex tasks through conversation.\n\n")

	sb.WriteString("**How fleets work:**\n")
	sb.WriteString("Fleets are autonomous agent teams that communicate through a shared channel.\n")
	sb.WriteString("Each agent has a role (PO, architect, developer, etc.) and they talk to each\n")
	sb.WriteString("other and to the customer using @mentions. The customer is a first-class participant.\n\n")

	sb.WriteString("**Starting a fleet:**\n")
	sb.WriteString("Fleets are started via the Studio UI or CLI (`astonish fleet start`).\n")
	sb.WriteString("They create a dedicated session where agents and the customer collaborate.\n\n")

	// List available fleets
	sb.WriteString("**Available fleets:**\n\n")
	for _, summary := range fleets {
		f, ok := fleetResolver(summary.Key)
		if !ok {
			continue
		}

		sb.WriteString(fmt.Sprintf("- **%s** (key: `%s`)", f.Name, summary.Key))
		if f.Description != "" {
			sb.WriteString(fmt.Sprintf(" — %s", f.Description))
		}
		sb.WriteString("\n")

		// Show communication flow
		if f.Communication != nil && len(f.Communication.Flow) > 0 {
			roles := make([]string, 0, len(f.Communication.Flow))
			for _, node := range f.Communication.Flow {
				roles = append(roles, node.Role)
			}
			sb.WriteString(fmt.Sprintf("  Flow: %s\n", strings.Join(roles, " → ")))
		}

		// List available agents
		if len(f.Agents) > 0 {
			agentParts := make([]string, 0, len(f.Agents))
			for key, agent := range f.Agents {
				agentParts = append(agentParts, fmt.Sprintf("%s (%s)", key, agent.Name))
			}
			sb.WriteString(fmt.Sprintf("  Agents: %s\n", strings.Join(agentParts, ", ")))
		}
	}
	sb.WriteString("\n")

	return sb.String()
}

// escapeCurlyPlaceholders replaces {variable} patterns in text with <variable>
// so that ADK's InjectSessionState does not treat user-authored text as session
// state variable references.
func escapeCurlyPlaceholders(s string) string {
	return curlyPlaceholderRe.ReplaceAllString(s, "<$1>")
}

// BuildAgentPrompt builds the system prompt for an agent when activated
// in a fleet session. It combines:
// - The agent identity (inline in the fleet config)
// - Fleet behaviors (from the fleet YAML)
// - Communication graph awareness (who the agent can talk to)
// - Delegate tool instructions (if applicable)
// - Progress tracker state (milestones from this session)
// - Project context (e.g., AGENTS.md content from the workspace)
func BuildAgentPrompt(agentCfg FleetAgentConfig, fleetCfg *FleetConfig, agentKey string, progress *ProgressTracker, projectContext string, taskSlug string, plan ...*FleetPlan) string {
	var sb strings.Builder

	// Agent identity — escape {placeholders} to prevent ADK's
	// InjectSessionState from interpreting user-authored text.
	sb.WriteString(escapeCurlyPlaceholders(agentCfg.Identity))
	sb.WriteString("\n")

	// Fleet behaviors — also user-authored, needs escaping.
	if agentCfg.Behaviors != "" {
		sb.WriteString("\n## Team Behaviors\n\n")
		sb.WriteString(escapeCurlyPlaceholders(agentCfg.Behaviors))
		sb.WriteString("\n")
	}

	// Communication graph awareness
	sb.WriteString("\n## Communication Rules\n\n")
	sb.WriteString("You are part of a team communicating through a shared conversation thread.\n")
	sb.WriteString("You can see all messages in the thread from all participants.\n\n")

	talksTo := fleetCfg.GetTalksTo(agentKey)
	if len(talksTo) > 0 {
		sb.WriteString("**You can communicate with:**\n")
		for _, target := range talksTo {
			if target == "customer" {
				sb.WriteString("- **@customer** — The customer/requester. Ask questions, present deliverables for approval.\n")
			} else {
				sb.WriteString(fmt.Sprintf("- **@%s** — Team member. Route work or ask questions.\n", target))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("**How communication works (IMPORTANT):**\n")
	sb.WriteString("Your TEXT OUTPUT is how you communicate with team members. When you write\n")
	sb.WriteString("a message addressing someone (e.g., \"@dev, please implement...\"), the system\n")
	sb.WriteString("automatically activates that person with your message. This is the ONLY way\n")
	sb.WriteString("to delegate work or ask questions.\n\n")
	sb.WriteString("- Use @mentions in your text to address team members.\n")
	sb.WriteString("  Example: \"@architect, please design the technical solution for this feature.\"\n")
	sb.WriteString("  Example: \"@customer, here are the requirements. Do you approve?\"\n")
	sb.WriteString("- Do NOT use shell_command, echo, or any tool to \"send\" messages.\n")
	sb.WriteString("  Your text output IS the message. Tools are for file operations and code.\n")
	sb.WriteString("- The system determines who acts next based on the @mentions in your message.\n")
	sb.WriteString("- If you want the customer to respond, address @customer and STOP. Wait for their reply.\n\n")

	sb.WriteString("**Progress updates:**\n")
	sb.WriteString("Your messages are posted to the team channel in real time as you work.\n")
	sb.WriteString("When you use tools (reading files, writing code, running commands), write a short\n")
	sb.WriteString("status update BEFORE each tool call so the team can follow your progress. For example:\n")
	sb.WriteString("- \"Reading the requirements document to understand the scope.\"\n")
	sb.WriteString("- \"Requirements are clear. I'll now design the API structure.\"\n")
	sb.WriteString("- \"Implementation complete. Created 3 files: ...\"\n\n")
	sb.WriteString("Keep updates concise (1-2 sentences). Do NOT include internal reasoning, tool arguments,\n")
	sb.WriteString("or raw tool output. The team cares about WHAT you are doing and WHAT you found, not HOW\n")
	sb.WriteString("the tools work internally.\n\n")

	sb.WriteString("**Important:**\n")
	sb.WriteString("- Think before acting. Ask questions when information is missing.\n")
	sb.WriteString("- Do NOT produce deliverables if you lack critical information. Ask first.\n")
	sb.WriteString("- The conversation thread gives you full context of what has happened so far.\n")

	// Delegate tool instructions
	if agentCfg.Delegate != nil {
		buildDelegatePromptSection(&sb, agentCfg.Delegate)
	}

	// Progress tracker state (milestones: approvals, completions, handoffs)
	if progress != nil {
		progressSection := progress.FormatForPrompt()
		if progressSection != "" {
			sb.WriteString(progressSection)
		}
	}

	// Fleet plan environment (channel + artifacts).
	// The environment section may contain {placeholder} patterns in config values
	// (e.g., branch_pattern: "fleet/{task}"). These must be escaped to prevent
	// ADK's InjectSessionState from treating them as session state keys.
	if len(plan) > 0 && plan[0] != nil {
		var envSb strings.Builder
		buildEnvironmentPromptSection(&envSb, plan[0], taskSlug)
		sb.WriteString(escapeCurlyPlaceholders(envSb.String()))
	}

	// Project context (e.g., AGENTS.md generated by OpenCode /init).
	// Gives agents understanding of the codebase structure, build commands,
	// conventions, and key patterns. Only present when the fleet template
	// defines a project_context section and the file was generated/loaded.
	if projectContext != "" {
		sb.WriteString("\n## Project Context\n\n")
		sb.WriteString(escapeCurlyPlaceholders(projectContext))
		sb.WriteString("\n")
	}

	return sb.String()
}

// buildDelegatePromptSection appends delegate tool instructions to a prompt builder.
func buildDelegatePromptSection(sb *strings.Builder, d *DelegateConfig) {
	sb.WriteString("\n## Delegate Tool\n\n")
	sb.WriteString(fmt.Sprintf("You have access to the `%s` tool for delegating work.\n\n", d.Tool))

	if d.Description != "" {
		sb.WriteString(d.Description)
		sb.WriteString("\n")
	}

	sb.WriteString("### How to Use\n\n")
	sb.WriteString(fmt.Sprintf("Call the `%s` tool with:\n", d.Tool))
	sb.WriteString("- `task`: A concise goal describing the desired outcome.\n")
	sb.WriteString("- `dir`: The project's working directory.\n")
	sb.WriteString("- `session_id`: Optional. Use the session_id from a previous result for follow-up tasks.\n\n")

	sb.WriteString("### Delegation Guidelines\n\n")
	sb.WriteString(fmt.Sprintf("`%s` is an **autonomous coding agent**, not a text generator. ", d.Tool))
	sb.WriteString("It reads files, makes design decisions, plans its own approach, and breaks work into steps.\n\n")
	sb.WriteString("**DO:**\n")
	sb.WriteString("- State the desired outcome clearly\n")
	sb.WriteString("- List the input file paths it should read\n")
	sb.WriteString("- Specify the output file path\n")
	sb.WriteString("- Mention key constraints or preferences\n\n")
	sb.WriteString("**DO NOT:**\n")
	sb.WriteString("- Prescribe the exact structure or content of the output\n")
	sb.WriteString("- Paste file contents into the task\n")
	sb.WriteString("- Write multi-page prompts\n\n")
	sb.WriteString(fmt.Sprintf("Trust `%s` to read the inputs and produce quality output.\n", d.Tool))
	sb.WriteString("A good task prompt is typically 3-10 sentences.\n\n")

	sb.WriteString("### After Completion\n\n")
	sb.WriteString(fmt.Sprintf("- After %s completes, use `read_file` to verify the expected deliverables exist.\n", d.Tool))
	sb.WriteString("- If the result is incomplete, call it again with specific follow-up instructions.\n")
	sb.WriteString("- Use the returned `session_id` to continue in the same context.\n")
}

// buildEnvironmentPromptSection appends channel and artifact configuration
// from a fleet plan to the agent's prompt. This tells the agent where work
// items come from, where artifacts should be delivered, and how to use the
// git branching workflow when artifacts target git repositories.
func buildEnvironmentPromptSection(sb *strings.Builder, plan *FleetPlan, taskSlug string) {
	sb.WriteString("\n## Environment Configuration\n\n")
	sb.WriteString("This fleet session was started from a fleet plan with the following environment settings.\n")
	sb.WriteString("Adjust your behavior accordingly.\n\n")

	// Workspace directory (most important: prevents agents from searching the filesystem)
	workspaceDir := plan.ResolveWorkspaceDir()
	if workspaceDir != "" {
		sb.WriteString("**Project Workspace:**\n")
		sb.WriteString(fmt.Sprintf("- All project files live under: `%s`\n", workspaceDir))
		sb.WriteString("- Source code follows the project's existing directory structure.\n")
		sb.WriteString("  Consult the Project Context section (AGENTS.md) for the actual layout.\n")

		// Derive the documentation path from the "docs" artifact config if available
		if docsArtifact, ok := plan.Artifacts["docs"]; ok && docsArtifact.SubPath != "" {
			if taskSlug != "" {
				sb.WriteString(fmt.Sprintf("- Fleet documentation for this task goes in: `%s/%s/%s/`\n", workspaceDir, docsArtifact.SubPath, taskSlug))
			} else {
				sb.WriteString(fmt.Sprintf("- Fleet documentation goes in: `%s/%s/<task-name>/`\n", workspaceDir, docsArtifact.SubPath))
				sb.WriteString(fmt.Sprintf("  Each task gets its own subfolder under %s/.\n", docsArtifact.SubPath))
			}
		} else if taskSlug != "" {
			sb.WriteString(fmt.Sprintf("- Fleet documentation for this task: organize under the task slug `%s`\n", taskSlug))
		}

		sb.WriteString("- This directory has already been created for you.\n")
		sb.WriteString("- **IMPORTANT:** Always use this workspace path for ALL file operations.\n")
		sb.WriteString("  Do NOT search the filesystem for the project. Do NOT use `/` or `~` as starting points.\n\n")
	}

	// Channel info
	sb.WriteString("**Communication Channel:**\n")
	chType := plan.Channel.Type
	if chType == "" {
		chType = "chat"
	}
	sb.WriteString(fmt.Sprintf("- Type: `%s`\n", chType))
	for k, v := range plan.Channel.Config {
		sb.WriteString(fmt.Sprintf("- %s: `%v`\n", k, v))
	}
	if plan.Channel.Schedule != "" {
		sb.WriteString(fmt.Sprintf("- Polling schedule: `%s`\n", plan.Channel.Schedule))
	}
	sb.WriteString("\n")

	// Artifact destinations
	if len(plan.Artifacts) > 0 {
		sb.WriteString("**Artifact Destinations:**\n")
		for name, artifact := range plan.Artifacts {
			sb.WriteString(fmt.Sprintf("- **%s** (%s)", name, artifact.Type))
			switch artifact.Type {
			case "local":
				if artifact.Path != "" {
					sb.WriteString(fmt.Sprintf(": path `%s`", artifact.Path))
				}
			case "git_repo":
				if artifact.Repo != "" {
					sb.WriteString(fmt.Sprintf(": repo `%s`", artifact.Repo))
				}
				if artifact.BranchPattern != "" {
					resolved := ResolveBranchPattern(artifact.BranchPattern, taskSlug)
					sb.WriteString(fmt.Sprintf(", branch `%s`", resolved))
				}
				if artifact.SubPath != "" {
					sb.WriteString(fmt.Sprintf(", subpath `%s`", artifact.SubPath))
				}
				if artifact.AutoPR {
					sb.WriteString(", auto-PR enabled")
				}
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
		sb.WriteString("When producing deliverables, ensure files are written to the correct artifact destination.\n")
		if workspaceDir != "" {
			sb.WriteString(fmt.Sprintf("All artifact destinations are relative to the project workspace at `%s`.\n", workspaceDir))
		} else {
			sb.WriteString("If an artifact destination specifies a git repo, work within that repo's directory.\n")
		}
	}

	// Git workflow instructions — only when at least one artifact uses git_repo
	buildGitWorkflowSection(sb, plan, taskSlug, workspaceDir)

	// Available credentials
	if len(plan.Credentials) > 0 {
		sb.WriteString("\n**Available Credentials:**\n")
		for logicalName, storeName := range plan.Credentials {
			sb.WriteString(fmt.Sprintf("- **%s** (credential: `%s`)", logicalName, storeName))
			switch strings.ToLower(logicalName) {
			case "github":
				sb.WriteString(": The `gh` CLI and `git` commands are pre-configured with these credentials. No manual auth setup needed.")
			case "jira":
				sb.WriteString(": Use the `web_fetch` tool with appropriate auth headers for Jira API calls.")
			case "ssh", "deploy-ssh":
				sb.WriteString(": Use `shell_command` with SSH for remote operations.")
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\nCredential secrets are injected into the environment automatically. ")
		sb.WriteString("Do NOT attempt to read or log credential values.\n")
	}
}

// buildGitWorkflowSection appends explicit git branching instructions when the
// fleet plan uses git_repo artifacts. This prevents agents from pushing directly
// to main/master by giving them a concrete branch name and step-by-step workflow.
func buildGitWorkflowSection(sb *strings.Builder, plan *FleetPlan, taskSlug, workspaceDir string) {
	// Collect all git_repo artifacts with a branch pattern
	var resolvedBranch string
	var repo string
	hasGitArtifacts := false
	for _, artifact := range plan.Artifacts {
		if artifact.Type == "git_repo" {
			hasGitArtifacts = true
			if artifact.Repo != "" && repo == "" {
				repo = artifact.Repo
			}
			if artifact.BranchPattern != "" && taskSlug != "" && resolvedBranch == "" {
				resolvedBranch = ResolveBranchPattern(artifact.BranchPattern, taskSlug)
			}
		}
	}

	if !hasGitArtifacts {
		return
	}

	sb.WriteString("\n## Git Workflow\n\n")
	sb.WriteString("**CRITICAL: NEVER push directly to the `main` or `master` branch.**\n")
	sb.WriteString("All work MUST be done on a dedicated feature branch.\n\n")

	if resolvedBranch != "" {
		sb.WriteString(fmt.Sprintf("**Feature branch for this task:** `%s`\n\n", resolvedBranch))
		sb.WriteString("**Before making any changes**, set up the branch:\n")
		sb.WriteString("```\n")
		if workspaceDir != "" {
			sb.WriteString(fmt.Sprintf("cd %s\n", workspaceDir))
		}
		sb.WriteString("git fetch origin\n")
		sb.WriteString(fmt.Sprintf("git checkout -b %s origin/main  # or origin/master\n", resolvedBranch))
		sb.WriteString("```\n\n")
		sb.WriteString("If the branch already exists (from a previous agent activation), check it out:\n")
		sb.WriteString("```\n")
		sb.WriteString(fmt.Sprintf("git checkout %s && git pull origin %s\n", resolvedBranch, resolvedBranch))
		sb.WriteString("```\n\n")
	} else {
		sb.WriteString("Create a descriptive feature branch before making changes.\n")
		sb.WriteString("Use a name like `fleet/<short-description>` (e.g., `fleet/add-today-line`).\n\n")
	}

	sb.WriteString("**Git workflow for every commit:**\n")
	sb.WriteString("1. Make your changes on the feature branch (never on main/master)\n")
	sb.WriteString("2. `git add` the changed files\n")
	sb.WriteString("3. `git commit -m \"<type>: <description>\"` with a descriptive message\n")
	sb.WriteString("4. `git push origin <branch-name>` to push to the remote feature branch\n\n")
	sb.WriteString("**NEVER run** `git push origin main` or `git push origin master`. This is strictly forbidden.\n")
	sb.WriteString("If you find yourself on main/master, switch to the feature branch first.\n\n")

	// Document commit + link-sharing instructions
	sb.WriteString("**Commit and share every document you write:**\n")
	sb.WriteString("Every agent — not just the developer — MUST commit and push documents they produce.\n")
	sb.WriteString("After writing or updating any file (requirements, architecture, UX design, test plans, etc.):\n")
	sb.WriteString("1. `git add <filepath>` and `git commit -m \"docs: <description>\"` on the feature branch\n")
	sb.WriteString("2. `git push origin <branch-name>` to make it available on GitHub\n")
	sb.WriteString("3. **When reporting the document** to another agent or the customer, you MUST include\n")
	sb.WriteString("   a direct GitHub link so the reader can open and review it.\n\n")

	// Provide a concrete link template when we have repo + branch
	if repo != "" && resolvedBranch != "" {
		sb.WriteString("**GitHub link format** (use this exact pattern):\n")
		sb.WriteString(fmt.Sprintf("```\nhttps://github.com/%s/blob/%s/<filepath-relative-to-repo-root>\n```\n", repo, resolvedBranch))
		sb.WriteString("Replace `<filepath-relative-to-repo-root>` with the actual path of the file you wrote.\n")
		sb.WriteString(fmt.Sprintf("For example, if you wrote `docs/my-task/requirements.md`, the link is:\n"))
		sb.WriteString(fmt.Sprintf("`https://github.com/%s/blob/%s/docs/my-task/requirements.md`\n\n", repo, resolvedBranch))
	}

	sb.WriteString("**This is mandatory.** Do NOT just mention a file path — always include the clickable GitHub link.\n")
	sb.WriteString("The customer and other agents need to be able to open and read your documents directly.\n")

	// Auto-PR instructions — when any artifact has auto_pr enabled, inject
	// explicit instructions for creating a pull request after all work is
	// pushed. Without these, agents only see a vague "auto-PR enabled" hint
	// and inconsistently create PRs.
	hasAutoPR := false
	for _, artifact := range plan.Artifacts {
		if artifact.Type == "git_repo" && artifact.AutoPR {
			hasAutoPR = true
			break
		}
	}
	if hasAutoPR && resolvedBranch != "" {
		sb.WriteString("\n## Auto-PR: Pull Request Creation\n\n")
		sb.WriteString("**Auto-PR is enabled.** After all work is complete and pushed to the feature branch,\n")
		sb.WriteString("you MUST create a pull request. This is mandatory — do NOT skip this step.\n\n")
		sb.WriteString("**Create the PR using the `gh` CLI** (already authenticated):\n")
		sb.WriteString("```\n")
		sb.WriteString(fmt.Sprintf("gh pr create --base main --head %s --title \"<title>\" --body \"<body>\"\n", resolvedBranch))
		sb.WriteString("```\n\n")
		sb.WriteString("**PR title:** Use the issue title or a clear summary of the changes.\n")
		sb.WriteString("**PR body:** Include a brief summary of what was implemented, key files changed,\n")
		sb.WriteString("and a reference to the issue (e.g., `Closes #<number>` or `Related to #<number>`).\n\n")
		sb.WriteString("After creating the PR, include the PR link in your completion report.\n")
		sb.WriteString("If `gh pr create` fails because a PR already exists for this branch, that is fine — just\n")
		sb.WriteString("include the existing PR link instead.\n")
	}
}
