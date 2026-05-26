package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/store"
)

// SQLiteToolVectorStore implements agent.ToolVectorStore using the tool_index
// table in the platform SQLite database. Embeddings are stored as BLOBs
// (binary-encoded []float32) and nearest-neighbor search is performed in Go
// using brute-force cosine similarity. This is acceptable for tool discovery
// where the corpus is small (typically <500 tool descriptions).
type SQLiteToolVectorStore struct {
	db        *sql.DB
	embedFunc store.EmbedFunc
	count     atomic.Int64
}

// NewSQLiteToolVectorStore creates a ToolVectorStore backed by SQLite.
func NewSQLiteToolVectorStore(db *sql.DB, embedFunc store.EmbedFunc) (agent.ToolVectorStore, error) {
	if db == nil {
		return nil, agent.ErrNilDB
	}
	if embedFunc == nil {
		return nil, agent.ErrNilEmbedFunc
	}

	s := &SQLiteToolVectorStore{
		db:        db,
		embedFunc: embedFunc,
	}

	// Initialize count from DB
	var cnt int64
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM tool_index`).Scan(&cnt); err == nil {
		s.count.Store(cnt)
	}

	return s, nil
}

func (s *SQLiteToolVectorStore) AddDocuments(ctx context.Context, docs []agent.ToolVectorDoc, concurrency int) error {
	if len(docs) == 0 {
		return nil
	}
	if concurrency <= 0 {
		concurrency = 4
	}

	type embeddedDoc struct {
		doc       agent.ToolVectorDoc
		embedding []float32
		err       error
	}

	results := make([]embeddedDoc, len(docs))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, doc := range docs {
		results[i].doc = doc
		wg.Add(1)
		go func(idx int, d agent.ToolVectorDoc) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			emb, err := s.embedFunc(ctx, d.Content)
			results[idx].embedding = emb
			results[idx].err = err
		}(i, doc)
	}
	wg.Wait()

	// Batch upsert in a transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO tool_index (id, content, embedding, metadata, updated_at)
		 VALUES (?, ?, ?, ?, datetime('now'))
		 ON CONFLICT(id) DO UPDATE
		 SET content = excluded.content,
		     embedding = excluded.embedding,
		     metadata = excluded.metadata,
		     updated_at = datetime('now')`)
	if err != nil {
		return fmt.Errorf("prepare upsert: %w", err)
	}
	defer stmt.Close()

	for _, r := range results {
		if r.err != nil {
			continue // skip failed embeddings (non-fatal)
		}

		var embBlob []byte
		if len(r.embedding) > 0 {
			embBlob = serializeEmbedding(r.embedding)
		}

		metaJSON, _ := json.Marshal(r.doc.Metadata)

		if _, err := stmt.ExecContext(ctx, r.doc.ID, r.doc.Content, embBlob, string(metaJSON)); err != nil {
			return fmt.Errorf("upsert tool %q: %w", r.doc.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	// Refresh count
	var cnt int64
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM tool_index`).Scan(&cnt); err == nil {
		s.count.Store(cnt)
	}

	return nil
}

func (s *SQLiteToolVectorStore) QueryByEmbedding(ctx context.Context, queryEmbedding []float32, topK int) ([]agent.ToolVectorResult, error) {
	if len(queryEmbedding) == 0 {
		return nil, nil
	}
	if topK <= 0 {
		topK = 10
	}

	// Load all documents with embeddings and compute cosine similarity in Go
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, content, metadata, embedding FROM tool_index WHERE embedding IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("query tool_index: %w", err)
	}
	defer rows.Close()

	type scored struct {
		doc        agent.ToolVectorDoc
		similarity float32
	}

	var all []scored
	for rows.Next() {
		var id, content string
		var metaJSON []byte
		var embBlob []byte

		if err := rows.Scan(&id, &content, &metaJSON, &embBlob); err != nil {
			return nil, fmt.Errorf("scan tool row: %w", err)
		}

		embedding := deserializeEmbedding(embBlob)
		if len(embedding) == 0 {
			continue
		}

		sim := float32(cosineSimilarity(queryEmbedding, embedding))

		metadata := make(map[string]string)
		if len(metaJSON) > 0 {
			_ = json.Unmarshal(metaJSON, &metadata)
		}

		all = append(all, scored{
			doc: agent.ToolVectorDoc{
				ID:       id,
				Content:  content,
				Metadata: metadata,
			},
			similarity: sim,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort by similarity descending and take topK
	sort.Slice(all, func(i, j int) bool {
		return all[i].similarity > all[j].similarity
	})
	if len(all) > topK {
		all = all[:topK]
	}

	results := make([]agent.ToolVectorResult, len(all))
	for i, s := range all {
		results[i] = agent.ToolVectorResult{
			ToolVectorDoc: s.doc,
			Similarity:    s.similarity,
		}
	}
	return results, nil
}

func (s *SQLiteToolVectorStore) GetByID(ctx context.Context, id string) (*agent.ToolVectorDoc, error) {
	var content string
	var metaJSON []byte

	err := s.db.QueryRowContext(ctx,
		`SELECT content, metadata FROM tool_index WHERE id = ?`, id,
	).Scan(&content, &metaJSON)
	if err != nil {
		return nil, nil // not found or error — same semantics as chromem adapter
	}

	metadata := make(map[string]string)
	if len(metaJSON) > 0 {
		_ = json.Unmarshal(metaJSON, &metadata)
	}

	return &agent.ToolVectorDoc{
		ID:       id,
		Content:  content,
		Metadata: metadata,
	}, nil
}

func (s *SQLiteToolVectorStore) Count() int {
	return int(s.count.Load())
}

// Compile-time interface check
var _ agent.ToolVectorStore = (*SQLiteToolVectorStore)(nil)
