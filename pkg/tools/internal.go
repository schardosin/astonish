package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

type ReadFileArgs struct {
	Path string `json:"path" jsonschema_description:"path to the file to read"`
}

type ReadFileResult struct {
	Content string `json:"content"`
}

func ReadFile(ctx tool.Context, args ReadFileArgs) (ReadFileResult, error) {
	content, err := os.ReadFile(args.Path)
	if err != nil {
		return ReadFileResult{}, fmt.Errorf("failed to read file: %w", err)
	}
	return ReadFileResult{Content: string(content)}, nil
}

// --- Write File Tool ---

type WriteFileArgs struct {
	FilePath string      `json:"file_path" jsonschema_description:"The path to the file where content will be written."`
	Content  interface{} `json:"content" jsonschema_description:"The content to write to the file. Can be a single string or a list of strings (each list item will be a new line)."`
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

// contentToString converts the content (string or []interface{}) to a single string
func contentToString(content interface{}) (string, error) {
	switch v := content.(type) {
	case string:
		return v, nil
	case []interface{}:
		var lines []string
		for _, item := range v {
			lines = append(lines, fmt.Sprintf("%v", item))
		}
		return strings.Join(lines, "\n"), nil
	case []string:
		return strings.Join(v, "\n"), nil
	default:
		return fmt.Sprintf("%v", content), nil
	}
}

func WriteFile(ctx tool.Context, args WriteFileArgs) (WriteFileResult, error) {
	// Convert content to string
	preliminaryString, err := contentToString(args.Content)
	if err != nil {
		return WriteFileResult{}, fmt.Errorf("failed to process content: %w", err)
	}

	// Try to extract stdout from JSON (for shell_command output)
	var finalContent string
	if extracted, ok := tryExtractStdout(preliminaryString); ok {
		finalContent = extracted
	} else {
		finalContent = preliminaryString
	}

	// Write to file
	if err := os.WriteFile(args.FilePath, []byte(finalContent), 0644); err != nil {
		return WriteFileResult{}, fmt.Errorf("failed to write to file %s: %w", args.FilePath, err)
	}

	return WriteFileResult{Message: fmt.Sprintf("Content successfully written to %s", args.FilePath)}, nil
}


type ShellCommandArgs struct {
	Command string `json:"command" jsonschema_description:"shell command to execute"`
}

type ShellCommandResult struct {
	Stdout string `json:"stdout"`
}

func ShellCommand(ctx tool.Context, args ShellCommandArgs) (ShellCommandResult, error) {
	fmt.Printf("Executing shell command: %s\n", args.Command)
	// Use sh -c to execute the command
	cmd := exec.Command("sh", "-c", args.Command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return ShellCommandResult{}, fmt.Errorf("failed to execute command: %w, output: %s", err, string(output))
	}
	return ShellCommandResult{Stdout: string(output)}, nil
}

// --- Filter JSON Tool ---

type FilterJsonArgs struct {
	JsonData        interface{} `json:"json_data" jsonschema_description:"The JSON data to filter. Can be a JSON string, a list of dicts, or a dict."`
	FieldsToExtract []string    `json:"fields_to_extract" jsonschema_description:"A list of fields to extract. Use dot notation for nested fields."`
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

	// 1. Handle input parsing
	switch v := args.JsonData.(type) {
	case string:
		// Try to parse string as JSON
		var parsed interface{}
		if err := json.Unmarshal([]byte(v), &parsed); err != nil {
			// If not valid JSON, maybe it's a Python literal? Go doesn't support that natively easily.
			// But let's assume valid JSON for now as per Python's main path.
			// If unmarshal fails, return error or treat as raw string?
			// Python returns error string.
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
	default:
		data = v
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

// --- Run Python Code Tool ---

type RunPythonCodeArgs struct {
	Code string `json:"code" jsonschema_description:"Python code to execute"`
}

type RunPythonCodeResult struct {
	Output string `json:"output"`
}

func RunPythonCode(ctx tool.Context, args RunPythonCodeArgs) (RunPythonCodeResult, error) {
	fmt.Printf("Executing Python code: %s\n", args.Code)
	// Use python3 -c to execute the code
	cmd := exec.Command("python3", "-c", args.Code)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return RunPythonCodeResult{}, fmt.Errorf("failed to execute Python code: %w, output: %s", err, string(output))
	}
	return RunPythonCodeResult{Output: string(output)}, nil
}

// --- Get Pull Request Files Tool ---



func GetInternalTools() ([]tool.Tool, error) {
	readFileTool, err := functiontool.New(functiontool.Config{
		Name:        "read_file_content",
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
		Name:        "shell_command",
		Description: "Execute a shell command to retrieve information or perform actions. Use this tool when you need to run CLI commands like 'gh', 'git', etc.",
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

	runPythonCodeTool, err := functiontool.New(functiontool.Config{
		Name:        "execute_python",
		Description: "Execute Python code and return the output. Use this for calculations, data processing, or any Python operations.",
	}, RunPythonCode)
	if err != nil {
		return nil, err
	}

	return []tool.Tool{readFileTool, writeFileTool, shellCommandTool, filterJsonTool, gitDiffAddLineNumbersTool, runPythonCodeTool}, nil
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
	case "read_file_content":
		var toolArgs ReadFileArgs
		if err := toStruct(args, &toolArgs); err != nil {
			return nil, fmt.Errorf("invalid args for read_file_content: %w", err)
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

	case "execute_python":
		var toolArgs RunPythonCodeArgs
		if err := toStruct(args, &toolArgs); err != nil {
			return nil, fmt.Errorf("invalid args for execute_python: %w", err)
		}
		return RunPythonCode(nil, toolArgs)

	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}
