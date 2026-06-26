package api

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/schardosin/astonish/pkg/store"
)

// ── Schema Reconciliation ────────────────────────────────────────────
//
// When a generated app sends CREATE TABLE IF NOT EXISTS, the table may
// already exist with a different (older) column set. This happens when
// the user refines an app and adds new fields — the table was already
// created on the first run.
//
// reconcileCreateTable detects this situation and issues ALTER TABLE ADD
// COLUMN for any columns present in the CREATE TABLE statement but
// missing from the actual database table. This is additive only — it
// never removes columns or changes types.

// createTableRE matches: CREATE TABLE IF NOT EXISTS <name> (<body>)
// Captures: group 1 = table name, group 2 = column definitions body.
var createTableRE = regexp.MustCompile(
	`(?is)^\s*CREATE\s+TABLE\s+IF\s+NOT\s+EXISTS\s+` +
		`["']?(\w+)["']?\s*\(\s*(.+)\s*\)\s*$`)

// columnDef represents a parsed column from a CREATE TABLE statement.
type columnDef struct {
	name       string
	definition string // full definition after the name (type + constraints)
}

// reconcileCreateTable inspects a CREATE TABLE IF NOT EXISTS statement.
// If the table already exists, it adds any missing columns via ALTER TABLE.
func reconcileCreateTable(ctx context.Context, appStore store.AppStateSQLStore, slug, sqlStr string, isPG bool) {
	matches := createTableRE.FindStringSubmatch(sqlStr)
	if matches == nil {
		return
	}

	tableName := matches[1]
	columnsBody := matches[2]

	// Parse declared columns from the CREATE TABLE statement.
	declared := parseColumnDefs(columnsBody)
	if len(declared) == 0 {
		return
	}

	// Query existing columns from the database.
	existing, err := getExistingColumns(ctx, appStore, slug, tableName, isPG)
	if err != nil {
		// Table might not exist yet — that's fine, CREATE TABLE will handle it.
		return
	}
	if len(existing) == 0 {
		// Table doesn't exist — nothing to reconcile, CREATE TABLE will create it.
		return
	}

	// Build a set of existing column names (lowercase for case-insensitive comparison).
	existingSet := make(map[string]bool, len(existing))
	for _, col := range existing {
		existingSet[strings.ToLower(col)] = true
	}

	// Find columns that are in the CREATE TABLE but not in the actual table.
	var toAdd []columnDef
	for _, col := range declared {
		if !existingSet[strings.ToLower(col.name)] {
			toAdd = append(toAdd, col)
		}
	}

	if len(toAdd) == 0 {
		return
	}

	slog.Debug("reconcile: adding missing columns",
		"table", tableName, "count", len(toAdd), "app", slug)

	// Issue ALTER TABLE ADD COLUMN for each missing column.
	for _, col := range toAdd {
		alterSQL := fmt.Sprintf("ALTER TABLE %q ADD COLUMN %s %s",
			tableName, col.name, col.definition)

		if isPG {
			// PostgreSQL supports IF NOT EXISTS on ADD COLUMN (safe idempotent).
			alterSQL = fmt.Sprintf(`ALTER TABLE "%s" ADD COLUMN IF NOT EXISTS "%s" %s`,
				tableName, col.name, col.definition)
		}

		_, _, err := appStore.Exec(ctx, slug, alterSQL)
		if err != nil {
			// On SQLite, "duplicate column name" error means it already exists — ignore.
			if strings.Contains(err.Error(), "duplicate column") {
				continue
			}
			slog.Debug("reconcile: ALTER TABLE failed",
				"table", tableName, "column", col.name, "error", err)
		}
	}
}

// getExistingColumns returns the list of column names for a table.
// Returns an empty slice if the table does not exist.
func getExistingColumns(ctx context.Context, appStore store.AppStateSQLStore, slug, tableName string, isPG bool) ([]string, error) {
	var rows []map[string]any
	var err error

	if isPG {
		// PostgreSQL: query information_schema scoped to the app's schema.
		// The Query method sets search_path to the app schema, so
		// current_schema() resolves to the correct schema name. Without this
		// filter, we'd get columns from ALL schemas that have a table with
		// this name — leading to incorrect reconciliation results.
		rows, err = appStore.Query(ctx, slug,
			`SELECT column_name FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1 ORDER BY ordinal_position`,
			tableName)
		if err != nil {
			return nil, err
		}
		cols := make([]string, 0, len(rows))
		for _, row := range rows {
			if name, ok := row["column_name"].(string); ok {
				cols = append(cols, name)
			}
		}
		return cols, nil
	}

	// SQLite: PRAGMA table_info(<table>).
	rows, err = appStore.Query(ctx, slug, fmt.Sprintf("PRAGMA table_info(%q)", tableName))
	if err != nil {
		return nil, err
	}
	cols := make([]string, 0, len(rows))
	for _, row := range rows {
		if name, ok := row["name"].(string); ok {
			cols = append(cols, name)
		}
	}
	return cols, nil
}

// parseColumnDefs parses the body of a CREATE TABLE statement into individual
// column definitions. It handles:
//   - Nested parentheses (e.g., CHECK(...), DEFAULT(...))
//   - Table-level constraints (PRIMARY KEY(...), UNIQUE(...), FOREIGN KEY(...), CHECK(...))
//     which are skipped since they aren't column definitions
func parseColumnDefs(body string) []columnDef {
	// Split by commas respecting parentheses depth.
	parts := splitRespectingParens(body)

	var defs []columnDef
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Skip table-level constraints (they start with a keyword, not a column name).
		upperPart := strings.ToUpper(part)
		if strings.HasPrefix(upperPart, "PRIMARY KEY") ||
			strings.HasPrefix(upperPart, "UNIQUE") ||
			strings.HasPrefix(upperPart, "FOREIGN KEY") ||
			strings.HasPrefix(upperPart, "CHECK") ||
			strings.HasPrefix(upperPart, "CONSTRAINT") {
			continue
		}

		// Parse: <column_name> <rest...>
		// Column name might be quoted with double quotes or backticks.
		col := parseOneColumn(part)
		if col.name != "" {
			defs = append(defs, col)
		}
	}

	return defs
}

// parseOneColumn extracts the column name and its type+constraints from a single
// column definition string like: `name TEXT NOT NULL DEFAULT ''`
func parseOneColumn(def string) columnDef {
	def = strings.TrimSpace(def)
	if def == "" {
		return columnDef{}
	}

	var name, rest string

	// Handle quoted names: "column_name" or `column_name`
	if def[0] == '"' || def[0] == '`' {
		quote := def[0]
		end := strings.IndexByte(def[1:], quote)
		if end < 0 {
			return columnDef{}
		}
		name = def[1 : end+1]
		rest = strings.TrimSpace(def[end+2:])
	} else {
		// Unquoted: take first word
		idx := strings.IndexAny(def, " \t\n\r")
		if idx < 0 {
			// Column name only, no type (unlikely but handle)
			name = def
			rest = ""
		} else {
			name = def[:idx]
			rest = strings.TrimSpace(def[idx+1:])
		}
	}

	// Skip if name looks like a keyword (shouldn't happen after constraint filter, but safety)
	upperName := strings.ToUpper(name)
	if upperName == "PRIMARY" || upperName == "UNIQUE" || upperName == "FOREIGN" ||
		upperName == "CHECK" || upperName == "CONSTRAINT" || upperName == "INDEX" {
		return columnDef{}
	}

	return columnDef{name: name, definition: rest}
}

// splitRespectingParens splits a string by commas, but respects nested parentheses.
// E.g., "a INTEGER, b TEXT DEFAULT ('x,y')" → ["a INTEGER", "b TEXT DEFAULT ('x,y')"]
func splitRespectingParens(s string) []string {
	var parts []string
	depth := 0
	start := 0

	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	// Last segment
	if start < len(s) {
		parts = append(parts, s[start:])
	}

	return parts
}
