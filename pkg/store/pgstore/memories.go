package pgstore

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/store"
)

// pgMemoryStore implements store.MemoryStore for PostgreSQL.
// It supports hybrid search: pgvector cosine similarity + tsvector keyword search.
type pgMemoryStore struct {
	pool            *pgxpool.Pool
	schema          string
	tablePrefix     string         // "org_" for org-level, "" for team/personal
	scope           string         // "personal", "team", "org" — set on results
	embedFunc       store.EmbedFunc // optional; when set, Search() uses vector+keyword hybrid
	createdByColumn string         // column name for creator tracking ("created_by" or "promoted_by")
}

func (m *pgMemoryStore) tableName() string {
	return pgx.Identifier{m.schema, m.tablePrefix + "memories"}.Sanitize()
}

// ownerColumn returns the column name used for creator/promoter tracking.
func (m *pgMemoryStore) ownerColumn() string {
	if m.createdByColumn != "" {
		return m.createdByColumn
	}
	return "created_by"
}

// scanSearchRow scans a row from a search query into a MemorySearchResult.
// Expected columns: id, chunk_text, category, source_path, score.
func (m *pgMemoryStore) scanSearchRow(rows pgx.Rows) (store.MemorySearchResult, error) {
	var id, content string
	var cat, sourcePath *string
	var score float64
	if err := rows.Scan(&id, &content, &cat, &sourcePath, &score); err != nil {
		return store.MemorySearchResult{}, err
	}
	r := store.MemorySearchResult{
		ID:      id,
		Snippet: content,
		Score:   score,
		Scope:   m.scope,
	}
	if cat != nil {
		r.Category = *cat
	}
	if sourcePath != nil {
		r.Path = *sourcePath
	}
	return r, nil
}

// scanDetailRow scans a row from a list/get query into a MemorySearchResult.
// Expected columns: id, chunk_text, category, source_path, created_by, created_at, session_id.
func (m *pgMemoryStore) scanDetailRow(rows pgx.Rows) (store.MemorySearchResult, error) {
	var id, content string
	var cat, sourcePath, createdBy, sessionID *string
	var createdAt *time.Time
	if err := rows.Scan(&id, &content, &cat, &sourcePath, &createdBy, &createdAt, &sessionID); err != nil {
		return store.MemorySearchResult{}, err
	}
	r := store.MemorySearchResult{
		ID:      id,
		Snippet: content,
		Score:   1.0,
		Scope:   m.scope,
	}
	if cat != nil {
		r.Category = *cat
	}
	if sourcePath != nil {
		r.Path = *sourcePath
	}
	if createdBy != nil {
		r.CreatedBy = *createdBy
	}
	if createdAt != nil {
		r.CreatedAt = createdAt.Format(time.RFC3339)
	}
	if sessionID != nil {
		r.SessionID = *sessionID
	}
	return r, nil
}

// Search performs a hybrid vector + tsvector keyword search.
// When an embedder is available, generates a query embedding and runs
// full RRF fusion (vector + keyword). Otherwise falls back to tsvector-only.
func (m *pgMemoryStore) Search(ctx context.Context, query string, maxResults int, minScore float64) ([]store.MemorySearchResult, error) {
	if m.embedFunc != nil {
		emb, err := m.embedFunc(ctx, query)
		if err == nil && len(emb) > 0 {
			return m.HybridSearch(ctx, emb, query, maxResults, minScore, "", 0.7, 0.3)
		}
		// Embedding failed — fall back to keyword-only
	}
	return m.hybridSearch(ctx, query, maxResults, minScore, "")
}

// SearchByCategory performs a hybrid search filtered by category.
func (m *pgMemoryStore) SearchByCategory(ctx context.Context, query string, maxResults int, minScore float64, category string) ([]store.MemorySearchResult, error) {
	if m.embedFunc != nil {
		emb, err := m.embedFunc(ctx, query)
		if err == nil && len(emb) > 0 {
			return m.HybridSearch(ctx, emb, query, maxResults, minScore, category, 0.7, 0.3)
		}
		// Embedding failed — fall back to keyword-only
	}
	return m.hybridSearch(ctx, query, maxResults, minScore, category)
}

// hybridSearch performs a tsvector full-text search with scoring via ts_rank.
// Uses OR semantics so multi-word queries return partial matches, ranked by
// how many terms match (via ts_rank). This approximates BM25-like behavior.
// Vector search is handled separately via HybridSearch (with embedding).
func (m *pgMemoryStore) hybridSearch(ctx context.Context, query string, maxResults int, minScore float64, category string) ([]store.MemorySearchResult, error) {
	// Build the tsquery with OR semantics:
	// plainto_tsquery produces 'a' & 'b' & 'c' (AND).
	// We convert to 'a' | 'b' | 'c' (OR) so partial matches are returned,
	// ranked by ts_rank (which scores higher when more terms match).
	const orTsquery = `(
		SELECT string_agg(token, ' | ')::tsquery
		FROM unnest(string_to_array(plainto_tsquery('english', $1)::text, ' & ')) AS token
	)`

	var sql string
	var args []any

	if category == "" {
		sql = fmt.Sprintf(
			`SELECT id::text, chunk_text, category, source_path,
			        ts_rank(tsv, %s) AS score
			 FROM %s
			 WHERE tsv @@ %s
			 ORDER BY score DESC
			 LIMIT $2`, orTsquery, m.tableName(), orTsquery)
		args = []any{query, maxResults}
	} else {
		sql = fmt.Sprintf(
			`SELECT id::text, chunk_text, category, source_path,
			        ts_rank(tsv, %s) AS score
			 FROM %s
			 WHERE tsv @@ %s
			   AND category = $3
			 ORDER BY score DESC
			 LIMIT $2`, orTsquery, m.tableName(), orTsquery)
		args = []any{query, maxResults, category}
	}

	rows, err := m.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("memory tsvector search failed: %w", err)
	}
	defer rows.Close()

	var results []store.MemorySearchResult
	for rows.Next() {
		r, err := m.scanSearchRow(rows)
		if err != nil {
			return nil, err
		}
		if r.Score < minScore {
			continue
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// HybridSearch performs a combined vector similarity + tsvector keyword search.
// Results from both are merged using reciprocal rank fusion (RRF).
//
// vectorWeight and keywordWeight control the balance (e.g., 0.7 / 0.3).
func (m *pgMemoryStore) HybridSearch(ctx context.Context, embedding []float32, query string, maxResults int, minScore float64, category string, vectorWeight, keywordWeight float64) ([]store.MemorySearchResult, error) {
	// Run vector + keyword searches in parallel, then merge.
	type searchResult struct {
		results []store.MemorySearchResult
		err     error
	}

	vecCh := make(chan searchResult, 1)
	kwCh := make(chan searchResult, 1)

	// Vector search
	go func() {
		r, err := m.VectorSearch(ctx, embedding, maxResults*2, 0, category)
		vecCh <- searchResult{r, err}
	}()

	// Keyword search (tsvector)
	go func() {
		r, err := m.hybridSearch(ctx, query, maxResults*2, 0, category)
		kwCh <- searchResult{r, err}
	}()

	vecRes := <-vecCh
	kwRes := <-kwCh

	if vecRes.err != nil {
		return nil, vecRes.err
	}
	if kwRes.err != nil {
		return nil, kwRes.err
	}

	// Reciprocal Rank Fusion
	merged := rrfMerge(vecRes.results, kwRes.results, vectorWeight, keywordWeight)

	// Filter by minScore and limit
	var filtered []store.MemorySearchResult
	for _, r := range merged {
		if r.Score >= minScore {
			filtered = append(filtered, r)
		}
		if len(filtered) >= maxResults {
			break
		}
	}
	return filtered, nil
}

// VectorSearch performs a pgvector cosine similarity search.
func (m *pgMemoryStore) VectorSearch(ctx context.Context, embedding []float32, maxResults int, minScore float64, category string) ([]store.MemorySearchResult, error) {
	embStr := float32SliceToString(embedding)
	var sql string
	var args []any

	if category == "" {
		sql = fmt.Sprintf(
			`SELECT id::text, chunk_text, category, source_path, 1 - (embedding <=> $1::vector) AS score
			 FROM %s
			 WHERE embedding IS NOT NULL
			 ORDER BY embedding <=> $1::vector
			 LIMIT $2`, m.tableName())
		args = []any{fmt.Sprintf("[%s]", embStr), maxResults}
	} else {
		sql = fmt.Sprintf(
			`SELECT id::text, chunk_text, category, source_path, 1 - (embedding <=> $1::vector) AS score
			 FROM %s
			 WHERE embedding IS NOT NULL
			   AND category = $3
			 ORDER BY embedding <=> $1::vector
			 LIMIT $2`, m.tableName())
		args = []any{fmt.Sprintf("[%s]", embStr), maxResults, category}
	}

	rows, err := m.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}
	defer rows.Close()

	var results []store.MemorySearchResult
	for rows.Next() {
		r, err := m.scanSearchRow(rows)
		if err != nil {
			return nil, err
		}
		if r.Score < minScore {
			continue
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// Add inserts a memory chunk into the store.
// The tsvector column is auto-populated by the DB trigger.
// When an embedder is available and no embedding is provided, one is generated automatically.
func (m *pgMemoryStore) Add(ctx context.Context, entry store.MemoryEntry) error {
	// Auto-generate embedding if embedder is available and none was provided
	if len(entry.Embedding) == 0 && m.embedFunc != nil {
		if emb, err := m.embedFunc(ctx, entry.Content); err == nil && len(emb) > 0 {
			entry.Embedding = emb
		}
		// Embedding failure is non-fatal — the memory is still stored
		// with tsvector indexing for keyword search.
	}

	var embStr *string
	if len(entry.Embedding) > 0 {
		s := fmt.Sprintf("[%s]", float32SliceToString(entry.Embedding))
		embStr = &s
	}

	metaJSON, _ := json.Marshal(entry.Metadata)

	_, err := m.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (chunk_text, embedding, category, source_path, metadata, %s, session_id)
		 VALUES ($1, $2::vector, $3, $4, $5, $6, $7)`, m.tableName(), m.ownerColumn()),
		entry.Content, embStr, nilIfEmpty(entry.Category), nilIfEmpty(entry.SourcePath), metaJSON, nilIfEmpty(entry.CreatedBy), nilIfEmpty(entry.SessionID),
	)
	return err
}

// Get retrieves a single memory entry by ID.
func (m *pgMemoryStore) Get(ctx context.Context, id string) (*store.MemorySearchResult, error) {
	sql := fmt.Sprintf(
		`SELECT id::text, chunk_text, category, source_path, %s::text, created_at, session_id::text
		 FROM %s WHERE id = $1`, m.ownerColumn(), m.tableName())

	var content string
	var cat, sourcePath, createdBy, sessionID *string
	var createdAt *time.Time
	err := m.pool.QueryRow(ctx, sql, id).Scan(&id, &content, &cat, &sourcePath, &createdBy, &createdAt, &sessionID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("memory get failed: %w", err)
	}

	r := &store.MemorySearchResult{
		ID:      id,
		Snippet: content,
		Score:   1.0,
		Scope:   m.scope,
	}
	if cat != nil {
		r.Category = *cat
	}
	if sourcePath != nil {
		r.Path = *sourcePath
	}
	if createdBy != nil {
		r.CreatedBy = *createdBy
	}
	if createdAt != nil {
		r.CreatedAt = createdAt.Format(time.RFC3339)
	}
	if sessionID != nil {
		r.SessionID = *sessionID
	}
	return r, nil
}

// Update modifies the content and/or category of an existing memory.
// Re-generates the embedding if content changes and an embedder is available.
func (m *pgMemoryStore) Update(ctx context.Context, id string, content string, category string) error {
	var embStr *string
	if m.embedFunc != nil {
		if emb, err := m.embedFunc(ctx, content); err == nil && len(emb) > 0 {
			s := fmt.Sprintf("[%s]", float32SliceToString(emb))
			embStr = &s
		}
	}

	result, err := m.pool.Exec(ctx, fmt.Sprintf(
		`UPDATE %s SET chunk_text = $1, category = $2, embedding = $3::vector WHERE id = $4`, m.tableName()),
		content, nilIfEmpty(category), embStr, id,
	)
	if err != nil {
		return fmt.Errorf("memory update failed: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("memory not found: %s", id)
	}
	return nil
}

// Delete removes a memory chunk by ID.
func (m *pgMemoryStore) Delete(ctx context.Context, id string) error {
	result, err := m.pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE id = $1`, m.tableName()), id)
	if err != nil {
		return fmt.Errorf("memory delete failed: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("memory not found: %s", id)
	}
	return nil
}

// List returns memory chunks, optionally filtered by category.
func (m *pgMemoryStore) List(ctx context.Context, category string, limit, offset int) ([]store.MemorySearchResult, error) {
	var sql string
	var args []any

	if category == "" {
		sql = fmt.Sprintf(
			`SELECT id::text, chunk_text, category, source_path, %s::text, created_at, session_id::text
			 FROM %s
			 ORDER BY created_at DESC
			 LIMIT $1 OFFSET $2`, m.ownerColumn(), m.tableName())
		args = []any{limit, offset}
	} else {
		sql = fmt.Sprintf(
			`SELECT id::text, chunk_text, category, source_path, %s::text, created_at, session_id::text
			 FROM %s
			 WHERE category = $3
			 ORDER BY created_at DESC
			 LIMIT $1 OFFSET $2`, m.ownerColumn(), m.tableName())
		args = []any{limit, offset, category}
	}

	rows, err := m.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("memory list failed: %w", err)
	}
	defer rows.Close()

	var results []store.MemorySearchResult
	for rows.Next() {
		r, err := m.scanDetailRow(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// ListBySession returns all memory chunks created during a specific session.
func (m *pgMemoryStore) ListBySession(ctx context.Context, sessionID string) ([]store.MemorySearchResult, error) {
	sql := fmt.Sprintf(
		`SELECT id::text, chunk_text, category, source_path, %s::text, created_at, session_id::text
		 FROM %s
		 WHERE session_id = $1
		 ORDER BY created_at ASC`, m.ownerColumn(), m.tableName())

	rows, err := m.pool.Query(ctx, sql, sessionID)
	if err != nil {
		return nil, fmt.Errorf("memory list by session failed: %w", err)
	}
	defer rows.Close()

	var results []store.MemorySearchResult
	for rows.Next() {
		r, err := m.scanDetailRow(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// AddMemory inserts a memory chunk into the store (legacy method).
// Prefer Add() for new code.
func (m *pgMemoryStore) AddMemory(ctx context.Context, content, category, sourcePath string, embedding []float32, metadata map[string]any) error {
	return m.Add(ctx, store.MemoryEntry{
		Content:    content,
		Category:   category,
		SourcePath: sourcePath,
		Embedding:  embedding,
		Metadata:   metadata,
	})
}

func (m *pgMemoryStore) Count() int {
	var count int
	err := m.pool.QueryRow(context.Background(), fmt.Sprintf(
		`SELECT count(*) FROM %s`, m.tableName()),
	).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

func (m *pgMemoryStore) Close() error {
	// Pool is managed by PoolManager, not closed here
	return nil
}

// rrfMerge merges two result sets using Reciprocal Rank Fusion.
// Each result set is ranked independently, and scores are combined as:
//
//	score = weight1 * 1/(k+rank1) + weight2 * 1/(k+rank2)
//
// where k=60 is the standard RRF constant.
func rrfMerge(vecResults, kwResults []store.MemorySearchResult, vecWeight, kwWeight float64) []store.MemorySearchResult {
	const k = 60.0

	type mergedItem struct {
		result store.MemorySearchResult
		score  float64
	}

	// Index by snippet (dedup key)
	bySnippet := make(map[string]*mergedItem)

	for rank, r := range vecResults {
		key := r.Snippet
		if item, ok := bySnippet[key]; ok {
			item.score += vecWeight * (1.0 / (k + float64(rank+1)))
		} else {
			bySnippet[key] = &mergedItem{
				result: r,
				score:  vecWeight * (1.0 / (k + float64(rank+1))),
			}
		}
	}

	for rank, r := range kwResults {
		key := r.Snippet
		if item, ok := bySnippet[key]; ok {
			item.score += kwWeight * (1.0 / (k + float64(rank+1)))
		} else {
			bySnippet[key] = &mergedItem{
				result: r,
				score:  kwWeight * (1.0 / (k + float64(rank+1))),
			}
		}
	}

	// Collect and sort by merged score
	items := make([]mergedItem, 0, len(bySnippet))
	for _, item := range bySnippet {
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].score > items[j].score
	})

	results := make([]store.MemorySearchResult, len(items))
	for i, item := range items {
		item.result.Score = roundScore(item.score)
		results[i] = item.result
	}
	return results
}

// roundScore rounds to 6 decimal places for clean output.
func roundScore(f float64) float64 {
	return math.Round(f*1e6) / 1e6
}

// float32SliceToString converts a float32 slice to a comma-separated string.
func float32SliceToString(v []float32) string {
	if len(v) == 0 {
		return ""
	}
	result := ""
	for i, f := range v {
		if i > 0 {
			result += ","
		}
		result += fmt.Sprintf("%f", f)
	}
	return result
}
