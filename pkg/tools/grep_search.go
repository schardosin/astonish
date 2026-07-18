package tools

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"google.golang.org/adk/tool"
)

// GrepSearchArgs defines arguments for the grep_search tool
type GrepSearchArgs struct {
	Pattern       string   `json:"pattern" jsonschema:"The search pattern (literal string by default, or regex if regex=true)"`
	SearchPath    string   `json:"search_path,omitempty" jsonschema:"Directory or file to search (default: current dir)"`
	IncludeGlobs  []string `json:"include_globs,omitempty" jsonschema:"File patterns to include (e.g., '*.go', '*.js')"`
	CaseSensitive bool     `json:"case_sensitive,omitempty" jsonschema:"Case-sensitive search (default: false)"`
	MaxResults    int      `json:"max_results,omitempty" jsonschema:"Maximum total results to return (default: 50)"`
	Regex         bool     `json:"regex,omitempty" jsonschema:"Treat pattern as a regular expression (default: false, literal search)"`
	Glob          string   `json:"glob,omitempty" jsonschema:"Single glob filter for file paths (e.g., '*.ts', 'src/**/*.go')"`
	Type          string   `json:"type,omitempty" jsonschema:"Ripgrep type filter (e.g., 'go', 'ts', 'py'). Requires ripgrep."`
	Context       int      `json:"context,omitempty" jsonschema:"Number of context lines before and after each match (symmetric)"`
	BeforeContext int      `json:"before_context,omitempty" jsonschema:"Number of context lines before each match"`
	AfterContext  int      `json:"after_context,omitempty" jsonschema:"Number of context lines after each match"`
	Multiline     bool     `json:"multiline,omitempty" jsonschema:"Enable multiline matching (dot matches newline). Requires ripgrep."`
	HeadLimit     int      `json:"head_limit,omitempty" jsonschema:"Stop reading output after this many bytes (default: 5MB). Prevents huge outputs."`
}

// GrepMatch represents a single search match or context line
type GrepMatch struct {
	File       string `json:"file"`
	LineNumber int    `json:"line_number"`
	Content    string `json:"content"`
	Kind       string `json:"kind"` // "match" or "context"
}

// GrepSearchResult is the result returned by the grep_search tool
type GrepSearchResult struct {
	Matches         []GrepMatch `json:"matches"`
	Total           int         `json:"total"`
	Capped          bool        `json:"capped"`
	SearchIn        string      `json:"search_in"`
	TruncatedReason string      `json:"truncated_reason,omitempty"`
	PatternMode     string      `json:"pattern_mode"`
	DurationMs      int64       `json:"duration_ms"`
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
	start := time.Now()

	// Set defaults
	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = 50
	}

	headLimit := args.HeadLimit
	if headLimit <= 0 {
		headLimit = 5 * 1024 * 1024 // 5MB
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
	absPath, err := filepath.Abs(expandPath(searchPath))
	if err != nil {
		return GrepSearchResult{}, err
	}

	patternMode := "literal"
	if args.Regex {
		patternMode = "regex"
	}

	// Validate regex pattern early
	if args.Regex {
		if _, err := regexp.Compile(args.Pattern); err != nil {
			return GrepSearchResult{}, fmt.Errorf("invalid regex pattern: %w", err)
		}
	}

	// Try ripgrep first, fall back to Go implementation
	matches, truncatedReason, err := tryRipgrep(args, absPath, maxResults, headLimit)
	if err != nil {
		// Check if unsupported features were requested
		if args.Multiline || args.Type != "" {
			return GrepSearchResult{}, fmt.Errorf("ripgrep required for multiline/type features but unavailable: %w", err)
		}
		// Fallback to Go implementation (supports literal, regex, case, globs, cap)
		matches, err = goGrep(args.Pattern, absPath, mergeGlobs(args), args.CaseSensitive, args.Regex, maxResults)
		if err != nil {
			return GrepSearchResult{}, err
		}
		truncatedReason = ""
	}

	capped := len(matches) >= maxResults
	if capped && truncatedReason == "" {
		truncatedReason = fmt.Sprintf("result limit reached (%d)", maxResults)
	}

	elapsed := time.Since(start).Milliseconds()

	return GrepSearchResult{
		Matches:         matches,
		Total:           len(matches),
		Capped:          capped,
		SearchIn:        absPath,
		TruncatedReason: truncatedReason,
		PatternMode:     patternMode,
		DurationMs:      elapsed,
	}, nil
}

// mergeGlobs combines IncludeGlobs and the single Glob field into one slice
func mergeGlobs(args GrepSearchArgs) []string {
	globs := append([]string{}, args.IncludeGlobs...)
	if args.Glob != "" {
		globs = append(globs, args.Glob)
	}
	return globs
}

// tryRipgrep attempts to use ripgrep for searching
func tryRipgrep(args GrepSearchArgs, searchPath string, maxResults, headLimit int) ([]GrepMatch, string, error) {
	// Check if rg is available
	rgPath, err := exec.LookPath("rg")
	if err != nil {
		return nil, "", fmt.Errorf("ripgrep not found")
	}

	// Build rg command
	rgArgs := []string{
		"--json",
		"--no-heading",
		"--max-filesize", "5M",
	}

	// Case sensitivity
	if !args.CaseSensitive {
		rgArgs = append(rgArgs, "--ignore-case")
	}

	// Pattern mode: literal (fixed strings) or regex
	if !args.Regex {
		rgArgs = append(rgArgs, "--fixed-strings")
	}

	// Multiline
	if args.Multiline {
		rgArgs = append(rgArgs, "--multiline", "--multiline-dotall")
	}

	// Type filter
	if args.Type != "" {
		rgArgs = append(rgArgs, "--type", args.Type)
	}

	// Context lines
	if args.Context > 0 {
		rgArgs = append(rgArgs, fmt.Sprintf("-C%d", args.Context))
	} else {
		if args.BeforeContext > 0 {
			rgArgs = append(rgArgs, fmt.Sprintf("-B%d", args.BeforeContext))
		}
		if args.AfterContext > 0 {
			rgArgs = append(rgArgs, fmt.Sprintf("-A%d", args.AfterContext))
		}
	}

	// Include globs (from both IncludeGlobs and Glob field)
	for _, glob := range args.IncludeGlobs {
		rgArgs = append(rgArgs, "--glob", glob)
	}
	if args.Glob != "" {
		rgArgs = append(rgArgs, "--glob", args.Glob)
	}

	// Add pattern and path
	rgArgs = append(rgArgs, args.Pattern, searchPath)

	cmd := exec.Command(rgPath, rgArgs...)
	output, _ := cmd.Output() // rg returns exit code 1 if no matches, but output is still valid

	// Check head limit
	truncatedReason := ""
	if len(output) > headLimit {
		output = output[:headLimit]
		truncatedReason = fmt.Sprintf("output truncated at %d bytes", headLimit)
	}

	matches, err := parseRipgrepOutput(output, maxResults)
	return matches, truncatedReason, err
}

// parseRipgrepOutput parses ripgrep's JSON output, returning both match and context lines
func parseRipgrepOutput(output []byte, maxResults int) ([]GrepMatch, error) {
	var matches []GrepMatch
	matchCount := 0
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

		var kind string
		switch rg.Type {
		case "match":
			kind = "match"
			matchCount++
		case "context":
			kind = "context"
		default:
			continue
		}

		match := GrepMatch{
			File:       rg.Data.Path.Text,
			LineNumber: rg.Data.LineNumber,
			Content:    strings.TrimRight(rg.Data.Lines.Text, "\n\r"),
			Kind:       kind,
		}
		matches = append(matches, match)

		// Cap based on match count (context lines are free)
		if matchCount >= maxResults {
			break
		}
	}

	return matches, nil
}

// goGrep is a pure Go fallback for grep functionality
func goGrep(pattern, searchPath string, includeGlobs []string, caseSensitive, isRegex bool, maxResults int) ([]GrepMatch, error) {
	var matches []GrepMatch

	// Prepare the matcher
	var matcher func(line string) bool
	if isRegex {
		flags := ""
		if !caseSensitive {
			flags = "(?i)"
		}
		re, err := regexp.Compile(flags + pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex pattern: %w", err)
		}
		matcher = func(line string) bool {
			return re.MatchString(line)
		}
	} else {
		searchPattern := pattern
		if !caseSensitive {
			searchPattern = strings.ToLower(pattern)
		}
		matcher = func(line string) bool {
			searchLine := line
			if !caseSensitive {
				searchLine = strings.ToLower(line)
			}
			return strings.Contains(searchLine, searchPattern)
		}
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
		fileMatches, err := searchFileWithMatcher(path, matcher, maxResults-len(matches))
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

// searchFileWithMatcher searches for matches in a single file using a matcher function
func searchFileWithMatcher(path string, matcher func(string) bool, maxMatches int) ([]GrepMatch, error) {
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

		if matcher(line) {
			matches = append(matches, GrepMatch{
				File:       path,
				LineNumber: lineNum,
				Content:    strings.TrimSpace(line),
				Kind:       "match",
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
