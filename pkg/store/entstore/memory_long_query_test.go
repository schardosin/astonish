package entstore

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SAP/astonish/pkg/store"
)

func TestSQLiteFTSQueryDeduplicatesLongPrompt(t *testing.T) {
	query := strings.Repeat("Memory memory MEMORY search_tools SEARCH_TOOLS ", 2500) + "unique_tail"
	got := sqliteFTSQuery(query)
	want := `"Memory" OR "search_tools" OR "unique_tail"`
	if got != want {
		t.Fatalf("query has %d bytes; want only the three distinct terms: %s", len(got), want)
	}
}

func TestPreparedMemoryStoresLongPrompt(t *testing.T) {
	for _, scope := range []string{"personal", "team", "org"} {
		t.Run(scope, func(t *testing.T) {
			db, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			db.SetMaxOpenConns(1)
			table, creator := "memories", "created_by"
			if scope == "org" {
				table, creator = "org_memories", "promoted_by"
			}
			for _, stmt := range []string{
				fmt.Sprintf("CREATE TABLE %s (id TEXT PRIMARY KEY, chunk_text TEXT, category TEXT, source_path TEXT, %s TEXT, session_id TEXT, created_at TEXT, embedding BLOB)", table, creator),
				fmt.Sprintf("CREATE VIRTUAL TABLE %s_fts USING fts5(chunk_text, content='%s', content_rowid='rowid')", table, table),
			} {
				if _, err := db.Exec(stmt); err != nil {
					t.Fatal(err)
				}
			}
			for i, text := range []string{"memory search_tools", "unique_tail", "unrelated"} {
				id := fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1)
				_, err := db.Exec(fmt.Sprintf("INSERT INTO %s (id,chunk_text,category,%s,created_at,embedding) VALUES (?,?,?,?,?,?)", table, creator), id, text, "guidance", "00000000-0000-0000-0000-000000000099", "2026-01-01T00:00:00Z", float32SliceToBytes([]float32{1, 0}))
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Exec(fmt.Sprintf("INSERT INTO %s_fts(rowid,chunk_text) SELECT rowid,chunk_text FROM %s", table, table)); err != nil {
				t.Fatal(err)
			}
			newStore := func() store.PreparedMemoryStore {
				switch scope {
				case "org":
					return &orgMemoryStore{db: db, dialect: DialectSQLite, vecIndex: newVectorIndex(), table: table, ftsTable: table + "_fts"}
				case "team":
					return &teamMemoryStore{db: db, dialect: DialectSQLite, vecIndex: newVectorIndex(), table: table, ftsTable: table + "_fts"}
				default:
					return &personalMemoryStore{db: db, dialect: DialectSQLite, vecIndex: newVectorIndex(), table: table, ftsTable: table + "_fts"}
				}
			}
			long := strings.Repeat("Memory memory MEMORY search_tools SEARCH_TOOLS ", 2500) + "unique_tail"
			for _, category := range []string{"guidance", "", "missing"} {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				search := func(query string) []store.MemorySearchResult {
					results, err := newStore().SearchPrepared(ctx, store.PreparedMemoryQuery{SemanticQuery: "full semantic query", KeywordQuery: query, Embedding: []float32{1, 0}}, 10, 0, category)
					if err != nil {
						t.Fatal(err)
					}
					return results
				}
				want, got := search("Memory search_tools unique_tail"), search(long)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("%s: repeated query changed results: got %#v, want %#v", category, got, want)
				}
				if category == "missing" && len(got) != 0 {
					t.Fatal("category isolation failed")
				}
				if category != "missing" && len(got) != 3 {
					t.Fatalf("got %d results", len(got))
				}
			}
		})
	}
}

func TestSQLiteFTSQueryDistinctTerms(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"empty", "", ""},
		{"punctuation", "?!", ""},
		{"case and punctuation", "Foo foo:BAR bar", `"Foo" OR "BAR"`},
		{"operators quoted", "OR or AND and NOT not", `"OR" OR "AND" OR "NOT"`},
		{"distinct identifiers", "foo_bar foo bar foo_bar2", `"foo_bar" OR "foo" OR "bar" OR "foo_bar2"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sqliteFTSQuery(tc.input); got != tc.want {
				t.Fatalf("query = %q, want %q", got, tc.want)
			}
		})
	}
}
