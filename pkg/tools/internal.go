package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

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

func GetInternalTools() ([]tool.Tool, error) {
	readFileTool, err := functiontool.New(functiontool.Config{
		Name:        "read_file_content",
		Description: "Read the contents of a file",
	}, ReadFile)
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

	return []tool.Tool{readFileTool, shellCommandTool}, nil
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

	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}
