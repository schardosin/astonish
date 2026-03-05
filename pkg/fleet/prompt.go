package fleet

import (
	"fmt"
	"strings"

	"github.com/schardosin/astonish/pkg/persona"
)

// BuildSystemPromptSection generates the fleet section for the ChatAgent's
// system prompt. This provides lightweight fleet awareness: listing available
// fleets and explaining fleet mode.
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
	sb.WriteString("You have access to agent fleets: teams of specialized AI agents that\n")
	sb.WriteString("collaborate on complex tasks through conversation.\n\n")

	sb.WriteString("**How fleets work:**\n")
	sb.WriteString("Fleets are autonomous agent teams that communicate through a shared channel.\n")
	sb.WriteString("Each agent has a role (PO, architect, developer, etc.) and they talk to each\n")
	sb.WriteString("other and to the human using @mentions. The human is a first-class participant.\n\n")

	sb.WriteString("**Starting a fleet:**\n")
	sb.WriteString("Fleets are started via the Studio UI or CLI (`astonish fleet start`).\n")
	sb.WriteString("They create a dedicated session where agents and the human collaborate.\n\n")

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

	return sb.String()
}

// BuildAgentPrompt builds the system prompt for an agent when activated
// in a fleet session. It combines:
// - The persona identity
// - Fleet behaviors (from the fleet YAML)
// - Communication graph awareness (who the agent can talk to)
// - Delegate tool instructions (if applicable)
func BuildAgentPrompt(personaCfg *persona.PersonaConfig, agentCfg FleetAgentConfig, fleetCfg *FleetConfig, agentKey string) string {
	var sb strings.Builder

	// Persona identity
	sb.WriteString(personaCfg.Prompt)
	sb.WriteString("\n")

	// Fleet behaviors
	if agentCfg.Behaviors != "" {
		sb.WriteString("\n## Team Behaviors\n\n")
		sb.WriteString(agentCfg.Behaviors)
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
			if target == "human" {
				sb.WriteString("- **@human** — The customer/requester. Ask questions, present deliverables for approval.\n")
			} else {
				sb.WriteString(fmt.Sprintf("- **@%s** — Team member. Route work or ask questions.\n", target))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("**How to communicate:**\n")
	sb.WriteString("- Use @mentions naturally in your messages to address team members.\n")
	sb.WriteString("  For example: '@architect, can you review this?' or 'Thanks @po, I'll proceed.'\n")
	sb.WriteString("- The system automatically determines who should act next based on your message.\n")
	sb.WriteString("  You do NOT need a special format or a single @mention at the end.\n")
	sb.WriteString("- If you want the customer to respond, address @human with a question or deliverable.\n")
	sb.WriteString("- If you are handing off work to someone, make it clear in your message what you need them to do.\n\n")

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
	sb.WriteString("- `timeout`: Generous timeout in seconds (e.g., 300-600) for complex tasks.\n")
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
