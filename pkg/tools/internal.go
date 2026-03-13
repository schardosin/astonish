package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/schardosin/astonish/pkg/config"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// --- Credential store file access protection ---

// protectedFileNames are filenames within the config directory that must never
// be accessed by LLM tools. These contain the encryption key and encrypted secrets.
var protectedFileNames = []string{".store_key", "credentials.enc"}

// isProtectedPath returns true if the given file path resolves to a protected
// credential store file inside the config directory.
func isProtectedPath(filePath string) bool {
	configDir, err := config.GetConfigDir()
	if err != nil {
		return false
	}

	// Resolve to absolute and follow symlinks to prevent bypass via relative paths or links
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		resolvedPath = absPath
	}

	for _, name := range protectedFileNames {
		protectedPath := filepath.Join(configDir, name)
		resolvedProtected, err := filepath.EvalSymlinks(protectedPath)
		if err != nil {
			resolvedProtected = protectedPath
		}
		if resolvedPath == resolvedProtected || absPath == protectedPath {
			return true
		}
	}
	return false
}

// expandPath resolves ~ to the user's home directory. Go's os and filepath
// packages do not expand ~ (it's a shell feature), so LLM-provided paths
// like "~/snake/main.py" would fail without this. Only the leading "~/" or
// bare "~" is expanded; ~user syntax is not supported.
func expandPath(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// commandReferencesProtectedFile checks if a shell command string references
// protected credential store files. Returns true and the matched filename
// if found. This is a best-effort check for defense-in-depth — it catches
// common patterns like cat, cp, xxd, base64, etc.
func commandReferencesProtectedFile(command string) (bool, string) {
	configDir, err := config.GetConfigDir()
	if err != nil {
		return false, ""
	}

	for _, name := range protectedFileNames {
		// Full path match
		fullPath := filepath.Join(configDir, name)
		if strings.Contains(command, fullPath) {
			return true, name
		}
	}

	// Also check for the bare filename — catches cases where the command
	// uses cd or relative paths to reach the config dir
	for _, name := range protectedFileNames {
		if strings.Contains(command, name) {
			return true, name
		}
	}

	return false, ""
}

// truncateString truncates a string to the given max length with ellipsis.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

type ReadFileArgs struct {
	Path string `json:"path" jsonschema:"The path to the file to read"`
}

type ReadFileResult struct {
	Content string `json:"content"`
}

func ReadFile(ctx tool.Context, args ReadFileArgs) (ReadFileResult, error) {
	args.Path = expandPath(args.Path)
	if isProtectedPath(args.Path) {
		return ReadFileResult{}, fmt.Errorf("access denied: this file is part of the credential store and cannot be read")
	}
	content, err := os.ReadFile(args.Path)
	if err != nil {
		return ReadFileResult{}, fmt.Errorf("failed to read file: %w", err)
	}
	return ReadFileResult{Content: string(content)}, nil
}

// --- Write File Tool ---

type WriteFileArgs struct {
	FilePath string `json:"file_path" jsonschema:"The path to the file where content will be written."`
	Content  string `json:"content" jsonschema:"The content to write to the file."`
}

type WriteFileResult struct {
	Message string `json:"message"`
}

// tryExtractStdout attempts to extract content from an 'stdout' key if the input
// is a JSON or dict-like string (e.g., from shell_command output).
func tryExtractStdout(input string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", false
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return "", false
	}

	if stdout, ok := parsed["stdout"]; ok {
		switch v := stdout.(type) {
		case string:
			return v, true
		case []interface{}:
			var lines []string
			for _, item := range v {
				lines = append(lines, fmt.Sprintf("%v", item))
			}
			return strings.Join(lines, "\n"), true
		}
	}
	return "", false
}

func WriteFile(ctx tool.Context, args WriteFileArgs) (WriteFileResult, error) {
	args.FilePath = expandPath(args.FilePath)
	if isProtectedPath(args.FilePath) {
		return WriteFileResult{}, fmt.Errorf("access denied: this file is part of the credential store and cannot be modified")
	}

	// Try to extract stdout from JSON (for shell_command output)
	var finalContent string
	if extracted, ok := tryExtractStdout(args.Content); ok {
		finalContent = extracted
	} else {
		finalContent = args.Content
	}

	// Ensure parent directories exist
	if dir := filepath.Dir(args.FilePath); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return WriteFileResult{}, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Write to file
	if err := os.WriteFile(args.FilePath, []byte(finalContent), 0644); err != nil {
		return WriteFileResult{}, fmt.Errorf("failed to write to file %s: %w", args.FilePath, err)
	}

	return WriteFileResult{Message: fmt.Sprintf("Content successfully written to %s", args.FilePath)}, nil
}

type ShellCommandArgs struct {
	Command    string `json:"command" jsonschema:"The shell command to execute"`
	Timeout    int    `json:"timeout,omitempty" jsonschema:"Timeout in seconds. Default 120. Max 3600."`
	WorkingDir string `json:"working_dir,omitempty" jsonschema:"Working directory for the command. Defaults to the current directory."`
	Background bool   `json:"background,omitempty" jsonschema:"If true, start the command in the background and return a session_id immediately. Use process_read/process_write/process_kill to interact with it."`
}

type ShellCommandResult struct {
	Stdout          string `json:"stdout"`
	TimedOut        bool   `json:"timed_out,omitempty"`
	WaitingForInput bool   `json:"waiting_for_input,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
	ExitCode        *int   `json:"exit_code,omitempty"`
}

func ShellCommand(ctx tool.Context, args ShellCommandArgs) (ShellCommandResult, error) {
	// Block commands that reference credential store files
	if matched, fileName := commandReferencesProtectedFile(args.Command); matched {
		return ShellCommandResult{}, fmt.Errorf("access denied: command references credential store file '%s' which cannot be accessed", fileName)
	}

	// Apply timeout defaults and bounds
	timeout := args.Timeout
	if timeout <= 0 {
		timeout = 120
	}
	if timeout > 3600 {
		timeout = 3600
	}

	pm := GetProcessManager()

	// Start process with PTY
	sess, err := pm.Start(args.Command, args.WorkingDir, 24, 80)
	if err != nil {
		return ShellCommandResult{}, fmt.Errorf("failed to start command: %w", err)
	}

	// Background mode: return immediately with session ID
	if args.Background {
		// Wait briefly for initial output
		time.Sleep(300 * time.Millisecond)
		data := sess.Output.Bytes()
		return ShellCommandResult{
			Stdout:    string(data),
			SessionID: sess.ID,
		}, nil
	}

	// One-shot mode: wait for completion or interactive detection
	deadline := time.After(time.Duration(timeout) * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-sess.done:
			// Process exited — return output
			data := sess.Output.Bytes()
			result := ShellCommandResult{
				Stdout:   string(data),
				ExitCode: sess.ExitCode,
			}
			return result, nil

		case <-deadline:
			// Timeout — kill process and return what we have
			_ = pm.Kill(sess.ID)
			data := sess.Output.Bytes()
			return ShellCommandResult{
				Stdout:    string(data),
				TimedOut:  true,
				SessionID: sess.ID,
			}, nil

		case <-ticker.C:
			// Check if process is waiting for input (alive but idle)
			if sess.IsRunning() && sess.IdleDuration() >= idleThreshold {
				data := sess.Output.Bytes()
				if len(data) > 0 && looksLikePrompt(string(data)) {
					return ShellCommandResult{
						Stdout:          string(data),
						WaitingForInput: true,
						SessionID:       sess.ID,
					}, nil
				}
			}
		}
	}
}

// looksLikePrompt checks if the output ends with something that looks like
// an interactive prompt waiting for user input.
func looksLikePrompt(output string) bool {
	trimmed := strings.TrimRight(output, " \t")
	if len(trimmed) == 0 {
		return false
	}

	// Check common prompt endings (check both with and without trailing whitespace)
	promptSuffixes := []string{
		"?", ":", ">", "$", "#", "%",
		"(yes/no)", "(y/n)", "(Y/n)", "(y/N)",
		"[yes/no]", "[y/n]", "[Y/n]", "[y/N]",
		"(yes/no/[fingerprint])",
		"password:", "Password:", "PASSWORD:",
		"passphrase:", "Passphrase:",
	}

	lower := strings.ToLower(trimmed)
	for _, suffix := range promptSuffixes {
		if strings.HasSuffix(trimmed, suffix) || strings.HasSuffix(lower, strings.ToLower(suffix)) {
			return true
		}
	}

	// Check if last line is short (< 200 chars) and ends with a prompt character
	lines := strings.Split(trimmed, "\n")
	lastLine := strings.TrimSpace(lines[len(lines)-1])
	if len(lastLine) > 0 && len(lastLine) < 200 {
		lastChar := lastLine[len(lastLine)-1]
		if lastChar == '?' || lastChar == ':' || lastChar == '>' || lastChar == '$' || lastChar == '#' || lastChar == '%' {
			return true
		}
	}

	return false
}

// --- Filter JSON Tool ---

type FilterJsonArgs struct {
	JsonData        string   `json:"json_data" jsonschema:"The JSON data to filter, as a JSON string."`
	FieldsToExtract []string `json:"fields_to_extract" jsonschema:"A list of fields to extract. Use dot notation for nested fields."`
}

type FilterJsonResult struct {
	Result interface{} `json:"result"`
}

func getNestedValue(data interface{}, path []string) interface{} {
	current := data
	for _, key := range path {
		if m, ok := current.(map[string]interface{}); ok {
			if val, exists := m[key]; exists {
				current = val
			} else {
				return nil
			}
		} else if l, ok := current.([]interface{}); ok {
			// Try to parse key as index
			if idx, err := strconv.Atoi(key); err == nil {
				if idx >= 0 && idx < len(l) {
					current = l[idx]
				} else {
					return nil
				}
			} else {
				return nil
			}
		} else {
			return nil
		}
	}
	return current
}

func setNestedValue(data map[string]interface{}, path []string, value interface{}) {
	current := data
	for i := 0; i < len(path)-1; i++ {
		key := path[i]
		if _, exists := current[key]; !exists {
			current[key] = make(map[string]interface{})
		}
		if nextMap, ok := current[key].(map[string]interface{}); ok {
			current = nextMap
		} else {
			// Conflict: trying to treat a non-map as a map. Overwrite or abort?
			// Python implementation uses setdefault, which implies it expects a dict.
			// If it's not a dict, we can't proceed down this path.
			// For simplicity, we'll overwrite if it's not a map, matching Python's likely behavior of "last write wins" or structure enforcement.
			newMap := make(map[string]interface{})
			current[key] = newMap
			current = newMap
		}
	}
	current[path[len(path)-1]] = value
}

func filterItem(item interface{}, fields []string) interface{} {
	if m, ok := item.(map[string]interface{}); ok {
		result := make(map[string]interface{})
		for _, field := range fields {
			path := strings.Split(field, ".")
			value := getNestedValue(m, path)
			if value != nil {
				setNestedValue(result, path, value)
			}
		}
		return result
	} else if l, ok := item.([]interface{}); ok {
		resultList := make([]interface{}, 0, len(l))
		for _, subItem := range l {
			if _, isDict := subItem.(map[string]interface{}); isDict {
				resultList = append(resultList, filterItem(subItem, fields))
			}
		}
		return resultList
	}
	return item
}

func FilterJson(ctx tool.Context, args FilterJsonArgs) (FilterJsonResult, error) {
	var data interface{}

	// 1. Parse the JSON string
	var parsed interface{}
	if err := json.Unmarshal([]byte(args.JsonData), &parsed); err != nil {
		return FilterJsonResult{Result: fmt.Sprintf("Error: Invalid JSON input - %v", err)}, nil
	}

	// Check for 'stdout' wrapping
	if m, ok := parsed.(map[string]interface{}); ok {
		if stdout, exists := m["stdout"]; exists {
			// If stdout is a string, try to parse IT as JSON
			if stdoutStr, ok := stdout.(string); ok {
				var innerParsed interface{}
				if err := json.Unmarshal([]byte(stdoutStr), &innerParsed); err == nil {
					data = innerParsed
				} else {
					data = stdoutStr
				}
			} else {
				data = stdout
			}
		} else {
			data = parsed
		}
	} else {
		data = parsed
	}

	// 2. Validate data type
	if _, isMap := data.(map[string]interface{}); !isMap {
		if _, isList := data.([]interface{}); !isList {
			return FilterJsonResult{Result: "Error: Parsed data must be a JSON object or a list of JSON objects."}, nil
		}
	}

	// 3. Filter
	var result interface{}
	if l, ok := data.([]interface{}); ok {
		// Filter list of dicts
		filteredList := make([]interface{}, 0)
		for _, item := range l {
			if _, isDict := item.(map[string]interface{}); isDict {
				filteredList = append(filteredList, filterItem(item, args.FieldsToExtract))
			}
		}
		result = filteredList
	} else {
		// Filter single dict
		result = filterItem(data, args.FieldsToExtract)
	}

	return FilterJsonResult{Result: result}, nil
}

// --- Get Pull Request Files Tool ---

func GetInternalTools() ([]tool.Tool, error) {
	readFileTool, err := functiontool.New(functiontool.Config{
		Name:        "read_file",
		Description: "Read the contents of a file",
	}, ReadFile)
	if err != nil {
		return nil, err
	}

	writeFileTool, err := functiontool.New(functiontool.Config{
		Name:        "write_file",
		Description: "Write content to a file with intelligent 'stdout' extraction. If the content is the output from shell_command (containing 'stdout' key), it will extract and write only the stdout value.",
	}, WriteFile)
	if err != nil {
		return nil, err
	}

	shellCommandTool, err := functiontool.New(functiontool.Config{
		Name: "shell_command",
		Description: `Execute a shell command with full PTY support.

One-shot mode (default): Runs the command and waits for it to complete. If the command is waiting for interactive input (SSH prompts, password, confirmations), it returns early with waiting_for_input=true and a session_id. Use process_write to respond to the prompt, and process_read to check output.

Background mode (background=true): Starts the command and returns immediately with a session_id. Use process_read, process_write, and process_kill to manage the process.`,
	}, ShellCommand)
	if err != nil {
		return nil, err
	}

	filterJsonTool, err := functiontool.New(functiontool.Config{
		Name:        "filter_json",
		Description: "Filters JSON data (either a single object or a list of objects) to include only a specified set of fields, supporting nested fields via dot notation.",
	}, FilterJson)
	if err != nil {
		return nil, err
	}

	gitDiffAddLineNumbersTool, err := functiontool.New(functiontool.Config{
		Name:        "git_diff_add_line_numbers",
		Description: "Parses a PR diff string or a patch snippet and adds line numbers to each line of change, returning the formatted result as a single string.",
	}, GitDiffAddLineNumbers)
	if err != nil {
		return nil, err
	}

	// Search tools
	fileTreeTool, err := functiontool.New(functiontool.Config{
		Name:        "file_tree",
		Description: "Get a structured view of the directory tree. Use this to understand project structure. Returns JSON with paths, names, and file sizes.",
	}, FileTree)
	if err != nil {
		return nil, err
	}

	grepSearchTool, err := functiontool.New(functiontool.Config{
		Name:        "grep_search",
		Description: "Search for text patterns in files. Returns file paths, line numbers, and matching content. Uses ripgrep when available for speed.",
	}, GrepSearch)
	if err != nil {
		return nil, err
	}

	findFilesTool, err := functiontool.New(functiontool.Config{
		Name:        "find_files",
		Description: "Find files by name pattern using glob matching (e.g., '*.go', 'test_*.py'). Returns matching file paths with sizes.",
	}, FindFiles)
	if err != nil {
		return nil, err
	}

	editFileTool, err := functiontool.New(functiontool.Config{
		Name:        "edit_file",
		Description: "Edit a file by finding and replacing text. Supports exact string matching or regex patterns. Returns an error if old_string matches multiple locations without replace_all=true, so you can refine the match.",
	}, EditFile)
	if err != nil {
		return nil, err
	}

	webFetchTool, err := functiontool.New(functiontool.Config{
		Name:        "web_fetch",
		Description: "Fetch a URL and extract its content as clean markdown, readable text, or raw HTML. PREFERRED tool for fetching specific URLs — always try this first before other web extraction tools. Fast, free, no API key required. Uses Mozilla Readability for article-focused extraction. If this tool returns empty or navigation-only content (common with JavaScript-heavy pages), then retry with the configured MCP web extract tool.",
	}, WebFetch)
	if err != nil {
		return nil, err
	}

	readPDFTool, err := functiontool.New(functiontool.Config{
		Name:        "read_pdf",
		Description: "Extract text content from a PDF file. Accepts a local file path or an HTTP/HTTPS URL. Returns plain text with page markers. Use this for reading PDF documents, reports, papers, etc.",
	}, ReadPDF)
	if err != nil {
		return nil, err
	}

	httpRequestTool, err := functiontool.New(functiontool.Config{
		Name: "http_request",
		Description: `Make an HTTP request to an API endpoint. Supports GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS.

Use this instead of curl via shell_command for cleaner API calls. Set 'credential' to a stored credential name for automatic authentication header injection (supports API key, bearer, basic, OAuth). JSON Content-Type is set automatically when the body starts with { or [.`,
	}, HttpRequest)
	if err != nil {
		return nil, err
	}

	return []tool.Tool{
		readFileTool, writeFileTool, shellCommandTool, filterJsonTool, gitDiffAddLineNumbersTool,
		fileTreeTool, grepSearchTool, findFilesTool, editFileTool, webFetchTool, readPDFTool,
		httpRequestTool,
	}, nil
}

func ExecuteTool(ctx context.Context, name string, args map[string]interface{}) (any, error) {
	// Helper to marshal map to struct
	toStruct := func(input map[string]interface{}, target interface{}) error {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, target)
	}

	switch name {
	case "read_file":
		var toolArgs ReadFileArgs
		if err := toStruct(args, &toolArgs); err != nil {
			return nil, fmt.Errorf("invalid args for read_file: %w", err)
		}
		// We need a tool.Context. For now, we can pass a dummy or the real one if available.
		// But ReadFile expects tool.Context.
		// We can cast the passed context if it implements it, or create a wrapper.
		// Since we are calling from handleToolNode, we have a context.Context.
		// But ReadFile needs tool.Context.
		// Let's assume for now we can pass nil or a basic wrapper if the tool doesn't use it heavily.
		// ReadFile doesn't use ctx.
		// ShellCommand doesn't use ctx.
		return ReadFile(nil, toolArgs)

	case "shell_command":
		var toolArgs ShellCommandArgs
		if err := toStruct(args, &toolArgs); err != nil {
			return nil, fmt.Errorf("invalid args for shell_command: %w", err)
		}
		return ShellCommand(nil, toolArgs)

	case "filter_json":
		var toolArgs FilterJsonArgs
		if err := toStruct(args, &toolArgs); err != nil {
			return nil, fmt.Errorf("invalid args for filter_json: %w", err)
		}
		return FilterJson(nil, toolArgs)

	case "git_diff_add_line_numbers":
		var toolArgs GitDiffAddLineNumbersArgs
		if err := toStruct(args, &toolArgs); err != nil {
			return nil, fmt.Errorf("invalid args for git_diff_add_line_numbers: %w", err)
		}
		return GitDiffAddLineNumbers(nil, toolArgs)

	case "file_tree":
		var toolArgs FileTreeArgs
		if err := toStruct(args, &toolArgs); err != nil {
			return nil, fmt.Errorf("invalid args for file_tree: %w", err)
		}
		return FileTree(nil, toolArgs)

	case "grep_search":
		var toolArgs GrepSearchArgs
		if err := toStruct(args, &toolArgs); err != nil {
			return nil, fmt.Errorf("invalid args for grep_search: %w", err)
		}
		return GrepSearch(nil, toolArgs)

	case "find_files":
		var toolArgs FindFilesArgs
		if err := toStruct(args, &toolArgs); err != nil {
			return nil, fmt.Errorf("invalid args for find_files: %w", err)
		}
		return FindFiles(nil, toolArgs)

	case "edit_file":
		var toolArgs EditFileArgs
		if err := toStruct(args, &toolArgs); err != nil {
			return nil, fmt.Errorf("invalid args for edit_file: %w", err)
		}
		return EditFile(nil, toolArgs)

	case "web_fetch":
		var toolArgs WebFetchArgs
		if err := toStruct(args, &toolArgs); err != nil {
			return nil, fmt.Errorf("invalid args for web_fetch: %w", err)
		}
		return WebFetch(nil, toolArgs)

	case "read_pdf":
		var toolArgs ReadPDFArgs
		if err := toStruct(args, &toolArgs); err != nil {
			return nil, fmt.Errorf("invalid args for read_pdf: %w", err)
		}
		return ReadPDF(nil, toolArgs)

	case "process_read":
		var toolArgs ProcessReadArgs
		if err := toStruct(args, &toolArgs); err != nil {
			return nil, fmt.Errorf("invalid args for process_read: %w", err)
		}
		return processRead(nil, toolArgs)

	case "process_write":
		var toolArgs ProcessWriteArgs
		if err := toStruct(args, &toolArgs); err != nil {
			return nil, fmt.Errorf("invalid args for process_write: %w", err)
		}
		return processWrite(nil, toolArgs)

	case "process_list":
		var toolArgs ProcessListArgs
		if err := toStruct(args, &toolArgs); err != nil {
			return nil, fmt.Errorf("invalid args for process_list: %w", err)
		}
		return processList(nil, toolArgs)

	case "process_kill":
		var toolArgs ProcessKillArgs
		if err := toStruct(args, &toolArgs); err != nil {
			return nil, fmt.Errorf("invalid args for process_kill: %w", err)
		}
		return processKill(nil, toolArgs)

	case "http_request":
		var toolArgs HttpRequestArgs
		if err := toStruct(args, &toolArgs); err != nil {
			return nil, fmt.Errorf("invalid args for http_request: %w", err)
		}
		return HttpRequest(nil, toolArgs)

	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}
