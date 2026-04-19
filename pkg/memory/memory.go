package memory

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Manager handles reading and writing the MEMORY.md file.
// Memory is a simple Markdown file organized by freeform category headings.
type Manager struct {
	Path      string
	DebugMode bool
}

// KnowledgeFiles maps kind values to their canonical file paths relative to the memory dir.
// This is the single source of truth for the knowledge tier file structure.
var KnowledgeFiles = map[string]string{
	"tools":          "knowledge/tools.md",
	"workarounds":    "knowledge/workarounds.md",
	"infrastructure": "knowledge/infrastructure.md",
	"projects":       "knowledge/projects.md",
	"others":         "knowledge/others.md",
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
// (exact match) within the same section are skipped. Section headings
// are matched using fuzzy word-overlap (Jaccard similarity) so that
// "Proxmox Server" and "Proxmox Server Configuration" merge into one section.
// When overwrite is true, the entire section is replaced with the new content.
func (m *Manager) Append(category, content string, overwrite bool) error {
	if err := m.EnsureDir(); err != nil {
		return fmt.Errorf("failed to create memory directory: %w", err)
	}
	return AppendToFile(m.Path, category, content, overwrite, m.DebugMode)
}

// AppendToFile adds content under a ## Category heading in any markdown file.
// It applies the same section-aware deduplication and fuzzy heading matching
// used for MEMORY.md. The file and parent directories are created if needed.
// This is the shared implementation used by both the core tier (MEMORY.md)
// and knowledge tier (topic-specific files).
func AppendToFile(path, category, content string, overwrite, debugMode bool) error {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", path, err)
	}

	// Read existing content
	existing, err := readFileOrEmpty(path)
	if err != nil {
		return err
	}

	// Parse existing file into sections
	sections := parseSections(existing)

	// Find or create the target section (fuzzy match)
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

		if debugMode {
			slog.Debug("overwrote section", "component", "memory", "category", category, "matched", section.heading, "lines", len(cleaned))
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
			if debugMode {
				slog.Debug("no new lines to add to section", "component", "memory", "category", category, "matched", section.heading)
			}
			return nil
		}

		if debugMode {
			slog.Debug("added lines to section", "component", "memory", "added", added, "category", category, "matched", section.heading)
		}
	}

	// Write back
	output := sections.render()
	if err := os.WriteFile(path, []byte(output), 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", path, err)
	}

	return nil
}

// readFileOrEmpty reads a file, returning "" if it does not exist.
func readFileOrEmpty(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read file %s: %w", path, err)
	}
	return string(data), nil
}

// GetSectionContent reads a markdown file and returns the content of the
// section that matches the given category heading (using fuzzy/subset matching).
// Returns the section content as a string and true if a matching section was
// found, or ("", false) if no match exists or the file doesn't exist.
// This is used by the memory reflector to check existing content before writing.
func GetSectionContent(path, category string) (string, bool) {
	existing, err := readFileOrEmpty(path)
	if err != nil || existing == "" {
		return "", false
	}

	sections := parseSections(existing)
	sectionKey := strings.TrimSpace(category)
	section, found := sections.get(sectionKey)
	if !found || len(section.lines) == 0 {
		return "", false
	}

	return strings.Join(section.lines, "\n"), true
}

// ResolveKnowledgeFile checks whether a section heading already exists in any
// of the knowledge tier files. If a matching section is found in a file other
// than the proposed one, it returns that file's absolute path and relative path
// so content is appended there instead (preventing cross-bucket duplication).
// If no cross-file match is found, returns (proposedPath, "").
//
// knowledgeFiles maps kind names to relative paths (e.g., "tools" -> "knowledge/tools.md").
func ResolveKnowledgeFile(memDir string, knowledgeFiles map[string]string, proposedPath, category string) (absPath, relPath string) {
	sectionKey := strings.TrimSpace(category)
	if sectionKey == "" {
		return proposedPath, ""
	}

	// Check each knowledge file (excluding the proposed one) for a matching section
	for _, rel := range knowledgeFiles {
		abs := filepath.Join(memDir, rel)
		if abs == proposedPath {
			continue
		}

		existing, err := readFileOrEmpty(abs)
		if err != nil || existing == "" {
			continue
		}

		sections := parseSections(existing)
		if _, found := sections.get(sectionKey); found {
			return abs, rel
		}
	}

	return proposedPath, ""
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

// fuzzyMatchThreshold is the minimum Jaccard similarity score for two section
// headings to be considered the same topic. 0.5 means at least half of the
// significant words must overlap.
const fuzzyMatchThreshold = 0.5

func (sl *sectionList) get(heading string) (*memorySection, bool) {
	// 1. Exact case-insensitive match (fast path, preserves existing behavior)
	for _, s := range sl.sections {
		if strings.EqualFold(s.heading, heading) {
			return s, true
		}
	}

	// 2. Subset match: if all significant words of one heading appear in the
	// other, the headings describe the same topic at different specificity
	// levels (e.g., "Proxmox" absorbs "Proxmox API Access").
	newWords := headingWords(heading)
	if len(newWords) == 0 {
		return nil, false
	}

	for _, s := range sl.sections {
		existWords := headingWords(s.heading)
		if len(existWords) == 0 {
			continue
		}
		if isSubset(newWords, existWords) || isSubset(existWords, newWords) {
			return s, true
		}
	}

	// 3. Fuzzy match: compare significant words using Jaccard similarity
	var bestSection *memorySection
	bestScore := 0.0

	for _, s := range sl.sections {
		existWords := headingWords(s.heading)
		score := jaccardSimilarity(newWords, existWords)
		if score > bestScore {
			bestScore = score
			bestSection = s
		}
	}

	if bestScore >= fuzzyMatchThreshold && bestSection != nil {
		return bestSection, true
	}

	return nil, false
}

// isSubset returns true if every word in set a also exists in set b.
func isSubset(a, b map[string]bool) bool {
	if len(a) == 0 {
		return false
	}
	for w := range a {
		if !b[w] {
			return false
		}
	}
	return true
}

// headingStopWords are structural/filler words stripped before comparing
// headings. These carry no topic identity and cause false negatives when
// the LLM rephrases the same concept (e.g., "Server Configuration" vs
// "Connection Details").
var headingStopWords = map[string]bool{
	"details":       true,
	"detail":        true,
	"configuration": true,
	"config":        true,
	"connection":    true,
	"service":       true,
	"environment":   true,
	"info":          true,
	"information":   true,
	"settings":      true,
	"setup":         true,
	"status":        true,
	"the":           true,
	"and":           true,
	"for":           true,
	"with":          true,
}

// headingWords extracts significant words from a heading for comparison.
// It lowercases, removes punctuation, filters stop words, and applies
// basic plural stemming (trailing "s" removal for words 5+ chars).
func headingWords(heading string) map[string]bool {
	words := make(map[string]bool)

	// Split on whitespace and common punctuation
	fields := strings.FieldsFunc(strings.ToLower(heading), func(r rune) bool {
		return unicode.IsSpace(r) || r == '-' || r == '/' || r == '(' || r == ')' || r == ',' || r == ':'
	})

	for _, w := range fields {
		// Remove any remaining non-alphanumeric chars
		w = strings.TrimFunc(w, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		})
		if w == "" {
			continue
		}
		if headingStopWords[w] {
			continue
		}
		// Basic plural stemming to normalize singular/plural forms.
		// Handles: "servers"->"server", "accounts"->"account",
		// "repositories"->"repository", "entries"->"entry".
		if len(w) >= 5 {
			if strings.HasSuffix(w, "ies") {
				w = w[:len(w)-3] + "y"
			} else if strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") {
				w = w[:len(w)-1]
			}
		}
		words[w] = true
	}
	return words
}

// jaccardSimilarity computes |A ∩ B| / |A ∪ B| for two word sets.
// Returns 0 if both sets are empty.
func jaccardSimilarity(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}

	intersection := 0
	for w := range a {
		if b[w] {
			intersection++
		}
	}

	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
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
