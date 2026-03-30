package astonish

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/credentials"
	"github.com/schardosin/astonish/pkg/memory"
)

func handleMemoryCommand(args []string) error {
	if len(args) < 1 || args[0] == "-h" || args[0] == "--help" {
		printMemoryUsage()
		return nil
	}

	switch args[0] {
	case "search":
		return handleMemorySearchCommand(args[1:])
	case "list":
		return handleMemoryListCommand(args[1:])
	case "status":
		return handleMemoryStatusCommand(args[1:])
	case "reindex":
		return handleMemoryReindexCommand(args[1:])
	default:
		printMemoryUsage()
		return fmt.Errorf("unknown memory command: %s", args[0])
	}
}

func printMemoryUsage() {
	fmt.Println("usage: astonish memory {search,list,status,reindex} ...")
	fmt.Println("")
	fmt.Println("Manage the semantic memory / knowledge system.")
	fmt.Println("")
	fmt.Println("commands:")
	fmt.Println("  search <query>        Semantic search across indexed memory")
	fmt.Println("  list                  List memory files and chunk count")
	fmt.Println("  status                Show memory system status")
	fmt.Println("  reindex               Force re-index all memory files")
}

// memoryEnv holds the initialized memory system components.
type memoryEnv struct {
	store   *memory.Store
	indexer *memory.Indexer
	memCfg  *config.MemoryConfig
	memDir  string
	vecDir  string
	cleanup func() error
}

// initMemoryEnv loads config, initializes the embedder + store + indexer.
func initMemoryEnv(verbose bool) (*memoryEnv, error) {
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	memCfg := &appCfg.Memory
	if !memCfg.IsMemoryEnabled() {
		return nil, fmt.Errorf("memory system is disabled in config (set memory.enabled: true)")
	}

	memDir, err := config.GetMemoryDir(memCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve memory directory: %w", err)
	}
	vecDir, err := config.GetVectorDir(memCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve vector directory: %w", err)
	}

	if err := os.MkdirAll(memDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create memory directory: %w", err)
	}

	// Initialize embedding function (local Hugot by default)
	// Try to resolve embedding API key from credential store if available
	var embGetSecret config.SecretGetter
	if cfgDir, err := config.GetConfigDir(); err == nil {
		if store, err := credentials.Open(cfgDir); err == nil {
			embGetSecret = store.GetSecret
		}
	}
	embResult, err := memory.ResolveEmbeddingFunc(appCfg, memCfg, verbose, embGetSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize embedder: %w", err)
	}

	// Build store config
	storeCfg := memory.DefaultStoreConfig()
	storeCfg.MemoryDir = memDir
	storeCfg.VectorDir = vecDir
	if memCfg.Chunking.MaxChars > 0 {
		storeCfg.ChunkMaxChars = memCfg.Chunking.MaxChars
	}
	if memCfg.Chunking.Overlap > 0 {
		storeCfg.ChunkOverlap = memCfg.Chunking.Overlap
	}
	if memCfg.Search.MaxResults > 0 {
		storeCfg.MaxResults = memCfg.Search.MaxResults
	}
	if memCfg.Search.MinScore > 0 {
		storeCfg.MinScore = memCfg.Search.MinScore
	}

	store, err := memory.NewStore(storeCfg, embResult.EmbeddingFunc)
	if err != nil {
		if embResult.Cleanup != nil {
			embResult.Cleanup()
		}
		return nil, fmt.Errorf("failed to create memory store: %w", err)
	}

	indexer := memory.NewIndexer(store, storeCfg, verbose)
	store.SetIndexer(indexer)

	return &memoryEnv{
		store:   store,
		indexer: indexer,
		memCfg:  memCfg,
		memDir:  memDir,
		vecDir:  vecDir,
		cleanup: embResult.Cleanup,
	}, nil
}

// close cleans up resources.
func (env *memoryEnv) close() {
	if env.cleanup != nil {
		env.cleanup()
	}
}

// --- search ---

func handleMemorySearchCommand(args []string) error {
	searchCmd := flag.NewFlagSet("search", flag.ExitOnError)
	maxResults := searchCmd.Int("max-results", 0, "Maximum number of results (default: from config, usually 6)")
	minScore := searchCmd.Float64("min-score", 0, "Minimum similarity score 0.0-1.0 (default: from config, usually 0.35)")
	verbose := searchCmd.Bool("verbose", false, "Show debug output")
	searchCmd.Parse(args)

	remaining := searchCmd.Args()
	if len(remaining) == 0 {
		fmt.Println("usage: astonish memory search [--max-results N] [--min-score F] <query>")
		return fmt.Errorf("no search query provided")
	}

	query := strings.Join(remaining, " ")

	fmt.Printf("Initializing memory system...\n")
	env, err := initMemoryEnv(*verbose)
	if err != nil {
		return err
	}
	defer env.close()

	// Index to ensure vectors are up-to-date
	fmt.Printf("Indexing memory files...\n")
	start := time.Now()
	if err := env.indexer.IndexAll(context.Background()); err != nil {
		return fmt.Errorf("indexing failed: %w", err)
	}
	indexTime := time.Since(start)

	chunkCount := env.store.Count()
	if chunkCount == 0 {
		fmt.Println("\nMemory index is empty. No .md files found in memory directory.")
		fmt.Printf("  Memory dir: %s\n", env.memDir)
		fmt.Println("  Tip: Save knowledge with the memory_save tool during chat, or add .md files manually.")
		return nil
	}

	fmt.Printf("Indexed %d chunks in %s. Searching...\n\n", chunkCount, indexTime.Round(time.Millisecond))

	// Run search
	results, err := env.store.Search(context.Background(), query, *maxResults, *minScore)
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	if len(results) == 0 {
		fmt.Printf("No matches found for: %q\n", query)
		fmt.Println("Tip: Try broader terms or lower --min-score (e.g., --min-score 0.2)")
		return nil
	}

	// Print results
	fmt.Printf("Found %d result(s) for: %q\n\n", len(results), query)

	for i, r := range results {
		snippet := truncateSnippet(r.Snippet, 200)
		fmt.Printf("  %d. [%.3f] %s", i+1, r.Score, r.Path)
		if r.StartLine > 0 {
			fmt.Printf(":%d-%d", r.StartLine, r.EndLine)
		}
		fmt.Println()
		// Indent snippet lines
		for _, line := range strings.Split(snippet, "\n") {
			fmt.Printf("     %s\n", line)
		}
		if i < len(results)-1 {
			fmt.Println()
		}
	}

	return nil
}

// --- list ---

func handleMemoryListCommand(args []string) error {
	listCmd := flag.NewFlagSet("list", flag.ExitOnError)
	verbose := listCmd.Bool("verbose", false, "Show debug output")
	listCmd.Parse(args)

	env, err := initMemoryEnv(*verbose)
	if err != nil {
		return err
	}
	defer env.close()

	// Walk memory dir for .md files
	memDir := env.memDir
	type fileInfo struct {
		relPath string
		size    int64
	}
	var files []fileInfo

	filepath.WalkDir(memDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && filepath.Base(path) == "vectors" {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".md") {
			info, infoErr := d.Info()
			if infoErr != nil {
				slog.Warn("failed to get file info", "path", path, "error", infoErr)
			}
			relPath, relErr := filepath.Rel(memDir, path)
			if relErr != nil {
				slog.Warn("failed to compute relative path", "path", path, "error", relErr)
			}
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			files = append(files, fileInfo{relPath: relPath, size: size})
		}
		return nil
	})

	chunkCount := env.store.Count()

	fmt.Printf("Memory directory: %s\n", memDir)
	fmt.Printf("Vector store:     %s\n", env.vecDir)
	fmt.Printf("Chunks indexed:   %d\n", chunkCount)
	fmt.Printf("Files:            %d\n\n", len(files))

	if len(files) == 0 {
		fmt.Println("  (no .md files found)")
		return nil
	}

	for _, f := range files {
		sizeStr := formatSize(f.size)
		fmt.Printf("  %-50s %s\n", f.relPath, sizeStr)
	}

	return nil
}

// --- status ---

func handleMemoryStatusCommand(args []string) error {
	statusCmd := flag.NewFlagSet("status", flag.ExitOnError)
	statusCmd.Parse(args)

	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	memCfg := &appCfg.Memory

	// Memory enabled?
	if !memCfg.IsMemoryEnabled() {
		fmt.Println("Memory system: DISABLED")
		fmt.Println("  Set memory.enabled: true in config.yaml to enable.")
		return nil
	}

	fmt.Println("Memory system: ENABLED")
	fmt.Println()

	// Directories
	memDir, err := config.GetMemoryDir(memCfg)
	if err != nil {
		slog.Warn("failed to get memory directory", "error", err)
	}
	vecDir, err := config.GetVectorDir(memCfg)
	if err != nil {
		slog.Warn("failed to get vector directory", "error", err)
	}
	modelsDir, err := config.GetModelsDir()
	if err != nil {
		slog.Warn("failed to get models directory", "error", err)
	}

	fmt.Printf("  Memory dir:   %s\n", memDir)
	fmt.Printf("  Vector dir:   %s\n", vecDir)
	fmt.Printf("  Models dir:   %s\n", modelsDir)
	fmt.Println()

	// Embedding provider
	provider := memCfg.Embedding.Provider
	if provider == "" || provider == "auto" || provider == "local" {
		fmt.Println("  Embedding:    local (Hugot / all-MiniLM-L6-v2)")

		// Check if model is downloaded
		modelPath := filepath.Join(modelsDir, "sentence-transformers_all-MiniLM-L6-v2")
		if _, err := os.Stat(modelPath); err == nil {
			fmt.Println("  Model status: downloaded")
		} else {
			fmt.Println("  Model status: not yet downloaded (will download on first use, ~23 MB)")
		}
	} else {
		model := memCfg.Embedding.Model
		if model == "" {
			model = "(default)"
		}
		fmt.Printf("  Embedding:    %s (model: %s)\n", provider, model)
	}
	fmt.Println()

	// File count
	fileCount := 0
	if memDir != "" {
		filepath.WalkDir(memDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && filepath.Base(path) == "vectors" {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.HasSuffix(d.Name(), ".md") {
				fileCount++
			}
			return nil
		})
	}
	fmt.Printf("  Memory files: %d\n", fileCount)

	// Chunk count (requires opening the store, which requires the embedder)
	// Only do this if the model is already downloaded to avoid triggering a download
	modelPath := filepath.Join(modelsDir, "sentence-transformers_all-MiniLM-L6-v2")
	if _, err := os.Stat(modelPath); err == nil {
		// Model exists, safe to open store
		env, envErr := initMemoryEnv(false)
		if envErr == nil {
			defer env.close()
			fmt.Printf("  Chunks:       %d\n", env.store.Count())
		}
	} else if provider != "" && provider != "auto" && provider != "local" {
		// Explicit cloud provider, safe to open
		env, envErr := initMemoryEnv(false)
		if envErr == nil {
			defer env.close()
			fmt.Printf("  Chunks:       %d\n", env.store.Count())
		}
	} else {
		fmt.Println("  Chunks:       (unknown — model not downloaded yet)")
	}

	// Config summary
	fmt.Println()
	fmt.Println("  Chunking:")
	maxChars := memCfg.Chunking.MaxChars
	if maxChars == 0 {
		maxChars = 1600
	}
	overlap := memCfg.Chunking.Overlap
	if overlap == 0 {
		overlap = 320
	}
	fmt.Printf("    max_chars:  %d\n", maxChars)
	fmt.Printf("    overlap:    %d\n", overlap)

	fmt.Println("  Search:")
	maxResults := memCfg.Search.MaxResults
	if maxResults == 0 {
		maxResults = 6
	}
	minScore := memCfg.Search.MinScore
	if minScore == 0 {
		minScore = 0.35
	}
	fmt.Printf("    max_results: %d\n", maxResults)
	fmt.Printf("    min_score:   %.2f\n", minScore)

	return nil
}

// --- reindex ---

func handleMemoryReindexCommand(args []string) error {
	reindexCmd := flag.NewFlagSet("reindex", flag.ExitOnError)
	verbose := reindexCmd.Bool("verbose", false, "Show debug output")
	reindexCmd.Parse(args)

	fmt.Println("Initializing memory system...")
	env, err := initMemoryEnv(*verbose)
	if err != nil {
		return err
	}
	defer env.close()

	fmt.Println("Re-indexing all memory files...")
	start := time.Now()
	if err := env.indexer.IndexAll(context.Background()); err != nil {
		return fmt.Errorf("indexing failed: %w", err)
	}

	elapsed := time.Since(start)
	chunkCount := env.store.Count()
	fmt.Printf("Done. Indexed %d chunks in %s.\n", chunkCount, elapsed.Round(time.Millisecond))

	return nil
}

// --- helpers ---

// truncateSnippet shortens text to maxLen characters, breaking at word boundaries.
func truncateSnippet(text string, maxLen int) string {
	// Collapse multiple whitespace/newlines for display
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= maxLen {
		return text
	}
	// Find last space before maxLen
	cut := strings.LastIndex(text[:maxLen], " ")
	if cut < maxLen/2 {
		cut = maxLen
	}
	return text[:cut] + "..."
}

// formatSize returns a human-readable file size string.
func formatSize(bytes int64) string {
	switch {
	case bytes >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
	case bytes >= 1024:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
