package skills

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SyncSkillsToMemory writes eligible skill files to the memory/skills/ directory
// so the existing memory indexer picks them up and indexes them in the vector store.
// Files are only written when content changes (hash comparison).
// Orphaned skill files (from skills that were removed or became ineligible) are cleaned up.
// For skills with a directory, supplementary .md files are also synced using
// the naming convention {name}--{filename}.md (e.g., docker--commands.md).
func SyncSkillsToMemory(skills []Skill, memoryDir string) error {
	skillsDir := filepath.Join(memoryDir, "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		return fmt.Errorf("create skills dir: %w", err)
	}

	expected := make(map[string]bool)

	for _, skill := range skills {
		if !skill.IsEligible() {
			continue
		}

		// Sync the main SKILL.md content
		filename := skill.Name + ".md"
		expected[filename] = true
		outPath := filepath.Join(skillsDir, filename)

		// Build indexable content with name/description at top for better embedding
		content := fmt.Sprintf("# Skill: %s\n\n%s\n\n%s\n", skill.Name, skill.Description, skill.Content)

		// Skip write if content hasn't changed
		if existing, err := os.ReadFile(outPath); err == nil {
			if hashBytes(existing) == hashStr(content) {
				continue
			}
		}

		if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("write skill %s: %w", skill.Name, err)
		}
	}

	// Second pass: sync supplementary .md files from skill directories
	for _, skill := range skills {
		if !skill.IsEligible() || skill.Directory == "" {
			continue
		}

		entries, err := os.ReadDir(skill.Directory)
		if err != nil {
			continue
		}

		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			// Only sync .md files that are not the main SKILL.md
			if !strings.HasSuffix(strings.ToLower(name), ".md") || strings.EqualFold(name, "SKILL.md") {
				continue
			}

			// Use {skillname}--{filename} to avoid conflicts between skills
			syncName := skill.Name + "--" + name
			expected[syncName] = true
			outPath := filepath.Join(skillsDir, syncName)

			data, err := os.ReadFile(filepath.Join(skill.Directory, name))
			if err != nil {
				continue
			}

			// Prefix with skill context for better embedding
			content := fmt.Sprintf("# Skill: %s / %s\n\n%s", skill.Name, name, string(data))

			if existing, err := os.ReadFile(outPath); err == nil {
				if hashBytes(existing) == hashStr(content) {
					continue
				}
			}

			if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
				continue // Non-critical, skip
			}
		}
	}

	// Clean up skills that no longer exist or are no longer eligible
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil // Not critical
	}
	for _, e := range entries {
		if !e.IsDir() && !expected[e.Name()] {
			_ = os.Remove(filepath.Join(skillsDir, e.Name())) // best-effort cleanup
		}
	}

	return nil
}

func hashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

func hashStr(s string) string {
	return hashBytes([]byte(s))
}
