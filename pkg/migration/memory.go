package migration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/schardosin/astonish/pkg/store"
)

// memoryChunk represents a chunk of text to be embedded and stored.
type memoryChunk struct {
	Content    string
	Category   string
	SourcePath string
}

func (m *Migrator) migrateMemory(ctx context.Context, teamDS store.TeamDataStore) (int, error) {
	memDir := filepath.Join(m.configDir, "memory")

	if _, err := os.Stat(memDir); os.IsNotExist(err) {
		m.emitProgress(CatMemory, 0, 0, "skipped", "")
		return 0, nil
	}

	m.emitProgress(CatMemory, 0, 0, "counting", "")

	// Collect all markdown files from memory directory
	var mdFiles []string
	_ = filepath.Walk(memDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		// Skip skills directory (migrated separately) and vector/model dirs
		rel, _ := filepath.Rel(memDir, path)
		if strings.HasPrefix(rel, "skills") || strings.HasPrefix(rel, "vectors") ||
			strings.HasPrefix(rel, "flows") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.IsDir() && (strings.HasSuffix(info.Name(), ".md") || strings.HasSuffix(info.Name(), ".txt")) {
			mdFiles = append(mdFiles, path)
		}
		return nil
	})

	if len(mdFiles) == 0 {
		m.emitProgress(CatMemory, 0, 0, "skipped", "")
		return 0, nil
	}

	// Read all files and split into chunks
	var chunks []memoryChunk
	for _, path := range mdFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}

		rel, _ := filepath.Rel(memDir, path)
		category := deriveCategory(rel)

		// Simple chunking: split by ## headings
		sections := splitByHeadings(content)
		for _, section := range sections {
			section = strings.TrimSpace(section)
			if section == "" {
				continue
			}
			// Split oversized sections
			for _, chunk := range splitOversized(section, 1200) {
				chunks = append(chunks, memoryChunk{
					Content:    chunk,
					Category:   category,
					SourcePath: rel,
				})
			}
		}
	}

	total := len(chunks)
	if total == 0 {
		m.emitProgress(CatMemory, 0, 0, "skipped", "")
		return 0, nil
	}

	m.emitProgress(CatMemory, 0, total, "migrating", "")

	// Get the memory store — needs the AddMemory method from pgstore
	memStore := teamDS.Memories()
	pgMem, hasAdd := memStore.(interface {
		AddMemory(ctx context.Context, content, category, sourcePath string, embedding []float32, metadata map[string]any) error
	})
	if !hasAdd {
		m.emitProgress(CatMemory, 0, total, "error", "memory store does not support AddMemory")
		return 0, fmt.Errorf("memory store does not support AddMemory; expected pgstore implementation")
	}

	// Try to initialize the embedder for vector embeddings
	embedder, embedErr := newMigrationEmbedder(m.appCfg)
	if embedErr != nil {
		// Proceed without embeddings — text search will still work
		fmt.Fprintf(os.Stderr, "warning: cannot initialize embedder, memories will be stored without vector embeddings: %v\n", embedErr)
	}
	if embedder != nil {
		defer embedder.Close()
	}

	count := 0
	for _, chunk := range chunks {
		if ctx.Err() != nil {
			return count, ctx.Err()
		}

		var embedding []float32
		if embedder != nil {
			emb, err := embedder.Embed(ctx, chunk.Content)
			if err == nil {
				embedding = emb
			}
		}

		metadata := map[string]any{
			"source": "migration",
			"path":   chunk.SourcePath,
		}

		if err := pgMem.AddMemory(ctx, chunk.Content, chunk.Category, chunk.SourcePath, embedding, metadata); err != nil {
			return count, fmt.Errorf("failed to add memory chunk: %w", err)
		}

		count++
		if count%10 == 0 || count == total {
			m.emitProgress(CatMemory, count, total, "migrating", "")
		}
	}

	m.emitProgress(CatMemory, count, total, "done", "")
	return count, nil
}

// deriveCategory returns the memory category based on relative file path.
func deriveCategory(relPath string) string {
	switch {
	case strings.HasPrefix(relPath, "knowledge/"):
		return "knowledge"
	case strings.HasPrefix(relPath, "guidance/"):
		return "guidance"
	case relPath == "MEMORY.md":
		return "memory"
	case relPath == "SELF.md":
		return "self"
	case relPath == "INSTRUCTIONS.md":
		return "instructions"
	default:
		return "knowledge"
	}
}

// splitByHeadings splits markdown content at ## headings.
func splitByHeadings(content string) []string {
	lines := strings.Split(content, "\n")
	var sections []string
	var current strings.Builder

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") && current.Len() > 0 {
			sections = append(sections, current.String())
			current.Reset()
		}
		current.WriteString(line)
		current.WriteString("\n")
	}
	if current.Len() > 0 {
		sections = append(sections, current.String())
	}

	return sections
}

// splitOversized splits a section into chunks of at most maxChars.
func splitOversized(section string, maxChars int) []string {
	if len(section) <= maxChars {
		return []string{section}
	}

	var chunks []string
	for len(section) > 0 {
		end := maxChars
		if end > len(section) {
			end = len(section)
		}
		// Try to break at a newline
		if end < len(section) {
			if idx := strings.LastIndex(section[:end], "\n"); idx > end/2 {
				end = idx + 1
			}
		}
		chunks = append(chunks, section[:end])
		section = section[end:]
	}
	return chunks
}
