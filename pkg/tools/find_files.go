package tools

import (
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/adk/tool"
)

// FindFilesArgs defines arguments for the find_files tool
type FindFilesArgs struct {
	Pattern    string `json:"pattern" jsonschema:"Filename pattern to match (supports glob: *.go, test_*.py)"`
	SearchPath string `json:"search_path,omitempty" jsonschema:"Directory to search from (default: current dir)"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"Maximum results to return (default: 50)"`
}

// FoundFile represents a matched file
type FoundFile struct {
	Path         string `json:"path"`
	RelativePath string `json:"relative_path"`
	Size         int64  `json:"size"`
	IsDir        bool   `json:"is_dir"`
}

// FindFilesResult is the result returned by the find_files tool
type FindFilesResult struct {
	Files    []FoundFile `json:"files"`
	Total    int         `json:"total"`
	Capped   bool        `json:"capped"`
	SearchIn string      `json:"search_in"`
}

// FindFiles locates files by name pattern using glob matching
func FindFiles(ctx tool.Context, args FindFilesArgs) (FindFilesResult, error) {
	// Set defaults
	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = 50
	}

	searchPath := args.SearchPath
	if searchPath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return FindFilesResult{}, err
		}
		searchPath = cwd
	}

	// Make path absolute
	absPath, err := filepath.Abs(searchPath)
	if err != nil {
		return FindFilesResult{}, err
	}

	// Verify path exists
	if _, err := os.Stat(absPath); err != nil {
		return FindFilesResult{}, err
	}

	var files []FoundFile

	err = filepath.Walk(absPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}

		// Skip excluded directories
		if info.IsDir() {
			name := info.Name()
			if defaultExclusions[name] {
				return filepath.SkipDir
			}
			// Don't add directories to results unless they match
		}

		// Check if we've hit max results
		if len(files) >= maxResults {
			return filepath.SkipAll
		}

		// Match the pattern against the filename
		name := info.Name()
		matched, err := filepath.Match(args.Pattern, name)
		if err != nil {
			// Invalid pattern, try case-insensitive match
			matched, _ = filepath.Match(strings.ToLower(args.Pattern), strings.ToLower(name))
		}

		if matched {
			relPath, _ := filepath.Rel(absPath, path)
			files = append(files, FoundFile{
				Path:         path,
				RelativePath: relPath,
				Size:         info.Size(),
				IsDir:        info.IsDir(),
			})
		}

		return nil
	})

	if err != nil && err != filepath.SkipAll {
		return FindFilesResult{}, err
	}

	capped := len(files) >= maxResults

	return FindFilesResult{
		Files:    files,
		Total:    len(files),
		Capped:   capped,
		SearchIn: absPath,
	}, nil
}
