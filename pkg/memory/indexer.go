package memory

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Indexer discovers, chunks, and indexes memory files.
type Indexer struct {
	store     *Store
	config    *StoreConfig
	fileIndex map[string]string // path -> content hash (for incremental sync)
	mu        sync.Mutex
	debugMode bool
}

// NewIndexer creates a new indexer bound to the given store.
func NewIndexer(store *Store, cfg *StoreConfig, debugMode bool) *Indexer {
	return &Indexer{
		store:     store,
		config:    cfg,
		fileIndex: make(map[string]string),
		debugMode: debugMode,
	}
}

// IndexAll performs a full index of all .md files in the memory directory.
func (idx *Indexer) IndexAll(ctx context.Context) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	memDir := idx.config.MemoryDir
	if memDir == "" {
		return fmt.Errorf("memory directory not set")
	}

	// Collect all .md files
	var files []string
	err := filepath.WalkDir(memDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Skip the vectors directory
		if d.IsDir() && filepath.Base(path) == "vectors" {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk memory directory: %w", err)
	}

	// Track which files still exist on disk
	existingPaths := make(map[string]bool)
	var indexErrors int

	for _, absPath := range files {
		relPath, err := filepath.Rel(memDir, absPath)
		if err != nil {
			indexErrors++
			continue
		}
		existingPaths[relPath] = true

		if err := idx.indexFileUnlocked(ctx, relPath); err != nil {
			indexErrors++
			if idx.debugMode {
				fmt.Printf("[Memory Indexer] Error indexing %s: %v\n", relPath, err)
			}
		}
	}

	// Remove chunks for deleted files
	for path := range idx.fileIndex {
		if !existingPaths[path] {
			if idx.debugMode {
				fmt.Printf("[Memory Indexer] Removing deleted file: %s\n", path)
			}
			if err := idx.store.DeleteByPath(ctx, path); err != nil {
				if idx.debugMode {
					fmt.Printf("[Memory Indexer] Error removing %s: %v\n", path, err)
				}
			}
			delete(idx.fileIndex, path)
		}
	}

	if idx.debugMode {
		fmt.Printf("[Memory Indexer] Indexed %d files (%d errors), %d chunks total\n", len(existingPaths), indexErrors, idx.store.Count())
	}

	// If every file failed, return an error so callers know indexing was unsuccessful
	if indexErrors > 0 && indexErrors >= len(files) && len(files) > 0 {
		return fmt.Errorf("indexing failed: all %d files had errors", len(files))
	}

	return nil
}

// IndexFile indexes or re-indexes a single file (thread-safe).
func (idx *Indexer) IndexFile(ctx context.Context, relPath string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	return idx.indexFileUnlocked(ctx, relPath)
}

// indexFileUnlocked indexes a single file. Must be called with idx.mu held.
func (idx *Indexer) indexFileUnlocked(ctx context.Context, relPath string) error {
	absPath := filepath.Join(idx.config.MemoryDir, relPath)

	content, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File was deleted — remove from index
			if err := idx.store.DeleteByPath(ctx, relPath); err != nil {
				return err
			}
			delete(idx.fileIndex, relPath)
			return nil
		}
		return fmt.Errorf("failed to read %s: %w", relPath, err)
	}

	// Compute content hash
	hash := fmt.Sprintf("%x", sha256.Sum256(content))

	// Skip if unchanged
	if idx.fileIndex[relPath] == hash {
		return nil
	}

	if idx.debugMode {
		fmt.Printf("[Memory Indexer] Indexing %s (changed)\n", relPath)
	}

	// Delete old chunks for this file
	if err := idx.store.DeleteByPath(ctx, relPath); err != nil {
		// Non-fatal: the path might not exist in the collection yet
		if idx.debugMode {
			fmt.Printf("[Memory Indexer] Note: delete for %s: %v\n", relPath, err)
		}
	}

	// Chunk the file
	chunks := ChunkFile(relPath, string(content), idx.config.ChunkMaxChars, idx.config.ChunkOverlap)

	// Add new chunks
	if len(chunks) > 0 {
		if err := idx.store.AddDocuments(ctx, chunks); err != nil {
			return fmt.Errorf("failed to add chunks for %s: %w", relPath, err)
		}
	}

	// Update index
	idx.fileIndex[relPath] = hash

	return nil
}

// WatchAndSync watches for file changes and re-indexes with debouncing.
// This blocks until the context is cancelled.
func (idx *Indexer) WatchAndSync(ctx context.Context, debounceMs int) error {
	if debounceMs <= 0 {
		debounceMs = 1500
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	defer watcher.Close()

	memDir := idx.config.MemoryDir

	// Watch the memory directory and all subdirectories
	err = filepath.WalkDir(memDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip vectors directory
			if filepath.Base(path) == "vectors" {
				return filepath.SkipDir
			}
			return watcher.Add(path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to set up watchers: %w", err)
	}

	// Debounce: collect changed paths over a window before indexing
	pendingPaths := make(map[string]bool)
	var pendingMu sync.Mutex
	var debounceTimer *time.Timer

	flushPending := func() {
		pendingMu.Lock()
		paths := make([]string, 0, len(pendingPaths))
		for p := range pendingPaths {
			paths = append(paths, p)
		}
		pendingPaths = make(map[string]bool)
		pendingMu.Unlock()

		for _, p := range paths {
			if err := idx.IndexFile(ctx, p); err != nil {
				if idx.debugMode {
					fmt.Printf("[Memory Watcher] Error indexing %s: %v\n", p, err)
				}
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return nil

		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			// Only care about .md files
			if !strings.HasSuffix(event.Name, ".md") {
				// But if a new directory was created, watch it
				if event.Has(fsnotify.Create) {
					info, err := os.Stat(event.Name)
					if err == nil && info.IsDir() && filepath.Base(event.Name) != "vectors" {
						_ = watcher.Add(event.Name)
					}
				}
				continue
			}

			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				relPath, err := filepath.Rel(memDir, event.Name)
				if err != nil {
					continue
				}

				pendingMu.Lock()
				pendingPaths[relPath] = true
				pendingMu.Unlock()

				// Reset debounce timer
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(time.Duration(debounceMs)*time.Millisecond, flushPending)
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			if idx.debugMode {
				fmt.Printf("[Memory Watcher] Error: %v\n", err)
			}
		}
	}
}
