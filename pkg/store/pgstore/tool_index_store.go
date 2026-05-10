package pgstore

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/store"
)

// PGToolVectorStore implements agent.ToolVectorStore using pgvector in PostgreSQL.
// Used in platform mode where all data lives in the platform database.
type PGToolVectorStore struct {
	pool      *pgxpool.Pool
	embedFunc store.EmbedFunc
	count     atomic.Int64
}

// NewPGToolVectorStore creates a ToolVectorStore backed by pgvector.
// The pool should be connected to the platform database.
// The embedFunc generates embeddings for tool descriptions.
func NewPGToolVectorStore(pool *pgxpool.Pool, embedFunc store.EmbedFunc) (agent.ToolVectorStore, error) {
	if pool == nil {
		return nil, agent.ErrNilDB
	}
	if embedFunc == nil {
		return nil, agent.ErrNilEmbedFunc
	}

	s := &PGToolVectorStore{
		pool:      pool,
		embedFunc: embedFunc,
	}

	// Initialize count from DB
	ctx := context.Background()
	var cnt int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tool_index`).Scan(&cnt); err == nil {
		s.count.Store(cnt)
	}

	return s, nil
}

func (s *PGToolVectorStore) AddDocuments(ctx context.Context, docs []agent.ToolVectorDoc, concurrency int) error {
	if len(docs) == 0 {
		return nil
	}
	if concurrency <= 0 {
		concurrency = 4
	}

	// Generate embeddings concurrently, then batch upsert.
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

	// Batch upsert (one transaction)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, r := range results {
		if r.err != nil {
			// Skip documents that failed embedding (non-fatal, same as memory stores)
			continue
		}

		var embStr *string
		if len(r.embedding) > 0 {
			v := fmt.Sprintf("[%s]", float32SliceToVectorString(r.embedding))
			embStr = &v
		}

		metaJSON, _ := json.Marshal(r.doc.Metadata)

		_, err := tx.Exec(ctx,
			`INSERT INTO tool_index (id, content, embedding, metadata, updated_at)
			 VALUES ($1, $2, $3::vector, $4, now())
			 ON CONFLICT (id) DO UPDATE
			 SET content = EXCLUDED.content,
			     embedding = EXCLUDED.embedding,
			     metadata = EXCLUDED.metadata,
			     updated_at = now()`,
			r.doc.ID, r.doc.Content, embStr, metaJSON,
		)
		if err != nil {
			return fmt.Errorf("upsert tool %q: %w", r.doc.ID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	// Refresh count
	var cnt int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tool_index`).Scan(&cnt); err == nil {
		s.count.Store(cnt)
	}

	return nil
}

func (s *PGToolVectorStore) QueryByEmbedding(ctx context.Context, queryEmbedding []float32, topK int) ([]agent.ToolVectorResult, error) {
	if len(queryEmbedding) == 0 {
		return nil, nil
	}
	if topK <= 0 {
		topK = 10
	}

	embStr := fmt.Sprintf("[%s]", float32SliceToVectorString(queryEmbedding))

	rows, err := s.pool.Query(ctx,
		`SELECT id, content, metadata, 1 - (embedding <=> $1::vector) AS similarity
		 FROM tool_index
		 WHERE embedding IS NOT NULL
		 ORDER BY embedding <=> $1::vector
		 LIMIT $2`,
		embStr, topK,
	)
	if err != nil {
		return nil, fmt.Errorf("tool vector search: %w", err)
	}
	defer rows.Close()

	var results []agent.ToolVectorResult
	for rows.Next() {
		var id, content string
		var metaJSON []byte
		var similarity float32

		if err := rows.Scan(&id, &content, &metaJSON, &similarity); err != nil {
			return nil, fmt.Errorf("scan tool result: %w", err)
		}

		metadata := make(map[string]string)
		if len(metaJSON) > 0 {
			_ = json.Unmarshal(metaJSON, &metadata)
		}

		results = append(results, agent.ToolVectorResult{
			ToolVectorDoc: agent.ToolVectorDoc{
				ID:       id,
				Content:  content,
				Metadata: metadata,
			},
			Similarity: similarity,
		})
	}

	return results, rows.Err()
}

func (s *PGToolVectorStore) GetByID(ctx context.Context, id string) (*agent.ToolVectorDoc, error) {
	var content string
	var metaJSON []byte

	err := s.pool.QueryRow(ctx,
		`SELECT content, metadata FROM tool_index WHERE id = $1`, id,
	).Scan(&content, &metaJSON)
	if err != nil {
		// Not found or other error — return nil (same semantics as chromem adapter)
		return nil, nil
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

func (s *PGToolVectorStore) Count() int {
	return int(s.count.Load())
}

// float32SliceToVectorString converts a float32 slice to a comma-separated string
// suitable for pgvector's vector type (e.g., "[0.1,0.2,0.3]").
func float32SliceToVectorString(v []float32) string {
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
