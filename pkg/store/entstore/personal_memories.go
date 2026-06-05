package entstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

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
	q := ms.client.Memory.Query()
	if category != "" {
		q = q.Where(memory.CategoryEQ(category))
	}
	q = q.Limit(maxResults)

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
