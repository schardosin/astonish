package api

import (
	"testing"
)

func TestTruncateSQL(t *testing.T) {
	short := "SELECT * FROM users"
	if got := truncateSQL(short); got != short {
		t.Errorf("truncateSQL(%q) = %q, want %q", short, got, short)
	}

	long := "SELECT very_long_column_name_1, very_long_column_name_2, very_long_column_name_3 FROM extremely_long_table_name WHERE condition = 'test'"
	got := truncateSQL(long)
	if len(got) > 103 { // 100 + "..."
		t.Errorf("truncateSQL should truncate to ~103 chars, got %d", len(got))
	}
	if got[len(got)-3:] != "..." {
		t.Error("truncated SQL should end with ...")
	}
}

func TestSqliteToPostgres(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "PRAGMA returns empty",
			input:    "PRAGMA journal_mode=WAL",
			expected: "",
		},
		{
			name:     "question marks replaced",
			input:    "SELECT * FROM t WHERE a = ? AND b = ?",
			expected: "SELECT * FROM t WHERE a = $1 AND b = $2",
		},
		{
			name:     "AUTOINCREMENT replaced",
			input:    "CREATE TABLE t (id INTEGER PRIMARY KEY AUTOINCREMENT)",
			expected: "CREATE TABLE t (id INTEGER PRIMARY KEY GENERATED ALWAYS AS IDENTITY)",
		},
		{
			name:     "DATETIME DEFAULT CURRENT_TIMESTAMP replaced",
			input:    "CREATE TABLE t (created DATETIME DEFAULT CURRENT_TIMESTAMP)",
			expected: "CREATE TABLE t (created TIMESTAMPTZ DEFAULT now())",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sqliteToPostgres(tt.input)
			if got != tt.expected {
				t.Errorf("sqliteToPostgres(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestReplaceQuestionMarks(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"SELECT ?", "SELECT $1"},
		{"INSERT INTO t VALUES (?, ?, ?)", "INSERT INTO t VALUES ($1, $2, $3)"},
		{"SELECT '?' FROM t WHERE x = ?", "SELECT '?' FROM t WHERE x = $1"},
		{`SELECT "?" FROM t WHERE x = ?`, `SELECT "?" FROM t WHERE x = $1`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := replaceQuestionMarks(tt.input)
			if got != tt.expected {
				t.Errorf("replaceQuestionMarks(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
