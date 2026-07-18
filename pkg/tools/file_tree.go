package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"google.golang.org/adk/tool"
)

// Default directories to exclude from file tree
var defaultExclusions = map[string]bool{
	".git":         true,
	"node_modules": true,
	"dist":         true,
	"__pycache__":  true,
	".venv":        true,
	"venv":         true,
	".idea":        true,
	".vscode":      true,
	"vendor":       true,
	"build":        true,
	".next":        true,
	"coverage":     true,
}

// FileTreeArgs defines arguments for the file_tree tool
type FileTreeArgs struct {
	Path           string `json:"path" jsonschema:"Root directory path to scan"`
	MaxDepth       int    `json:"max_depth,omitempty" jsonschema:"Maximum depth to traverse (default: 3)"`
	MaxEntries     int    `json:"max_entries,omitempty" jsonschema:"Maximum entries to return (default: 500)"`
	MaxOutputChars int    `json:"max_output_chars,omitempty" jsonschema:"Approximate char budget for output (default: 20000)"`
	Summarize      *bool  `json:"summarize,omitempty" jsonschema:"Summarize large subtrees instead of listing all files (default: true)"`
}

// FileTreeEntry represents a single file or directory in the tree (flat structure)
type FileTreeEntry struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	IsDir     bool   `json:"is_dir"`
	Size      int64  `json:"size,omitempty"`
	Depth     int    `json:"depth"`                // Indentation level for tree visualization
	Summary   string `json:"summary,omitempty"`    // Extension summary for collapsed subtrees
	Truncated bool   `json:"truncated,omitempty"`  // True if this entry represents a collapsed subtree
	FileCount int    `json:"file_count,omitempty"` // Total files in subtree (when summarized)
	DirCount  int    `json:"dir_count,omitempty"`  // Total dirs in subtree (when summarized)
}

// FileTreeResult is the result returned by the file_tree tool
type FileTreeResult struct {
	Entries         []FileTreeEntry `json:"entries"`
	Root            string          `json:"root"`
	Total           int             `json:"total"`
	TruncatedReason string          `json:"truncated_reason,omitempty"`
}

// FileTree returns a structured view of the directory tree with budget-aware traversal
func FileTree(ctx tool.Context, args FileTreeArgs) (FileTreeResult, error) {
	// Default max depth
	maxDepth := args.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 3
	}

	maxEntries := args.MaxEntries
	if maxEntries <= 0 {
		maxEntries = 500
	}

	maxOutputChars := args.MaxOutputChars
	if maxOutputChars <= 0 {
		maxOutputChars = 20000
	}

	summarize := true
	if args.Summarize != nil {
		summarize = *args.Summarize
	}

	// Resolve the path
	rootPath := args.Path
	if rootPath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return FileTreeResult{}, err
		}
		rootPath = cwd
	}

	// Make path absolute
	absPath, err := filepath.Abs(expandPath(rootPath))
	if err != nil {
		return FileTreeResult{}, err
	}

	// Verify path exists and is a directory
	info, err := os.Stat(absPath)
	if err != nil {
		return FileTreeResult{}, err
	}
	if !info.IsDir() {
		return FileTreeResult{}, os.ErrInvalid
	}

	// Build the tree with budget tracking
	budget := &treeBudget{
		maxEntries:     maxEntries,
		maxOutputChars: maxOutputChars,
		maxDepth:       maxDepth,
		summarize:      summarize,
		charCount:      0,
		entryCount:     0,
	}

	var entries []FileTreeEntry
	truncatedReason := collectBudgetedEntries(absPath, 0, budget, &entries)

	return FileTreeResult{
		Entries:         entries,
		Root:            absPath,
		Total:           len(entries),
		TruncatedReason: truncatedReason,
	}, nil
}

// treeBudget tracks traversal budgets
type treeBudget struct {
	maxEntries     int
	maxOutputChars int
	maxDepth       int
	summarize      bool
	charCount      int
	entryCount     int
}

// exhausted returns true when any budget is exceeded
func (b *treeBudget) exhausted() bool {
	return b.entryCount >= b.maxEntries || b.charCount >= b.maxOutputChars
}

// addEntry tracks an entry against the budget. Returns the estimated char cost.
func (b *treeBudget) addEntry(entry FileTreeEntry) int {
	b.entryCount++
	// Rough char estimate: path + name + overhead
	cost := len(entry.Path) + len(entry.Name) + len(entry.Summary) + 50
	b.charCount += cost
	return cost
}

// collectBudgetedEntries does a breadth-first-ish traversal with budget awareness.
// It always shows immediate children first, then recurses into subdirs.
// Returns a truncation reason if budget was hit.
func collectBudgetedEntries(path string, currentDepth int, budget *treeBudget, entries *[]FileTreeEntry) string {
	if currentDepth >= budget.maxDepth {
		return ""
	}

	if budget.exhausted() {
		return fmt.Sprintf("budget exhausted (entries: %d/%d, chars: ~%d/%d)",
			budget.entryCount, budget.maxEntries, budget.charCount, budget.maxOutputChars)
	}

	dirEntries, err := os.ReadDir(path)
	if err != nil {
		return ""
	}

	// Separate dirs and files, sort each group
	var dirs, files []os.DirEntry
	for _, entry := range dirEntries {
		name := entry.Name()

		// Skip hidden files (except at root depth) and excluded directories
		if strings.HasPrefix(name, ".") && currentDepth > 0 {
			continue
		}
		if entry.IsDir() && defaultExclusions[name] {
			continue
		}

		if entry.IsDir() {
			dirs = append(dirs, entry)
		} else {
			files = append(files, entry)
		}
	}

	// Add all immediate children (dirs first, then files) — this ensures
	// the sibling level is never starved by a single large subtree
	for _, d := range dirs {
		if budget.exhausted() {
			return fmt.Sprintf("budget exhausted (entries: %d/%d, chars: ~%d/%d)",
				budget.entryCount, budget.maxEntries, budget.charCount, budget.maxOutputChars)
		}

		fullPath := filepath.Join(path, d.Name())
		entry := FileTreeEntry{
			Path:  fullPath,
			Name:  d.Name(),
			IsDir: true,
			Depth: currentDepth,
		}
		budget.addEntry(entry)
		*entries = append(*entries, entry)
	}

	for _, f := range files {
		if budget.exhausted() {
			return fmt.Sprintf("budget exhausted (entries: %d/%d, chars: ~%d/%d)",
				budget.entryCount, budget.maxEntries, budget.charCount, budget.maxOutputChars)
		}

		fullPath := filepath.Join(path, f.Name())
		entry := FileTreeEntry{
			Path:  fullPath,
			Name:  f.Name(),
			IsDir: false,
			Depth: currentDepth,
		}
		if info, err := f.Info(); err == nil {
			entry.Size = info.Size()
		}
		budget.addEntry(entry)
		*entries = append(*entries, entry)
	}

	// Now recurse into each subdirectory
	for _, d := range dirs {
		if budget.exhausted() {
			return fmt.Sprintf("budget exhausted (entries: %d/%d, chars: ~%d/%d)",
				budget.entryCount, budget.maxEntries, budget.charCount, budget.maxOutputChars)
		}

		fullPath := filepath.Join(path, d.Name())

		// If summarize is enabled and this subtree is large, collapse it
		if budget.summarize && currentDepth+1 < budget.maxDepth {
			fileCount, dirCount, extCounts := countSubtree(fullPath)
			totalItems := fileCount + dirCount

			// Heuristic: summarize if subtree has > 50 items or would consume > 20% remaining budget
			remainingEntries := budget.maxEntries - budget.entryCount
			if totalItems > 50 || (remainingEntries > 0 && totalItems > remainingEntries/3) {
				summary := buildExtensionSummary(fileCount, dirCount, extCounts)
				summaryEntry := FileTreeEntry{
					Path:      fullPath,
					Name:      d.Name(),
					IsDir:     true,
					Depth:     currentDepth,
					Summary:   summary,
					Truncated: true,
					FileCount: fileCount,
					DirCount:  dirCount,
				}
				// Find and update the existing directory entry with summary info
				for i := range *entries {
					if (*entries)[i].Path == fullPath && (*entries)[i].IsDir {
						(*entries)[i].Summary = summary
						(*entries)[i].Truncated = true
						(*entries)[i].FileCount = fileCount
						(*entries)[i].DirCount = dirCount
						break
					}
				}
				_ = summaryEntry // used for budget tracking only
				continue
			}
		}

		reason := collectBudgetedEntries(fullPath, currentDepth+1, budget, entries)
		if reason != "" {
			return reason
		}
	}

	return ""
}

// countSubtree counts total files, dirs, and extension distribution in a subtree
func countSubtree(path string) (fileCount, dirCount int, extCounts map[string]int) {
	extCounts = make(map[string]int)

	filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Respect exclusions
		if info.IsDir() {
			if defaultExclusions[info.Name()] {
				return filepath.SkipDir
			}
			if p != path { // don't count the root itself
				dirCount++
			}
			return nil
		}

		fileCount++
		ext := strings.ToLower(filepath.Ext(info.Name()))
		if ext == "" {
			ext = "(no ext)"
		}
		extCounts[ext]++
		return nil
	})

	return
}

// buildExtensionSummary creates a human-readable summary of a subtree
func buildExtensionSummary(fileCount, dirCount int, extCounts map[string]int) string {
	// Sort extensions by count descending
	type extEntry struct {
		ext   string
		count int
	}
	var sorted []extEntry
	for ext, count := range extCounts {
		sorted = append(sorted, extEntry{ext, count})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].count > sorted[j].count
	})

	// Take top 5 extensions
	var parts []string
	shown := 0
	for _, e := range sorted {
		if shown >= 5 {
			break
		}
		parts = append(parts, fmt.Sprintf("%d %s", e.count, e.ext))
		shown++
	}

	summary := fmt.Sprintf("[%d files, %d dirs", fileCount, dirCount)
	if len(parts) > 0 {
		summary += ": " + strings.Join(parts, ", ")
	}
	summary += "]"
	return summary
}
