package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestIndexerIndexAll(t *testing.T) {
	// Create a temporary memory directory
	tmpDir := t.TempDir()
	memDir := filepath.Join(tmpDir, "memory")
	vecDir := filepath.Join(tmpDir, "vectors")

	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write some test memory files
	if err := os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte("# Core Memory\n\n## Preferences\n- Dark mode\n"), 0644); err != nil {
		t.Fatal(err)
	}

	projectsDir := filepath.Join(memDir, "projects")
	if err := os.MkdirAll(projectsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectsDir, "test.md"), []byte("# Test Project\n\nThis is a test project with some details.\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a mock embedding function (returns fixed-size vector)
	mockEmbed := func(ctx context.Context, text string) ([]float32, error) {
		// Return a normalized vector (length = 1)
		dim := 8
		vec := make([]float32, dim)
		for i := range vec {
			vec[i] = 1.0 / float32(dim)
		}
		return vec, nil
	}

	cfg := &StoreConfig{
		MemoryDir:     memDir,
		VectorDir:     vecDir,
		MaxResults:    6,
		MinScore:      0.0, // Accept all scores for testing
		ChunkMaxChars: 1600,
		ChunkOverlap:  320,
	}

	store, err := NewStore(cfg, mockEmbed)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	indexer := NewIndexer(store, cfg, true)
	store.SetIndexer(indexer)

	ctx := context.Background()
	if err := indexer.IndexAll(ctx); err != nil {
		t.Fatalf("IndexAll failed: %v", err)
	}

	// Should have indexed at least 2 files (MEMORY.md + projects/test.md)
	count := store.Count()
	if count < 2 {
		t.Errorf("expected at least 2 documents, got %d", count)
	}

	// Search should work
	results, err := store.Search(ctx, "test project", 5, 0)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least one search result")
	}
}

func TestIndexerIncrementalSync(t *testing.T) {
	tmpDir := t.TempDir()
	memDir := filepath.Join(tmpDir, "memory")
	vecDir := filepath.Join(tmpDir, "vectors")

	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(memDir, "notes.md"), []byte("original content"), 0644); err != nil {
		t.Fatal(err)
	}

	mockEmbed := func(ctx context.Context, text string) ([]float32, error) {
		dim := 8
		vec := make([]float32, dim)
		for i := range vec {
			vec[i] = 1.0 / float32(dim)
		}
		return vec, nil
	}

	cfg := &StoreConfig{
		MemoryDir:     memDir,
		VectorDir:     vecDir,
		MaxResults:    6,
		MinScore:      0.0,
		ChunkMaxChars: 1600,
		ChunkOverlap:  320,
	}

	store, err := NewStore(cfg, mockEmbed)
	if err != nil {
		t.Fatal(err)
	}

	indexer := NewIndexer(store, cfg, false)
	store.SetIndexer(indexer)

	ctx := context.Background()

	// First index
	if err := indexer.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}
	count1 := store.Count()

	// Second index with same content (should be no-op)
	if err := indexer.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}
	count2 := store.Count()
	if count2 != count1 {
		t.Errorf("incremental sync should not change count: %d -> %d", count1, count2)
	}

	// Modify content
	if err := os.WriteFile(filepath.Join(memDir, "notes.md"), []byte("updated content with more text"), 0644); err != nil {
		t.Fatal(err)
	}

	// Third index should detect change
	if err := indexer.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}
	// Count should be the same (1 chunk for small content)
	count3 := store.Count()
	if count3 != count1 {
		t.Logf("count changed after update: %d -> %d (expected for re-chunking)", count1, count3)
	}
}

func TestIndexerDeletedFile(t *testing.T) {
	tmpDir := t.TempDir()
	memDir := filepath.Join(tmpDir, "memory")
	vecDir := filepath.Join(tmpDir, "vectors")

	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatal(err)
	}

	filePath := filepath.Join(memDir, "temp.md")
	if err := os.WriteFile(filePath, []byte("temporary content"), 0644); err != nil {
		t.Fatal(err)
	}

	mockEmbed := func(ctx context.Context, text string) ([]float32, error) {
		dim := 8
		vec := make([]float32, dim)
		for i := range vec {
			vec[i] = 1.0 / float32(dim)
		}
		return vec, nil
	}

	cfg := &StoreConfig{
		MemoryDir:     memDir,
		VectorDir:     vecDir,
		MaxResults:    6,
		MinScore:      0.0,
		ChunkMaxChars: 1600,
		ChunkOverlap:  320,
	}

	store, err := NewStore(cfg, mockEmbed)
	if err != nil {
		t.Fatal(err)
	}

	indexer := NewIndexer(store, cfg, false)
	store.SetIndexer(indexer)

	ctx := context.Background()

	// Index
	if err := indexer.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}
	if store.Count() == 0 {
		t.Fatal("expected at least 1 document after indexing")
	}

	// Delete the file
	if err := os.Remove(filePath); err != nil {
		t.Fatal(err)
	}

	// Re-index should remove the chunks
	if err := indexer.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}
	if store.Count() != 0 {
		t.Errorf("expected 0 documents after deletion, got %d", store.Count())
	}
}
