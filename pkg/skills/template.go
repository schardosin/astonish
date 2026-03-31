package skills

import "fmt"

// NewSkillTemplate returns the default SKILL.md template content for a new skill.
// Both the CLI and the Studio API use this to generate scaffold files.
func NewSkillTemplate(name string) string {
	return fmt.Sprintf(`---
name: %s
description: "TODO: One-line description of what this skill does"
require_bins: []
---

# %s

## When to Use
- TODO: Describe when this skill should be used

## When NOT to Use
- TODO: Describe when other tools are more appropriate

## Common Commands
`+"```"+`
# TODO: Add common commands and patterns
`+"```"+`

## Tips
- TODO: Add tips and best practices
`, name, name)
}
