package entstore

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"

	orgent "github.com/schardosin/astonish/ent/org"
	"github.com/schardosin/astonish/ent/org/orgmemory"
	"github.com/schardosin/astonish/pkg/store"
)

// orgMemoryStore implements store.MemoryStore for org-level memories.
type orgMemoryStore struct {
	client    *orgent.Client
	db        *sql.DB
	dialect   Dialect
	embedFunc store.EmbedFunc
}

var _ store.MemoryStore = (*orgMemoryStore)(nil)

func (ms *orgMemoryStore) Search(ctx context.Context, query string, maxResults int, minScore float64) ([]store.MemorySearchResult, error) {
	return ms.SearchByCategory(ctx, query, maxResults, minScore, "")
}

func (ms *orgMemoryStore) SearchByCategory(ctx context.Context, query string, maxResults int, minScore float64, category string) ([]store.MemorySearchResult, error) {
	// Simple keyword search using LIKE as fallback.
	q := ms.client.OrgMemory.Query()
	if category != "" {
		q = q.Where(orgmemory.CategoryEQ(category))
	}
	q = q.Limit(maxResults)

	ents, err := q.All(ctx)
	if err != nil {
		return nil, err
	}

	results := make([]store.MemorySearchResult, 0, len(ents))
	for _, e := range ents {
		r := orgMemoryToSearchResult(e)
		if query == "" || strings.Contains(strings.ToLower(e.ChunkText), strings.ToLower(query)) {
			r.Score = 1.0
			results = append(results, r)
		}
	}
	return results, nil
}

func (ms *orgMemoryStore) Add(ctx context.Context, entry store.MemoryEntry) error {
	create := ms.client.OrgMemory.Create().
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

	// Set promoted_by (required field) — use CreatedBy or a nil UUID.
	if entry.CreatedBy != "" {
		uid, err := uuid.Parse(entry.CreatedBy)
		if err == nil {
			create.SetPromotedBy(uid)
		} else {
			create.SetPromotedBy(uuid.Nil)
		}
	} else {
		create.SetPromotedBy(uuid.Nil)
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
			`UPDATE org_memories SET embedding = $1::vector WHERE id = $2`,
			vecStr, saved.ID)
		if err != nil {
			return fmt.Errorf("set embedding: %w", err)
		}
	}

	return nil
}

func (ms *orgMemoryStore) Get(ctx context.Context, id string) (*store.MemorySearchResult, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid memory ID: %w", err)
	}
	e, err := ms.client.OrgMemory.Get(ctx, uid)
	if err != nil {
		if orgent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	r := orgMemoryToSearchResult(e)
	return &r, nil
}

func (ms *orgMemoryStore) Update(ctx context.Context, id string, content string, category string) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid memory ID: %w", err)
	}

	update := ms.client.OrgMemory.UpdateOneID(uid).
		SetChunkText(content)

	if category != "" {
		update.SetCategory(category)
	} else {
		update.ClearCategory()
	}

	// Re-generate embedding if content changed.
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
			`UPDATE org_memories SET embedding = $1::vector WHERE id = $2`,
			vecStr, uid)
		if err != nil {
			return fmt.Errorf("set embedding: %w", err)
		}
	}

	return nil
}

func (ms *orgMemoryStore) Delete(ctx context.Context, id string) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid memory ID: %w", err)
	}
	return ms.client.OrgMemory.DeleteOneID(uid).Exec(ctx)
}

func (ms *orgMemoryStore) List(ctx context.Context, category string, limit, offset int) ([]store.MemorySearchResult, error) {
	q := ms.client.OrgMemory.Query()
	if category != "" {
		q = q.Where(orgmemory.CategoryEQ(category))
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	if offset > 0 {
		q = q.Offset(offset)
	}
	q = q.Order(orgmemory.ByCreatedAt())

	ents, err := q.All(ctx)
	if err != nil {
		return nil, err
	}

	results := make([]store.MemorySearchResult, len(ents))
	for i, e := range ents {
		results[i] = orgMemoryToSearchResult(e)
	}
	return results, nil
}

func (ms *orgMemoryStore) ListBySession(ctx context.Context, sessionID string) ([]store.MemorySearchResult, error) {
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return nil, fmt.Errorf("invalid session ID: %w", err)
	}

	ents, err := ms.client.OrgMemory.Query().
		Where(orgmemory.SessionIDEQ(sid)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	results := make([]store.MemorySearchResult, len(ents))
	for i, e := range ents {
		results[i] = orgMemoryToSearchResult(e)
	}
	return results, nil
}

func (ms *orgMemoryStore) Count() int {
	count, _ := ms.client.OrgMemory.Query().Count(context.Background())
	return count
}

func (ms *orgMemoryStore) Close() error {
	return nil
}

func orgMemoryToSearchResult(e *orgent.OrgMemory) store.MemorySearchResult {
	r := store.MemorySearchResult{
		ID:        e.ID.String(),
		Snippet:   e.ChunkText,
		Scope:     "org",
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
	r.CreatedBy = e.PromotedBy.String()
	return r
}

// float32SliceToBytes converts a []float32 to []byte for SQLite BLOB storage.
func float32SliceToBytes(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// float32SliceToPGVectorString converts a []float32 to pgvector text format string: [f1,f2,...,fN]
// This format is passed as a string parameter with ::vector cast in SQL queries.
func float32SliceToPGVectorString(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(fmt.Sprintf("%g", f))
	}
	b.WriteByte(']')
	return b.String()
}
