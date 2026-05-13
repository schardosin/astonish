package agent

import (
	"context"

	chromem "github.com/philippgille/chromem-go"
)

// chromemToolVectorStore adapts a chromem-go Collection to the ToolVectorStore interface.
// Used in personal mode where chromem-go provides in-memory + file-persisted vector storage.
type chromemToolVectorStore struct {
	collection    *chromem.Collection
	embeddingFunc chromem.EmbeddingFunc
}

// NewChromemToolVectorStore creates a ToolVectorStore backed by a chromem-go DB.
// The collection "tools" is created (or retrieved) on the provided DB using
// the given embedding function.
func NewChromemToolVectorStore(db *chromem.DB, embeddingFunc chromem.EmbeddingFunc) (ToolVectorStore, error) {
	if db == nil {
		return nil, ErrNilDB
	}
	if embeddingFunc == nil {
		return nil, ErrNilEmbedFunc
	}

	col, err := db.GetOrCreateCollection("tools", nil, embeddingFunc)
	if err != nil {
		return nil, err
	}

	return &chromemToolVectorStore{
		collection:    col,
		embeddingFunc: embeddingFunc,
	}, nil
}

func (s *chromemToolVectorStore) AddDocuments(ctx context.Context, docs []ToolVectorDoc, concurrency int) error {
	chromemDocs := make([]chromem.Document, len(docs))
	for i, d := range docs {
		chromemDocs[i] = chromem.Document{
			ID:       d.ID,
			Content:  d.Content,
			Metadata: d.Metadata,
		}
	}
	return s.collection.AddDocuments(ctx, chromemDocs, concurrency)
}

func (s *chromemToolVectorStore) QueryByEmbedding(ctx context.Context, queryEmbedding []float32, topK int) ([]ToolVectorResult, error) {
	results, err := s.collection.QueryEmbedding(ctx, queryEmbedding, topK, nil, nil)
	if err != nil {
		return nil, err
	}

	out := make([]ToolVectorResult, len(results))
	for i, r := range results {
		out[i] = ToolVectorResult{
			ToolVectorDoc: ToolVectorDoc{
				ID:       r.ID,
				Content:  r.Content,
				Metadata: r.Metadata,
			},
			Similarity: r.Similarity,
		}
	}
	return out, nil
}

func (s *chromemToolVectorStore) GetByID(ctx context.Context, id string) (*ToolVectorDoc, error) {
	doc, err := s.collection.GetByID(ctx, id)
	if err != nil {
		// chromem returns error when not found — treat as nil
		return nil, nil
	}
	return &ToolVectorDoc{
		ID:       doc.ID,
		Content:  doc.Content,
		Metadata: doc.Metadata,
	}, nil
}

func (s *chromemToolVectorStore) Count() int {
	return s.collection.Count()
}
