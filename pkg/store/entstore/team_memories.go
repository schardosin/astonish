package entstore

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	teament "github.com/schardosin/astonish/ent/team"
	"github.com/schardosin/astonish/ent/team/memory"
	"github.com/schardosin/astonish/pkg/store"
)

// teamMemoryStore implements store.MemoryStore backed by the team Ent client.
type teamMemoryStore struct {
	client    *teament.Client
	db        *sql.DB
	dialect   Dialect
	embedFunc store.EmbedFunc
}

var _ store.MemoryStore = (*teamMemoryStore)(nil)

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

func (m *teamMemoryStore) Search(ctx context.Context, query string, maxResults int, minScore float64) ([]store.MemorySearchResult, error) {
	return m.searchInternal(ctx, query, maxResults, minScore, "")
}

func (m *teamMemoryStore) SearchByCategory(ctx context.Context, query string, maxResults int, minScore float64, category string) ([]store.MemorySearchResult, error) {
	return m.searchInternal(ctx, query, maxResults, minScore, category)
}

func (m *teamMemoryStore) searchInternal(ctx context.Context, query string, maxResults int, minScore float64, category string) ([]store.MemorySearchResult, error) {
	// Try vector search if embedding function is available and we're on postgres.
	if m.embedFunc != nil && m.dialect == DialectPostgres {
		results, err := m.vectorSearch(ctx, query, maxResults, minScore, category)
		if err == nil {
			return results, nil
		}
		// Fall through to text search on error.
	}

	// Fallback: text-based search using LIKE.
	return m.textSearch(ctx, query, maxResults, category)
}

func (m *teamMemoryStore) vectorSearch(ctx context.Context, query string, maxResults int, minScore float64, category string) ([]store.MemorySearchResult, error) {
	embedding, err := m.embedFunc(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("generate query embedding: %w", err)
	}

	embBytes := float32SliceToBytes(embedding)

	var rows *sql.Rows
	if category != "" {
		rows, err = m.db.QueryContext(ctx,
			`SELECT id, chunk_text, category, source_path, created_by, session_id, created_at,
				1 - (embedding <=> $1::vector) AS score
			FROM memories
			WHERE category = $2
			ORDER BY embedding <=> $1::vector
			LIMIT $3`,
			embBytes, category, maxResults)
	} else {
		rows, err = m.db.QueryContext(ctx,
			`SELECT id, chunk_text, category, source_path, created_by, session_id, created_at,
				1 - (embedding <=> $1::vector) AS score
			FROM memories
			ORDER BY embedding <=> $1::vector
			LIMIT $2`,
			embBytes, maxResults)
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

func (m *teamMemoryStore) textSearch(ctx context.Context, query string, maxResults int, category string) ([]store.MemorySearchResult, error) {
	q := m.client.Memory.Query().
		Limit(maxResults).
		Order(memory.ByCreatedAt())

	if query != "" {
		q = q.Where(memory.ChunkTextContainsFold(query))
	}
	if category != "" {
		q = q.Where(memory.CategoryEQ(category))
	}

	ents, err := q.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("text search: %w", err)
	}

	results := make([]store.MemorySearchResult, 0, len(ents))
	for _, e := range ents {
		results = append(results, entMemoryToResult(e))
	}
	return results, nil
}

// ---------------------------------------------------------------------------
// CRUD
// ---------------------------------------------------------------------------

func (m *teamMemoryStore) Add(ctx context.Context, entry store.MemoryEntry) error {
	create := m.client.Memory.Create().
		SetChunkText(entry.Content).
		SetNillableCategory(nilStrPtr(entry.Category)).
		SetNillableSourcePath(nilStrPtr(entry.SourcePath))

	if entry.Metadata != nil {
		create.SetMetadata(entry.Metadata)
	}

	if entry.CreatedBy != "" {
		uid, err := uuid.Parse(entry.CreatedBy)
		if err == nil {
			create.SetCreatedBy(uid)
		}
	}

	if entry.SessionID != "" {
		sid, err := uuid.Parse(entry.SessionID)
		if err == nil {
			create.SetSessionID(sid)
		}
	}

	// Generate embedding if function is available.
	if m.embedFunc != nil && entry.Embedding == nil {
		emb, err := m.embedFunc(ctx, entry.Content)
		if err == nil {
			entry.Embedding = emb
		}
	}

	if entry.Embedding != nil {
		create.SetEmbedding(float32SliceToBytes(entry.Embedding))
	}

	// Set TSV for keyword search (store raw text for now).
	create.SetTsv(entry.Content)

	_, err := create.Save(ctx)
	return err
}

func (m *teamMemoryStore) Get(ctx context.Context, id string) (*store.MemorySearchResult, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid memory ID: %w", err)
	}

	ent, err := m.client.Memory.Get(ctx, uid)
	if err != nil {
		if teament.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get memory: %w", err)
	}

	r := entMemoryToResult(ent)
	return &r, nil
}

func (m *teamMemoryStore) Update(ctx context.Context, id string, content string, category string) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid memory ID: %w", err)
	}

	update := m.client.Memory.UpdateOneID(uid).
		SetChunkText(content).
		SetTsv(content)

	if category != "" {
		update.SetCategory(category)
	} else {
		update.ClearCategory()
	}

	// Re-generate embedding if function is available.
	if m.embedFunc != nil {
		emb, err := m.embedFunc(ctx, content)
		if err == nil {
			update.SetEmbedding(float32SliceToBytes(emb))
		}
	}

	return update.Exec(ctx)
}

func (m *teamMemoryStore) Delete(ctx context.Context, id string) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid memory ID: %w", err)
	}
	return m.client.Memory.DeleteOneID(uid).Exec(ctx)
}

func (m *teamMemoryStore) List(ctx context.Context, category string, limit, offset int) ([]store.MemorySearchResult, error) {
	q := m.client.Memory.Query().
		Limit(limit).
		Offset(offset).
		Order(memory.ByCreatedAt())

	if category != "" {
		q = q.Where(memory.CategoryEQ(category))
	}

	ents, err := q.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}

	results := make([]store.MemorySearchResult, 0, len(ents))
	for _, e := range ents {
		results = append(results, entMemoryToResult(e))
	}
	return results, nil
}

func (m *teamMemoryStore) ListBySession(ctx context.Context, sessionID string) ([]store.MemorySearchResult, error) {
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return nil, fmt.Errorf("invalid session ID: %w", err)
	}

	ents, err := m.client.Memory.Query().
		Where(memory.SessionIDEQ(sid)).
		Order(memory.ByCreatedAt()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list by session: %w", err)
	}

	results := make([]store.MemorySearchResult, 0, len(ents))
	for _, e := range ents {
		results = append(results, entMemoryToResult(e))
	}
	return results, nil
}

func (m *teamMemoryStore) Count() int {
	n, err := m.client.Memory.Query().Count(context.Background())
	if err != nil {
		return 0
	}
	return n
}

func (m *teamMemoryStore) Close() error {
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// entMemoryToResult converts a team Memory entity to store.MemorySearchResult.
func entMemoryToResult(e *teament.Memory) store.MemorySearchResult {
	r := store.MemorySearchResult{
		ID:        e.ID.String(),
		Snippet:   e.ChunkText,
		CreatedAt: e.CreatedAt.Format(time.RFC3339),
	}
	if e.Category != nil {
		r.Category = *e.Category
	}
	if e.SourcePath != nil {
		r.Path = *e.SourcePath
	}
	if e.CreatedBy != nil {
		r.CreatedBy = e.CreatedBy.String()
	}
	if e.SessionID != nil {
		r.SessionID = e.SessionID.String()
	}
	return r
}


