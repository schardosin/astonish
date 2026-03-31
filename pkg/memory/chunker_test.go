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

func TestChunkFile_HeadingAwareSections(t *testing.T) {
	// Each section must be > 100 chars to avoid merging
	content := `## Section A
Content for section A line 1 with enough text to exceed the minimum threshold for chunk creation.
Content for section A line 2 with more padding to be safe.

## Section B
Content for section B line 1 with enough text to exceed the minimum threshold for chunk creation.
Content for section B line 2 with more padding to be safe.

## Section C
Content for section C line 1 with enough text to exceed the minimum threshold for chunk creation.
Content for section C line 2 with more padding to be safe.
Content for section C line 3 with more padding to be safe.`

	chunks := ChunkFile("test.md", content, 1200, 320)

	// Should produce 3 chunks (one per section)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	// First chunk should contain Section A
	if !strings.Contains(chunks[0].Text, "## Section A") {
		t.Errorf("chunk 0 should contain Section A heading")
	}

	// Second chunk should contain Section B
	if !strings.Contains(chunks[1].Text, "## Section B") {
		t.Errorf("chunk 1 should contain Section B heading")
	}

	// Third chunk should contain Section C
	if !strings.Contains(chunks[2].Text, "## Section C") {
		t.Errorf("chunk 2 should contain Section C heading")
	}

	// Sections should NOT contain content from other sections
	if strings.Contains(chunks[0].Text, "Section B") {
		t.Error("chunk 0 should not contain Section B content")
	}
	if strings.Contains(chunks[1].Text, "Section C") {
		t.Error("chunk 1 should not contain Section C content")
	}
}

func TestChunkFile_HeadingAwareLargeSection(t *testing.T) {
	// Create a section that exceeds maxChars
	var lines []string
	lines = append(lines, "## Big Section")
	for i := 0; i < 30; i++ {
		lines = append(lines, strings.Repeat("x", 40))
	}
	lines = append(lines, "## Small Section")
	lines = append(lines, "Small content here")

	content := strings.Join(lines, "\n")
	chunks := ChunkFile("test.md", content, 500, 100)

	// Should have at least 2 chunks for the big section + 1 for small
	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
	}

	// Last chunk should contain the small section
	lastChunk := chunks[len(chunks)-1]
	if !strings.Contains(lastChunk.Text, "## Small Section") {
		t.Errorf("last chunk should contain Small Section, got: %q", lastChunk.Text)
	}
	if !strings.Contains(lastChunk.Text, "Small content here") {
		t.Error("last chunk should contain small section content")
	}
}

func TestChunkFile_SmallSectionsMerged(t *testing.T) {
	// Small sections below 100 chars get merged with the next section.
	// When a small section is followed by a large section, the small one
	// is absorbed into the large one (they share a chunk).
	normalContent := strings.Repeat("This section has enough content. ", 5)
	content := "## Tiny\nx\n\n## Also Tiny\ny\n\n## Normal Section\n" + normalContent

	chunks := ChunkFile("test.md", content, 1200, 320)

	// All sections fit within maxChars, and the two tiny ones are absorbed
	// into the Normal section, resulting in a single chunk.
	if len(chunks) != 1 {
		for i, c := range chunks {
			t.Logf("  chunk %d: lines %d-%d (%d chars)", i, c.StartLine, c.EndLine, len(c.Text))
		}
		t.Fatalf("expected 1 chunk (tiny sections absorbed into normal), got %d", len(chunks))
	}

	// The single chunk should contain all three sections
	if !strings.Contains(chunks[0].Text, "## Tiny") {
		t.Error("chunk should contain Tiny heading")
	}
	if !strings.Contains(chunks[0].Text, "## Also Tiny") {
		t.Error("chunk should contain Also Tiny heading")
	}
	if !strings.Contains(chunks[0].Text, "## Normal Section") {
		t.Error("chunk should contain Normal Section heading")
	}
}

func TestChunkFile_SmallThenLargeSections(t *testing.T) {
	// A medium section (> 100 chars) followed by another medium section
	// should produce separate chunks.
	sec1 := "## Section One\n" + strings.Repeat("Section one has meaningful content here. ", 4)
	sec2 := "## Section Two\n" + strings.Repeat("Section two also has meaningful content. ", 4)
	content := sec1 + "\n\n" + sec2

	chunks := ChunkFile("test.md", content, 1200, 320)

	if len(chunks) != 2 {
		for i, c := range chunks {
			t.Logf("  chunk %d: lines %d-%d (%d chars)", i, c.StartLine, c.EndLine, len(c.Text))
		}
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	if !strings.Contains(chunks[0].Text, "## Section One") {
		t.Error("chunk 0 should contain Section One")
	}
	if !strings.Contains(chunks[1].Text, "## Section Two") {
		t.Error("chunk 1 should contain Section Two")
	}
	if strings.Contains(chunks[0].Text, "Section Two") {
		t.Error("chunk 0 should not contain Section Two content")
	}
}

func TestChunkFile_PreambleHandled(t *testing.T) {
	content := `# Title
Some preamble text before any ## heading.

## First Section
Section content here.`

	chunks := ChunkFile("test.md", content, 1200, 320)

	if len(chunks) < 1 {
		t.Fatal("expected at least 1 chunk")
	}

	// First chunk should contain the preamble
	if !strings.Contains(chunks[0].Text, "# Title") {
		t.Error("first chunk should contain the preamble title")
	}

	// Should have a chunk with the section
	found := false
	for _, c := range chunks {
		if strings.Contains(c.Text, "## First Section") {
			found = true
			break
		}
	}
	if !found {
		t.Error("should have a chunk containing ## First Section")
	}
}

func TestChunkFile_NoHeadings(t *testing.T) {
	// Content without any ## headings should work like the old chunker
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, strings.Repeat("y", 40))
	}
	content := strings.Join(lines, "\n")

	chunks := ChunkFile("test.md", content, 500, 100)

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for large content without headings, got %d", len(chunks))
	}

	// Should cover all content
	if chunks[0].StartLine != 1 {
		t.Errorf("first chunk should start at line 1, got %d", chunks[0].StartLine)
	}
	lastChunk := chunks[len(chunks)-1]
	if lastChunk.EndLine != 50 {
		t.Errorf("last chunk should end at line 50, got %d", lastChunk.EndLine)
	}
}

func TestSplitSections(t *testing.T) {
	lines := strings.Split("## A\nline 1\nline 2\n## B\nline 3", "\n")
	sections := splitSections(lines)

	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(sections))
	}

	if sections[0].startLine != 1 {
		t.Errorf("section 0 should start at line 1, got %d", sections[0].startLine)
	}
	if sections[0].endLine != 3 {
		t.Errorf("section 0 should end at line 3, got %d", sections[0].endLine)
	}
	if sections[1].startLine != 4 {
		t.Errorf("section 1 should start at line 4, got %d", sections[1].startLine)
	}
	if sections[1].endLine != 5 {
		t.Errorf("section 1 should end at line 5, got %d", sections[1].endLine)
	}
}
