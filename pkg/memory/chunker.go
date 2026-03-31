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

// ChunkFile splits a file into overlapping chunks.
// Uses character-based, line-oriented chunking: accumulates lines until
// maxChars is reached, then flushes a chunk and carries trailing lines
// as overlap for the next chunk.
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

	lines := strings.Split(content, "\n")
	var chunks []Chunk
	category := CategoryFromPath(path)

	startLine := 1
	var currentLines []string
	currentChars := 0

	flushChunk := func(endLine int) {
		if len(currentLines) == 0 {
			return
		}
		text := strings.Join(currentLines, "\n")
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

	for i, line := range lines {
		lineNum := i + 1 // 1-indexed
		lineLen := len(line)
		if len(currentLines) > 0 {
			lineLen++ // account for the \n separator
		}

		// If adding this line would exceed maxChars and we already have content,
		// flush the current chunk
		if currentChars+lineLen > maxChars && len(currentLines) > 0 {
			endLine := lineNum - 1
			flushChunk(endLine)

			// Compute overlap: carry trailing lines whose total chars fit in overlapChars
			overlapLines, overlapCharCount := computeOverlap(currentLines, overlapChars)

			currentLines = overlapLines
			currentChars = overlapCharCount
			startLine = endLine - len(overlapLines) + 1
			if startLine < 1 {
				startLine = 1
			}
		}

		currentLines = append(currentLines, line)
		currentChars += lineLen
	}

	// Flush remaining content
	if len(currentLines) > 0 {
		flushChunk(len(lines))
	}

	return chunks
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
