package skills

// BuiltinSkills returns skills that are always available regardless of
// platform/org/team configuration. These ship with the binary and provide
// essential guidance that the LLM must load before performing certain tasks.
//
// Built-in skills are:
// - Always included in the "Available Skills" index in the system prompt
// - Always resolvable via skill_lookup (as a fallback after DB stores)
// - Never require validation (they ship with the binary)
func BuiltinSkills() []Skill {
	return []Skill{
		{
			Name:        "generative-ui",
			Description: "Complete API docs, design system, and examples for building visual apps (astonish-app). MUST load before generating any visual app.",
			Content:     BuiltinGenerativeUI,
			Source:      "builtin",
		},
	}
}
