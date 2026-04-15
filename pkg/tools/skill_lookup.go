package tools

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/schardosin/astonish/pkg/skills"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// SkillLookupArgs defines the arguments for the skill_lookup tool.
type SkillLookupArgs struct {
	Name string `json:"name" jsonschema:"Skill name from the Available Skills list (e.g. github, docker, git)"`
}

// SkillLookupResult is returned from skill_lookup.
type SkillLookupResult struct {
	Name                string   `json:"name"`
	Description         string   `json:"description"`
	Content             string   `json:"content"`
	Directory           string   `json:"directory,omitempty"`            // Absolute path to skill dir (for supplementary files)
	Files               []string `json:"files,omitempty"`                // Files in skill directory
	MissingRequirements []string `json:"missing_requirements,omitempty"` // Unmet requirements (empty if eligible)
	Error               string   `json:"error,omitempty"`
}

// SkillLookup returns full skill content by name.
// All installed skills are indexed (eligible and ineligible) so the agent can
// discover skills that need setup and guide the user through configuration.
func SkillLookup(allSkills []skills.Skill) func(ctx tool.Context, args SkillLookupArgs) (SkillLookupResult, error) {
	// Build index of all installed skills by name
	index := make(map[string]*skills.Skill, len(allSkills))
	for i := range allSkills {
		index[allSkills[i].Name] = &allSkills[i]
	}

	return func(ctx tool.Context, args SkillLookupArgs) (SkillLookupResult, error) {
		if args.Name == "" {
			return SkillLookupResult{Error: "name is required"}, nil
		}

		name := strings.ToLower(strings.TrimSpace(args.Name))
		skill, ok := index[name]
		if !ok {
			// List available names in the error for LLM self-correction
			names := make([]string, 0, len(index))
			for n := range index {
				names = append(names, n)
			}
			sort.Strings(names)
			return SkillLookupResult{
				Error: fmt.Sprintf("skill %q not found. Available: %s", args.Name, strings.Join(names, ", ")),
			}, nil
		}

		result := SkillLookupResult{
			Name:        skill.Name,
			Description: skill.Description,
			Content:     skill.Content,
		}

		// Include missing requirements so the agent can guide setup
		if missing := skill.MissingRequirements(); len(missing) > 0 {
			result.MissingRequirements = missing
		}

		// Include directory and file listing for disk-based skills
		if skill.Directory != "" {
			result.Directory = skill.Directory
			if entries, err := os.ReadDir(skill.Directory); err == nil {
				for _, e := range entries {
					if !e.IsDir() {
						result.Files = append(result.Files, e.Name())
					}
				}
			}
		}

		return result, nil
	}
}

// NewSkillLookupTool creates the skill_lookup tool.
func NewSkillLookupTool(allSkills []skills.Skill) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "skill_lookup",
		Description: "Load full instructions for a CLI tool skill by name. " +
			"Use this to learn how to use a CLI tool or workflow before executing commands. " +
			"If a skill references environment variables for auth, resolve them from the credential store. " +
			"Check the Available Skills list in the system prompt for valid skill names.",
	}, SkillLookup(allSkills))
}
