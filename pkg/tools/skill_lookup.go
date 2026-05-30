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
	Name     string `json:"name" jsonschema:"Skill name from the Available Skills list (e.g. github, docker, git)"`
	File     string `json:"file,omitempty" jsonschema:"Optional relative path to a specific file within the skill (e.g. 'scripts/deploy.sh' or 'references/api.md'). If omitted, returns the main SKILL.md plus a files manifest."`
	Path     string `json:"path,omitempty" jsonschema:"Alternative to 'file': directory part (e.g. 'scripts')"`
	Filename string `json:"filename,omitempty" jsonschema:"Alternative to 'file': filename part (e.g. 'deploy.sh')"`
}

// SkillLookupResult is returned from skill_lookup.
type SkillLookupResult struct {
	Name                string              `json:"name"`
	Description         string              `json:"description"`
	Content             string              `json:"content"`
	File                string              `json:"file,omitempty"`       // When a specific file was requested
	Directory           string              `json:"directory,omitempty"`  // Legacy (disk-based skills)
	Files               []string            `json:"files,omitempty"`      // Flat list of files (for compatibility)
	FilesManifest       map[string][]string `json:"files_manifest,omitempty"` // Structured: path -> [filenames]
	MissingRequirements []string            `json:"missing_requirements,omitempty"`
	Error               string              `json:"error,omitempty"`
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
						return handlePlatformSkillLookup(ctx, ss.Team, skill, args), nil
					}
				}
				// Then org store
				if ss.Org != nil {
					if skill, err := ss.Org.Get(ctx, name); err == nil && skill != nil {
						return handlePlatformSkillLookup(ctx, ss.Org, skill, args), nil
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

// handlePlatformSkillLookup handles skill_lookup for skills coming from platform stores (DB).
// It supports fetching specific auxiliary files and returns a files manifest when loading the main skill.
// SECURITY: Skills with non-usable validation_status are blocked at runtime.
func handlePlatformSkillLookup(ctx tool.Context, store store.SkillStore, skill *store.Skill, args SkillLookupArgs) SkillLookupResult {
	// Runtime validation gate — only skills with usable status can be loaded
	if !skills.IsUsableStatus(skill.ValidationStatus) {
		return SkillLookupResult{
			Name: skill.Name,
			Error: fmt.Sprintf("Skill %q is blocked (validation_status: %q). "+
				"A team member must validate and acknowledge any critical security issues "+
				"in Settings → Skills before this skill can be used.", skill.Name, skill.ValidationStatus),
		}
	}
	// Determine if user asked for a specific file
	filePath := strings.TrimSpace(args.File)
	if filePath == "" && args.Filename != "" {
		if args.Path != "" {
			filePath = args.Path + "/" + args.Filename
		} else {
			filePath = args.Filename
		}
	}

	if filePath != "" {
		// Validate for path traversal
		if strings.Contains(filePath, "..") || strings.HasPrefix(filePath, "/") {
			return SkillLookupResult{
				Name:  skill.Name,
				Error: "invalid file path: must not contain '..' or start with '/'",
			}
		}

		// Specific file requested
		// Normalize path/filename
		dir, name := "", filePath
		if idx := strings.LastIndex(filePath, "/"); idx != -1 {
			dir = filePath[:idx]
			name = filePath[idx+1:]
		}
		f, err := store.GetFile(ctx, skill.Name, dir, name)
		if err != nil {
			// DB error — distinct from "file not found"
			return SkillLookupResult{
				Name:  skill.Name,
				Error: fmt.Sprintf("failed to load file %q from skill %q (database error)", filePath, skill.Name),
			}
		}
		if f != nil {
			return SkillLookupResult{
				Name:      skill.Name,
				File:      filePath,
				Content:   f.Content,
				Directory: "", // not meaningful for DB-backed files
			}
		}
		// File not found
		return SkillLookupResult{
			Name:  skill.Name,
			Error: fmt.Sprintf("file %q not found in skill %q", filePath, skill.Name),
		}
	}

	// Default: return main skill content + files manifest
	result := storeSkillToResult(skill)

	// Attach files manifest from the new skill_files table
	if files, err := store.ListFiles(ctx, skill.Name); err == nil && len(files) > 0 {
		manifest := make(map[string][]string)
		for _, f := range files {
			manifest[f.Path] = append(manifest[f.Path], f.Filename)
		}
		// For backward compat with existing result shape, also populate flat Files list
		for _, f := range files {
			full := f.Filename
			if f.Path != "" {
				full = f.Path + "/" + f.Filename
			}
			result.Files = append(result.Files, full)
		}
		// Also expose structured manifest (new field)
		result.FilesManifest = manifest
	}

	return result
}
