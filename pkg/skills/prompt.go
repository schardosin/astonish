package skills

import (
	"fmt"
	"strings"
)

// BuildSkillIndex creates the lightweight skill listing for the system prompt.
// All installed skills are listed (eligible and ineligible) so the agent can discover
// them. Ineligible skills are marked with their missing requirements.
// Full skill content is loaded on-demand via the skill_lookup tool.
func BuildSkillIndex(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Available Skills\n\n")
	sb.WriteString("**MANDATORY:** When a task matches any skill below, call `skill_lookup` to load it as part of your ")
	sb.WriteString("first round of tool calls. This is a context-loading step, not optional — skills provide canonical ")
	sb.WriteString("commands and up-to-date patterns that complement stored memory. Never skip this because you already ")
	sb.WriteString("have a working method from memory or knowledge.\n\n")
	sb.WriteString("**Credentials:** If a skill references environment variables for authentication (e.g. `$PVE_TOKEN`, `$AWS_ACCESS_KEY_ID`), ")
	sb.WriteString("use `resolve_credential` or `list_credentials` to find matching credentials in the store, then pass the resolved values ")
	sb.WriteString("(as `{{CREDENTIAL:name:field}}` placeholders) directly in shell commands. Do NOT skip a skill because an env var is unset.\n\n")

	for _, skill := range skills {
		if skill.IsEligible() {
			sb.WriteString(fmt.Sprintf("- **%s**: %s\n", skill.Name, skill.Description))
		} else {
			missing := skill.MissingRequirements()
			sb.WriteString(fmt.Sprintf("- **%s**: %s *(setup required: %s)*\n",
				skill.Name, skill.Description, strings.Join(missing, ", ")))
		}
	}

	return sb.String()
}
