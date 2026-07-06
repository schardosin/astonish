package sandbox

import (
	"testing"
)

func TestBackendPool_Alias(t *testing.T) {
	// Use a nil backend — we only test pool-level aliasing, not Exec.
	pool := &backendPool{
		clients: make(map[string]*backendNodeClient),
	}

	parentID := "parent-session-111"
	childID := "child-session-222"

	// Pre-populate the parent client.
	parentClient := &backendNodeClient{}
	pool.clients[parentID] = parentClient

	// Alias child → parent.
	pool.Alias(childID, parentID)

	// GetOrCreate(child) should return the same client as the parent.
	got := pool.GetOrCreate(childID)
	if got != parentClient {
		t.Fatalf("Alias: GetOrCreate(child) returned %p, want %p (parent)", got, parentClient)
	}

	// GetOrCreate(parent) should still return the parent.
	gotParent := pool.GetOrCreate(parentID)
	if gotParent != parentClient {
		t.Fatalf("Alias: GetOrCreate(parent) returned %p, want %p", gotParent, parentClient)
	}
}

func TestBackendPool_Alias_NoOpWhenParentMissing(t *testing.T) {
	pool := &backendPool{
		clients: make(map[string]*backendNodeClient),
	}

	// Alias with no parent in pool — should be a no-op.
	pool.Alias("child", "nonexistent-parent")

	if _, ok := pool.clients["child"]; ok {
		t.Fatal("Alias should not create an entry when parent is missing")
	}
}

func TestBackendPool_Alias_NoOpAfterCleanup(t *testing.T) {
	pool := &backendPool{
		clients: make(map[string]*backendNodeClient),
	}

	parentClient := &backendNodeClient{}
	pool.clients["parent"] = parentClient

	// Close the pool.
	pool.closed = true

	pool.Alias("child", "parent")

	// Should not have created the alias because pool is closed.
	if _, ok := pool.clients["child"]; ok {
		t.Fatal("Alias should not create an entry when pool is closed")
	}
}

func TestBackendPool_Alias_EmptyIDs(t *testing.T) {
	pool := &backendPool{
		clients: make(map[string]*backendNodeClient),
	}

	parentClient := &backendNodeClient{}
	pool.clients["parent"] = parentClient

	// Empty child — no-op.
	pool.Alias("", "parent")
	if len(pool.clients) != 1 {
		t.Fatal("empty child should not add entry")
	}

	// Empty parent — no-op.
	pool.Alias("child", "")
	if len(pool.clients) != 1 {
		t.Fatal("empty parent should not add entry")
	}
}
