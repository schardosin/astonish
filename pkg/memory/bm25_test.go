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

func TestBM25Search_ConversationalContextBoost(t *testing.T) {
	// Simulates the follow-up query problem: user asks "now provide me the
	// information itemized" after a conversation about Proxmox. Without
	// context, BM25 can't find Proxmox docs. With the augmented query
	// (prior response tail + user message), BM25 finds them via "proxmox",
	// "server", "memory" keywords from the conversation.
	t.Parallel()
	docs := []bm25InputDoc{
		{
			ID:       "proxmox-skill",
			Content:  "Proxmox VE Full Management. Complete control over Proxmox VE hypervisor via REST API. List VMs on node. List LXC containers.",
			Path:     "skills/proxmox-full.md",
			Category: "skill",
		},
		{
			ID:       "proxmox-memory",
			Content:  "Infrastructure. Proxmox server 192.168.1.200 SSH root access 62 GiB RAM credential proxmox-ssh",
			Path:     "MEMORY.md",
			Category: "knowledge",
		},
		{
			ID:       "aws-iam",
			Content:  "IAM. List IAM users. List roles. Get current user info. Manage access keys.",
			Path:     "skills/aws.md",
			Category: "skill",
		},
		{
			ID:       "k8s-resources",
			Content:  "Viewing Resources. kubectl get pods. List pods. List deployments. List services.",
			Path:     "skills/kubernetes.md",
			Category: "skill",
		},
	}
	idx := buildBM25(docs)

	// Raw follow-up query — no topic keywords, BM25 shouldn't find Proxmox
	rawResults := idx.search("now provide me the information itemized I want to see each individual item consumption", 4, "")
	for _, r := range rawResults {
		if r.path == "skills/proxmox-full.md" || r.path == "MEMORY.md" {
			t.Errorf("raw query should NOT find Proxmox docs, but found %q", r.path)
		}
	}

	// Augmented query — prior response tail contains Proxmox context
	augmented := "The Proxmox server is using about 35.5 percent of its 62 GB RAM with approximately 22 GB used. No swap is configured. now provide me the information itemized I want to see each individual item consumption"
	augResults := idx.search(augmented, 4, "")
	if len(augResults) == 0 {
		t.Fatal("augmented query should find results")
	}

	// Proxmox docs should appear in augmented results
	foundProxmox := false
	for _, r := range augResults {
		if r.path == "skills/proxmox-full.md" || r.path == "MEMORY.md" {
			foundProxmox = true
			break
		}
	}
	if !foundProxmox {
		t.Error("augmented query with conversation context should find Proxmox docs via keyword matching")
	}
}

func TestApplyTopicRelevancePenalty_PenalizesUnrelated(t *testing.T) {
	t.Parallel()
	// Simulate fused results from a Proxmox conversation follow-up
	fused := map[string]*fusedEntry{
		"proxmox-api:1:10": {
			path: "tools/proxmox-api.md", startLine: 1, endLine: 10,
			snippet:  "Proxmox API base URL proxmox server node status memory",
			rrfScore: 0.016, // roughly 0.49 after normalization
		},
		"k8s:1:10": {
			path: "skills/kubernetes.md", startLine: 1, endLine: 10,
			snippet:  "kubectl get pods list deployments services",
			rrfScore: 0.016,
		},
		"aws:1:10": {
			path: "skills/aws.md", startLine: 1, endLine: 10,
			snippet:  "aws iam list users list roles get user info",
			rrfScore: 0.016,
		},
	}

	// BM25 query contains conversation context about Proxmox
	bm25Query := "The Proxmox server is using about 35 percent of its 62 GB RAM now provide me the information itemized"

	applyTopicRelevancePenalty(fused, bm25Query)

	// Proxmox doc should NOT be penalized (shares "proxmox", "server", "memory")
	if fused["proxmox-api:1:10"].rrfScore < 0.015 {
		t.Errorf("Proxmox doc should not be penalized, score = %f", fused["proxmox-api:1:10"].rrfScore)
	}

	// K8s doc should be penalized (zero overlap with proxmox context)
	if fused["k8s:1:10"].rrfScore >= 0.016 {
		t.Errorf("K8s doc should be penalized, score = %f", fused["k8s:1:10"].rrfScore)
	}
	if fused["k8s:1:10"].rrfScore != 0.016*topicPenaltyFactor {
		t.Errorf("K8s doc should be penalized by factor %f, got score %f (expected %f)",
			topicPenaltyFactor, fused["k8s:1:10"].rrfScore, 0.016*topicPenaltyFactor)
	}

	// AWS doc should be penalized (zero overlap)
	if fused["aws:1:10"].rrfScore >= 0.016 {
		t.Errorf("AWS doc should be penalized, score = %f", fused["aws:1:10"].rrfScore)
	}
}

func TestApplyTopicRelevancePenalty_NoContextNoPenalty(t *testing.T) {
	t.Parallel()
	// When bm25Query is empty, no penalty should be applied
	fused := map[string]*fusedEntry{
		"k8s:1:10": {
			path: "skills/kubernetes.md", startLine: 1, endLine: 10,
			snippet:  "kubectl get pods",
			rrfScore: 0.016,
		},
	}

	// Empty context — function should return immediately
	applyTopicRelevancePenalty(fused, "")

	if fused["k8s:1:10"].rrfScore != 0.016 {
		t.Errorf("No penalty should be applied with empty context, score = %f", fused["k8s:1:10"].rrfScore)
	}
}

func TestApplyTopicRelevancePenalty_PartialOverlapKept(t *testing.T) {
	t.Parallel()
	// A result that shares even one meaningful keyword should NOT be penalized
	fused := map[string]*fusedEntry{
		"memory-doc:1:10": {
			path: "guidance/memory-usage.md", startLine: 1, endLine: 10,
			snippet:  "How to check memory usage on a server",
			rrfScore: 0.016,
		},
	}

	bm25Query := "The Proxmox server is using 35 percent of its memory"

	applyTopicRelevancePenalty(fused, bm25Query)

	// Should NOT be penalized — shares "memory" and "server" with context
	if fused["memory-doc:1:10"].rrfScore != 0.016 {
		t.Errorf("Doc with topic overlap should not be penalized, score = %f", fused["memory-doc:1:10"].rrfScore)
	}
}

func TestApplyTopicRelevancePenalty_StopWordsIgnored(t *testing.T) {
	t.Parallel()
	// Results that only share stop words should still be penalized
	fused := map[string]*fusedEntry{
		"unrelated:1:10": {
			path: "unrelated.md", startLine: 1, endLine: 10,
			snippet:  "provide the information about this item",
			rrfScore: 0.016,
		},
	}

	// Context with mostly stop words but "proxmox" as the real topic
	bm25Query := "now provide me the information about the proxmox server"

	applyTopicRelevancePenalty(fused, bm25Query)

	// "provide", "information", "item" are stop words — should not count as overlap
	// The doc has no real topic keywords ("proxmox", "server") → penalized
	if fused["unrelated:1:10"].rrfScore >= 0.016 {
		t.Errorf("Doc sharing only stop words should be penalized, score = %f", fused["unrelated:1:10"].rrfScore)
	}
}
