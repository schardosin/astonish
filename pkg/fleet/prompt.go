package fleet

import (
	"fmt"
	"strings"

	"github.com/schardosin/astonish/pkg/persona"
)

// BuildSystemPromptSection generates the fleet section for the ChatAgent's
// system prompt. This provides lightweight fleet awareness: listing available
// fleets and guiding the LLM to use fleet_plan/fleet_execute tools.
//
// The leader persona is NOT injected here (it lives in the orchestrator
// sub-agent built by BuildOrchestratorPrompt). The main ChatAgent keeps
// its general assistant identity.
func BuildSystemPromptSection(
	fleets []FleetSummary,
	fleetResolver func(key string) (*FleetConfig, bool),
	personaResolver func(key string) (*persona.PersonaConfig, bool),
) string {
	if len(fleets) == 0 {
		return ""
	}

	var sb strings.Builder

	sb.WriteString("\n## Fleet-Based Development\n\n")
	sb.WriteString("You have access to agent fleets: teams of specialized AI agents that can\n")
	sb.WriteString("handle complex multi-phase tasks collaboratively.\n\n")

	sb.WriteString("**When to use fleets:**\n")
	sb.WriteString("- Substantial development tasks involving multiple concerns (requirements,\n")
	sb.WriteString("  architecture, implementation, testing, security)\n")
	sb.WriteString("- When the user explicitly asks to use a fleet or development team\n\n")

	sb.WriteString("**When NOT to use fleets:**\n")
	sb.WriteString("- Simple tasks (single file edits, quick fixes, one-step operations)\n")
	sb.WriteString("- Questions or explanations\n")
	sb.WriteString("- Tasks you can complete with a few tool calls\n\n")

	sb.WriteString("**How it works:**\n")
	sb.WriteString("1. Call `fleet_plan` to create or load a phased execution plan\n")
	sb.WriteString("2. Present the plan to the user for review and customization\n")
	sb.WriteString("3. Iterate until the user approves\n")
	sb.WriteString("4. Call `fleet_execute` with the approved plan\n")
	sb.WriteString("5. Summarize the results when execution completes\n\n")

	sb.WriteString("If you think a fleet could help with the user's request, suggest it:\n")
	sb.WriteString("\"This looks like a task that could benefit from the development fleet.\n")
	sb.WriteString("Want me to create a plan using specialized agents?\"\n\n")

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

		if f.SuggestedFlow != nil && len(f.SuggestedFlow.Phases) > 0 {
			phaseNames := make([]string, 0, len(f.SuggestedFlow.Phases))
			for _, phase := range f.SuggestedFlow.Phases {
				phaseNames = append(phaseNames, phase.Name)
			}
			sb.WriteString(fmt.Sprintf("  Workflow: %s\n", strings.Join(phaseNames, " → ")))
		}

		// List available agents for this fleet
		if len(f.Agents) > 0 {
			agentParts := make([]string, 0, len(f.Agents))
			for key, agent := range f.Agents {
				personaName := agent.Persona
				if personaResolver != nil {
					if p, pOk := personaResolver(agent.Persona); pOk {
						personaName = p.Name
					}
				}
				agentParts = append(agentParts, fmt.Sprintf("%s (%s)", key, personaName))
			}
			sb.WriteString(fmt.Sprintf("  Agents: %s\n", strings.Join(agentParts, ", ")))
		}
	}
	sb.WriteString("\n")
	sb.WriteString("When customizing fleet plans, you MUST only assign agents from the fleet's agent list above.\n")
	sb.WriteString("Do not invent agent names or roles that are not listed.\n\n")

	return sb.String()
}

// BuildOrchestratorPrompt builds the complete system prompt for the fleet
// orchestrator sub-agent. The orchestrator executes an approved fleet plan
// by delegating phases to worker sub-agents via run_fleet_phase.
//
// The prompt combines:
// - The leader persona identity (e.g., Project Lead)
// - Leader behaviors from the fleet config
// - The approved execution plan (phases, instructions, deliverables, dependencies)
// - Team roster with agent capabilities
// - User preferences
func BuildOrchestratorPrompt(
	leaderPersona *persona.PersonaConfig,
	fleetCfg *FleetConfig,
	plan *FleetPlan,
	request string,
) string {
	var sb strings.Builder

	// Leader persona identity
	sb.WriteString(leaderPersona.Prompt)
	sb.WriteString("\n")

	// Leader behaviors from fleet config
	if fleetCfg.Leader != nil && fleetCfg.Leader.Behaviors != "" {
		sb.WriteString("\n## Fleet Management Behaviors\n\n")
		sb.WriteString(fleetCfg.Leader.Behaviors)
		sb.WriteString("\n")
	}

	// The user's original request
	sb.WriteString("\n## User Request\n\n")
	sb.WriteString(request)
	sb.WriteString("\n")

	// The approved execution plan
	sb.WriteString("\n## Execution Plan\n\n")
	sb.WriteString(fmt.Sprintf("Fleet: `%s` (%s)\n\n", plan.BaseFleet, fleetCfg.Name))

	for i, phase := range plan.Phases {
		sb.WriteString(fmt.Sprintf("### Phase %d: %s\n", i+1, phase.Name))
		sb.WriteString(fmt.Sprintf("- **Primary:** `%s`\n", phase.GetPrimaryAgent()))
		if phase.IsConversation() {
			sb.WriteString(fmt.Sprintf("- **Reviewers:** `%s`\n", strings.Join(phase.Reviewers, "`, `")))
			sb.WriteString("- **Mode:** Conversation (primary + reviewers discuss before producing deliverables)\n")
		} else {
			sb.WriteString("- **Mode:** Single-agent\n")
		}
		if len(phase.DependsOn) > 0 {
			sb.WriteString(fmt.Sprintf("- **Depends on:** %s\n", strings.Join(phase.DependsOn, ", ")))
		}
		if phase.Instructions != "" {
			sb.WriteString(fmt.Sprintf("- **Instructions:** %s\n", strings.TrimSpace(phase.Instructions)))
		}
		if len(phase.Deliverables) > 0 {
			sb.WriteString("- **Expected deliverables:**\n")
			for _, d := range phase.Deliverables {
				sb.WriteString(fmt.Sprintf("  - %s\n", d))
			}
		}
		sb.WriteString("\n")
	}

	// Review dependencies
	if len(plan.Reviews) > 0 {
		sb.WriteString("### Review Dependencies\n\n")
		for reviewer, targets := range plan.Reviews {
			sb.WriteString(fmt.Sprintf("- **%s** reviews: %s\n", reviewer, strings.Join(targets, ", ")))
		}
		sb.WriteString("\n")
	}

	// User preferences
	if plan.Preferences != "" {
		sb.WriteString("### User Preferences\n\n")
		sb.WriteString(plan.Preferences)
		sb.WriteString("\n")
	}

	// Settings
	sb.WriteString(fmt.Sprintf("\nMax review iterations per phase: %d\n", plan.Settings.GetMaxReviewsPerPhase()))

	// Team roster
	sb.WriteString("\n## Your Team\n\n")
	sb.WriteString("Use `run_fleet_phase` to delegate work to these agents:\n\n")
	for key, agent := range fleetCfg.Agents {
		personaName := agent.Persona
		sb.WriteString(fmt.Sprintf("- **`%s`** (persona: %s)", key, personaName))
		if agent.Delegate != nil {
			sb.WriteString(fmt.Sprintf(" — delegates to %s", agent.Delegate.Tool))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	// Execution instructions
	sb.WriteString("## How to Execute\n\n")
	sb.WriteString("You have one tool for executing all phases: `run_fleet_phase`.\n\n")

	sb.WriteString("### run_fleet_phase\n\n")
	sb.WriteString("Call `run_fleet_phase` for every phase with:\n")
	sb.WriteString(fmt.Sprintf("- `fleet`: `%s`\n", plan.BaseFleet))
	sb.WriteString("- `phase`: the phase name (for tracking)\n")
	sb.WriteString("- `primary`: the agent key that produces deliverables\n")
	sb.WriteString("- `goal`: the task description (see context guidelines below)\n")
	sb.WriteString("- `deliverables`: (optional) array of expected output file paths\n")
	sb.WriteString("- `artifacts`: (optional) map of name -> file path for inputs from prior phases\n\n")

	sb.WriteString("**For conversation phases** (where agents need to discuss before producing deliverables),\n")
	sb.WriteString("also pass:\n")
	sb.WriteString("- `reviewers`: array of agent keys that review/discuss with the primary\n")
	sb.WriteString("- `max_turns`: (optional, default 10) max conversation turns\n\n")
	sb.WriteString("The tool manages the back-and-forth internally and returns when all\n")
	sb.WriteString("deliverable files exist on disk or max turns are reached.\n\n")

	sb.WriteString("**For phases that delegate to external tools** (e.g., OpenCode), set:\n")
	sb.WriteString("- `timeout`: generous timeout in seconds (e.g., 600) since delegate tools may take longer\n\n")

	sb.WriteString("### Context Guidelines\n\n")
	sb.WriteString("Agents run in isolation with no memory of other phases. How you pass context\n")
	sb.WriteString("depends on the agent type:\n\n")
	sb.WriteString("**Agents with built-in tools only** (e.g., PO using write_file directly):\n")
	sb.WriteString("Include the full context in the goal: the original request, relevant decisions,\n")
	sb.WriteString("constraints, and what to produce. These agents cannot read prior deliverables\n")
	sb.WriteString("unless you tell them where to find the files.\n\n")
	sb.WriteString("**Agents that delegate to external tools** (e.g., architect/dev/qa using OpenCode):\n")
	sb.WriteString("Keep the goal concise. State the desired outcome, list the input file paths\n")
	sb.WriteString("(from prior phases), and specify where to write the output. Do NOT paste file\n")
	sb.WriteString("contents or write multi-page instructions. The delegate tool is an autonomous\n")
	sb.WriteString("agent that will read the files and plan its own approach.\n\n")
	sb.WriteString("**IMPORTANT: All deliverables must be written to files on disk.** When a phase\n")
	sb.WriteString("produces a deliverable (document, code, tests, etc.), explicitly tell the agent\n")
	sb.WriteString("in the goal to write it to a specific file path and to include the\n")
	sb.WriteString("file path in their response. For example:\n")
	sb.WriteString("\"Write the requirements document to ~/project/docs/requirements.md\".\n")
	sb.WriteString("This is critical because downstream agents need to read these files from disk.\n")
	sb.WriteString("If a phase returns an ERROR status, do NOT assume the deliverable was written.\n")
	sb.WriteString("Check whether the expected files exist before proceeding. If they don't, re-run.\n\n")
	sb.WriteString("After each phase, review the output. If incomplete or incorrect, re-run with\n")
	sb.WriteString("specific feedback.\n")

	return sb.String()
}

// BuildSubAgentPrompt builds the complete system prompt for a fleet sub-agent.
// It combines the persona's identity prompt with fleet-level behaviors and
// delegate instructions (if applicable).
//
// When a delegate is configured, the prompt instructs the agent to use the
// delegate's native tool (e.g., the `opencode` tool) directly.
func BuildSubAgentPrompt(p *persona.PersonaConfig, agent FleetAgentConfig) string {
	var sb strings.Builder

	// Persona identity (immutable)
	sb.WriteString(p.Prompt)
	sb.WriteString("\n")

	// Fleet behaviors (team-specific)
	if agent.Behaviors != "" {
		sb.WriteString("\n## Team Behaviors\n")
		sb.WriteString(agent.Behaviors)
		sb.WriteString("\n")
	}

	// Delegate tool instructions
	if agent.Delegate != nil {
		buildDelegatePromptSection(&sb, agent.Delegate)
	}

	return sb.String()
}

// BuildConversationAgentPrompt builds the system prompt for an agent participating
// in a multi-agent conversation. Unlike BuildSubAgentPrompt, this variant tells
// the agent it is allowed (and encouraged) to ask questions when things are unclear,
// and that its questions will be forwarded to other team members.
//
// The role parameter ("primary" or "reviewer") adjusts the prompt emphasis:
// - Primary agents are told to ask questions when unsure and produce deliverables when ready.
// - Reviewer agents are told to answer questions, provide expertise, and update artifacts.
func BuildConversationAgentPrompt(p *persona.PersonaConfig, agent FleetAgentConfig, role string) string {
	var sb strings.Builder

	// Persona identity (immutable)
	sb.WriteString(p.Prompt)
	sb.WriteString("\n")

	// Fleet behaviors (team-specific)
	if agent.Behaviors != "" {
		sb.WriteString("\n## Team Behaviors\n")
		sb.WriteString(agent.Behaviors)
		sb.WriteString("\n")
	}

	// Conversation-specific instructions
	sb.WriteString("\n## Conversation Mode\n\n")
	sb.WriteString("You are participating in a multi-agent conversation with your team.\n")

	if role == "primary" {
		sb.WriteString("You are the **primary** agent responsible for producing deliverables.\n\n")
		sb.WriteString("**Important:**\n")
		sb.WriteString("- If requirements or context are unclear, ASK SPECIFIC QUESTIONS. Your questions will be forwarded to a reviewer.\n")
		sb.WriteString("- Do NOT guess or make assumptions when information is missing. Ask first.\n")
		sb.WriteString("- When you have enough clarity, produce the deliverables by writing them to the specified file paths.\n")
		sb.WriteString("- If you produce deliverables, make sure they are complete and written to disk.\n")
	} else {
		sb.WriteString("You are a **reviewer** providing expertise and answering questions.\n\n")
		sb.WriteString("**Important:**\n")
		sb.WriteString("- Answer all questions from the primary agent thoroughly and precisely.\n")
		sb.WriteString("- If you need to update artifacts (e.g., clarify requirements in a document), use your tools to edit the files.\n")
		sb.WriteString("- Provide your expert perspective. If you see issues with the approach, say so.\n")
		sb.WriteString("- Be concise but complete in your responses.\n")
	}

	// Delegate tool instructions (same as BuildSubAgentPrompt)
	if agent.Delegate != nil {
		buildDelegatePromptSection(&sb, agent.Delegate)
	}

	return sb.String()
}

// buildDelegatePromptSection appends delegate tool instructions to a prompt builder.
// It tells the agent to use the delegate's native tool (e.g., `opencode`) with
// concise, goal-oriented prompts rather than prescriptive instructions.
func buildDelegatePromptSection(sb *strings.Builder, d *DelegateConfig) {
	sb.WriteString("\n## Delegate Tool\n\n")
	sb.WriteString(fmt.Sprintf("You have access to the `%s` tool for delegating work.\n\n", d.Tool))

	if d.Description != "" {
		sb.WriteString(d.Description)
		sb.WriteString("\n")
	}

	sb.WriteString("### How to Use\n\n")
	sb.WriteString(fmt.Sprintf("Call the `%s` tool with:\n", d.Tool))
	sb.WriteString("- `task`: A concise goal describing the desired outcome. See delegation guidelines below.\n")
	sb.WriteString("- `dir`: The project's working directory.\n")
	sb.WriteString("- `timeout`: Generous timeout in seconds (e.g., 300-600) for complex tasks.\n")
	sb.WriteString("- `session_id`: Optional. Use the session_id from a previous result for follow-up tasks.\n\n")

	sb.WriteString("### Delegation Guidelines\n\n")
	sb.WriteString(fmt.Sprintf("`%s` is an **autonomous coding agent**, not a text generator. ", d.Tool))
	sb.WriteString("It reads files, makes design decisions, plans its own approach, and breaks work into steps.\n\n")
	sb.WriteString("**DO:**\n")
	sb.WriteString("- State the desired outcome clearly (e.g., \"Create a technical design document\")\n")
	sb.WriteString("- List the input file paths it should read (e.g., \"Read ~/project/docs/requirements.md\")\n")
	sb.WriteString("- Specify the output file path (e.g., \"Write the result to ~/project/docs/design.md\")\n")
	sb.WriteString("- Mention key constraints or preferences (e.g., \"vanilla JS, no frameworks\")\n\n")
	sb.WriteString("**DO NOT:**\n")
	sb.WriteString("- Prescribe the exact structure, sections, or content of the output\n")
	sb.WriteString("- Paste or summarize the contents of input files into the task (it will read them itself)\n")
	sb.WriteString("- Write multi-page prompts detailing every aspect of what to produce\n")
	sb.WriteString("- Micromanage how it should organize its work\n\n")
	sb.WriteString(fmt.Sprintf("Trust `%s` to read the inputs, understand the domain, and produce quality output.\n", d.Tool))
	sb.WriteString("A good task prompt is typically 3-10 sentences, not pages.\n\n")

	sb.WriteString("### After Completion\n\n")
	sb.WriteString(fmt.Sprintf("- After %s completes, use `read_file` to verify the expected deliverables exist.\n", d.Tool))
	sb.WriteString("- If the result is incomplete, call it again with specific follow-up instructions.\n")
	sb.WriteString("- Use the returned `session_id` to continue in the same context for follow-up work.\n")
}
