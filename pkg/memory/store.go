package memory

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	chromem "github.com/philippgille/chromem-go"
)

// Store wraps chromem-go and manages the memory vector database.
type Store struct {
	db         *chromem.DB
	collection *chromem.Collection
	config     *StoreConfig
	indexer    *Indexer

	mu sync.RWMutex
}

// StoreConfig configures the vector store.
type StoreConfig struct {
	MemoryDir     string  // Root directory for memory files (contains MEMORY.md, projects/, etc.)
	VectorDir     string  // Persistence directory for chromem-go
	MaxResults    int     // Default max results per search
	MinScore      float64 // Default minimum similarity score
	ChunkMaxChars int     // Max characters per chunk
	ChunkOverlap  int     // Overlap characters between chunks
}

// DefaultStoreConfig returns sane defaults.
func DefaultStoreConfig() *StoreConfig {
	return &StoreConfig{
		MaxResults:    6,
		MinScore:      0.35,
		ChunkMaxChars: 1600,
		ChunkOverlap:  320,
	}
}

// NewStore creates a persistent vector store backed by chromem-go.
func NewStore(cfg *StoreConfig, embeddingFunc chromem.EmbeddingFunc) (*Store, error) {
	if cfg.VectorDir == "" {
		return nil, fmt.Errorf("vector directory is required")
	}
	if embeddingFunc == nil {
		return nil, fmt.Errorf("embedding function is required")
	}

	db, err := chromem.NewPersistentDB(cfg.VectorDir, false)
	if err != nil {
		return nil, fmt.Errorf("failed to create persistent DB: %w", err)
	}

	col, err := db.GetOrCreateCollection("memory", nil, embeddingFunc)
	if err != nil {
		return nil, fmt.Errorf("failed to get/create collection: %w", err)
	}

	return &Store{
		db:         db,
		collection: col,
		config:     cfg,
	}, nil
}

// SetIndexer sets the indexer reference (called during initialization to wire
// the store and indexer together without circular dependency).
func (s *Store) SetIndexer(idx *Indexer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.indexer = idx
}

// SearchResult represents a single search hit.
type SearchResult struct {
	Path      string  `json:"path"`
	StartLine int     `json:"startLine"`
	EndLine   int     `json:"endLine"`
	Score     float64 `json:"score"`
	Snippet   string  `json:"snippet"`
	Category  string  `json:"category,omitempty"`
}

// Search performs semantic search across indexed memory.
func (s *Store) Search(ctx context.Context, query string, maxResults int, minScore float64) ([]SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if maxResults <= 0 {
		maxResults = s.config.MaxResults
	}
	if minScore <= 0 {
		minScore = s.config.MinScore
	}

	// chromem-go returns error if nResults > document count
	docCount := s.collection.Count()
	if docCount == 0 {
		return nil, nil
	}
	if maxResults > docCount {
		maxResults = docCount
	}

	results, err := s.collection.Query(ctx, query, maxResults, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	var filtered []SearchResult
	for _, r := range results {
		score := float64(r.Similarity)
		if score < minScore {
			continue
		}

		startLine, _ := strconv.Atoi(r.Metadata["startLine"])
		endLine, _ := strconv.Atoi(r.Metadata["endLine"])

		filtered = append(filtered, SearchResult{
			Path:      r.Metadata["path"],
			StartLine: startLine,
			EndLine:   endLine,
			Score:     score,
			Snippet:   r.Content,
			Category:  r.Metadata["category"],
		})
	}

	return filtered, nil
}

// AddDocuments adds chunks to the collection. Each chunk is stored as a
// chromem-go Document with metadata for path, startLine, endLine.
func (s *Store) AddDocuments(ctx context.Context, chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	docs := make([]chromem.Document, len(chunks))
	for i, c := range chunks {
		docs[i] = chromem.Document{
			ID:      c.ID,
			Content: c.Text,
			Metadata: map[string]string{
				"path":      c.Path,
				"startLine": strconv.Itoa(c.StartLine),
				"endLine":   strconv.Itoa(c.EndLine),
				"category":  c.Category,
			},
		}
	}

	// Use concurrency of 4 for embedding
	return s.collection.AddDocuments(ctx, docs, 4)
}

// DeleteByPath removes all chunks for a given file path.
func (s *Store) DeleteByPath(ctx context.Context, path string) error {
	return s.collection.Delete(ctx, map[string]string{"path": path}, nil)
}

// DeleteByIDs removes specific documents by their IDs.
func (s *Store) DeleteByIDs(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return s.collection.Delete(ctx, nil, nil, ids...)
}

// Count returns the number of documents in the collection.
func (s *Store) Count() int {
	return s.collection.Count()
}

// Config returns the store configuration.
func (s *Store) Config() *StoreConfig {
	return s.config
}

// SearchByCategory performs a semantic search filtered to a specific category.
// If category is empty, searches all documents (same as Search).
// Categories are derived from file paths: "guidance", "skill", "flow",
// "self", "instructions", "knowledge".
func (s *Store) SearchByCategory(ctx context.Context, query string, maxResults int,
	minScore float64, category string) ([]SearchResult, error) {

	s.mu.RLock()
	defer s.mu.RUnlock()

	if maxResults <= 0 {
		maxResults = s.config.MaxResults
	}
	if minScore <= 0 {
		minScore = s.config.MinScore
	}

	docCount := s.collection.Count()
	if docCount == 0 {
		return nil, nil
	}
	if maxResults > docCount {
		maxResults = docCount
	}

	var where map[string]string
	if category != "" {
		where = map[string]string{"category": category}
	}

	results, err := s.collection.Query(ctx, query, maxResults, where, nil)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	var filtered []SearchResult
	for _, r := range results {
		score := float64(r.Similarity)
		if score < minScore {
			continue
		}

		startLine, _ := strconv.Atoi(r.Metadata["startLine"])
		endLine, _ := strconv.Atoi(r.Metadata["endLine"])

		filtered = append(filtered, SearchResult{
			Path:      r.Metadata["path"],
			StartLine: startLine,
			EndLine:   endLine,
			Score:     score,
			Snippet:   r.Content,
			Category:  r.Metadata["category"],
		})
	}
	return filtered, nil
}

// ReindexFile triggers re-indexing of a single file. This is called after
// memory_save writes to a file.
func (s *Store) ReindexFile(ctx context.Context, relPath string) error {
	s.mu.RLock()
	idx := s.indexer
	s.mu.RUnlock()

	if idx == nil {
		return nil
	}
	return idx.IndexFile(ctx, relPath)
}

// Close is a no-op for chromem-go (it auto-persists), but included for
// interface completeness.
func (s *Store) Close() error {
	return nil
}
