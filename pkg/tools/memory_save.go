package tools

import (
	"fmt"

	"github.com/schardosin/astonish/pkg/memory"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// MemorySaveArgs defines the arguments for the memory_save tool.
type MemorySaveArgs struct {
	Category string `json:"category" jsonschema:"A short category heading for organizing the memory (e.g., Infrastructure, Preferences, Projects, Credentials)"`
	Content  string `json:"content" jsonschema:"The facts to save, one per line. Use '- ' prefix for bullet points."`
}

// MemorySaveResult is returned after saving to memory.
type MemorySaveResult struct {
	Saved   bool   `json:"saved"`
	Message string `json:"message"`
}

// MemorySave saves facts to persistent memory. This function is used
// as the handler for the memory_save tool.
func MemorySave(mgr *memory.Manager) func(ctx tool.Context, args MemorySaveArgs) (MemorySaveResult, error) {
	return func(ctx tool.Context, args MemorySaveArgs) (MemorySaveResult, error) {
		if args.Category == "" {
			return MemorySaveResult{}, fmt.Errorf("category is required")
		}
		if args.Content == "" {
			return MemorySaveResult{}, fmt.Errorf("content is required")
		}

		if err := mgr.Append(args.Category, args.Content); err != nil {
			return MemorySaveResult{}, fmt.Errorf("failed to save to memory: %w", err)
		}

		return MemorySaveResult{
			Saved:   true,
			Message: fmt.Sprintf("Saved to memory under '%s'", args.Category),
		}, nil
	}
}

// NewMemorySaveTool creates the memory_save tool using the given memory manager.
func NewMemorySaveTool(mgr *memory.Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "memory_save",
		Description: "Save durable facts to persistent memory for future recall. " +
			"Use when you discover: connection details (IPs, hostnames, users, auth methods, ports), " +
			"server roles, network topology, or user preferences. " +
			"Do NOT save: lists of VMs/containers/pods, their running status, resource usage, " +
			"command outputs, or any data that changes over time -- those must be fetched live.",
	}, MemorySave(mgr))
}
