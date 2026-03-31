package memory

import (
	"math"
	"testing"
)

func TestBm25Tokenize_Basic(t *testing.T) {
	t.Parallel()
	tokens := bm25Tokenize("Hello, World! This is a test.")
	expected := []string{"hello", "world", "this", "is", "a", "test"}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d: %v", len(expected), len(tokens), tokens)
	}
	for i, tok := range tokens {
		if tok != expected[i] {
			t.Errorf("token %d: expected %q, got %q", i, expected[i], tok)
		}
	}
}

func TestBm25Tokenize_MixedAlphanumeric(t *testing.T) {
	t.Parallel()
	tokens := bm25Tokenize("DDR5 64GB SO-DIMM 5600MHz")
	expected := []string{"ddr5", "64gb", "so", "dimm", "5600mhz"}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d: %v", len(expected), len(tokens), tokens)
	}
	for i, tok := range tokens {
		if tok != expected[i] {
			t.Errorf("token %d: expected %q, got %q", i, expected[i], tok)
		}
	}
}

func TestBm25Tokenize_Empty(t *testing.T) {
	t.Parallel()
	tokens := bm25Tokenize("")
	if len(tokens) != 0 {
		t.Errorf("expected 0 tokens, got %d", len(tokens))
	}
}

func TestBm25Tokenize_Lowercase(t *testing.T) {
	t.Parallel()
	tokens := bm25Tokenize("FIND Best PRICES")
	expected := []string{"find", "best", "prices"}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d: %v", len(expected), len(tokens), tokens)
	}
	for i, tok := range tokens {
		if tok != expected[i] {
			t.Errorf("token %d: expected %q, got %q", i, expected[i], tok)
		}
	}
}

func TestBuildBM25_Empty(t *testing.T) {
	t.Parallel()
	idx := buildBM25(nil)
	if idx != nil {
		t.Error("expected nil index for empty input")
	}
}

func TestBuildBM25_SingleDoc(t *testing.T) {
	t.Parallel()
	docs := []bm25InputDoc{
		{ID: "1", Content: "find product prices online", Path: "test.md", Category: "guidance"},
	}
	idx := buildBM25(docs)
	if idx == nil {
		t.Fatal("expected non-nil index")
	}
	if idx.totalDocs != 1 {
		t.Errorf("expected 1 doc, got %d", idx.totalDocs)
	}
}

func TestBM25Search_BasicMatch(t *testing.T) {
	t.Parallel()
	docs := []bm25InputDoc{
		{ID: "1", Content: "Web Research and Information Gathering. Find the best price for a product using web search API.", Path: "guidance/web-research.md", StartLine: 1, EndLine: 5, Category: "guidance"},
		{ID: "2", Content: "Memory Usage. How to save and recall information from memory.", Path: "guidance/memory-usage.md", StartLine: 1, EndLine: 5, Category: "guidance"},
		{ID: "3", Content: "Job Scheduling. Create scheduled jobs that run automatically.", Path: "guidance/job-scheduling.md", StartLine: 1, EndLine: 5, Category: "guidance"},
	}
	idx := buildBM25(docs)

	results := idx.search("find best prices product", 3, "")
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if results[0].path != "guidance/web-research.md" {
		t.Errorf("expected web-research.md as top result, got %q", results[0].path)
	}
	if results[0].score <= 0 {
		t.Error("expected positive score")
	}
}

func TestBM25Search_CategoryFilter(t *testing.T) {
	t.Parallel()
	docs := []bm25InputDoc{
		{ID: "1", Content: "find product prices online shopping", Path: "guidance/web-research.md", Category: "guidance"},
		{ID: "2", Content: "find product prices shopping list", Path: "projects/shopping.md", Category: "knowledge"},
	}
	idx := buildBM25(docs)

	// Search with category filter — should only return guidance
	results := idx.search("find product prices", 10, "guidance")
	if len(results) != 1 {
		t.Fatalf("expected 1 result with category filter, got %d", len(results))
	}
	if results[0].category != "guidance" {
		t.Errorf("expected guidance category, got %q", results[0].category)
	}

	// Search without filter — should return both
	results = idx.search("find product prices", 10, "")
	if len(results) != 2 {
		t.Fatalf("expected 2 results without filter, got %d", len(results))
	}
}

func TestBM25Search_NoMatch(t *testing.T) {
	t.Parallel()
	docs := []bm25InputDoc{
		{ID: "1", Content: "browser automation click navigate", Path: "guidance/browser.md", Category: "guidance"},
	}
	idx := buildBM25(docs)

	results := idx.search("quantum physics equations", 10, "")
	if len(results) != 0 {
		t.Errorf("expected no results for unrelated query, got %d", len(results))
	}
}

func TestBM25Search_TopK(t *testing.T) {
	t.Parallel()
	docs := []bm25InputDoc{
		{ID: "1", Content: "price comparison shopping", Path: "a.md", Category: "knowledge"},
		{ID: "2", Content: "price deals discount", Path: "b.md", Category: "knowledge"},
		{ID: "3", Content: "price value money", Path: "c.md", Category: "knowledge"},
	}
	idx := buildBM25(docs)

	results := idx.search("price", 2, "")
	if len(results) != 2 {
		t.Fatalf("expected 2 results (topK=2), got %d", len(results))
	}
}

func TestBM25Search_NilIndex(t *testing.T) {
	t.Parallel()
	var idx *bm25Index
	results := idx.search("anything", 10, "")
	if len(results) != 0 {
		t.Errorf("expected 0 results from nil index, got %d", len(results))
	}
}

func TestBM25Search_EmptyQuery(t *testing.T) {
	t.Parallel()
	docs := []bm25InputDoc{
		{ID: "1", Content: "some content", Path: "a.md", Category: "knowledge"},
	}
	idx := buildBM25(docs)

	results := idx.search("", 10, "")
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty query, got %d", len(results))
	}
}

func TestBM25Search_ScoresNormalized(t *testing.T) {
	t.Parallel()
	docs := []bm25InputDoc{
		{ID: "1", Content: "find the best prices for products online", Path: "a.md", Category: "guidance"},
	}
	idx := buildBM25(docs)

	results := idx.search("find best prices products", 10, "")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	// BM25 cosine similarity should be between 0 and 1
	if results[0].score < 0 || results[0].score > 1 {
		t.Errorf("expected score in [0,1], got %f", results[0].score)
	}
}

func TestBM25Search_MetadataPreserved(t *testing.T) {
	t.Parallel()
	docs := []bm25InputDoc{
		{ID: "doc1", Content: "price comparison tool", Path: "guidance/web-research.md", StartLine: 10, EndLine: 20, Category: "guidance"},
	}
	idx := buildBM25(docs)

	results := idx.search("price comparison", 10, "")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.docID != "doc1" {
		t.Errorf("expected docID doc1, got %q", r.docID)
	}
	if r.path != "guidance/web-research.md" {
		t.Errorf("expected path guidance/web-research.md, got %q", r.path)
	}
	if r.startLine != 10 {
		t.Errorf("expected startLine 10, got %d", r.startLine)
	}
	if r.endLine != 20 {
		t.Errorf("expected endLine 20, got %d", r.endLine)
	}
	if r.category != "guidance" {
		t.Errorf("expected category guidance, got %q", r.category)
	}
	if r.content != "price comparison tool" {
		t.Errorf("expected content preserved, got %q", r.content)
	}
}

func TestBM25Search_DiluteQueryStillFinds(t *testing.T) {
	// This is the core scenario: a query dominated by product specs
	// should still find guidance about price research via keyword overlap.
	t.Parallel()
	docs := []bm25InputDoc{
		{
			ID:       "web-research-1",
			Content:  "Web Research and Information Gathering. Find the best price for a product using web search API. Compare prices across retailers like Amazon, Newegg, B&H. Do NOT use browser agents.",
			Path:     "guidance/web-research.md",
			Category: "guidance",
		},
		{
			ID:       "memory-usage-1",
			Content:  "Memory Usage. How to save and recall information from memory. Use memory_save to store durable facts.",
			Path:     "guidance/memory-usage.md",
			Category: "guidance",
		},
		{
			ID:       "browser-1",
			Content:  "Browser Automation. Navigate, click, type, and observe web pages using browser tools.",
			Path:     "guidance/browser-automation.md",
			Category: "guidance",
		},
	}
	idx := buildBM25(docs)

	// Query dominated by product specs, but contains "prices" and "find"
	results := idx.search("Can you find for me the best prices for the Kingston FURY Impact 64GB DDR5 SO-DIMM DDR5 5600 Laptop Memory Provide the links to the product", 3, "")
	if len(results) == 0 {
		t.Fatal("expected at least one result for diluted query")
	}
	if results[0].path != "guidance/web-research.md" {
		t.Errorf("expected web-research.md as top result, got %q", results[0].path)
	}
}

func TestMaxRRFScore(t *testing.T) {
	t.Parallel()
	// Verify the constant matches the formula
	expected := 2.0 / (60.0 + 1.0)
	if math.Abs(maxRRFScore-expected) > 1e-10 {
		t.Errorf("maxRRFScore = %f, expected %f", maxRRFScore, expected)
	}
}
