package tools

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/schardosin/astonish/pkg/skills"
	"github.com/schardosin/astonish/pkg/store"
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
//
// At runtime, this function also checks the context for tenant-scoped skill
// stores (org + team) injected via store.WithSkillStores. Skills from these
// stores override bundled skills of the same name (team > org > bundled).
func SkillLookup(allSkills []skills.Skill) func(ctx tool.Context, args SkillLookupArgs) (SkillLookupResult, error) {
	// Build index of bundled/filesystem skills by name
	bundledIndex := make(map[string]*skills.Skill, len(allSkills))
	for i := range allSkills {
		bundledIndex[allSkills[i].Name] = &allSkills[i]
	}

	return func(ctx tool.Context, args SkillLookupArgs) (SkillLookupResult, error) {
		if args.Name == "" {
			return SkillLookupResult{Error: "name is required"}, nil
		}

		name := strings.ToLower(strings.TrimSpace(args.Name))

		// Check tenant-scoped stores first (team overrides org overrides bundled)
		if ctx != nil {
			if ss := store.SkillStoresFromContext(ctx); ss != nil {
				// Team store takes highest priority
				if ss.Team != nil {
					if skill, err := ss.Team.Get(ctx, name); err == nil && skill != nil {
						return storeSkillToResult(skill), nil
					}
				}
				// Then org store
				if ss.Org != nil {
					if skill, err := ss.Org.Get(ctx, name); err == nil && skill != nil {
						return storeSkillToResult(skill), nil
					}
				}
			}
		}

		// Fall back to bundled/filesystem index
		skill, ok := bundledIndex[name]
		if !ok {
			// List all available names (bundled + store skills) for LLM self-correction
			names := collectAllSkillNames(bundledIndex, ctx)
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

// storeSkillToResult converts a store.Skill to a SkillLookupResult.
func storeSkillToResult(s *store.Skill) SkillLookupResult {
	// Parse the raw content to extract the body (after frontmatter)
	parsed, err := skills.ParseSkillFile("store:"+s.Name, []byte(s.Content))
	if err != nil {
		// If parsing fails, return the raw content
		return SkillLookupResult{
			Name:        s.Name,
			Description: s.Description,
			Content:     s.Content,
		}
	}
	result := SkillLookupResult{
		Name:        parsed.Name,
		Description: parsed.Description,
		Content:     parsed.Content,
	}
	if missing := parsed.MissingRequirements(); len(missing) > 0 {
		result.MissingRequirements = missing
	}
	return result
}

// collectAllSkillNames gathers skill names from bundled index + context stores.
func collectAllSkillNames(bundledIndex map[string]*skills.Skill, ctx tool.Context) []string {
	nameSet := make(map[string]struct{}, len(bundledIndex))
	for n := range bundledIndex {
		nameSet[n] = struct{}{}
	}

	if ctx != nil {
		if ss := store.SkillStoresFromContext(ctx); ss != nil {
			if ss.Org != nil {
				if orgSkills, err := ss.Org.List(ctx); err == nil {
					for _, s := range orgSkills {
						nameSet[s.Name] = struct{}{}
					}
				}
			}
			if ss.Team != nil {
				if teamSkills, err := ss.Team.List(ctx); err == nil {
					for _, s := range teamSkills {
						nameSet[s.Name] = struct{}{}
					}
				}
			}
		}
	}

	names := make([]string, 0, len(nameSet))
	for n := range nameSet {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
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
