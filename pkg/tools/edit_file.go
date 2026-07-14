package tools

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"google.golang.org/adk/tool"
)

// EditFileArgs are the arguments for the edit_file tool.
type EditFileArgs struct {
	Path       string `json:"path" jsonschema:"Absolute path to the file to edit"`
	OldString  string `json:"old_string" jsonschema:"Text to find in the file. Exact string match by default or a regex pattern if regex is true"`
	NewString  string `json:"new_string" jsonschema:"Replacement text. For regex mode use $1 etc. to reference capture groups"`
	Regex      bool   `json:"regex,omitempty" jsonschema:"Treat old_string as a regular expression pattern (default false)"`
	ReplaceAll bool   `json:"replace_all,omitempty" jsonschema:"Replace all occurrences instead of just the first (default false)"`
}

// EditFileResult is the result of the edit_file tool.
type EditFileResult struct {
	Success             bool   `json:"success"`
	Path                string `json:"path"`
	Replacements        int    `json:"replacements"`
	Message             string `json:"message"`
	VerificationContext string `json:"verification_context,omitempty"`
}

// EditFile performs a find-and-replace operation on a file.
func EditFile(ctx tool.Context, args EditFileArgs) (EditFileResult, error) {
	if args.Path == "" {
		return EditFileResult{}, fmt.Errorf("path is required")
	}
	if args.OldString == "" {
		return EditFileResult{}, fmt.Errorf("old_string is required")
	}

	args.Path = expandPath(args.Path)

	// Must-read-before-edit guard: if the cache is active (has any read entries),
	// verify that this specific file has been read before allowing edits.
	// This prevents hallucinated edits where the LLM guesses file content.
	// The guard is lenient when no cache exists (e.g., in tests or first-time use).
	cache := LoadFileReadCache()
	if cache != nil && cache.HasAnyReadEntries() && !cache.HasReadEntry(args.Path) {
		return EditFileResult{
			Success: false,
			Path:    args.Path,
			Message: "You must read this file before editing it. Use read_file first.",
		}, nil
	}

	// Read the file
	data, err := os.ReadFile(args.Path)
	if err != nil {
		return EditFileResult{}, fmt.Errorf("failed to read file: %w", err)
	}
	content := string(data)

	var newContent string
	var replacements int

	if args.Regex {
		newContent, replacements, err = editFileRegex(content, args.OldString, args.NewString, args.ReplaceAll)
		if err != nil {
			return EditFileResult{}, err
		}
	} else {
		newContent, replacements, err = editFileExact(content, args.OldString, args.NewString, args.ReplaceAll)
		if err != nil {
			return EditFileResult{}, err
		}
	}

	// Write the modified content back
	if err := os.WriteFile(args.Path, []byte(newContent), 0644); err != nil {
		return EditFileResult{}, fmt.Errorf("failed to write file: %w", err)
	}

	// Build verification context: ±10 lines around the edit point
	verificationCtx := buildVerificationContext(newContent, args.OldString, args.NewString, args.ReplaceAll)

	// Invalidate cache entries for this path
	if cache != nil {
		cache.InvalidatePath(args.Path)
		// Update with new mtime, source="edit"
		info, statErr := os.Stat(args.Path)
		if statErr == nil {
			lines := strings.Split(newContent, "\n")
			totalLines := len(lines)
			if totalLines > 0 && lines[totalLines-1] == "" {
				totalLines--
			}
			cache.Set(buildCacheKey(args.Path, 1, 0), CacheEntry{
				MtimeNs:    info.ModTime().UnixNano(),
				TotalLines: totalLines,
				Offset:     1,
				Limit:      0,
				Source:     "edit",
				Verified:   true,
			})
			cache.Save()
		}
	}

	msg := fmt.Sprintf("Replaced %d occurrence(s) in %s", replacements, args.Path)
	return EditFileResult{
		Success:             true,
		Path:                args.Path,
		Replacements:        replacements,
		Message:             msg,
		VerificationContext: verificationCtx,
	}, nil
}

// buildVerificationContext extracts ±10 lines around the edit point with line numbers.
func buildVerificationContext(newContent, oldString, newString string, replaceAll bool) string {
	// For deletions (empty newString), find where the old content was (use surrounding context)
	searchStr := newString
	if searchStr == "" {
		// For deletions, we need to find the context around where old_string was removed.
		// Find a line that would be adjacent to the deletion point.
		// Use the content before oldString would have been to locate the edit region.
		// Approximate: search for lines adjacent to where old content existed.
		// Since old_string is gone, use a heuristic: find the first line in newContent
		// that differs from what would exist with old_string inserted back.
		// Simpler approach: just show the first 20 lines of the file as context.
		searchStr = ""
	}

	lines := strings.Split(newContent, "\n")
	totalLines := len(lines)
	if totalLines > 0 && lines[totalLines-1] == "" {
		totalLines--
		lines = lines[:totalLines]
	}

	// Find the line where the edit landed
	editLine := 0
	if searchStr != "" {
		// Find byte offset of newString in newContent
		idx := strings.Index(newContent, searchStr)
		if idx >= 0 {
			// Count newlines before this position to get line number
			editLine = strings.Count(newContent[:idx], "\n")
		}
	} else {
		// Deletion: find approximate location by looking for where old_string would have been
		// Use the byte position approach: find the first difference between old and new content
		// Since we don't have the old content here, just center on the middle of the file
		// Actually, for deletions we can search for surrounding context of old_string
		// Simple heuristic: use line 0 (show first 20 lines)
		editLine = 0
	}

	// Extract window: ±10 lines around editLine, capped at 30 total
	const contextRadius = 10
	const maxContextLines = 30
	startLine := editLine - contextRadius
	if startLine < 0 {
		startLine = 0
	}
	endLine := editLine + contextRadius + 1
	if searchStr != "" {
		// Account for multi-line replacements
		newStringLines := strings.Count(searchStr, "\n") + 1
		endLine = editLine + newStringLines + contextRadius
	}
	if endLine > totalLines {
		endLine = totalLines
	}
	if endLine-startLine > maxContextLines {
		endLine = startLine + maxContextLines
	}

	if startLine >= totalLines {
		return ""
	}

	var sb strings.Builder
	for i := startLine; i < endLine; i++ {
		if i > startLine {
			sb.WriteByte('\n')
		}
		sb.WriteString(strconv.Itoa(i + 1)) // 1-indexed
		sb.WriteString(": ")
		sb.WriteString(lines[i])
	}
	return sb.String()
}

// editFileExact performs exact string matching and replacement.
func editFileExact(content, oldString, newString string, replaceAll bool) (string, int, error) {
	count := strings.Count(content, oldString)
	if count == 0 {
		return "", 0, fmt.Errorf("old_string not found in file")
	}

	if count > 1 && !replaceAll {
		return "", 0, fmt.Errorf("found %d matches for old_string; set replace_all=true to replace all, or provide more context to match uniquely", count)
	}

	if replaceAll {
		return strings.ReplaceAll(content, oldString, newString), count, nil
	}

	// Replace first occurrence only
	result := strings.Replace(content, oldString, newString, 1)
	return result, 1, nil
}

// editFileRegex performs regex-based matching and replacement.
func editFileRegex(content, pattern, replacement string, replaceAll bool) (string, int, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", 0, fmt.Errorf("invalid regex pattern: %w", err)
	}

	matches := re.FindAllStringIndex(content, -1)
	count := len(matches)
	if count == 0 {
		return "", 0, fmt.Errorf("regex pattern matched no occurrences in file")
	}

	if count > 1 && !replaceAll {
		return "", 0, fmt.Errorf("regex matched %d occurrences; set replace_all=true to replace all, or refine the pattern", count)
	}

	if replaceAll {
		result := re.ReplaceAllString(content, replacement)
		return result, count, nil
	}

	// Replace first occurrence only
	firstMatch := re.FindStringIndex(content)
	if firstMatch == nil {
		return "", 0, fmt.Errorf("regex pattern matched no occurrences in file")
	}
	matched := content[firstMatch[0]:firstMatch[1]]
	replaced := re.ReplaceAllString(matched, replacement)
	result := content[:firstMatch[0]] + replaced + content[firstMatch[1]:]
	return result, 1, nil
}
