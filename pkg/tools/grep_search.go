package tools

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"google.golang.org/adk/tool"
)

// GrepSearchArgs defines arguments for the grep_search tool
type GrepSearchArgs struct {
	Pattern       string   `json:"pattern" jsonschema:"The search pattern (literal string)"`
	SearchPath    string   `json:"search_path,omitempty" jsonschema:"Directory or file to search (default: current dir)"`
	IncludeGlobs  []string `json:"include_globs,omitempty" jsonschema:"File patterns to include (e.g., '*.go', '*.js')"`
	CaseSensitive bool     `json:"case_sensitive,omitempty" jsonschema:"Case-sensitive search (default: false)"`
	MaxResults    int      `json:"max_results,omitempty" jsonschema:"Maximum results to return (default: 50)"`
}

// GrepMatch represents a single search match
type GrepMatch struct {
	File       string `json:"file"`
	LineNumber int    `json:"line_number"`
	Content    string `json:"content"`
}

// GrepSearchResult is the result returned by the grep_search tool
type GrepSearchResult struct {
	Matches  []GrepMatch `json:"matches"`
	Total    int         `json:"total"`
	Capped   bool        `json:"capped"`
	SearchIn string      `json:"search_in"`
}

// ripgrepMatch represents a match from ripgrep's JSON output
type ripgrepMatch struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber int `json:"line_number"`
	} `json:"data"`
}

// GrepSearch searches for text patterns in files using ripgrep or Go fallback
func GrepSearch(ctx tool.Context, args GrepSearchArgs) (GrepSearchResult, error) {
	// Set defaults
	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = 50
	}

	searchPath := args.SearchPath
	if searchPath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return GrepSearchResult{}, err
		}
		searchPath = cwd
	}

	// Make path absolute
	absPath, err := filepath.Abs(searchPath)
	if err != nil {
		return GrepSearchResult{}, err
	}

	// Try ripgrep first, fall back to Go implementation
	matches, err := tryRipgrep(args.Pattern, absPath, args.IncludeGlobs, args.CaseSensitive, maxResults)
	if err != nil {
		// Fallback to Go implementation
		matches, err = goGrep(args.Pattern, absPath, args.IncludeGlobs, args.CaseSensitive, maxResults)
		if err != nil {
			return GrepSearchResult{}, err
		}
	}

	capped := len(matches) >= maxResults

	return GrepSearchResult{
		Matches:  matches,
		Total:    len(matches),
		Capped:   capped,
		SearchIn: absPath,
	}, nil
}

// tryRipgrep attempts to use ripgrep for searching
func tryRipgrep(pattern, searchPath string, includeGlobs []string, caseSensitive bool, maxResults int) ([]GrepMatch, error) {
	// Check if rg is available
	rgPath, err := exec.LookPath("rg")
	if err != nil {
		return nil, fmt.Errorf("ripgrep not found")
	}

	// Build rg command
	args := []string{
		"--json",
		"--max-count", fmt.Sprintf("%d", maxResults),
		"--no-heading",
	}

	if !caseSensitive {
		args = append(args, "--ignore-case")
	}

	// Add include globs
	for _, glob := range includeGlobs {
		args = append(args, "--glob", glob)
	}

	// Add pattern and path
	args = append(args, "--fixed-strings", pattern, searchPath)

	cmd := exec.Command(rgPath, args...)
	output, _ := cmd.Output() // rg returns exit code 1 if no matches, but output is still valid

	return parseRipgrepOutput(output, maxResults)
}

// parseRipgrepOutput parses ripgrep's JSON output
func parseRipgrepOutput(output []byte, maxResults int) ([]GrepMatch, error) {
	var matches []GrepMatch
	scanner := bufio.NewScanner(strings.NewReader(string(output)))

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var rg ripgrepMatch
		if err := json.Unmarshal([]byte(line), &rg); err != nil {
			continue
		}

		if rg.Type != "match" {
			continue
		}

		match := GrepMatch{
			File:       rg.Data.Path.Text,
			LineNumber: rg.Data.LineNumber,
			Content:    strings.TrimSpace(rg.Data.Lines.Text),
		}
		matches = append(matches, match)

		if len(matches) >= maxResults {
			break
		}
	}

	return matches, nil
}

// goGrep is a pure Go fallback for grep functionality
func goGrep(pattern, searchPath string, includeGlobs []string, caseSensitive bool, maxResults int) ([]GrepMatch, error) {
	var matches []GrepMatch
	searchPattern := pattern
	if !caseSensitive {
		searchPattern = strings.ToLower(pattern)
	}

	err := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}

		// Skip directories
		if info.IsDir() {
			name := info.Name()
			// Skip excluded directories
			if defaultExclusions[name] {
				return filepath.SkipDir
			}
			return nil
		}

		// Check if we've hit max results
		if len(matches) >= maxResults {
			return filepath.SkipAll
		}

		// Check include globs
		if len(includeGlobs) > 0 {
			matched := false
			for _, glob := range includeGlobs {
				if m, _ := filepath.Match(glob, info.Name()); m {
					matched = true
					break
				}
			}
			if !matched {
				return nil
			}
		}

		// Skip binary files (simple heuristic: skip files without common text extensions)
		if !isLikelyTextFile(path) {
			return nil
		}

		// Search file
		fileMatches, err := searchFile(path, searchPattern, caseSensitive, maxResults-len(matches))
		if err != nil {
			return nil // Skip files we can't read
		}

		matches = append(matches, fileMatches...)
		return nil
	})

	if err != nil && err != filepath.SkipAll {
		return nil, err
	}

	return matches, nil
}

// searchFile searches for a pattern in a single file
func searchFile(path, pattern string, caseSensitive bool, maxMatches int) ([]GrepMatch, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var matches []GrepMatch
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		var searchLine string
		if caseSensitive {
			searchLine = line
		} else {
			searchLine = strings.ToLower(line)
		}

		if strings.Contains(searchLine, pattern) {
			matches = append(matches, GrepMatch{
				File:       path,
				LineNumber: lineNum,
				Content:    strings.TrimSpace(line),
			})

			if len(matches) >= maxMatches {
				break
			}
		}
	}

	return matches, nil
}

// isLikelyTextFile checks if a file is likely a text file based on extension
func isLikelyTextFile(path string) bool {
	textExtensions := map[string]bool{
		".go": true, ".js": true, ".ts": true, ".jsx": true, ".tsx": true,
		".py": true, ".rb": true, ".java": true, ".c": true, ".cpp": true,
		".h": true, ".hpp": true, ".cs": true, ".rs": true, ".swift": true,
		".kt": true, ".scala": true, ".php": true, ".pl": true, ".pm": true,
		".sh": true, ".bash": true, ".zsh": true, ".fish": true,
		".html": true, ".htm": true, ".css": true, ".scss": true, ".less": true,
		".json": true, ".xml": true, ".yaml": true, ".yml": true, ".toml": true,
		".md": true, ".txt": true, ".rst": true, ".adoc": true,
		".sql": true, ".graphql": true, ".proto": true,
		".env": true, ".gitignore": true, ".dockerignore": true,
		".makefile": true, ".dockerfile": true,
		".vue": true, ".svelte": true, ".astro": true,
	}

	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		// Check for common extensionless files
		base := strings.ToLower(filepath.Base(path))
		return base == "makefile" || base == "dockerfile" || base == "readme" || base == "license"
	}

	return textExtensions[ext]
}
