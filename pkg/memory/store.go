package memory

import (
	"context"
	"fmt"
	"sort"
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
	bm25       *bm25Index     // keyword search index (rebuilt after indexing)
	bm25Docs   []bm25InputDoc // retained for incremental BM25 rebuilds

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
		MinScore:      0.30,
		ChunkMaxChars: 1200,
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

// DB returns the underlying chromem-go database instance.
// This allows creating additional collections on the same DB (e.g., a dedicated
// tool index collection) while sharing the same persistence directory.
func (s *Store) DB() *chromem.DB {
	return s.db
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
// Also updates the BM25 keyword index.
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
		// Track for BM25
		s.bm25Docs = append(s.bm25Docs, bm25InputDoc{
			ID:        c.ID,
			Content:   c.Text,
			Path:      c.Path,
			StartLine: c.StartLine,
			EndLine:   c.EndLine,
			Category:  c.Category,
		})
	}

	// Use concurrency of 4 for embedding
	return s.collection.AddDocuments(ctx, docs, 4)
}

// DeleteByPath removes all chunks for a given file path.
// Also removes the corresponding entries from the BM25 index data.
func (s *Store) DeleteByPath(ctx context.Context, path string) error {
	// Remove from BM25 tracking
	filtered := s.bm25Docs[:0]
	for _, d := range s.bm25Docs {
		if d.Path != path {
			filtered = append(filtered, d)
		}
	}
	s.bm25Docs = filtered

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

// RebuildBM25 rebuilds the BM25 keyword index from the tracked documents.
// This should be called after IndexAll or IndexFile completes.
func (s *Store) RebuildBM25() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bm25 = buildBM25(s.bm25Docs)
}

// rrfK is the constant for Reciprocal Rank Fusion (Cormack et al. 2009).
const rrfK = 60.0

// maxRRFScore is the maximum possible RRF score with 2 retrieval methods
// and k=60: 2 × 1/(60+1) ≈ 0.03279. Used to normalize scores to 0-1.
var maxRRFScore = 2.0 / (rrfK + 1.0)

// SearchHybrid performs hybrid search: vector (semantic) + BM25 (keyword),
// fused with Reciprocal Rank Fusion. This solves the "query dilution" problem
// where specific terms (product specs, model numbers) shift the dense embedding
// away from relevant documents, but keyword matching on shared terms still works.
//
// Scores are normalized to 0-1 range (1.0 = rank 1 in both methods).
func (s *Store) SearchHybrid(ctx context.Context, query string, maxResults int,
	minScore float64) ([]SearchResult, error) {

	return s.searchHybrid(ctx, query, "", maxResults, minScore, "")
}

// SearchHybridByCategory performs hybrid search filtered to a specific category.
func (s *Store) SearchHybridByCategory(ctx context.Context, query string, maxResults int,
	minScore float64, category string) ([]SearchResult, error) {

	return s.searchHybrid(ctx, query, "", maxResults, minScore, category)
}

// SearchHybridWithContext performs hybrid search where vector search uses the
// raw query but BM25 keyword search uses an augmented query that includes
// conversational context. This helps follow-up queries like "show me per VM"
// find relevant docs by matching topic keywords from the prior conversation
// that the user's message alone doesn't contain.
//
// If bm25Query is empty, BM25 falls back to the same query as vector search.
func (s *Store) SearchHybridWithContext(ctx context.Context, query string, bm25Query string,
	maxResults int, minScore float64) ([]SearchResult, error) {

	return s.searchHybrid(ctx, query, bm25Query, maxResults, minScore, "")
}

// SearchHybridByCategoryWithContext is like SearchHybridWithContext but filtered
// to a specific category.
func (s *Store) SearchHybridByCategoryWithContext(ctx context.Context, query string,
	bm25Query string, maxResults int, minScore float64, category string) ([]SearchResult, error) {

	return s.searchHybrid(ctx, query, bm25Query, maxResults, minScore, category)
}

// searchHybrid is the internal implementation of hybrid search.
// When bm25Query is non-empty, BM25 keyword search uses it instead of query,
// allowing conversational context to boost keyword matching while keeping
// vector (semantic) search clean on the raw user query.
func (s *Store) searchHybrid(ctx context.Context, query string, bm25Query string,
	maxResults int, minScore float64, category string) ([]SearchResult, error) {

	s.mu.RLock()
	defer s.mu.RUnlock()

	if maxResults <= 0 {
		maxResults = s.config.MaxResults
	}
	if minScore <= 0 {
		minScore = s.config.MinScore
	}

	// --- Vector search ---
	docCount := s.collection.Count()
	var vectorResults []chromem.Result
	if docCount > 0 {
		candidateK := maxResults * 3
		if candidateK < 20 {
			candidateK = 20
		}
		if candidateK > docCount {
			candidateK = docCount
		}

		var where map[string]string
		if category != "" {
			where = map[string]string{"category": category}
		}

		var err error
		vectorResults, err = s.collection.Query(ctx, query, candidateK, where, nil)
		if err != nil {
			return nil, fmt.Errorf("vector search failed: %w", err)
		}
	}

	// --- BM25 keyword search ---
	// When bm25Query is provided (conversational context), use it for keyword
	// matching to catch topic terms the raw user message doesn't contain.
	bm25Input := query
	if bm25Query != "" {
		bm25Input = bm25Query
	}
	candidateK := maxResults * 3
	if candidateK < 20 {
		candidateK = 20
	}
	bm25Results := s.bm25.search(bm25Input, candidateK, category)

	// --- Reciprocal Rank Fusion ---
	type fusedEntry struct {
		path      string
		startLine int
		endLine   int
		category  string
		snippet   string
		rrfScore  float64
	}
	fused := make(map[string]*fusedEntry) // keyed by chunkID (path:startLine:endLine)

	// Helper to build a stable key for deduplication
	chunkKey := func(path string, startLine, endLine int) string {
		return fmt.Sprintf("%s:%d:%d", path, startLine, endLine)
	}

	// Add vector results
	for rank, r := range vectorResults {
		startLine, _ := strconv.Atoi(r.Metadata["startLine"])
		endLine, _ := strconv.Atoi(r.Metadata["endLine"])
		key := chunkKey(r.Metadata["path"], startLine, endLine)

		e, ok := fused[key]
		if !ok {
			e = &fusedEntry{
				path:      r.Metadata["path"],
				startLine: startLine,
				endLine:   endLine,
				category:  r.Metadata["category"],
				snippet:   r.Content,
			}
			fused[key] = e
		}
		e.rrfScore += 1.0 / (rrfK + float64(rank+1))
	}

	// Add BM25 results
	for rank, r := range bm25Results {
		key := chunkKey(r.path, r.startLine, r.endLine)

		e, ok := fused[key]
		if !ok {
			e = &fusedEntry{
				path:      r.path,
				startLine: r.startLine,
				endLine:   r.endLine,
				category:  r.category,
				snippet:   r.content,
			}
			fused[key] = e
		}
		e.rrfScore += 1.0 / (rrfK + float64(rank+1))
	}

	// Collect, normalize, sort, and filter
	results := make([]SearchResult, 0, len(fused))
	for _, e := range fused {
		normalizedScore := e.rrfScore / maxRRFScore
		if normalizedScore < minScore {
			continue
		}
		results = append(results, SearchResult{
			Path:      e.path,
			StartLine: e.startLine,
			EndLine:   e.endLine,
			Score:     normalizedScore,
			Snippet:   e.snippet,
			Category:  e.category,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	return results, nil
}
