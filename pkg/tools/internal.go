package tools

import (
	"fmt"
	"os"

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

func GetInternalTools() ([]tool.Tool, error) {
	t, err := functiontool.New(functiontool.Config{
		Name:        "read_file",
		Description: "Read the contents of a file",
	}, ReadFile)
	if err != nil {
		return nil, err
	}
	return []tool.Tool{t}, nil
}
