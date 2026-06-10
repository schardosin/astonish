package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/schardosin/astonish/pkg/apps"
	"github.com/schardosin/astonish/pkg/store"
)

// ── SQL Safety Validation ────────────────────────────────────────────

// disallowedSQLTokenRE matches SQL keywords that are not permitted in
// read-only query execution. This prevents write, DDL, and administrative
// operations from being smuggled through the query endpoint.
var disallowedSQLTokenRE = regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|MERGE|UPSERT|REPLACE|ALTER|DROP|CREATE|TRUNCATE|GRANT|REVOKE|COPY|CALL|DO|EXECUTE|PREPARE|DEALLOCATE|VACUUM|ANALYZE|ATTACH|DETACH)\b`)

// isSafeReadOnlySQL validates that a SQL string is a single, read-only
// statement without comment injection or disallowed keywords.
func isSafeReadOnlySQL(sql string) bool {
	trimmed := strings.TrimSpace(sql)
	if trimmed == "" {
		return false
	}

	// Reject multiple statements.
	if strings.Contains(trimmed, ";") {
		return false
	}

	// Reject SQL comments which could be used to obfuscate intent.
	if strings.Contains(trimmed, "--") || strings.Contains(trimmed, "/*") || strings.Contains(trimmed, "*/") {
		return false
	}

	// Reject any disallowed (non-read-only) keywords.
	if disallowedSQLTokenRE.MatchString(trimmed) {
		return false
	}

	return true
}

// ── SQLite → PostgreSQL Dialect Translation ──────────────────────────

// sqliteToPostgres translates common SQLite SQL dialect differences to
// PostgreSQL equivalents. This allows apps written for SQLite to run
// unmodified against per-app PostgreSQL schemas.
//
// Translations performed:
//   - ? parameter placeholders → $1, $2, ... (positional)
//   - AUTOINCREMENT → GENERATED ALWAYS AS IDENTITY
//   - INTEGER PRIMARY KEY AUTOINCREMENT → INTEGER PRIMARY KEY GENERATED ALWAYS AS IDENTITY
//   - DATETIME DEFAULT CURRENT_TIMESTAMP → TIMESTAMPTZ DEFAULT now()
//   - PRAGMA statements → no-op (returns original SQL unchanged, caller handles)
//   - EXPLAIN → passed through (PG supports it natively)
func sqliteToPostgres(sql string) string {
	trimmed := strings.TrimSpace(strings.ToUpper(sql))

	// PRAGMA statements are SQLite-specific and have no PG equivalent.
	// Return empty string as a signal to the caller to return a no-op result.
	if strings.HasPrefix(trimmed, "PRAGMA") {
		return ""
	}

	result := sql

	// Replace ? placeholders with $N positional params.
	// We must be careful not to replace ? inside string literals.
	result = replaceQuestionMarks(result)

	// AUTOINCREMENT → GENERATED ALWAYS AS IDENTITY
	// Match: INTEGER PRIMARY KEY AUTOINCREMENT
	reAutoInc := regexp.MustCompile(`(?i)\bINTEGER\s+PRIMARY\s+KEY\s+AUTOINCREMENT\b`)
	result = reAutoInc.ReplaceAllString(result, "INTEGER PRIMARY KEY GENERATED ALWAYS AS IDENTITY")

	// Standalone AUTOINCREMENT (shouldn't normally appear, but just in case)
	reAutoIncStandalone := regexp.MustCompile(`(?i)\bAUTOINCREMENT\b`)
	result = reAutoIncStandalone.ReplaceAllString(result, "GENERATED ALWAYS AS IDENTITY")

	// DATETIME DEFAULT CURRENT_TIMESTAMP → TIMESTAMPTZ DEFAULT now()
	reDatetime := regexp.MustCompile(`(?i)\bDATETIME\s+DEFAULT\s+CURRENT_TIMESTAMP\b`)
	result = reDatetime.ReplaceAllString(result, "TIMESTAMPTZ DEFAULT now()")

	// Standalone DATETIME type → TIMESTAMPTZ
	reDatetimeType := regexp.MustCompile(`(?i)\bDATETIME\b`)
	result = reDatetimeType.ReplaceAllString(result, "TIMESTAMPTZ")

	return result
}

// replaceQuestionMarks replaces ? placeholders with $1, $2, etc.,
// while preserving ? characters inside string literals.
func replaceQuestionMarks(sql string) string {
	var buf strings.Builder
	buf.Grow(len(sql) + 20)

	paramIndex := 0
	inSingle := false
	inDouble := false

	for i := 0; i < len(sql); i++ {
		c := sql[i]

		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
			buf.WriteByte(c)
		case c == '"' && !inSingle:
			inDouble = !inDouble
			buf.WriteByte(c)
		case c == '?' && !inSingle && !inDouble:
			paramIndex++
			buf.WriteByte('$')
			buf.WriteString(strconv.Itoa(paramIndex))
		default:
			buf.WriteByte(c)
		}
	}

	return buf.String()
}

// ── Request/Response Types ───────────────────────────────────────────

type appStateRequest struct {
	AppName   string `json:"appName"`
	SQL       string `json:"sql"`
	Params    []any  `json:"params"`
	RequestID string `json:"requestId"`
}

type appStateResponse struct {
	RequestID string `json:"requestId"`
	Data      any    `json:"data,omitempty"`
	Error     string `json:"error,omitempty"`
}

// ── Handlers ─────────────────────────────────────────────────────────

// AppStateQueryHandler handles read-only SQL queries against an app's database.
// Only SELECT, PRAGMA, and EXPLAIN statements are allowed.
//
// POST /api/apps/state/query
func AppStateQueryHandler(w http.ResponseWriter, r *http.Request) {
	var req appStateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, appStateResponse{
			Error: "invalid request body",
		})
		return
	}

	if req.AppName == "" {
		respondJSON(w, http.StatusBadRequest, appStateResponse{
			RequestID: req.RequestID,
			Error:     "appName is required",
		})
		return
	}

	if req.SQL == "" {
		respondJSON(w, http.StatusBadRequest, appStateResponse{
			RequestID: req.RequestID,
			Error:     "sql is required",
		})
		return
	}

	// Validate: only read-only statements allowed
	trimmed := strings.TrimSpace(strings.ToUpper(req.SQL))
	if !strings.HasPrefix(trimmed, "SELECT") &&
		!strings.HasPrefix(trimmed, "PRAGMA") &&
		!strings.HasPrefix(trimmed, "EXPLAIN") {
		respondJSON(w, http.StatusBadRequest, appStateResponse{
			RequestID: req.RequestID,
			Error:     "only SELECT, PRAGMA, and EXPLAIN statements are allowed on the query endpoint",
		})
		return
	}

	// Strict validation: reject unsafe SQL patterns (multi-statement, comments, write keywords).
	if !isSafeReadOnlySQL(req.SQL) {
		respondJSON(w, http.StatusBadRequest, appStateResponse{
			RequestID: req.RequestID,
			Error:     "unsafe SQL detected; only single-statement read-only queries are allowed",
		})
		return
	}

	slog.Debug("app state query", "app", req.AppName, "requestId", req.RequestID, "sql", truncateSQL(req.SQL))

	svc := store.FromRequest(r)
	if svc == nil || svc.AppStateSQL == nil {
		respondJSON(w, http.StatusInternalServerError, appStateResponse{
			RequestID: req.RequestID,
			Error:     "app state store not available",
		})
		return
	}

	slug := apps.Slugify(req.AppName)

	// Determine if PG dialect translation is needed.
	// SQLite backend passes SQL as-is; PG backend needs translation.
	sqlStr := req.SQL
	if _, isPG := svc.AppStateSQL.(pgDetector); isPG {
		sqlStr = sqliteToPostgres(req.SQL)
		// PRAGMA → no-op in PG; return empty result set
		if sqlStr == "" {
			respondJSON(w, http.StatusOK, appStateResponse{
				RequestID: req.RequestID,
				Data:      []map[string]any{},
			})
			return
		}
	}

	rows, err := svc.AppStateSQL.Query(r.Context(), slug, sqlStr, req.Params...)
	if err != nil {
		respondJSON(w, http.StatusOK, appStateResponse{
			RequestID: req.RequestID,
			Error:     fmt.Sprintf("query error: %v", err),
		})
		return
	}

	respondJSON(w, http.StatusOK, appStateResponse{
		RequestID: req.RequestID,
		Data:      rows,
	})
}

// AppStateExecHandler handles write/DDL SQL statements against an app's database.
// CREATE, INSERT, UPDATE, DELETE, ALTER, DROP are all allowed.
//
// POST /api/apps/state/exec
func AppStateExecHandler(w http.ResponseWriter, r *http.Request) {
	var req appStateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, appStateResponse{
			Error: "invalid request body",
		})
		return
	}

	if req.AppName == "" {
		respondJSON(w, http.StatusBadRequest, appStateResponse{
			RequestID: req.RequestID,
			Error:     "appName is required",
		})
		return
	}

	if req.SQL == "" {
		respondJSON(w, http.StatusBadRequest, appStateResponse{
			RequestID: req.RequestID,
			Error:     "sql is required",
		})
		return
	}

	slog.Debug("app state exec", "app", req.AppName, "requestId", req.RequestID, "sql", truncateSQL(req.SQL))

	svc := store.FromRequest(r)
	if svc == nil || svc.AppStateSQL == nil {
		respondJSON(w, http.StatusInternalServerError, appStateResponse{
			RequestID: req.RequestID,
			Error:     "app state store not available",
		})
		return
	}

	slug := apps.Slugify(req.AppName)

	// Determine if PG dialect translation is needed.
	sqlStr := req.SQL
	if _, isPG := svc.AppStateSQL.(pgDetector); isPG {
		sqlStr = sqliteToPostgres(req.SQL)
		// PRAGMA → no-op in PG; return success with zero rows affected
		if sqlStr == "" {
			respondJSON(w, http.StatusOK, appStateResponse{
				RequestID: req.RequestID,
				Data: map[string]any{
					"rowsAffected": int64(0),
					"lastInsertId": int64(0),
				},
			})
			return
		}
	}

	rowsAffected, lastInsertID, err := svc.AppStateSQL.Exec(r.Context(), slug, sqlStr, req.Params...)
	if err != nil {
		respondJSON(w, http.StatusOK, appStateResponse{
			RequestID: req.RequestID,
			Error:     fmt.Sprintf("exec error: %v", err),
		})
		return
	}

	respondJSON(w, http.StatusOK, appStateResponse{
		RequestID: req.RequestID,
		Data: map[string]any{
			"rowsAffected": rowsAffected,
			"lastInsertId": lastInsertID,
		},
	})
}

// ── PG Detection ─────────────────────────────────────────────────────

// pgDetector is implemented by PostgreSQL-backed AppStateSQLStore implementations.
// Used to determine whether SQLite→PG dialect translation should be applied.
// The method MUST be exported — unexported interface methods cannot be satisfied
// by types in other packages (pgAppStateSQLStore lives in pkg/store/pgstore).
type pgDetector interface {
	IsPGBackend()
}

// ── Helpers ──────────────────────────────────────────────────────────

// truncateSQL truncates a SQL string for logging.
func truncateSQL(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 100 {
		return s[:100] + "..."
	}
	return s
}
