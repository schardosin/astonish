package migration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/schardosin/astonish/pkg/store"
	"gopkg.in/yaml.v3"
)

// skillFrontmatter matches the YAML frontmatter in SKILL.md files.
type skillFrontmatter struct {
	Name        string      `yaml:"name"`
	Description string      `yaml:"description"`
	OS          []string    `yaml:"os,omitempty"`
	RequireBins []string    `yaml:"require_bins,omitempty"`
	RequireEnv  []string    `yaml:"require_env,omitempty"`
	Metadata    interface{} `yaml:"metadata,omitempty"`
}

func (m *Migrator) migrateSkills(ctx context.Context, orgDS store.OrgDataStore) (int, error) {
	// Skills live in ~/.config/astonish/skills/ (not memory/skills/).
	skillsDir := filepath.Join(m.configDir, "skills")

	if _, err := os.Stat(skillsDir); os.IsNotExist(err) {
		m.emitProgress(CatSkills, 0, 0, "skipped", "")
		return 0, nil
	}

	m.emitProgress(CatSkills, 0, 0, "counting", "")

	// Find all SKILL.md files
	var skillFiles []string
	_ = filepath.Walk(skillsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && info.Name() == "SKILL.md" {
			skillFiles = append(skillFiles, path)
		}
		return nil
	})

	total := len(skillFiles)
	if total == 0 {
		m.emitProgress(CatSkills, 0, 0, "skipped", "")
		return 0, nil
	}

	m.emitProgress(CatSkills, 0, total, "migrating", "")

	skillStore := orgDS.OrgSkills()
	count := 0

	for _, path := range skillFiles {
		if ctx.Err() != nil {
			return count, ctx.Err()
		}

		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		// Parse frontmatter and content
		fm, _, err := parseSkillFrontmatter(string(data))
		if err != nil || fm.Name == "" {
			continue
		}

		// Store the full raw SKILL.md file in content so the PG store
		// can re-parse frontmatter fields on load (description, os, etc.).
		skill := &store.Skill{
			Name:      fm.Name,
			Content:   string(data),
			Source:    "migrated",
			Directory: filepath.Dir(path),
			FilePath:  path,
			CreatedBy: "00000000-0000-0000-0000-000000000000", // system migration
		}

		if err := skillStore.Save(skill); err != nil {
			return count, fmt.Errorf("failed to save skill %q: %w", fm.Name, err)
		}

		count++
		m.emitProgress(CatSkills, count, total, "migrating", "")
	}

	m.emitProgress(CatSkills, count, total, "done", "")
	return count, nil
}

// parseSkillFrontmatter extracts YAML frontmatter and markdown content from a SKILL.md file.
func parseSkillFrontmatter(data string) (skillFrontmatter, string, error) {
	var fm skillFrontmatter

	// Find frontmatter delimiters (---)
	if !strings.HasPrefix(data, "---\n") && !strings.HasPrefix(data, "---\r\n") {
		return fm, data, fmt.Errorf("no frontmatter found")
	}

	// Find the closing ---
	rest := data[4:] // skip opening "---\n"
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return fm, data, fmt.Errorf("unclosed frontmatter")
	}

	fmStr := rest[:idx]
	content := strings.TrimSpace(rest[idx+4:]) // skip "\n---"

	if err := yaml.Unmarshal([]byte(fmStr), &fm); err != nil {
		return fm, content, fmt.Errorf("invalid frontmatter YAML: %w", err)
	}

	return fm, content, nil
}
