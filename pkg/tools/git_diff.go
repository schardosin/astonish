package tools

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"google.golang.org/adk/tool"
)

// GitDiffAddLineNumbersArgs defines arguments for git_diff_add_line_numbers
type GitDiffAddLineNumbersArgs struct {
	DiffContent string `json:"diff_content" jsonschema:"The diff or patch content to add line numbers to"`
}

// GitDiffAddLineNumbersResult defines the result for git_diff_add_line_numbers
type GitDiffAddLineNumbersResult struct {
	FormattedDiff string `json:"formatted_diff"`
}

// GitDiffAddLineNumbers parses a PR diff string or a patch snippet and adds line numbers
func GitDiffAddLineNumbers(ctx tool.Context, args GitDiffAddLineNumbersArgs) (GitDiffAddLineNumbersResult, error) {
	processedDiff := strings.TrimSpace(args.DiffContent)
	isPartialPatch := false

	// Check for partial patch
	if strings.HasPrefix(processedDiff, "@@") && !strings.Contains(processedDiff, "--- a/") {
		isPartialPatch = true
		processedDiff = "--- a/file.patch\n+++ b/file.patch\n" + processedDiff
	}

	// Parse the diff
	files, err := parseUnifiedDiff(processedDiff)
	if err != nil {
		return GitDiffAddLineNumbersResult{}, fmt.Errorf("critical error while parsing the PR diff: %w", err)
	}

	if len(files) == 0 {
		return GitDiffAddLineNumbersResult{FormattedDiff: "No file changes were found in the provided diff."}, nil
	}

	var formattedDiffParts []string

	for _, file := range files {
		if file.IsBinary {
			source := file.SourceFile
			if source == "" {
				source = "source_binary"
			}
			target := file.TargetFile
			if target == "" {
				target = "target_binary"
			}
			formattedDiffParts = append(formattedDiffParts, fmt.Sprintf("--- a/%s\n+++ b/%s\nBinary files differ\n", source, target))
			continue
		}

		if !isPartialPatch {
			formattedDiffParts = append(formattedDiffParts, file.Header)
		}

		for _, hunk := range file.Hunks {
			// Reconstruct hunk header
			header := fmt.Sprintf("@@ -%d,%d +%d,%d @@ %s",
				hunk.SourceStart, hunk.SourceLength,
				hunk.TargetStart, hunk.TargetLength,
				hunk.SectionHeader)
			header = strings.TrimRight(header, " ")
			formattedDiffParts = append(formattedDiffParts, header)

			for _, line := range hunk.Lines {
				oldLn := ""
				if line.SourceLineNo > 0 {
					oldLn = fmt.Sprintf("%4d", line.SourceLineNo)
				} else {
					oldLn = "    "
				}

				newLn := ""
				if line.TargetLineNo > 0 {
					newLn = fmt.Sprintf("%4d", line.TargetLineNo)
				} else {
					newLn = "    "
				}

				content := strings.TrimRight(line.Content, "\r\n")
				formattedDiffParts = append(formattedDiffParts, fmt.Sprintf("%s%s %s %s", line.Type, oldLn, newLn, content))
			}
		}
	}

	return GitDiffAddLineNumbersResult{FormattedDiff: strings.Join(formattedDiffParts, "\n")}, nil
}

// --- Diff Parser Implementation ---

type DiffFile struct {
	Header     string
	SourceFile string
	TargetFile string
	IsBinary   bool
	Hunks      []DiffHunk
}

type DiffHunk struct {
	SourceStart   int
	SourceLength  int
	TargetStart   int
	TargetLength  int
	SectionHeader string
	Lines         []DiffLine
}

type DiffLine struct {
	Type         string // " ", "+", "-"
	SourceLineNo int
	TargetLineNo int
	Content      string
}

func parseUnifiedDiff(diff string) ([]DiffFile, error) {
	lines := strings.Split(diff, "\n")
	var files []DiffFile
	var currentFile *DiffFile
	var currentHunk *DiffHunk

	sourceLineNo := 0
	targetLineNo := 0

	fileHeaderRegex := regexp.MustCompile(`^diff --git a/(.*) b/(.*)`)
	sourceFileRegex := regexp.MustCompile(`^--- a/(.*)`)
	targetFileRegex := regexp.MustCompile(`^\+\+\+ b/(.*)`)
	binaryFileRegex := regexp.MustCompile(`^Binary files (?:a/)?(.*) and (?:b/)?(.*) differ`)
	hunkHeaderRegex := regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(.*)`)

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		// Check for file header start (git diff)
		if strings.HasPrefix(line, "diff --git") {
			if currentFile != nil {
				if currentHunk != nil {
					currentFile.Hunks = append(currentFile.Hunks, *currentHunk)
					currentHunk = nil
				}
				files = append(files, *currentFile)
			}
			matches := fileHeaderRegex.FindStringSubmatch(line)
			source := ""
			target := ""
			if len(matches) == 3 {
				source = matches[1]
				target = matches[2]
			}
			currentFile = &DiffFile{
				Header:     line, // We might append more lines to header
				SourceFile: source,
				TargetFile: target,
			}
			continue
		}

		// Handle binary files
		if strings.HasPrefix(line, "Binary files") {
			if currentFile == nil {
				// Should not happen in standard git diff, but maybe in partial?
				currentFile = &DiffFile{Header: line, IsBinary: true}
			}
			currentFile.IsBinary = true
			matches := binaryFileRegex.FindStringSubmatch(line)
			if len(matches) == 3 {
				currentFile.SourceFile = matches[1]
				currentFile.TargetFile = matches[2]
			}
			continue
		}

		// Handle file metadata (index, mode, etc) - append to header
		if strings.HasPrefix(line, "index") || strings.HasPrefix(line, "new file") || strings.HasPrefix(line, "deleted file") || strings.HasPrefix(line, "similarity") || strings.HasPrefix(line, "rename") || strings.HasPrefix(line, "old mode") || strings.HasPrefix(line, "new mode") {
			if currentFile != nil {
				currentFile.Header += "\n" + line
			}
			continue
		}

		// Handle --- a/
		if strings.HasPrefix(line, "--- ") {
			if currentFile == nil {
				// Start of a patch without git header
				currentFile = &DiffFile{Header: line}
			} else {
				currentFile.Header += "\n" + line
			}
			matches := sourceFileRegex.FindStringSubmatch(line)
			if len(matches) == 2 {
				currentFile.SourceFile = matches[1]
			}
			continue
		}

		// Handle +++ b/
		if strings.HasPrefix(line, "+++ ") {
			if currentFile != nil {
				currentFile.Header += "\n" + line
			}
			matches := targetFileRegex.FindStringSubmatch(line)
			if len(matches) == 2 {
				currentFile.TargetFile = matches[1]
			}
			continue
		}

		// Handle Hunk Header
		if strings.HasPrefix(line, "@@ ") {
			if currentFile == nil {
				// Should not happen
				continue
			}
			if currentHunk != nil {
				currentFile.Hunks = append(currentFile.Hunks, *currentHunk)
			}

			matches := hunkHeaderRegex.FindStringSubmatch(line)
			if len(matches) >= 4 {
				sStart, _ := strconv.Atoi(matches[1])
				sLen := 1
				if matches[2] != "" {
					sLen, _ = strconv.Atoi(matches[2])
				}
				tStart, _ := strconv.Atoi(matches[3])
				tLen := 1
				if matches[4] != "" {
					tLen, _ = strconv.Atoi(matches[4])
				}
				section := ""
				if len(matches) >= 6 {
					section = matches[5]
				}

				currentHunk = &DiffHunk{
					SourceStart:   sStart,
					SourceLength:  sLen,
					TargetStart:   tStart,
					TargetLength:  tLen,
					SectionHeader: section,
				}
				sourceLineNo = sStart
				targetLineNo = tStart
			}
			continue
		}

		// Handle Content Lines
		if currentHunk != nil {
			if len(line) > 0 {
				typeChar := string(line[0])
				content := line[1:]

				diffLine := DiffLine{
					Type:    typeChar,
					Content: content,
				}

				if typeChar == " " {
					diffLine.SourceLineNo = sourceLineNo
					diffLine.TargetLineNo = targetLineNo
					sourceLineNo++
					targetLineNo++
				} else if typeChar == "-" {
					diffLine.SourceLineNo = sourceLineNo
					sourceLineNo++
				} else if typeChar == "+" {
					diffLine.TargetLineNo = targetLineNo
					targetLineNo++
				} else if typeChar == "\\" {
					// " No newline at end of file"
					diffLine.Type = "\\"
					diffLine.Content = line[1:]
				}

				currentHunk.Lines = append(currentHunk.Lines, diffLine)
			} else {
				// Empty line, treat as context?
				// Usually diff lines have at least one char (space)
			}
		}
	}

	if currentFile != nil {
		if currentHunk != nil {
			currentFile.Hunks = append(currentFile.Hunks, *currentHunk)
		}
		files = append(files, *currentFile)
	}

	return files, nil
}
