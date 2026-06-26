package api

import (
	"testing"
)

func TestParseColumnDefs(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected []columnDef
	}{
		{
			name: "simple columns",
			body: "id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, age INTEGER",
			expected: []columnDef{
				{name: "id", definition: "INTEGER PRIMARY KEY AUTOINCREMENT"},
				{name: "name", definition: "TEXT NOT NULL"},
				{name: "age", definition: "INTEGER"},
			},
		},
		{
			name: "with defaults and nested parens",
			body: "id INTEGER PRIMARY KEY, description TEXT DEFAULT '', amount REAL DEFAULT 0.0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP",
			expected: []columnDef{
				{name: "id", definition: "INTEGER PRIMARY KEY"},
				{name: "description", definition: "TEXT DEFAULT ''"},
				{name: "amount", definition: "REAL DEFAULT 0.0"},
				{name: "created_at", definition: "DATETIME DEFAULT CURRENT_TIMESTAMP"},
			},
		},
		{
			name: "skip table constraints",
			body: "id INTEGER, name TEXT, PRIMARY KEY(id), UNIQUE(name)",
			expected: []columnDef{
				{name: "id", definition: "INTEGER"},
				{name: "name", definition: "TEXT"},
			},
		},
		{
			name: "skip CHECK constraint",
			body: "id INTEGER, amount REAL, CHECK(amount > 0)",
			expected: []columnDef{
				{name: "id", definition: "INTEGER"},
				{name: "amount", definition: "REAL"},
			},
		},
		{
			name: "skip FOREIGN KEY",
			body: "id INTEGER PRIMARY KEY, user_id INTEGER, FOREIGN KEY(user_id) REFERENCES users(id)",
			expected: []columnDef{
				{name: "id", definition: "INTEGER PRIMARY KEY"},
				{name: "user_id", definition: "INTEGER"},
			},
		},
		{
			name: "quoted column names",
			body: `"id" INTEGER PRIMARY KEY, "full name" TEXT, "type" TEXT DEFAULT 'general'`,
			expected: []columnDef{
				{name: "id", definition: "INTEGER PRIMARY KEY"},
				{name: "full name", definition: "TEXT"},
				{name: "type", definition: "TEXT DEFAULT 'general'"},
			},
		},
		{
			name: "complex with nested defaults",
			body: "id INTEGER PRIMARY KEY AUTOINCREMENT, data TEXT DEFAULT ('{}'), tags TEXT DEFAULT ('')",
			expected: []columnDef{
				{name: "id", definition: "INTEGER PRIMARY KEY AUTOINCREMENT"},
				{name: "data", definition: "TEXT DEFAULT ('{}')"},
				{name: "tags", definition: "TEXT DEFAULT ('')"},
			},
		},
		{
			name: "real expense tracker schema",
			body: "id INTEGER PRIMARY KEY AUTOINCREMENT, description TEXT NOT NULL, amount REAL NOT NULL, type TEXT DEFAULT 'other', created_at DATETIME DEFAULT CURRENT_TIMESTAMP",
			expected: []columnDef{
				{name: "id", definition: "INTEGER PRIMARY KEY AUTOINCREMENT"},
				{name: "description", definition: "TEXT NOT NULL"},
				{name: "amount", definition: "REAL NOT NULL"},
				{name: "type", definition: "TEXT DEFAULT 'other'"},
				{name: "created_at", definition: "DATETIME DEFAULT CURRENT_TIMESTAMP"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseColumnDefs(tt.body)
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d columns, got %d: %+v", len(tt.expected), len(result), result)
			}
			for i, col := range result {
				if col.name != tt.expected[i].name {
					t.Errorf("column %d: expected name %q, got %q", i, tt.expected[i].name, col.name)
				}
				if col.definition != tt.expected[i].definition {
					t.Errorf("column %d (%s): expected definition %q, got %q",
						i, col.name, tt.expected[i].definition, col.definition)
				}
			}
		})
	}
}

func TestCreateTableRE(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		wantMatch bool
		wantTable string
	}{
		{
			name:      "simple create table if not exists",
			sql:       "CREATE TABLE IF NOT EXISTS expenses (id INTEGER PRIMARY KEY, amount REAL)",
			wantMatch: true,
			wantTable: "expenses",
		},
		{
			name:      "case insensitive",
			sql:       "create table if not exists Expenses (id INTEGER PRIMARY KEY, amount REAL)",
			wantMatch: true,
			wantTable: "Expenses",
		},
		{
			name:      "multiline",
			sql:       "CREATE TABLE IF NOT EXISTS todos (\n  id INTEGER PRIMARY KEY AUTOINCREMENT,\n  text TEXT NOT NULL\n)",
			wantMatch: true,
			wantTable: "todos",
		},
		{
			name:      "without IF NOT EXISTS - no match",
			sql:       "CREATE TABLE expenses (id INTEGER PRIMARY KEY, amount REAL)",
			wantMatch: false,
		},
		{
			name:      "INSERT statement - no match",
			sql:       "INSERT INTO expenses (amount) VALUES (42.5)",
			wantMatch: false,
		},
		{
			name:      "quoted table name",
			sql:       `CREATE TABLE IF NOT EXISTS "my_table" (id INTEGER PRIMARY KEY)`,
			wantMatch: true,
			wantTable: "my_table",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := createTableRE.FindStringSubmatch(tt.sql)
			if tt.wantMatch {
				if matches == nil {
					t.Fatal("expected match, got nil")
				}
				if matches[1] != tt.wantTable {
					t.Errorf("expected table %q, got %q", tt.wantTable, matches[1])
				}
			} else {
				if matches != nil {
					t.Errorf("expected no match, got: %v", matches)
				}
			}
		})
	}
}

func TestSplitRespectingParens(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple",
			input:    "a, b, c",
			expected: []string{"a", " b", " c"},
		},
		{
			name:     "nested parens",
			input:    "a INTEGER, b TEXT DEFAULT ('x,y'), c REAL",
			expected: []string{"a INTEGER", " b TEXT DEFAULT ('x,y')", " c REAL"},
		},
		{
			name:     "CHECK constraint with comma",
			input:    "id INTEGER, CHECK(a > 0 AND b < 100), name TEXT",
			expected: []string{"id INTEGER", " CHECK(a > 0 AND b < 100)", " name TEXT"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitRespectingParens(tt.input)
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d parts, got %d: %v", len(tt.expected), len(result), result)
			}
			for i, part := range result {
				if part != tt.expected[i] {
					t.Errorf("part %d: expected %q, got %q", i, tt.expected[i], part)
				}
			}
		})
	}
}
