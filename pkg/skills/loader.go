package skills

import (
	"bufio"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed bundled/*/SKILL.md
var bundledSkills embed.FS

// Skill represents a loaded skill with its metadata and content.
type Skill struct {
	Name        string      `yaml:"name"`
	Description string      `yaml:"description"`
	OS          []string    `yaml:"os,omitempty"`
	RequireBins []string    `yaml:"require_bins,omitempty"`
	RequireEnv  []string    `yaml:"require_env,omitempty"`
	Metadata    interface{} `yaml:"metadata,omitempty"` // ClawHub nested metadata (parsed but not used directly)
	Content     string      `yaml:"-"`                  // Full markdown body (after frontmatter)
	FilePath    string      `yaml:"-"`                  // Source file path
	Source      string      `yaml:"-"`                  // "bundled", "user", "extra", "project"
	Directory   string      `yaml:"-"`                  // Absolute path to skill directory (empty for bundled)
}

// IsEligible checks if a skill can run in the current environment.
func (s *Skill) IsEligible() bool {
	if len(s.OS) > 0 {
		found := false
		for _, o := range s.OS {
			if strings.EqualFold(o, runtime.GOOS) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, bin := range s.RequireBins {
		if _, err := exec.LookPath(bin); err != nil {
			return false
		}
	}
	for _, env := range s.RequireEnv {
		if os.Getenv(env) == "" {
			return false
		}
	}
	return true
}

// MissingRequirements returns human-readable reasons why a skill is not eligible.
func (s *Skill) MissingRequirements() []string {
	var missing []string
	if len(s.OS) > 0 {
		found := false
		for _, o := range s.OS {
			if strings.EqualFold(o, runtime.GOOS) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, fmt.Sprintf("os: requires %s (current: %s)", strings.Join(s.OS, "/"), runtime.GOOS))
		}
	}
	for _, bin := range s.RequireBins {
		if _, err := exec.LookPath(bin); err != nil {
			missing = append(missing, bin)
		}
	}
	for _, env := range s.RequireEnv {
		if os.Getenv(env) == "" {
			missing = append(missing, "$"+env)
		}
	}
	return missing
}

// ParseSkillFile parses a SKILL.md file into a Skill struct.
// The file must have YAML frontmatter delimited by --- lines.
func ParseSkillFile(path string, content []byte) (*Skill, error) {
	if len(content) > 256*1024 {
		return nil, fmt.Errorf("skill file exceeds 256KB: %s", path)
	}

	frontmatter, body, err := splitFrontmatter(content)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	var skill Skill
	if err := yaml.Unmarshal(frontmatter, &skill); err != nil {
		return nil, fmt.Errorf("parse frontmatter in %s: %w", path, err)
	}

	if skill.Name == "" {
		return nil, fmt.Errorf("skill in %s has no name", path)
	}
	if skill.Description == "" {
		return nil, fmt.Errorf("skill %q in %s has no description", skill.Name, path)
	}

	// Normalize ClawHub metadata into flat fields if present
	normalizeClawHubMetadata(&skill)

	skill.Content = strings.TrimSpace(body)
	skill.FilePath = path
	return &skill, nil
}

// normalizeClawHubMetadata extracts bins, env, and OS from ClawHub's nested
// metadata format into the flat Skill fields.
// ClawHub uses:
//
//	metadata: {"clawdbot":{"emoji":"...","requires":{"bins":["docker"]},"os":["linux","darwin","win32"]}}
//
// or nested YAML equivalent. Only populates flat fields if they are empty.
func normalizeClawHubMetadata(skill *Skill) {
	if skill.Metadata == nil {
		return
	}

	// If metadata is a JSON string, unmarshal it first
	var metaMap map[string]interface{}
	switch v := skill.Metadata.(type) {
	case string:
		if err := json.Unmarshal([]byte(v), &metaMap); err != nil {
			return
		}
	case map[string]interface{}:
		metaMap = v
	default:
		return
	}

	// Navigate to clawdbot subtree
	clawdbot, ok := getNestedMap(metaMap, "clawdbot")
	if !ok {
		return
	}

	// Extract requires.bins → RequireBins (only if flat field empty)
	if len(skill.RequireBins) == 0 {
		if requires, ok := getNestedMap(clawdbot, "requires"); ok {
			skill.RequireBins = getStringSlice(requires, "bins")
			if len(skill.RequireEnv) == 0 {
				skill.RequireEnv = getStringSlice(requires, "env")
			}
		}
	}

	// Extract os → OS (only if flat field empty), mapping win32 → windows
	if len(skill.OS) == 0 {
		if osList := getStringSlice(clawdbot, "os"); len(osList) > 0 {
			for i, o := range osList {
				if strings.EqualFold(o, "win32") {
					osList[i] = "windows"
				}
			}
			skill.OS = osList
		}
	}
}

// getNestedMap extracts a map[string]interface{} value by key.
func getNestedMap(m map[string]interface{}, key string) (map[string]interface{}, bool) {
	v, ok := m[key]
	if !ok {
		return nil, false
	}
	nested, ok := v.(map[string]interface{})
	return nested, ok
}

// getStringSlice extracts a []string from a map field that may be []interface{}.
func getStringSlice(m map[string]interface{}, key string) []string {
	v, ok := m[key]
	if !ok {
		return nil
	}
	switch arr := v.(type) {
	case []interface{}:
		var result []string
		for _, item := range arr {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case []string:
		return arr
	default:
		return nil
	}
}

// splitFrontmatter splits --- delimited YAML frontmatter from the markdown body.
func splitFrontmatter(data []byte) ([]byte, string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	var frontLines []string
	var bodyLines []string
	state := 0 // 0=before, 1=in frontmatter, 2=body

	for scanner.Scan() {
		line := scanner.Text()
		switch state {
		case 0:
			if strings.TrimSpace(line) == "---" {
				state = 1
			}
		case 1:
			if strings.TrimSpace(line) == "---" {
				state = 2
			} else {
				frontLines = append(frontLines, line)
			}
		case 2:
			bodyLines = append(bodyLines, line)
		}
	}

	if state < 2 {
		return nil, "", fmt.Errorf("no valid YAML frontmatter found (need opening and closing --- lines)")
	}

	return []byte(strings.Join(frontLines, "\n")), strings.Join(bodyLines, "\n"), nil
}

// LoadSkills loads skills from all configured sources.
// Later sources override earlier ones by skill name.
// The workspaceDir parameter is kept for API compatibility but is no longer used
// (project-local skills were removed — all skills live in the config directory).
func LoadSkills(userDir string, extraDirs []string, _ string, allowlist []string) ([]Skill, error) {
	byName := make(map[string]*Skill)

	// 1. Bundled skills (embedded in binary)
	if err := loadBundledSkills(byName); err != nil {
		return nil, fmt.Errorf("load bundled skills: %w", err)
	}

	// 2. User skills
	if userDir != "" {
		loadSkillsFromDir(userDir, "user", byName)
	}

	// 3. Extra directories
	for _, dir := range extraDirs {
		loadSkillsFromDir(dir, "extra", byName)
	}

	// Apply allowlist filter
	var result []Skill
	for _, skill := range byName {
		if len(allowlist) > 0 && !containsStr(allowlist, skill.Name) {
			continue
		}
		result = append(result, *skill)
	}

	// Sort by name for deterministic output
	sortSkills(result)
	return result, nil
}

// loadBundledSkills loads skills from the embedded filesystem.
func loadBundledSkills(byName map[string]*Skill) error {
	dirs, err := fs.ReadDir(bundledSkills, "bundled")
	if err != nil {
		return err
	}

	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		skillPath := filepath.Join("bundled", dir.Name(), "SKILL.md")
		data, err := bundledSkills.ReadFile(skillPath)
		if err != nil {
			continue // Skip dirs without SKILL.md
		}

		skill, err := ParseSkillFile(skillPath, data)
		if err != nil {
			continue // Skip unparseable skills
		}
		skill.Source = "bundled"
		byName[skill.Name] = skill
	}
	return nil
}

// loadSkillsFromDir loads skills from a directory on disk.
// Each immediate subdirectory containing a SKILL.md is loaded.
func loadSkillsFromDir(dir string, source string, byName map[string]*Skill) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // Directory doesn't exist or unreadable — skip silently
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDir := filepath.Join(dir, entry.Name())
		skillPath := filepath.Join(skillDir, "SKILL.md")
		data, err := os.ReadFile(skillPath)
		if err != nil {
			continue
		}

		skill, err := ParseSkillFile(skillPath, data)
		if err != nil {
			continue
		}
		skill.Source = source
		// Set absolute directory path for disk-based skills
		if absDir, err := filepath.Abs(skillDir); err == nil {
			skill.Directory = absDir
		} else {
			skill.Directory = skillDir
		}
		byName[skill.Name] = skill
	}
}

// FilterEligible returns only skills that pass environment checks.
func FilterEligible(skills []Skill) []Skill {
	var eligible []Skill
	for _, s := range skills {
		if s.IsEligible() {
			eligible = append(eligible, s)
		}
	}
	return eligible
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

func sortSkills(skills []Skill) {
	for i := 0; i < len(skills); i++ {
		for j := i + 1; j < len(skills); j++ {
			if skills[i].Name > skills[j].Name {
				skills[i], skills[j] = skills[j], skills[i]
			}
		}
	}
}
