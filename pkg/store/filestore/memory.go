package filestore

import (
	"context"
	"fmt"

	"github.com/schardosin/astonish/pkg/memory"
	"github.com/schardosin/astonish/pkg/store"
)

// MemoryStoreWrapper wraps the existing memory.Store behind the
// store.MemoryStore interface.
type MemoryStoreWrapper struct {
	inner *memory.Store
}

// NewMemoryStore creates a MemoryStore backed by the existing file-based memory store.
func NewMemoryStore(ms *memory.Store) store.MemoryStore {
	return &MemoryStoreWrapper{inner: ms}
}

// Inner returns the underlying memory.Store for code that still needs
// direct access during the transition period.
func (w *MemoryStoreWrapper) Inner() *memory.Store {
	return w.inner
}

func (w *MemoryStoreWrapper) Search(ctx context.Context, query string, maxResults int, minScore float64) ([]store.MemorySearchResult, error) {
	results, err := w.inner.SearchHybrid(ctx, query, maxResults, minScore)
	if err != nil {
		return nil, err
	}
	return convertSearchResults(results), nil
}

func (w *MemoryStoreWrapper) SearchByCategory(ctx context.Context, query string, maxResults int, minScore float64, category string) ([]store.MemorySearchResult, error) {
	results, err := w.inner.SearchHybridByCategory(ctx, query, maxResults, minScore, category)
	if err != nil {
		return nil, err
	}
	return convertSearchResults(results), nil
}

// Add inserts a memory chunk into the file-based store.
// In personal mode, this delegates to chromem-go's AddDocuments.
func (w *MemoryStoreWrapper) Add(ctx context.Context, entry store.MemoryEntry) error {
	chunk := memory.Chunk{
		ID:       fmt.Sprintf("manual-%s", entry.Category),
		Text:     entry.Content,
		Path:     entry.SourcePath,
		Category: entry.Category,
	}
	return w.inner.AddDocuments(ctx, []memory.Chunk{chunk})
}

// Delete removes a memory chunk by ID from the file-based store.
func (w *MemoryStoreWrapper) Delete(ctx context.Context, id string) error {
	return w.inner.DeleteByIDs(ctx, []string{id})
}

// List returns memory chunks from the file-based store.
// The file-based store doesn't support offset/limit natively, so this
// returns up to 'limit' results with a simple search.
func (w *MemoryStoreWrapper) List(ctx context.Context, category string, limit, _ int) ([]store.MemorySearchResult, error) {
	// The file-based chromem-go store doesn't have a "list all" API.
	// Use a broad search to approximate listing.
	var results []memory.SearchResult
	var err error
	if category == "" {
		results, err = w.inner.Search(ctx, "", limit, 0)
	} else {
		results, err = w.inner.SearchByCategory(ctx, "", limit, 0, category)
	}
	if err != nil {
		return nil, err
	}
	return convertSearchResults(results), nil
}

func (w *MemoryStoreWrapper) Count() int {
	return w.inner.Count()
}

func (w *MemoryStoreWrapper) Close() error {
	return w.inner.Close()
}

func convertSearchResults(results []memory.SearchResult) []store.MemorySearchResult {
	out := make([]store.MemorySearchResult, len(results))
	for i, r := range results {
		out[i] = store.MemorySearchResult{
			Path:      r.Path,
			StartLine: r.StartLine,
			EndLine:   r.EndLine,
			Score:     r.Score,
			Snippet:   r.Snippet,
			Category:  r.Category,
			Scope:     string(store.MemoryScopePersonal),
		}
	}
	return out
}

// MemoryManagerWrapper wraps the existing memory.Manager behind the
// store.MemoryManager interface.
type MemoryManagerWrapper struct {
	inner *memory.Manager
}

// NewMemoryManager creates a MemoryManager backed by the existing file-based memory manager.
func NewMemoryManager(mm *memory.Manager) store.MemoryManager {
	return &MemoryManagerWrapper{inner: mm}
}

// Inner returns the underlying memory.Manager for code that still needs
// direct access during the transition period.
func (w *MemoryManagerWrapper) Inner() *memory.Manager {
	return w.inner
}

func (w *MemoryManagerWrapper) Load() (string, error) {
	return w.inner.Load()
}

func (w *MemoryManagerWrapper) Append(category, content string, overwrite bool) error {
	return w.inner.Append(category, content, overwrite)
}

func (w *MemoryManagerWrapper) EnsureDir() error {
	return w.inner.EnsureDir()
}

func (w *MemoryManagerWrapper) MemoryPath() string {
	return w.inner.Path
}

// Compile-time checks.
var _ store.MemoryStore = (*MemoryStoreWrapper)(nil)
var _ store.MemoryManager = (*MemoryManagerWrapper)(nil)
