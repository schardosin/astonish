package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Manager handles reading and writing the MEMORY.md file.
// Memory is a simple Markdown file organized by freeform category headings.
type Manager struct {
	Path      string
	DebugMode bool
}

// DefaultPath returns the default MEMORY.md path.
func DefaultPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "astonish", "memory", "MEMORY.md"), nil
}

// NewManager creates a Manager with the given path.
// If path is empty, the default path is used.
func NewManager(path string, debugMode bool) (*Manager, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return nil, fmt.Errorf("failed to determine default memory path: %w", err)
		}
	}
	return &Manager{Path: path, DebugMode: debugMode}, nil
}

// EnsureDir creates the memory directory if it doesn't exist.
func (m *Manager) EnsureDir() error {
	dir := filepath.Dir(m.Path)
	return os.MkdirAll(dir, 0755)
}

// Load reads the entire MEMORY.md contents.
// Returns empty string if the file doesn't exist (not an error).
func (m *Manager) Load() (string, error) {
	data, err := os.ReadFile(m.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read memory file: %w", err)
	}
	return string(data), nil
}

// Append adds content under a ## Category heading in MEMORY.md.
// If the section doesn't exist, it is created. Duplicate lines
// (exact match) within the same section are skipped.
// When overwrite is true, the entire section is replaced with the new content.
func (m *Manager) Append(category, content string, overwrite bool) error {
	if err := m.EnsureDir(); err != nil {
		return fmt.Errorf("failed to create memory directory: %w", err)
	}

	existing, err := m.Load()
	if err != nil {
		return err
	}

	// Parse existing file into sections
	sections := parseSections(existing)

	// Find or create the target section
	sectionKey := strings.TrimSpace(category)
	section, exists := sections.get(sectionKey)
	if !exists {
		section = &memorySection{heading: sectionKey}
		sections.add(section)
	}

	newLines := strings.Split(strings.TrimSpace(content), "\n")

	if overwrite {
		// Replace the entire section content
		var cleaned []string
		for _, line := range newLines {
			if strings.TrimSpace(line) != "" {
				cleaned = append(cleaned, line)
			}
		}
		section.lines = cleaned

		if m.DebugMode {
			fmt.Printf("[Memory DEBUG] Overwrote section '%s' with %d lines\n", category, len(cleaned))
		}
	} else {
		// Add new lines, skipping duplicates
		existingLines := make(map[string]bool)
		for _, line := range section.lines {
			existingLines[strings.TrimSpace(line)] = true
		}

		added := 0
		for _, line := range newLines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			if !existingLines[trimmed] {
				section.lines = append(section.lines, line)
				existingLines[trimmed] = true
				added++
			}
		}

		if added == 0 {
			if m.DebugMode {
				fmt.Printf("[Memory DEBUG] No new lines to add to section '%s'\n", category)
			}
			return nil
		}

		if m.DebugMode {
			fmt.Printf("[Memory DEBUG] Added %d lines to section '%s'\n", added, category)
		}
	}

	// Write back
	output := sections.render()
	if err := os.WriteFile(m.Path, []byte(output), 0644); err != nil {
		return fmt.Errorf("failed to write memory file: %w", err)
	}

	return nil
}

// memorySection represents a ## heading and its content lines.
type memorySection struct {
	heading string
	lines   []string
}

// sectionList maintains ordered sections from the memory file.
type sectionList struct {
	preamble []string         // Lines before the first ## heading
	sections []*memorySection // Ordered sections
	index    map[string]int   // heading -> index in sections slice
}

func newSectionList() *sectionList {
	return &sectionList{
		index: make(map[string]int),
	}
}

func (sl *sectionList) get(heading string) (*memorySection, bool) {
	// Case-insensitive lookup
	for _, s := range sl.sections {
		if strings.EqualFold(s.heading, heading) {
			return s, true
		}
	}
	return nil, false
}

func (sl *sectionList) add(s *memorySection) {
	sl.index[strings.ToLower(s.heading)] = len(sl.sections)
	sl.sections = append(sl.sections, s)
}

func (sl *sectionList) render() string {
	var sb strings.Builder

	// Write preamble
	for _, line := range sl.preamble {
		sb.WriteString(line)
		sb.WriteString("\n")
	}

	// Write sections
	for i, s := range sl.sections {
		if i > 0 || len(sl.preamble) > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("## ")
		sb.WriteString(s.heading)
		sb.WriteString("\n")
		for _, line := range s.lines {
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// parseSections splits a MEMORY.md file into its component sections.
func parseSections(content string) *sectionList {
	sl := newSectionList()

	if content == "" {
		return sl
	}

	lines := strings.Split(content, "\n")
	var currentSection *memorySection

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check for ## heading
		if strings.HasPrefix(trimmed, "## ") {
			heading := strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			currentSection = &memorySection{heading: heading}
			sl.add(currentSection)
			continue
		}

		if currentSection == nil {
			// Lines before first heading (preamble)
			// Skip empty lines at the very start
			if trimmed != "" || len(sl.preamble) > 0 {
				sl.preamble = append(sl.preamble, line)
			}
		} else {
			// Skip leading empty lines in a section, but keep content lines
			if trimmed != "" || len(currentSection.lines) > 0 {
				currentSection.lines = append(currentSection.lines, line)
			}
		}
	}

	// Trim trailing empty lines from each section
	for _, s := range sl.sections {
		for len(s.lines) > 0 && strings.TrimSpace(s.lines[len(s.lines)-1]) == "" {
			s.lines = s.lines[:len(s.lines)-1]
		}
	}
	// Trim trailing empty lines from preamble
	for len(sl.preamble) > 0 && strings.TrimSpace(sl.preamble[len(sl.preamble)-1]) == "" {
		sl.preamble = sl.preamble[:len(sl.preamble)-1]
	}

	return sl
}
