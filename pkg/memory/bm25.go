package memory

import (
	"math"
	"sort"
	"unicode"
)

// bm25Index is a TF-IDF inverted index for keyword-based search.
// It stores pre-computed TF-IDF weights so queries are O(query_terms * matching_docs).
// This complements the vector (embedding) search by catching keyword matches
// that semantic embeddings miss due to "proper noun dilution" — where specific
// terms (product specs, model numbers) shift the dense embedding away from
// relevant documents, but keyword matching on known terms still works.
type bm25Index struct {
	// idf maps term → inverse document frequency: log(1 + N/df)
	idf map[string]float64
	// docTermFreqs maps docID → {term → sublinear TF-IDF weight}
	docTermFreqs map[string]map[string]float64
	// docNorms maps docID → L2 norm of the TF-IDF vector (for cosine normalization)
	docNorms map[string]float64
	// docMeta maps docID → metadata
	docMeta map[string]bm25DocMeta
	// totalDocs is the number of documents in the index
	totalDocs int
}

// bm25DocMeta holds the metadata needed to build SearchResult from BM25 hits.
type bm25DocMeta struct {
	path      string
	startLine int
	endLine   int
	category  string
	content   string // chunk text (for snippet)
}

// bm25InputDoc is the input format for building a BM25 index.
type bm25InputDoc struct {
	ID        string
	Content   string
	Path      string
	StartLine int
	EndLine   int
	Category  string
}

// bm25Result is a single BM25 search result.
type bm25Result struct {
	docID     string
	path      string
	startLine int
	endLine   int
	category  string
	content   string
	score     float64
}

// bm25Tokenize splits text into lowercase alphanumeric tokens.
// Non-alphanumeric characters (including underscores) are token separators.
func bm25Tokenize(s string) []string {
	var tokens []string
	var current []rune
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current = append(current, unicode.ToLower(r))
		} else {
			if len(current) > 0 {
				tokens = append(tokens, string(current))
				current = current[:0]
			}
		}
	}
	if len(current) > 0 {
		tokens = append(tokens, string(current))
	}
	return tokens
}

// buildBM25 constructs a BM25 inverted index from a set of input documents.
// Each document's Content is tokenized and scored with sublinear TF * IDF.
func buildBM25(docs []bm25InputDoc) *bm25Index {
	n := len(docs)
	if n == 0 {
		return nil
	}

	// Step 1: Compute raw term frequencies per document
	type docTF struct {
		terms map[string]int
	}
	docTFs := make(map[string]*docTF, n)
	docDF := make(map[string]int) // term → number of docs containing it

	for _, doc := range docs {
		tf := &docTF{terms: make(map[string]int)}
		seen := make(map[string]bool)
		tokens := bm25Tokenize(doc.Content)
		for _, tok := range tokens {
			tf.terms[tok]++
			if !seen[tok] {
				docDF[tok]++
				seen[tok] = true
			}
		}
		docTFs[doc.ID] = tf
	}

	// Step 2: Compute IDF for each term: log(1 + N/df)
	idf := make(map[string]float64, len(docDF))
	for term, df := range docDF {
		idf[term] = math.Log(1.0 + float64(n)/float64(df))
	}

	// Step 3: Compute sublinear TF-IDF vectors and L2 norms
	docTermFreqs := make(map[string]map[string]float64, n)
	docNorms := make(map[string]float64, n)
	docMeta := make(map[string]bm25DocMeta, n)

	for _, doc := range docs {
		tf := docTFs[doc.ID]
		tfidf := make(map[string]float64, len(tf.terms))
		var normSq float64
		for term, rawTF := range tf.terms {
			// Sublinear TF: 1 + log(tf)
			subTF := 1.0 + math.Log(float64(rawTF))
			w := subTF * idf[term]
			tfidf[term] = w
			normSq += w * w
		}
		docTermFreqs[doc.ID] = tfidf
		if normSq > 0 {
			docNorms[doc.ID] = math.Sqrt(normSq)
		}

		docMeta[doc.ID] = bm25DocMeta{
			path:      doc.Path,
			startLine: doc.StartLine,
			endLine:   doc.EndLine,
			category:  doc.Category,
			content:   doc.Content,
		}
	}

	return &bm25Index{
		idf:          idf,
		docTermFreqs: docTermFreqs,
		docNorms:     docNorms,
		docMeta:      docMeta,
		totalDocs:    n,
	}
}

// search scores all documents against the query using cosine similarity
// on TF-IDF vectors. If category is non-empty, only documents with that
// category are considered. Returns results sorted by score descending.
func (b *bm25Index) search(query string, topK int, category string) []bm25Result {
	if b == nil || b.totalDocs == 0 {
		return nil
	}

	queryTokens := bm25Tokenize(query)
	if len(queryTokens) == 0 {
		return nil
	}

	// Build query TF-IDF vector
	queryTF := make(map[string]int)
	for _, tok := range queryTokens {
		queryTF[tok]++
	}
	queryVec := make(map[string]float64, len(queryTF))
	var queryNormSq float64
	for term, rawTF := range queryTF {
		termIDF, ok := b.idf[term]
		if !ok {
			continue // term not in any document
		}
		w := (1.0 + math.Log(float64(rawTF))) * termIDF
		queryVec[term] = w
		queryNormSq += w * w
	}
	if queryNormSq == 0 {
		return nil
	}
	queryNorm := math.Sqrt(queryNormSq)

	// Score each document via dot product / (queryNorm * docNorm)
	type scored struct {
		docID string
		score float64
	}
	var results []scored
	for docID, docVec := range b.docTermFreqs {
		// Category filter
		if category != "" {
			if meta, ok := b.docMeta[docID]; ok && meta.category != category {
				continue
			}
		}

		var dot float64
		for term, qw := range queryVec {
			if dw, ok := docVec[term]; ok {
				dot += qw * dw
			}
		}
		if dot <= 0 {
			continue
		}
		docNorm := b.docNorms[docID]
		if docNorm <= 0 {
			continue
		}
		score := dot / (queryNorm * docNorm)
		results = append(results, scored{docID: docID, score: score})
	}

	// Sort descending by score
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}

	out := make([]bm25Result, len(results))
	for i, r := range results {
		meta := b.docMeta[r.docID]
		out[i] = bm25Result{
			docID:     r.docID,
			path:      meta.path,
			startLine: meta.startLine,
			endLine:   meta.endLine,
			category:  meta.category,
			content:   meta.content,
			score:     r.score,
		}
	}
	return out
}
