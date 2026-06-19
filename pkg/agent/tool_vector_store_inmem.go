package agent

import (
	"context"
	"math"
	"sort"
	"sync"
)

// inMemoryToolVectorStore is a simple in-memory ToolVectorStore for tests.
// It stores documents and performs brute-force cosine similarity search.
type inMemoryToolVectorStore struct {
	mu            sync.RWMutex
	docs          []ToolVectorDoc
	embeddings    [][]float32
	embeddingFunc EmbedFunc
}

// NewInMemoryToolVectorStore creates a ToolVectorStore backed by an in-memory
// slice. It uses the provided embedding function to embed documents on add
// and performs brute-force cosine similarity search on query.
func NewInMemoryToolVectorStore(embeddingFunc EmbedFunc) (ToolVectorStore, error) {
	if embeddingFunc == nil {
		return nil, ErrNilEmbedFunc
	}
	return &inMemoryToolVectorStore{
		embeddingFunc: embeddingFunc,
	}, nil
}

func (s *inMemoryToolVectorStore) AddDocuments(ctx context.Context, docs []ToolVectorDoc, concurrency int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, d := range docs {
		emb, err := s.embeddingFunc(ctx, d.Content)
		if err != nil {
			return err
		}
		s.docs = append(s.docs, d)
		s.embeddings = append(s.embeddings, emb)
	}
	return nil
}

func (s *inMemoryToolVectorStore) QueryByEmbedding(_ context.Context, queryEmbedding []float32, topK int) ([]ToolVectorResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.docs) == 0 {
		return nil, nil
	}

	type scored struct {
		idx  int
		sim  float32
	}

	var scores []scored
	for i, emb := range s.embeddings {
		sim := cosineSimilarity(queryEmbedding, emb)
		scores = append(scores, scored{idx: i, sim: sim})
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].sim > scores[j].sim
	})

	if topK > len(scores) {
		topK = len(scores)
	}

	results := make([]ToolVectorResult, topK)
	for i := 0; i < topK; i++ {
		results[i] = ToolVectorResult{
			ToolVectorDoc: s.docs[scores[i].idx],
			Similarity:    scores[i].sim,
		}
	}
	return results, nil
}

func (s *inMemoryToolVectorStore) GetByID(_ context.Context, id string) (*ToolVectorDoc, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, d := range s.docs {
		if d.ID == id {
			return &d, nil
		}
	}
	return nil, nil
}

func (s *inMemoryToolVectorStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.docs)
}

func (s *inMemoryToolVectorStore) DeleteByIDs(_ context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	toDelete := make(map[string]bool, len(ids))
	for _, id := range ids {
		toDelete[id] = true
	}

	// Filter in place
	n := 0
	for i, d := range s.docs {
		if !toDelete[d.ID] {
			s.docs[n] = s.docs[i]
			s.embeddings[n] = s.embeddings[i]
			n++
		}
	}
	s.docs = s.docs[:n]
	s.embeddings = s.embeddings[:n]
	return nil
}

// cosineSimilarity computes the cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return float32(dot / denom)
}
