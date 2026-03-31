package memory

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// Chunk represents a text chunk from a memory file.
type Chunk struct {
	ID        string // SHA-256 of path:startLine:endLine:contentHash
	Text      string
	Path      string // Relative path within memory dir
	StartLine int
	EndLine   int
	Hash      string // SHA-256 of text content
	Category  string // Derived from path prefix (guidance, skill, flow, knowledge, etc.)
}

// CategoryFromPath derives a chunk category from the relative file path.
// Categories enable partitioned vector search (e.g. guidance-only queries).
func CategoryFromPath(path string) string {
	switch {
	case strings.HasPrefix(path, "guidance/"):
		return "guidance"
	case strings.HasPrefix(path, "skills/"):
		return "skill"
	case strings.HasPrefix(path, "flows/"):
		return "flow"
	case path == "SELF.md":
		return "self"
	case path == "INSTRUCTIONS.md":
		return "instructions"
	default:
		return "knowledge"
	}
}

// section represents a markdown section (## heading + body lines).
type section struct {
	startLine int      // 1-indexed line number of the heading (or first line for preamble)
	endLine   int      // 1-indexed line number of the last content line
	lines     []string // all lines including the heading
}

// splitSections splits markdown content into sections at ## heading boundaries.
// The preamble (content before the first ## heading) becomes its own section.
func splitSections(lines []string) []section {
	var sections []section
	var current *section

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "## ") {
			// Flush previous section
			if current != nil && len(current.lines) > 0 {
				current.endLine = lineNum - 1
				sections = append(sections, *current)
			}
			current = &section{startLine: lineNum}
			current.lines = append(current.lines, line)
			continue
		}

		if current == nil {
			// Preamble (before first ## heading)
			// Skip leading empty lines
			if trimmed == "" && len(sections) == 0 {
				continue
			}
			current = &section{startLine: lineNum}
		}
		current.lines = append(current.lines, line)
	}

	// Flush last section
	if current != nil && len(current.lines) > 0 {
		current.endLine = len(lines)
		sections = append(sections, *current)
	}

	return sections
}

// sectionCharCount returns the total character count of a section's lines
// joined by newlines.
func sectionCharCount(s *section) int {
	total := 0
	for i, line := range s.lines {
		if i > 0 {
			total++ // newline separator
		}
		total += len(line)
	}
	return total
}

// mergeSmallSections merges sections shorter than minChars with the following
// section so we don't create tiny chunks with noisy embeddings. Only merges
// forward into sections that are themselves small; a small section followed by
// a large section gets absorbed into the large one rather than consuming it.
func mergeSmallSections(sections []section, minChars int) []section {
	if len(sections) <= 1 {
		return sections
	}

	var merged []section
	for i := 0; i < len(sections); i++ {
		s := sections[i]
		// Merge small sections with the next one, but only if the next one
		// is also small. This prevents a tiny 2-line section from swallowing
		// a large, semantically distinct section.
		for i+1 < len(sections) && sectionCharCount(&s) < minChars {
			next := sections[i+1]
			// If the next section is large enough to stand alone, absorb the
			// small accumulated section into it rather than merging further.
			if sectionCharCount(&next) >= minChars {
				// Prepend current small content to the next section
				next.lines = append(s.lines, next.lines...)
				next.startLine = s.startLine
				s = next
				i++
				break
			}
			s.lines = append(s.lines, next.lines...)
			s.endLine = next.endLine
			i++
		}
		merged = append(merged, s)
	}
	return merged
}

// ChunkFile splits a markdown file into chunks using heading-aware boundaries.
//
// The algorithm:
//  1. Split content into sections at ## heading boundaries
//  2. Merge very small sections (< 100 chars) with the next section
//  3. Sections that fit within maxChars become a single chunk
//  4. Sections exceeding maxChars are split by size with overlap
//
// This produces semantically coherent chunks where each ## section gets its
// own embedding, preventing unrelated content from diluting search signals.
func ChunkFile(path string, content string, maxChars int, overlapChars int) []Chunk {
	if content == "" {
		return nil
	}
	if maxChars <= 0 {
		maxChars = 1200
	}
	if overlapChars < 0 {
		overlapChars = 0
	}

	allLines := strings.Split(content, "\n")
	category := CategoryFromPath(path)

	// Split into sections at ## boundaries
	sections := splitSections(allLines)
	if len(sections) == 0 {
		return nil
	}

	// Merge very small sections
	const minSectionChars = 100
	sections = mergeSmallSections(sections, minSectionChars)

	var chunks []Chunk

	makeChunk := func(text string, startLine, endLine int) {
		contentHash := sha256Hex(text)
		id := sha256Hex(fmt.Sprintf("%s:%d:%d:%s", path, startLine, endLine, contentHash))
		chunks = append(chunks, Chunk{
			ID:        id,
			Text:      text,
			Path:      path,
			StartLine: startLine,
			EndLine:   endLine,
			Hash:      contentHash,
			Category:  category,
		})
	}

	for _, sec := range sections {
		chars := sectionCharCount(&sec)

		if chars <= maxChars {
			// Section fits in one chunk
			text := strings.Join(sec.lines, "\n")
			makeChunk(text, sec.startLine, sec.endLine)
			continue
		}

		// Section too large — split by size with overlap
		chunkBySize(sec.lines, sec.startLine, maxChars, overlapChars, makeChunk)
	}

	return chunks
}

// chunkBySize splits lines into size-limited chunks with overlap.
// This is the fallback for sections that exceed maxChars.
func chunkBySize(lines []string, globalStartLine int, maxChars int, overlapChars int,
	emit func(text string, startLine, endLine int)) {

	startLine := globalStartLine
	var currentLines []string
	currentChars := 0

	for i, line := range lines {
		lineNum := globalStartLine + i
		lineLen := len(line)
		if len(currentLines) > 0 {
			lineLen++ // newline separator
		}

		if currentChars+lineLen > maxChars && len(currentLines) > 0 {
			endLine := lineNum - 1
			text := strings.Join(currentLines, "\n")
			emit(text, startLine, endLine)

			overlapLines, overlapCharCount := computeOverlap(currentLines, overlapChars)
			currentLines = overlapLines
			currentChars = overlapCharCount
			startLine = endLine - len(overlapLines) + 1
			if startLine < globalStartLine {
				startLine = globalStartLine
			}
		}

		currentLines = append(currentLines, line)
		currentChars += lineLen
	}

	if len(currentLines) > 0 {
		endLine := globalStartLine + len(lines) - 1
		text := strings.Join(currentLines, "\n")
		emit(text, startLine, endLine)
	}
}

// computeOverlap returns the trailing lines from the slice that fit within
// the given character budget.
func computeOverlap(lines []string, maxChars int) ([]string, int) {
	if maxChars <= 0 || len(lines) == 0 {
		return nil, 0
	}

	var result []string
	chars := 0

	// Walk from the end
	for i := len(lines) - 1; i >= 0; i-- {
		lineLen := len(lines[i])
		if len(result) > 0 {
			lineLen++ // account for separator
		}
		if chars+lineLen > maxChars {
			break
		}
		result = append([]string{lines[i]}, result...)
		chars += lineLen
	}

	return result, chars
}

// sha256Hex returns the hex-encoded SHA-256 hash of a string.
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}
