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
	sb.WriteString("You have skill guides for the following CLI tools and workflows. ")
	sb.WriteString("When the user's request relates to any of these topics, you MUST use the `skill_lookup` tool to load the relevant skill by name before proceeding. ")
	sb.WriteString("Skills are NOT automatically injected — you must explicitly look them up.\n\n")

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
