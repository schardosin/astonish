package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// MemoryGetArgs defines the arguments for the memory_get tool.
type MemoryGetArgs struct {
	Path  string `json:"path" jsonschema:"Relative path within the memory directory (e.g. projects/astonish.md or MEMORY.md)"`
	From  int    `json:"from,omitempty" jsonschema:"Starting line number, 1-indexed (default 1)"`
	Lines int    `json:"lines,omitempty" jsonschema:"Number of lines to read (default 50)"`
}

// MemoryGetResult is returned from memory get.
type MemoryGetResult struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	From       int    `json:"from"`
	To         int    `json:"to"`
	TotalLines int    `json:"total_lines"`
}

// MemoryGet reads specific lines from a memory file.
func MemoryGet(memoryDir string) func(ctx tool.Context, args MemoryGetArgs) (MemoryGetResult, error) {
	return func(ctx tool.Context, args MemoryGetArgs) (MemoryGetResult, error) {
		if args.Path == "" {
			return MemoryGetResult{}, fmt.Errorf("path is required")
		}

		// Prevent path traversal
		clean := filepath.Clean(args.Path)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return MemoryGetResult{}, fmt.Errorf("invalid path: must be relative and within the memory directory")
		}

		absPath := filepath.Join(memoryDir, clean)
		content, err := os.ReadFile(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				return MemoryGetResult{}, fmt.Errorf("file not found: %s", args.Path)
			}
			return MemoryGetResult{}, fmt.Errorf("failed to read file: %w", err)
		}

		allLines := strings.Split(string(content), "\n")
		totalLines := len(allLines)

		from := args.From
		if from <= 0 {
			from = 1
		}
		lines := args.Lines
		if lines <= 0 {
			lines = 50
		}

		// Convert to 0-indexed for slicing
		startIdx := from - 1
		if startIdx >= totalLines {
			return MemoryGetResult{
				Path:       args.Path,
				Content:    "",
				From:       from,
				To:         from,
				TotalLines: totalLines,
			}, nil
		}

		endIdx := startIdx + lines
		if endIdx > totalLines {
			endIdx = totalLines
		}

		selectedLines := allLines[startIdx:endIdx]
		result := strings.Join(selectedLines, "\n")

		return MemoryGetResult{
			Path:       args.Path,
			Content:    result,
			From:       from,
			To:         startIdx + len(selectedLines),
			TotalLines: totalLines,
		}, nil
	}
}

// NewMemoryGetTool creates the memory_get tool for reading specific lines from memory files.
func NewMemoryGetTool(memoryDir string) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "memory_get",
		Description: "Read specific lines from a memory file. Use after memory_search " +
			"to pull the full context around a search result. " +
			"Specify the path from the search result and optionally the line range.",
	}, MemoryGet(memoryDir))
}
