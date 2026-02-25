package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/schardosin/astonish/pkg/memory"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// MemorySaveArgs defines the arguments for the memory_save tool.
type MemorySaveArgs struct {
	Category  string `json:"category" jsonschema:"A short category heading for organizing the memory (e.g., Infrastructure, Preferences, Projects, Credentials)"`
	Content   string `json:"content" jsonschema:"The facts to save, one per line. Use '- ' prefix for bullet points."`
	File      string `json:"file,omitempty" jsonschema:"Target file relative to memory dir. Default MEMORY.md (core identity). Use paths like projects/astonish.md for detailed knowledge."`
	Overwrite bool   `json:"overwrite,omitempty" jsonschema:"When true, replaces the entire section instead of appending. Use this to correct outdated facts — provide the complete corrected section content."`
}

// MemorySaveResult is returned after saving to memory.
type MemorySaveResult struct {
	Saved   bool   `json:"saved"`
	Message string `json:"message"`
}

// MemorySaveStore is an optional interface for triggering reindexing after save.
type MemorySaveStore interface {
	ReindexFile(ctx context.Context, relPath string) error
	Config() *memory.StoreConfig
}

// MemorySave saves facts to persistent memory. This function is used
// as the handler for the memory_save tool.
// If store is provided, it triggers reindexing after writing knowledge tier files.
func MemorySave(mgr *memory.Manager, store MemorySaveStore) func(ctx tool.Context, args MemorySaveArgs) (MemorySaveResult, error) {
	return func(ctx tool.Context, args MemorySaveArgs) (MemorySaveResult, error) {
		if args.Category == "" {
			return MemorySaveResult{}, fmt.Errorf("category is required")
		}
		if args.Content == "" {
			return MemorySaveResult{}, fmt.Errorf("content is required")
		}

		targetFile := strings.TrimSpace(args.File)

		// If no file specified or file is MEMORY.md, use the core tier (existing behavior)
		if targetFile == "" || strings.EqualFold(targetFile, "MEMORY.md") {
			if err := mgr.Append(args.Category, args.Content, args.Overwrite); err != nil {
				return MemorySaveResult{}, fmt.Errorf("failed to save to memory: %w", err)
			}

			// Trigger reindex of MEMORY.md if store is available
			if store != nil {
				go func() {
					_ = store.ReindexFile(context.Background(), "MEMORY.md")
				}()
			}

			return MemorySaveResult{
				Saved:   true,
				Message: fmt.Sprintf("Saved to MEMORY.md under '%s'", args.Category),
			}, nil
		}

		// Knowledge tier: write to memory/<file>
		// Validate path (prevent traversal)
		clean := filepath.Clean(targetFile)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return MemorySaveResult{}, fmt.Errorf("invalid file path: must be relative and within memory directory")
		}

		// Resolve the absolute path using the memory directory
		var memDir string
		if store != nil {
			memDir = store.Config().MemoryDir
		}
		if memDir == "" {
			memDir = filepath.Dir(mgr.Path)
		}

		absPath := filepath.Join(memDir, clean)

		// Create directories as needed
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			return MemorySaveResult{}, fmt.Errorf("failed to create directory: %w", err)
		}

		// Read existing content
		existing, _ := os.ReadFile(absPath)
		existingStr := string(existing)

		// Append content under category heading
		var sb strings.Builder
		if existingStr != "" {
			sb.WriteString(existingStr)
			if !strings.HasSuffix(existingStr, "\n") {
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}
		sb.WriteString("## ")
		sb.WriteString(args.Category)
		sb.WriteString("\n")
		sb.WriteString(args.Content)
		sb.WriteString("\n")

		if err := os.WriteFile(absPath, []byte(sb.String()), 0644); err != nil {
			return MemorySaveResult{}, fmt.Errorf("failed to write to %s: %w", clean, err)
		}

		// Trigger reindex
		if store != nil {
			go func() {
				_ = store.ReindexFile(context.Background(), clean)
			}()
		}

		return MemorySaveResult{
			Saved:   true,
			Message: fmt.Sprintf("Saved to %s under '%s'", clean, args.Category),
		}, nil
	}
}

// NewMemorySaveTool creates the memory_save tool using the given memory manager.
// If store is provided, knowledge tier writes are supported and reindexing is triggered.
func NewMemorySaveTool(mgr *memory.Manager, store MemorySaveStore) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "memory_save",
		Description: "Save durable facts to persistent memory for future recall. " +
			"For core identity/preferences/connection details, save to MEMORY.md (default). " +
			"For procedural knowledge discovered during problem-solving (API quirks, command " +
			"recipes, workarounds, configuration steps), save to a topic file like " +
			"'infrastructure/proxmox.md'. To correct outdated facts, set overwrite=true and " +
			"provide the complete corrected section content. NEVER save command outputs, " +
			"resource listings, or any data that changes over time — those must be fetched live.",
	}, MemorySave(mgr, store))
}
