package skills

import (
	"fmt"
	"strings"
)

// BuildSkillIndex creates the lightweight Tier 1 skill listing for the system prompt.
// Only skill names and one-line descriptions are included.
// Full skill content is retrieved on-demand via vector search (automatic)
// or the skill_lookup tool (explicit).
func BuildSkillIndex(skills []Skill) string {
	eligible := FilterEligible(skills)
	if len(eligible) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Available Skills\n\n")
	sb.WriteString("You have skill guides for the following CLI tools and workflows. ")
	sb.WriteString("Relevant skills are automatically retrieved when needed via knowledge search. ")
	sb.WriteString("You can also use the `skill_lookup` tool to load a specific skill by name.\n\n")

	for _, skill := range eligible {
		sb.WriteString(fmt.Sprintf("- **%s**: %s\n", skill.Name, skill.Description))
	}

	return sb.String()
}
