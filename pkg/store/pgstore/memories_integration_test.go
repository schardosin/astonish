//go:build integration

// Package pgstore integration tests.
//
// These tests require a running PostgreSQL instance with the pgvector extension.
// Set the ASTONISH_TEST_DSN environment variable to point at the test database:
//
//	export ASTONISH_TEST_DSN="postgres://postgres:password@localhost:5432/astonish_test?sslmode=disable"
//
// Run with:
//
//	go test -tags=integration ./pkg/store/pgstore/...
package pgstore

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/store"
)

// testSchema returns a unique schema name for a test function to avoid collisions.
func testSchema(t *testing.T) string {
	t.Helper()
	// Use a sanitized test name + timestamp to ensure uniqueness
	return fmt.Sprintf("test_%d", time.Now().UnixNano())
}

// testPool creates a connection pool for integration tests.
// It reads the DSN from ASTONISH_TEST_DSN environment variable.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("ASTONISH_TEST_DSN")
	if dsn == "" {
		t.Skip("ASTONISH_TEST_DSN not set; skipping integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	t.Cleanup(pool.Close)

	// Ensure pgvector extension is available
	if _, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		t.Fatalf("pgvector extension not available: %v", err)
	}

	return pool
}

// testConn acquires a raw connection for running migrations.
func testConn(t *testing.T, pool *pgxpool.Pool) *pgx.Conn {
	t.Helper()
	dsn := os.Getenv("ASTONISH_TEST_DSN")
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { conn.Close(ctx) })
	return conn
}

// setupTestSchema creates a test schema, runs team migrations, and returns cleanup.
func setupTestSchema(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()
	schema := testSchema(t)

	// Create schema
	_, err := pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", pgx.Identifier{schema}.Sanitize()))
	if err != nil {
		t.Fatalf("failed to create test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA %s CASCADE", pgx.Identifier{schema}.Sanitize()))
	})

	// Run team migrations
	conn := testConn(t, pool)
	if err := Migrate(ctx, conn, MigrationTeam, schema); err != nil {
		t.Fatalf("team migrations failed: %v", err)
	}

	return schema
}

// ---------------------------------------------------------------------------
// Memory Store Tests
// ---------------------------------------------------------------------------

func TestPGMemoryStore_AddAndGet(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	ms := &pgMemoryStore{
		pool:            pool,
		schema:          schema,
		tablePrefix:     "",
		scope:           "team",
		embedFunc:       nil, // No vector embeddings for this test
		createdByColumn: "created_by",
	}

	// Add a memory
	err := ms.Add(ctx, store.MemoryEntry{
		Content:   "The project uses Kubernetes on AWS EKS with Helm charts",
		Category:  "infrastructure",
		CreatedBy: "4d9e9d4d-81b6-42b3-9b01-af5fd728b3c2",
		SessionID: "85530f3e-0003-40b9-8a9f-a323f1e08264",
	})
	if err != nil {
		t.Fatalf("Add() failed: %v", err)
	}

	// Verify count
	if count := ms.Count(); count != 1 {
		t.Errorf("Count() = %d, want 1", count)
	}

	// List all
	results, err := ms.List(ctx, "", 10, 0)
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("List() returned %d results, want 1", len(results))
	}

	r := results[0]
	if r.Snippet != "The project uses Kubernetes on AWS EKS with Helm charts" {
		t.Errorf("Snippet = %q, want expected content", r.Snippet)
	}
	if r.Category != "infrastructure" {
		t.Errorf("Category = %q, want %q", r.Category, "infrastructure")
	}
	if r.Scope != "team" {
		t.Errorf("Scope = %q, want %q", r.Scope, "team")
	}
	if r.CreatedBy != "4d9e9d4d-81b6-42b3-9b01-af5fd728b3c2" {
		t.Errorf("CreatedBy = %q, want %q", r.CreatedBy, "4d9e9d4d-81b6-42b3-9b01-af5fd728b3c2")
	}
	if r.SessionID != "85530f3e-0003-40b9-8a9f-a323f1e08264" {
		t.Errorf("SessionID = %q, want %q", r.SessionID, "85530f3e-0003-40b9-8a9f-a323f1e08264")
	}
	if r.Score != 1.0 {
		t.Errorf("Score = %f, want 1.0", r.Score)
	}

	// Get by ID
	got, err := ms.Get(ctx, r.ID)
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}
	if got == nil {
		t.Fatal("Get() returned nil")
	}
	if got.Snippet != r.Snippet {
		t.Errorf("Get().Snippet = %q, want %q", got.Snippet, r.Snippet)
	}
}

func TestPGMemoryStore_Update(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	ms := &pgMemoryStore{
		pool:            pool,
		schema:          schema,
		tablePrefix:     "",
		scope:           "team",
		embedFunc:       nil,
		createdByColumn: "created_by",
	}

	// Add
	err := ms.Add(ctx, store.MemoryEntry{
		Content:  "Original content",
		Category: "original",
	})
	if err != nil {
		t.Fatalf("Add() failed: %v", err)
	}

	// Get the ID
	results, _ := ms.List(ctx, "", 10, 0)
	id := results[0].ID

	// Update
	err = ms.Update(ctx, id, "Updated content", "updated-category")
	if err != nil {
		t.Fatalf("Update() failed: %v", err)
	}

	// Verify
	got, err := ms.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get() after Update failed: %v", err)
	}
	if got.Snippet != "Updated content" {
		t.Errorf("Snippet after update = %q, want %q", got.Snippet, "Updated content")
	}
	if got.Category != "updated-category" {
		t.Errorf("Category after update = %q, want %q", got.Category, "updated-category")
	}
}

func TestPGMemoryStore_Delete(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	ms := &pgMemoryStore{
		pool:            pool,
		schema:          schema,
		tablePrefix:     "",
		scope:           "team",
		embedFunc:       nil,
		createdByColumn: "created_by",
	}

	// Add
	_ = ms.Add(ctx, store.MemoryEntry{Content: "To be deleted", Category: "temp"})
	results, _ := ms.List(ctx, "", 10, 0)
	id := results[0].ID

	// Delete
	err := ms.Delete(ctx, id)
	if err != nil {
		t.Fatalf("Delete() failed: %v", err)
	}

	// Verify gone
	got, err := ms.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get() after Delete failed: %v", err)
	}
	if got != nil {
		t.Error("Get() after Delete returned non-nil, want nil")
	}

	// Delete again should error
	err = ms.Delete(ctx, id)
	if err == nil {
		t.Error("Delete() of non-existent ID should fail")
	}
}

func TestPGMemoryStore_ListBySession(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	ms := &pgMemoryStore{
		pool:            pool,
		schema:          schema,
		tablePrefix:     "",
		scope:           "team",
		embedFunc:       nil,
		createdByColumn: "created_by",
	}

	sessionA := "aaaa0000-bbbb-cccc-dddd-eeeeeeeeeeee"
	sessionB := "11110000-2222-3333-4444-555555555555"

	// Add memories to different sessions
	_ = ms.Add(ctx, store.MemoryEntry{Content: "Session A memory 1", Category: "cat1", SessionID: sessionA})
	_ = ms.Add(ctx, store.MemoryEntry{Content: "Session A memory 2", Category: "cat2", SessionID: sessionA})
	_ = ms.Add(ctx, store.MemoryEntry{Content: "Session B memory 1", Category: "cat1", SessionID: sessionB})

	// List by session A
	results, err := ms.ListBySession(ctx, sessionA)
	if err != nil {
		t.Fatalf("ListBySession() failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("ListBySession(A) returned %d results, want 2", len(results))
	}

	// List by session B
	results, err = ms.ListBySession(ctx, sessionB)
	if err != nil {
		t.Fatalf("ListBySession() failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("ListBySession(B) returned %d results, want 1", len(results))
	}

	// List by non-existent session
	results, err = ms.ListBySession(ctx, "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("ListBySession() failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("ListBySession(nonexistent) returned %d results, want 0", len(results))
	}
}

func TestPGMemoryStore_ListByCategory(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	ms := &pgMemoryStore{
		pool:            pool,
		schema:          schema,
		tablePrefix:     "",
		scope:           "team",
		embedFunc:       nil,
		createdByColumn: "created_by",
	}

	_ = ms.Add(ctx, store.MemoryEntry{Content: "Infra fact 1", Category: "infrastructure"})
	_ = ms.Add(ctx, store.MemoryEntry{Content: "Infra fact 2", Category: "infrastructure"})
	_ = ms.Add(ctx, store.MemoryEntry{Content: "Team fact", Category: "team"})

	// List by category
	results, err := ms.List(ctx, "infrastructure", 10, 0)
	if err != nil {
		t.Fatalf("List(infrastructure) failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("List(infrastructure) returned %d results, want 2", len(results))
	}

	results, err = ms.List(ctx, "team", 10, 0)
	if err != nil {
		t.Fatalf("List(team) failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("List(team) returned %d results, want 1", len(results))
	}
}

func TestPGMemoryStore_KeywordSearch(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	ms := &pgMemoryStore{
		pool:            pool,
		schema:          schema,
		tablePrefix:     "",
		scope:           "team",
		embedFunc:       nil,
		createdByColumn: "created_by",
	}

	// Add diverse content
	_ = ms.Add(ctx, store.MemoryEntry{Content: "The database uses PostgreSQL with pgvector for semantic search", Category: "infrastructure"})
	_ = ms.Add(ctx, store.MemoryEntry{Content: "Frontend is built with React and TypeScript", Category: "tech-stack"})
	_ = ms.Add(ctx, store.MemoryEntry{Content: "Deployments happen via GitHub Actions CI/CD pipeline", Category: "devops"})

	// Search for "PostgreSQL"
	results, err := ms.Search(ctx, "PostgreSQL database", 10, 0.0)
	if err != nil {
		t.Fatalf("Search() failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Search(PostgreSQL) returned no results, want at least 1")
	}
	if results[0].Category != "infrastructure" {
		t.Errorf("top result category = %q, want %q", results[0].Category, "infrastructure")
	}

	// Search for "React TypeScript"
	results, err = ms.Search(ctx, "React TypeScript frontend", 10, 0.0)
	if err != nil {
		t.Fatalf("Search() failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Search(React) returned no results, want at least 1")
	}
}

func TestPGMemoryStore_Pagination(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	ms := &pgMemoryStore{
		pool:            pool,
		schema:          schema,
		tablePrefix:     "",
		scope:           "team",
		embedFunc:       nil,
		createdByColumn: "created_by",
	}

	// Add 5 memories
	for i := 0; i < 5; i++ {
		_ = ms.Add(ctx, store.MemoryEntry{
			Content:  fmt.Sprintf("Memory number %d", i),
			Category: "test",
		})
	}

	// Page 1 (limit 2, offset 0)
	page1, err := ms.List(ctx, "", 2, 0)
	if err != nil {
		t.Fatalf("List page 1 failed: %v", err)
	}
	if len(page1) != 2 {
		t.Errorf("page1 len = %d, want 2", len(page1))
	}

	// Page 2 (limit 2, offset 2)
	page2, err := ms.List(ctx, "", 2, 2)
	if err != nil {
		t.Fatalf("List page 2 failed: %v", err)
	}
	if len(page2) != 2 {
		t.Errorf("page2 len = %d, want 2", len(page2))
	}

	// Ensure no overlap
	if page1[0].ID == page2[0].ID {
		t.Error("page1 and page2 overlap — pagination is broken")
	}

	// Page 3 (limit 2, offset 4)
	page3, err := ms.List(ctx, "", 2, 4)
	if err != nil {
		t.Fatalf("List page 3 failed: %v", err)
	}
	if len(page3) != 1 {
		t.Errorf("page3 len = %d, want 1", len(page3))
	}
}
