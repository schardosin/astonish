package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/schardosin/astonish/pkg/store"
)

// sqliteMemoryStore implements store.MemoryStore with FTS5 + vector hybrid search.
type sqliteMemoryStore struct {
	db              *sql.DB
	table           string // "memories" or "org_memories"
	ftsTable        string // "memories_fts" or "org_memories_fts"
	scope           string // "personal", "team", "org"
	embedFunc       store.EmbedFunc
	createdByColumn string // "created_by" or "promoted_by"
	vecIndex        *vectorIndex
}

func (m *sqliteMemoryStore) Search(ctx context.Context, query string, maxResults int, minScore float64) ([]store.MemorySearchResult, error) {
	return m.hybridSearch(ctx, query, maxResults, minScore, "")
}

func (m *sqliteMemoryStore) SearchByCategory(ctx context.Context, query string, maxResults int, minScore float64, category string) ([]store.MemorySearchResult, error) {
	return m.hybridSearch(ctx, query, maxResults, minScore, category)
}

func (m *sqliteMemoryStore) hybridSearch(ctx context.Context, query string, maxResults int, minScore float64, category string) ([]store.MemorySearchResult, error) {
	if maxResults <= 0 {
		maxResults = 10
	}

	// Run keyword search via FTS5
	keywordResults := m.ftsSearch(ctx, query, maxResults*2, category)

	// Run vector search if embedding function is available
	var vectorResults []scoredResult
	if m.embedFunc != nil {
		queryVec, err := m.embedFunc(ctx, query)
		if err == nil && queryVec != nil {
			m.ensureLoaded(ctx)
			vectorResults = m.vecIndex.search(queryVec, maxResults*2, minScore)
		}
	}

	// If both are empty, fall back to simple text search
	if len(keywordResults) == 0 && len(vectorResults) == 0 {
		return m.fallbackSearch(ctx, query, maxResults, category)
	}

	// Merge results. RRF fusion is only meaningful when merging two
	// independent ranked lists. When only one source produced results,
	// pass its scores through directly (normalized so the top result ≈ 1.0).
	// This matches PG's behavior where ts_rank scores pass through directly
	// in the keyword-only path.
	var fused []scoredResult
	switch {
	case len(vectorResults) > 0 && len(keywordResults) > 0:
		// Both sources: RRF fusion
		fused = rrfFuse(vectorResults, keywordResults, 60, maxResults)
	case len(keywordResults) > 0:
		// Keyword-only: normalize FTS5 scores to 0–1 range.
		fused = normalizeSingleSource(keywordResults, maxResults)
	default:
		// Vector-only: cosine similarity scores are already 0–1.
		if len(vectorResults) > maxResults {
			vectorResults = vectorResults[:maxResults]
		}
		fused = vectorResults
	}

	// Load full records for the fused IDs
	return m.loadResults(ctx, fused)
}

func (m *sqliteMemoryStore) ftsSearch(ctx context.Context, query string, limit int, category string) []scoredResult {
	if query == "" {
		return nil
	}

	// Build FTS5 query: OR-join all tokens for broad matching
	tokens := strings.Fields(query)
	if len(tokens) == 0 {
		return nil
	}
	ftsQuery := strings.Join(tokens, " OR ")

	var sqlStr string
	var args []interface{}

	if category != "" {
		sqlStr = fmt.Sprintf(
			`SELECT m.id, -fts.rank AS score
			 FROM %s fts
			 JOIN %s m ON m.rowid = fts.rowid
			 WHERE %s MATCH ? AND m.category = ?
			 ORDER BY fts.rank
			 LIMIT ?`, m.ftsTable, m.table, m.ftsTable)
		args = []interface{}{ftsQuery, category, limit}
	} else {
		sqlStr = fmt.Sprintf(
			`SELECT m.id, -fts.rank AS score
			 FROM %s fts
			 JOIN %s m ON m.rowid = fts.rowid
			 WHERE %s MATCH ?
			 ORDER BY fts.rank
			 LIMIT ?`, m.ftsTable, m.table, m.ftsTable)
		args = []interface{}{ftsQuery, limit}
	}

	rows, err := m.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var results []scoredResult
	for rows.Next() {
		var id string
		var score float64
		if err := rows.Scan(&id, &score); err != nil {
			continue
		}
		results = append(results, scoredResult{ID: id, Score: score})
	}
	return results
}

func (m *sqliteMemoryStore) fallbackSearch(ctx context.Context, query string, limit int, category string) ([]store.MemorySearchResult, error) {
	var sqlStr string
	var args []interface{}

	if category != "" {
		sqlStr = fmt.Sprintf(
			`SELECT id, chunk_text, category, source_path, metadata, %s, session_id, created_at
			 FROM %s WHERE chunk_text LIKE ? AND category = ?
			 LIMIT ?`, m.createdByColumn, m.table)
		args = []interface{}{"%" + query + "%", category, limit}
	} else {
		sqlStr = fmt.Sprintf(
			`SELECT id, chunk_text, category, source_path, metadata, %s, session_id, created_at
			 FROM %s WHERE chunk_text LIKE ?
			 LIMIT ?`, m.createdByColumn, m.table)
		args = []interface{}{"%" + query + "%", limit}
	}

	rows, err := m.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return m.scanMemoryRows(rows)
}

func (m *sqliteMemoryStore) loadResults(ctx context.Context, scored []scoredResult) ([]store.MemorySearchResult, error) {
	if len(scored) == 0 {
		return nil, nil
	}

	ids := make([]interface{}, len(scored))
	placeholders := make([]string, len(scored))
	scoreMap := make(map[string]float64)
	for i, r := range scored {
		ids[i] = r.ID
		placeholders[i] = "?"
		scoreMap[r.ID] = r.Score
	}

	sqlStr := fmt.Sprintf(
		`SELECT id, chunk_text, category, source_path, metadata, %s, session_id, created_at
		 FROM %s WHERE id IN (%s)`,
		m.createdByColumn, m.table, strings.Join(placeholders, ","))

	rows, err := m.db.QueryContext(ctx, sqlStr, ids...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results, err := m.scanMemoryRows(rows)
	if err != nil {
		return nil, err
	}

	// Apply scores and sort
	for i := range results {
		if score, ok := scoreMap[results[i].ID]; ok {
			results[i].Score = score
		}
		results[i].Scope = m.scope
	}

	return results, nil
}

func (m *sqliteMemoryStore) scanMemoryRows(rows *sql.Rows) ([]store.MemorySearchResult, error) {
	var results []store.MemorySearchResult
	for rows.Next() {
		var r store.MemorySearchResult
		var category, sourcePath, metadata, createdBy, sessionID, createdAt sql.NullString
		err := rows.Scan(&r.ID, &r.Snippet, &category, &sourcePath, &metadata, &createdBy, &sessionID, &createdAt)
		if err != nil {
			return nil, fmt.Errorf("scan memory row: %w", err)
		}
		r.Category = category.String
		r.Path = sourcePath.String
		r.CreatedBy = createdBy.String
		r.SessionID = sessionID.String
		r.CreatedAt = createdAt.String
		r.Scope = m.scope
		results = append(results, r)
	}
	return results, rows.Err()
}

func (m *sqliteMemoryStore) Add(ctx context.Context, entry store.MemoryEntry) error {
	id := uuid.New().String()

	// Generate embedding if function is available
	var embBlob []byte
	if m.embedFunc != nil && entry.Content != "" {
		vec, err := m.embedFunc(ctx, entry.Content)
		if err == nil && vec != nil {
			embBlob = serializeEmbedding(vec)
			m.vecIndex.add(id, vec)
		}
	}

	_, err := m.db.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (id, chunk_text, embedding, category, source_path, metadata, %s, session_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`, m.table, m.createdByColumn),
		id, entry.Content, embBlob, nilStr(entry.Category), nilStr(entry.SourcePath),
		nilStr(""), nilStr(entry.CreatedBy), nilStr(entry.SessionID))
	return err
}

func (m *sqliteMemoryStore) Get(ctx context.Context, id string) (*store.MemorySearchResult, error) {
	row := m.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT id, chunk_text, category, source_path, metadata, %s, session_id, created_at
		 FROM %s WHERE id = ?`, m.createdByColumn, m.table), id)

	r := &store.MemorySearchResult{}
	var category, sourcePath, metadata, createdBy, sessionID, createdAt sql.NullString
	err := row.Scan(&r.ID, &r.Snippet, &category, &sourcePath, &metadata, &createdBy, &sessionID, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.Category = category.String
	r.Path = sourcePath.String
	r.CreatedBy = createdBy.String
	r.SessionID = sessionID.String
	r.CreatedAt = createdAt.String
	r.Scope = m.scope
	return r, nil
}

func (m *sqliteMemoryStore) Update(ctx context.Context, id string, content string, category string) error {
	// Re-generate embedding
	var embBlob []byte
	if m.embedFunc != nil && content != "" {
		vec, err := m.embedFunc(ctx, content)
		if err == nil && vec != nil {
			embBlob = serializeEmbedding(vec)
			m.vecIndex.add(id, vec)
		}
	}

	_, err := m.db.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s SET chunk_text = ?, embedding = ?, category = ? WHERE id = ?`, m.table),
		content, embBlob, nilStr(category), id)
	return err
}

func (m *sqliteMemoryStore) Delete(ctx context.Context, id string) error {
	m.vecIndex.remove(id)
	_, err := m.db.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE id = ?`, m.table), id)
	return err
}

func (m *sqliteMemoryStore) List(ctx context.Context, category string, limit, offset int) ([]store.MemorySearchResult, error) {
	var sqlStr string
	var args []interface{}

	if category != "" {
		sqlStr = fmt.Sprintf(
			`SELECT id, chunk_text, category, source_path, metadata, %s, session_id, created_at
			 FROM %s WHERE category = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`,
			m.createdByColumn, m.table)
		args = []interface{}{category, limit, offset}
	} else {
		sqlStr = fmt.Sprintf(
			`SELECT id, chunk_text, category, source_path, metadata, %s, session_id, created_at
			 FROM %s ORDER BY created_at DESC LIMIT ? OFFSET ?`,
			m.createdByColumn, m.table)
		args = []interface{}{limit, offset}
	}

	rows, err := m.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return m.scanMemoryRows(rows)
}

func (m *sqliteMemoryStore) ListBySession(ctx context.Context, sessionID string) ([]store.MemorySearchResult, error) {
	rows, err := m.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT id, chunk_text, category, source_path, metadata, %s, session_id, created_at
		 FROM %s WHERE session_id = ? ORDER BY created_at DESC`,
			m.createdByColumn, m.table), sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return m.scanMemoryRows(rows)
}

func (m *sqliteMemoryStore) Count() int {
	var count int
	m.db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s`, m.table)).Scan(&count)
	return count
}

func (m *sqliteMemoryStore) Close() error {
	return nil // DB lifecycle managed by parent store
}

// ensureLoaded lazily loads all embeddings from the database into the vector index.
func (m *sqliteMemoryStore) ensureLoaded(ctx context.Context) {
	if m.vecIndex.isLoaded() {
		return
	}

	rows, err := m.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT id, embedding FROM %s WHERE embedding IS NOT NULL`, m.table))
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			continue
		}
		if vec := deserializeEmbedding(blob); vec != nil {
			m.vecIndex.add(id, vec)
		}
	}
	m.vecIndex.markLoaded()
}
