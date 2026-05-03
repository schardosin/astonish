package tools

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/schardosin/astonish/pkg/memory"
	"github.com/schardosin/astonish/pkg/store"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// MemorySaveArgs defines the arguments for the memory_save tool.
type MemorySaveArgs struct {
	Category  string `json:"category" jsonschema:"A short category heading for organizing the memory (e.g., SSH Interactive Login, Proxmox API, Browser Quirks)"`
	Content   string `json:"content" jsonschema:"The facts to save, one per line. Use '- ' prefix for bullet points."`
	Kind      string `json:"kind,omitempty" jsonschema:"Knowledge category: tools, workarounds, infrastructure, projects, others. Omit for core MEMORY.md."`
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
// In platform mode, checks tool context for a PG-backed MemoryStore
// (injected by ChatRunner.InjectMemoryStores) and writes there instead.
func MemorySave(mgr *memory.Manager, saveStore MemorySaveStore) func(ctx tool.Context, args MemorySaveArgs) (MemorySaveResult, error) {
	return func(ctx tool.Context, args MemorySaveArgs) (MemorySaveResult, error) {
		if args.Category == "" {
			return MemorySaveResult{}, fmt.Errorf("category is required")
		}
		if args.Content == "" {
			return MemorySaveResult{}, fmt.Errorf("content is required")
		}

		// In platform mode, prefer the PG-backed memory store from context.
		if pgMem := store.MemoryStoreFromContext(ctx); pgMem != nil {
			return platformMemorySave(ctx, args, mgr, pgMem)
		}

		// Personal mode: use file-based memory system.
		kind := strings.TrimSpace(strings.ToLower(args.Kind))

		// If no kind specified, use the core tier (MEMORY.md)
		if kind == "" {
			if err := mgr.Append(args.Category, args.Content, args.Overwrite); err != nil {
				return MemorySaveResult{}, fmt.Errorf("failed to save to memory: %w", err)
			}

			// Trigger reindex of MEMORY.md if store is available
			if saveStore != nil {
				if err := saveStore.ReindexFile(context.Background(), "MEMORY.md"); err != nil {
					slog.Warn("failed to reindex memory file after save", "file", "MEMORY.md", "error", err)
				}
			}

			return MemorySaveResult{
				Saved:   true,
				Message: fmt.Sprintf("Saved to MEMORY.md under '%s'", args.Category),
			}, nil
		}

		// Knowledge tier: resolve kind to canonical file path
		relPath, ok := memory.KnowledgeFiles[kind]
		if !ok {
			return MemorySaveResult{}, fmt.Errorf("invalid kind %q: must be one of tools, workarounds, infrastructure, projects, others", kind)
		}

		// Resolve the absolute path using the memory directory
		var memDir string
		if saveStore != nil {
			memDir = saveStore.Config().MemoryDir
		}
		if memDir == "" {
			memDir = filepath.Dir(mgr.Path)
		}

		absPath := filepath.Join(memDir, relPath)

		// Cross-file section check: if the section already exists in a different
		// knowledge file, redirect to that file to prevent cross-bucket duplication.
		resolvedPath, resolvedRel := memory.ResolveKnowledgeFile(memDir, memory.KnowledgeFiles, absPath, args.Category)
		if resolvedRel == "" {
			resolvedRel = relPath
		}

		// Use section-aware append with dedup and fuzzy heading matching
		if err := memory.AppendToFile(resolvedPath, args.Category, args.Content, args.Overwrite, false); err != nil {
			return MemorySaveResult{}, fmt.Errorf("failed to write to %s: %w", resolvedRel, err)
		}

		// Trigger reindex
		if saveStore != nil {
			if err := saveStore.ReindexFile(context.Background(), resolvedRel); err != nil {
				slog.Warn("failed to reindex memory file after save", "file", resolvedRel, "error", err)
			}
		}

		return MemorySaveResult{
			Saved:   true,
			Message: fmt.Sprintf("Saved to %s under '%s'", resolvedRel, args.Category),
		}, nil
	}
}

// platformMemorySave is the shared implementation for platform-mode saves.
// Writes to the PG team memory store, with optional MEMORY.md fallback for
// core tier (backward compat with personal-mode tools).
func platformMemorySave(ctx context.Context, args MemorySaveArgs, fileMgr *memory.Manager, pgMem store.MemoryStore) (MemorySaveResult, error) {
	kind := strings.TrimSpace(strings.ToLower(args.Kind))

	// Determine the category for the database entry.
	// In platform mode, "kind" maps to the PG category column.
	dbCategory := args.Category
	if kind != "" {
		dbCategory = kind + "/" + args.Category
	}

	// If file-based manager is available and no kind specified, also append
	// to MEMORY.md for backward compatibility with personal-mode tools that read it.
	if kind == "" && fileMgr != nil {
		if err := fileMgr.Append(args.Category, args.Content, args.Overwrite); err != nil {
			slog.Warn("failed to append to MEMORY.md in platform mode", "error", err)
			// Don't fail — the PG store write below is the primary target
		}
	}

	// Save to the team memory store (PG)
	if pgMem != nil {
		entry := store.MemoryEntry{
			Content:  args.Content,
			Category: dbCategory,
		}
		if err := pgMem.Add(ctx, entry); err != nil {
			return MemorySaveResult{}, fmt.Errorf("failed to save memory: %w", err)
		}
	}

	return MemorySaveResult{
		Saved:   true,
		Message: fmt.Sprintf("Saved to team memory under '%s'", dbCategory),
	}, nil
}

// PlatformMemorySave saves facts to memory using the store.MemoryStore interface.
// Used in platform mode where memories are stored in PostgreSQL instead of files.
// The memMgr is used for MEMORY.md core tier (file-based), while the memStore
// handles team-scoped knowledge tier saves.
func PlatformMemorySave(memMgr store.MemoryManager, memStore store.MemoryStore) func(ctx tool.Context, args MemorySaveArgs) (MemorySaveResult, error) {
	return func(ctx tool.Context, args MemorySaveArgs) (MemorySaveResult, error) {
		if args.Category == "" {
			return MemorySaveResult{}, fmt.Errorf("category is required")
		}
		if args.Content == "" {
			return MemorySaveResult{}, fmt.Errorf("content is required")
		}

		// Wrap store.MemoryManager as *memory.Manager if available
		// (PlatformMemorySave uses store.MemoryManager, not *memory.Manager)
		return platformMemorySave(ctx, args, nil, memStore)
	}
}

// NewMemorySaveTool creates the memory_save tool using the given memory manager.
// If store is provided, knowledge tier writes are supported and reindexing is triggered.
// In platform mode, the tool checks the context for a PG-backed MemoryStore
// (injected by ChatRunner.InjectMemoryStores) and writes there instead.
func NewMemorySaveTool(mgr *memory.Manager, saveStore MemorySaveStore) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "memory_save",
		Description: "Save durable facts to persistent memory. Omit 'kind' for core MEMORY.md (identity/preferences/connections). " +
			"Use kind='tools' for tool quirks/CLI syntax/API patterns, kind='workarounds' for problems+solutions, " +
			"kind='infrastructure' for servers/networking/services, kind='projects' for project-specific knowledge, " +
			"kind='others' for anything else. Set overwrite=true to replace outdated sections. NEVER save ephemeral data.",
	}, MemorySave(mgr, saveStore))
}

// NewPlatformMemorySaveTool creates the memory_save tool for platform mode.
// Saves to the team-scoped PostgreSQL memory store.
func NewPlatformMemorySaveTool(memMgr store.MemoryManager, memStore store.MemoryStore) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "memory_save",
		Description: "Save durable facts to persistent memory. Omit 'kind' for core MEMORY.md (identity/preferences/connections). " +
			"Use kind='tools' for tool quirks/CLI syntax/API patterns, kind='workarounds' for problems+solutions, " +
			"kind='infrastructure' for servers/networking/services, kind='projects' for project-specific knowledge, " +
			"kind='others' for anything else. Set overwrite=true to replace outdated sections. NEVER save ephemeral data.",
	}, PlatformMemorySave(memMgr, memStore))
}
