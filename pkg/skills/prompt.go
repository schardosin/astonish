package skills

import (
	"fmt"
	"strings"
)

// BuildSkillIndex creates the lightweight Tier 1 skill listing for the system prompt.
// All installed skills are listed (eligible and ineligible) so the agent can discover
// them. Ineligible skills are marked with their missing requirements.
// Full skill content is retrieved on-demand via vector search (automatic)
// or the skill_lookup tool (explicit).
func BuildSkillIndex(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Available Skills\n\n")
	sb.WriteString("You have skill guides for the following CLI tools and workflows. ")
	sb.WriteString("Relevant skills are automatically retrieved when needed via knowledge search. ")
	sb.WriteString("You can also use the `skill_lookup` tool to load a specific skill by name.\n\n")

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
