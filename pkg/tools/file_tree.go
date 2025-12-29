package tools

import (
	"os"
	"path/filepath"
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
	Path     string `json:"path" jsonschema:"Root directory path to scan"`
	MaxDepth int    `json:"max_depth,omitempty" jsonschema:"Maximum depth to traverse (default: 3)"`
}

// FileTreeEntry represents a single file or directory in the tree (flat structure)
type FileTreeEntry struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size,omitempty"`
	Depth int    `json:"depth"` // Indentation level for tree visualization
}

// FileTreeResult is the result returned by the file_tree tool
type FileTreeResult struct {
	Entries []FileTreeEntry `json:"entries"`
	Root    string          `json:"root"`
	Total   int             `json:"total"`
}

// FileTree returns a structured view of the directory tree
func FileTree(ctx tool.Context, args FileTreeArgs) (FileTreeResult, error) {
	// Default max depth
	maxDepth := args.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 3
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
	absPath, err := filepath.Abs(rootPath)
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

	// Build the flat tree
	var entries []FileTreeEntry
	collectEntries(absPath, 0, maxDepth, &entries)

	return FileTreeResult{
		Entries: entries,
		Root:    absPath,
		Total:   len(entries),
	}, nil
}

// collectEntries recursively collects entries into a flat list
func collectEntries(path string, currentDepth, maxDepth int, entries *[]FileTreeEntry) {
	if currentDepth >= maxDepth {
		return
	}

	dirEntries, err := os.ReadDir(path)
	if err != nil {
		return
	}

	for _, entry := range dirEntries {
		name := entry.Name()

		// Skip hidden files (except at root) and excluded directories
		if strings.HasPrefix(name, ".") && currentDepth > 0 {
			continue
		}

		// Skip excluded directories
		if entry.IsDir() && defaultExclusions[name] {
			continue
		}

		fullPath := filepath.Join(path, name)
		treeEntry := FileTreeEntry{
			Path:  fullPath,
			Name:  name,
			IsDir: entry.IsDir(),
			Depth: currentDepth,
		}

		if !entry.IsDir() {
			// Get file size
			if info, err := entry.Info(); err == nil {
				treeEntry.Size = info.Size()
			}
		}

		*entries = append(*entries, treeEntry)

		// Recursively collect children for directories
		if entry.IsDir() {
			collectEntries(fullPath, currentDepth+1, maxDepth, entries)
		}
	}
}
