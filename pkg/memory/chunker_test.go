package memory

import (
	"strings"
	"testing"
)

func TestChunkFile_EmptyContent(t *testing.T) {
	chunks := ChunkFile("test.md", "", 1600, 320)
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty content, got %d", len(chunks))
	}
}

func TestChunkFile_SingleChunk(t *testing.T) {
	content := "line one\nline two\nline three"
	chunks := ChunkFile("test.md", content, 1600, 320)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}

	c := chunks[0]
	if c.Path != "test.md" {
		t.Errorf("expected path 'test.md', got %q", c.Path)
	}
	if c.StartLine != 1 {
		t.Errorf("expected startLine 1, got %d", c.StartLine)
	}
	if c.EndLine != 3 {
		t.Errorf("expected endLine 3, got %d", c.EndLine)
	}
	if c.Text != content {
		t.Errorf("expected text %q, got %q", content, c.Text)
	}
	if c.ID == "" {
		t.Error("expected non-empty ID")
	}
	if c.Hash == "" {
		t.Error("expected non-empty Hash")
	}
}

func TestChunkFile_MultipleChunks(t *testing.T) {
	// Create content that's larger than maxChars
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, strings.Repeat("x", 40)) // 40 chars per line
	}
	content := strings.Join(lines, "\n") // ~50*41 = ~2050 chars

	chunks := ChunkFile("big.md", content, 500, 100)

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	// First chunk should start at line 1
	if chunks[0].StartLine != 1 {
		t.Errorf("first chunk should start at line 1, got %d", chunks[0].StartLine)
	}

	// Last chunk should end at the total number of lines
	lastChunk := chunks[len(chunks)-1]
	if lastChunk.EndLine != 50 {
		t.Errorf("last chunk should end at line 50, got %d", lastChunk.EndLine)
	}

	// All chunks should have the same path
	for i, c := range chunks {
		if c.Path != "big.md" {
			t.Errorf("chunk %d: expected path 'big.md', got %q", i, c.Path)
		}
		if c.ID == "" {
			t.Errorf("chunk %d: expected non-empty ID", i)
		}
	}

	// Chunks should overlap (second chunk's start should be before first chunk's end)
	if len(chunks) >= 2 {
		if chunks[1].StartLine > chunks[0].EndLine {
			t.Errorf("expected overlap: chunk1 starts at %d, chunk0 ends at %d",
				chunks[1].StartLine, chunks[0].EndLine)
		}
	}
}

func TestChunkFile_ContentHashing(t *testing.T) {
	content := "hello world"
	chunks1 := ChunkFile("a.md", content, 1600, 320)
	chunks2 := ChunkFile("a.md", content, 1600, 320)

	if len(chunks1) != 1 || len(chunks2) != 1 {
		t.Fatal("expected 1 chunk each")
	}

	// Same content + same path should produce same ID and Hash
	if chunks1[0].ID != chunks2[0].ID {
		t.Error("same content+path should produce same ID")
	}
	if chunks1[0].Hash != chunks2[0].Hash {
		t.Error("same content should produce same Hash")
	}

	// Different path should produce different ID
	chunks3 := ChunkFile("b.md", content, 1600, 320)
	if chunks1[0].ID == chunks3[0].ID {
		t.Error("different path should produce different ID")
	}
	// But same content hash
	if chunks1[0].Hash != chunks3[0].Hash {
		t.Error("same content should produce same Hash regardless of path")
	}
}

func TestChunkFile_NoOverlap(t *testing.T) {
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, strings.Repeat("a", 50))
	}
	content := strings.Join(lines, "\n")

	chunks := ChunkFile("test.md", content, 200, 0)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	// With 0 overlap, chunks should not share lines
	for i := 1; i < len(chunks); i++ {
		if chunks[i].StartLine <= chunks[i-1].StartLine {
			t.Errorf("chunk %d start (%d) should be after chunk %d start (%d)",
				i, chunks[i].StartLine, i-1, chunks[i-1].StartLine)
		}
	}
}

func TestComputeOverlap(t *testing.T) {
	lines := []string{"short", "medium length line", "another line"}

	// Should return trailing lines fitting in budget
	result, chars := computeOverlap(lines, 20)
	if len(result) == 0 {
		t.Fatal("expected at least one overlap line")
	}
	if chars > 20 {
		t.Errorf("overlap chars %d exceeds budget 20", chars)
	}

	// Zero budget should return nothing
	result, chars = computeOverlap(lines, 0)
	if len(result) != 0 {
		t.Error("expected no overlap with 0 budget")
	}
	if chars != 0 {
		t.Error("expected 0 chars with 0 budget")
	}

	// Large budget should return all lines
	result, _ = computeOverlap(lines, 10000)
	if len(result) != len(lines) {
		t.Errorf("expected all %d lines, got %d", len(lines), len(result))
	}
}

func TestSha256Hex(t *testing.T) {
	h := sha256Hex("hello")
	if len(h) != 64 {
		t.Errorf("expected 64 char hex string, got %d chars", len(h))
	}
	// Same input should give same output
	if sha256Hex("hello") != h {
		t.Error("sha256Hex should be deterministic")
	}
	// Different input should give different output
	if sha256Hex("world") == h {
		t.Error("different inputs should give different hashes")
	}
}
