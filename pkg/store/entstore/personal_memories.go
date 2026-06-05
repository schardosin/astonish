package entstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	personalent "github.com/schardosin/astonish/ent/personal"
	"github.com/schardosin/astonish/ent/personal/memory"
	"github.com/schardosin/astonish/pkg/store"
)

// personalMemoryStore implements store.MemoryStore for personal scope.
type personalMemoryStore struct {
	client    *personalent.Client
	db        *sql.DB
	dialect   Dialect
	embedFunc store.EmbedFunc
}

var _ store.MemoryStore = (*personalMemoryStore)(nil)

func (ms *personalMemoryStore) Search(ctx context.Context, query string, maxResults int, minScore float64) ([]store.MemorySearchResult, error) {
	return ms.SearchByCategory(ctx, query, maxResults, minScore, "")
}

func (ms *personalMemoryStore) SearchByCategory(ctx context.Context, query string, maxResults int, minScore float64, category string) ([]store.MemorySearchResult, error) {
	if ms.dialect == DialectPostgres {
		return ms.hybridSearch(ctx, query, maxResults, minScore, category)
	}
	// SQLite: text-based fallback.
	return ms.textSearch(ctx, query, maxResults, category)
}

func (ms *personalMemoryStore) hybridSearch(ctx context.Context, query string, maxResults int, minScore float64, category string) ([]store.MemorySearchResult, error) {
	var vectorResults, keywordResults []store.MemorySearchResult

	// 1. Vector search (semantic similarity).
	if ms.embedFunc != nil {
		results, err := ms.vectorSearch(ctx, query, maxResults, minScore, category)
		if err == nil {
			vectorResults = results
		}
	}

	// 2. tsvector search (keyword matching).
	if query != "" {
		results, err := ms.tsvectorSearch(ctx, query, maxResults, category)
		if err == nil {
			keywordResults = results
		}
	}

	// 3. Merge + dedup by ID, keep best score.
	return mergeMemoryResults(vectorResults, keywordResults, maxResults), nil
}

func (ms *personalMemoryStore) vectorSearch(ctx context.Context, query string, maxResults int, minScore float64, category string) ([]store.MemorySearchResult, error) {
	embedding, err := ms.embedFunc(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("generate query embedding: %w", err)
	}

	vecStr := float32SliceToPGVectorString(embedding)

	var rows *sql.Rows
	if category != "" {
		rows, err = ms.db.QueryContext(ctx,
			`SELECT id, chunk_text, category, source_path, created_by, session_id, created_at,
				1 - (embedding <=> $1::vector) AS score
			FROM memories
			WHERE category = $2
			ORDER BY embedding <=> $1::vector
			LIMIT $3`,
			vecStr, category, maxResults)
	} else {
		rows, err = ms.db.QueryContext(ctx,
			`SELECT id, chunk_text, category, source_path, created_by, session_id, created_at,
				1 - (embedding <=> $1::vector) AS score
			FROM memories
			ORDER BY embedding <=> $1::vector
			LIMIT $2`,
			vecStr, maxResults)
	}
	if err != nil {
		return nil, fmt.Errorf("vector search query: %w", err)
	}
	defer rows.Close()

	var results []store.MemorySearchResult
	for rows.Next() {
		var (
			id        uuid.UUID
			chunkText string
			cat       sql.NullString
			srcPath   sql.NullString
			createdBy sql.NullString
			sessionID sql.NullString
			createdAt time.Time
			score     float64
		)
		if err := rows.Scan(&id, &chunkText, &cat, &srcPath, &createdBy, &sessionID, &createdAt, &score); err != nil {
			continue
		}
		if score < minScore {
			continue
		}
		r := store.MemorySearchResult{
			ID:        id.String(),
			Snippet:   chunkText,
			Score:     score,
			Scope:     "personal",
			CreatedAt: createdAt.Format(time.RFC3339),
		}
		if cat.Valid {
			r.Category = cat.String
		}
		if srcPath.Valid {
			r.Path = srcPath.String
		}
		if createdBy.Valid {
			r.CreatedBy = createdBy.String
		}
		if sessionID.Valid {
			r.SessionID = sessionID.String
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func (ms *personalMemoryStore) tsvectorSearch(ctx context.Context, query string, maxResults int, category string) ([]store.MemorySearchResult, error) {
	orQuery := buildORTsquery(query)
	if orQuery == "" {
		return nil, nil
	}

	var rows *sql.Rows
	var err error
	if category != "" {
		rows, err = ms.db.QueryContext(ctx,
			`SELECT id, chunk_text, category, source_path, created_by, session_id, created_at,
				ts_rank(tsv, websearch_to_tsquery('english', $1)) AS score
			FROM memories
			WHERE tsv @@ websearch_to_tsquery('english', $1) AND category = $2
			ORDER BY score DESC
			LIMIT $3`,
			orQuery, category, maxResults)
	} else {
		rows, err = ms.db.QueryContext(ctx,
			`SELECT id, chunk_text, category, source_path, created_by, session_id, created_at,
				ts_rank(tsv, websearch_to_tsquery('english', $1)) AS score
			FROM memories
			WHERE tsv @@ websearch_to_tsquery('english', $1)
			ORDER BY score DESC
			LIMIT $2`,
			orQuery, maxResults)
	}
	if err != nil {
		return nil, fmt.Errorf("tsvector search query: %w", err)
	}
	defer rows.Close()

	var results []store.MemorySearchResult
	for rows.Next() {
		var (
			id        uuid.UUID
			chunkText string
			cat       sql.NullString
			srcPath   sql.NullString
			createdBy sql.NullString
			sessionID sql.NullString
			createdAt time.Time
			score     float64
		)
		if err := rows.Scan(&id, &chunkText, &cat, &srcPath, &createdBy, &sessionID, &createdAt, &score); err != nil {
			continue
		}
		// Apply floor score (see tsvectorFloorScore doc in team_memories.go).
		if score < tsvectorFloorScore {
			score = tsvectorFloorScore
		}
		r := store.MemorySearchResult{
			ID:        id.String(),
			Snippet:   chunkText,
			Score:     score,
			Scope:     "personal",
			CreatedAt: createdAt.Format(time.RFC3339),
		}
		if cat.Valid {
			r.Category = cat.String
		}
		if srcPath.Valid {
			r.Path = srcPath.String
		}
		if createdBy.Valid {
			r.CreatedBy = createdBy.String
		}
		if sessionID.Valid {
			r.SessionID = sessionID.String
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func (ms *personalMemoryStore) textSearch(ctx context.Context, query string, maxResults int, category string) ([]store.MemorySearchResult, error) {
	q := ms.client.Memory.Query()
	if category != "" {
		q = q.Where(memory.CategoryEQ(category))
	}
	q = q.Limit(maxResults).Order(memory.ByCreatedAt())

	ents, err := q.All(ctx)
	if err != nil {
		return nil, err
	}

	results := make([]store.MemorySearchResult, 0, len(ents))
	for _, e := range ents {
		r := personalMemoryToSearchResult(e)
		if query == "" || strings.Contains(strings.ToLower(e.ChunkText), strings.ToLower(query)) {
			r.Score = 1.0
			results = append(results, r)
		}
	}
	return results, nil
}

func (ms *personalMemoryStore) Add(ctx context.Context, entry store.MemoryEntry) error {
	create := ms.client.Memory.Create().
		SetChunkText(entry.Content)

	if entry.Category != "" {
		create.SetCategory(entry.Category)
	}
	if entry.SourcePath != "" {
		create.SetSourcePath(entry.SourcePath)
	}
	if entry.Metadata != nil {
		create.SetMetadata(entry.Metadata)
	}
	if entry.SessionID != "" {
		sid, err := uuid.Parse(entry.SessionID)
		if err == nil {
			create.SetSessionID(sid)
		}
	}
	if entry.CreatedBy != "" {
		uid, err := uuid.Parse(entry.CreatedBy)
		if err == nil {
			create.SetCreatedBy(uid)
		}
	}

	// Generate embedding if embed function is available.
	var newEmb []float32
	if ms.embedFunc != nil && entry.Content != "" {
		embedding, err := ms.embedFunc(ctx, entry.Content)
		if err == nil && len(embedding) > 0 {
			newEmb = embedding
			if ms.dialect == DialectSQLite {
				create.SetEmbedding(float32SliceToBytes(embedding))
			}
		}
	}

	// On SQLite, store raw text for LIKE-based keyword search.
	// On PG, the tsvector trigger generates this from chunk_text.
	if ms.dialect == DialectSQLite {
		create.SetTsv(entry.Content)
	}

	saved, err := create.Save(ctx)
	if err != nil {
		return err
	}

	// On PG, update embedding via raw SQL with vector text format.
	if newEmb != nil && ms.dialect == DialectPostgres {
		vecStr := float32SliceToPGVectorString(newEmb)
		_, err = ms.db.ExecContext(ctx,
			`UPDATE memories SET embedding = $1::vector WHERE id = $2`,
			vecStr, saved.ID)
		if err != nil {
			return fmt.Errorf("set embedding: %w", err)
		}
	}

	return nil
}

func (ms *personalMemoryStore) Get(ctx context.Context, id string) (*store.MemorySearchResult, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid memory ID: %w", err)
	}
	e, err := ms.client.Memory.Get(ctx, uid)
	if err != nil {
		if personalent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	r := personalMemoryToSearchResult(e)
	return &r, nil
}

func (ms *personalMemoryStore) Update(ctx context.Context, id string, content string, category string) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid memory ID: %w", err)
	}

	update := ms.client.Memory.UpdateOneID(uid).
		SetChunkText(content)

	if category != "" {
		update.SetCategory(category)
	} else {
		update.ClearCategory()
	}

	// Re-generate embedding.
	var newEmb []float32
	if ms.embedFunc != nil && content != "" {
		embedding, err := ms.embedFunc(ctx, content)
		if err == nil && len(embedding) > 0 {
			newEmb = embedding
			if ms.dialect == DialectSQLite {
				update.SetEmbedding(float32SliceToBytes(embedding))
			}
		}
	}

	if ms.dialect == DialectSQLite {
		update.SetTsv(content)
	}

	if err := update.Exec(ctx); err != nil {
		return err
	}

	// On PG, update embedding via raw SQL with vector text format.
	if newEmb != nil && ms.dialect == DialectPostgres {
		vecStr := float32SliceToPGVectorString(newEmb)
		_, err = ms.db.ExecContext(ctx,
			`UPDATE memories SET embedding = $1::vector WHERE id = $2`,
			vecStr, uid)
		if err != nil {
			return fmt.Errorf("set embedding: %w", err)
		}
	}

	return nil
}

func (ms *personalMemoryStore) Delete(ctx context.Context, id string) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid memory ID: %w", err)
	}
	return ms.client.Memory.DeleteOneID(uid).Exec(ctx)
}

func (ms *personalMemoryStore) List(ctx context.Context, category string, limit, offset int) ([]store.MemorySearchResult, error) {
	q := ms.client.Memory.Query()
	if category != "" {
		q = q.Where(memory.CategoryEQ(category))
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	if offset > 0 {
		q = q.Offset(offset)
	}
	q = q.Order(memory.ByCreatedAt())

	ents, err := q.All(ctx)
	if err != nil {
		return nil, err
	}

	results := make([]store.MemorySearchResult, len(ents))
	for i, e := range ents {
		results[i] = personalMemoryToSearchResult(e)
	}
	return results, nil
}

func (ms *personalMemoryStore) ListBySession(ctx context.Context, sessionID string) ([]store.MemorySearchResult, error) {
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return nil, fmt.Errorf("invalid session ID: %w", err)
	}

	ents, err := ms.client.Memory.Query().
		Where(memory.SessionIDEQ(sid)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	results := make([]store.MemorySearchResult, len(ents))
	for i, e := range ents {
		results[i] = personalMemoryToSearchResult(e)
	}
	return results, nil
}

func (ms *personalMemoryStore) Count() int {
	count, _ := ms.client.Memory.Query().Count(context.Background())
	return count
}

func (ms *personalMemoryStore) Close() error {
	return nil
}

func personalMemoryToSearchResult(e *personalent.Memory) store.MemorySearchResult {
	r := store.MemorySearchResult{
		ID:        e.ID.String(),
		Snippet:   e.ChunkText,
		Scope:     "personal",
		CreatedAt: e.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if e.Category != nil {
		r.Category = *e.Category
	}
	if e.SourcePath != nil {
		r.Path = *e.SourcePath
	}
	if e.SessionID != nil {
		r.SessionID = e.SessionID.String()
	}
	if e.CreatedBy != nil {
		r.CreatedBy = e.CreatedBy.String()
	}
	return r
}
