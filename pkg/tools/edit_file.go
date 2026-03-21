package tools

import (
	"fmt"
	"os"
	"regexp"
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
	Success      bool   `json:"success"`
	Path         string `json:"path"`
	Replacements int    `json:"replacements"`
	Message      string `json:"message"`
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

	msg := fmt.Sprintf("Replaced %d occurrence(s) in %s", replacements, args.Path)
	return EditFileResult{
		Success:      true,
		Path:         args.Path,
		Replacements: replacements,
		Message:      msg,
	}, nil
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
